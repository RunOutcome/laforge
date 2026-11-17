// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Hooks: Claude Code and Codex do not send OSC 7501 themselves, but both run
// hooks on their lifecycle events. `la hooks install` adds one command to
// each event in the agent's own settings; the command does nothing outside
// a LaForge pane (no LAFORGE_SESSION), so the agent run anywhere else is
// unchanged. Everything else in the settings file is kept as it was, keys in
// their order.

// hookTarget is one agent's settings file and the events LaForge listens to.
type hookTarget struct {
	agent  string
	label  string
	dir    func() string // the agent's config folder
	file   string        // the hooks file in it
	events []string
	// toolEvents take a matcher ("*": every tool).
	toolEvents map[string]bool
	// enable switches the agent's hooks on, when it needs that (Codex).
	enable func(dir string, dryRun bool, out io.Writer) error
}

func homeDir() string { h, _ := os.UserHomeDir(); return h }

var hookTargets = []hookTarget{
	{
		agent: "claude", label: "Claude Code",
		dir:        func() string { return envOr("CLAUDE_CONFIG_DIR", filepath.Join(homeDir(), ".claude")) },
		file:       "settings.json",
		events:     []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "PermissionRequest", "Notification", "Stop", "SessionEnd"},
		toolEvents: map[string]bool{"PreToolUse": true, "PostToolUse": true, "PermissionRequest": true},
	},
	{
		agent: "codex", label: "Codex",
		dir:        func() string { return envOr("CODEX_HOME", filepath.Join(homeDir(), ".codex")) },
		file:       "hooks.json",
		events:     []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Stop"},
		toolEvents: map[string]bool{"PreToolUse": true, "PermissionRequest": true},
		enable:     enableCodexHooks,
	},
}

// hookMark is in every command LaForge installs, so it can find its own.
const hookMark = "report --hook "

// hookCommand is the shell command an agent runs on each event.
func (c *cmd) hookCommand(agent string) string {
	return `[ -z "$LAFORGE_SESSION" ] || ` + c.selfCommand() + " " + hookMark + agent + " 2>/dev/null || true"
}

// selfCommand is how a hook reaches this command tree: "la" when la on PATH
// is this binary (it then follows upgrades), else this binary's full path.
func (c *cmd) selfCommand() string {
	exe, err := os.Executable()
	if err != nil {
		exe = "la"
	}
	self := shellQuote(exe)
	if len(c.cfg.Reexec) == 0 {
		if onPath, err := exec.LookPath("la"); err == nil && sameFile(onPath, exe) {
			self = "la"
		}
	}
	for _, a := range c.cfg.Reexec {
		self += " " + shellQuote(a)
	}
	return self
}

func sameFile(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return false
	}
	ia, err1 := os.Stat(ra)
	ib, err2 := os.Stat(rb)
	return err1 == nil && err2 == nil && os.SameFile(ia, ib)
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r == '/' || r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (c *cmd) hooks(args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	action := args[0]
	fs := flag.NewFlagSet("hooks "+action, flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	dryRun := fs.Bool("dry-run", false, "show what would change, change nothing")
	if err := fs.Parse(args[1:]); err != nil {
		return usageError{err}
	}
	targets, err := pickTargets(fs.Args())
	if err != nil {
		return err
	}
	switch action {
	case "install", "uninstall":
		var errs []error
		for _, t := range targets {
			if err := c.changeHooks(t, action == "install", *dryRun); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", t.label, err))
			}
		}
		return errors.Join(errs...)
	case "status":
		for _, t := range targets {
			fmt.Fprintf(c.cfg.Stdout, "%-12s %s\n", t.label, c.hookStatus(t))
		}
		return nil
	default:
		return usageError{fmt.Errorf("hooks: install, uninstall or status, then claude, codex or nothing for both")}
	}
}

func pickTargets(names []string) ([]hookTarget, error) {
	if len(names) == 0 || len(names) == 1 && names[0] == "all" {
		return hookTargets, nil
	}
	var out []hookTarget
	for _, n := range names {
		found := false
		for _, t := range hookTargets {
			if t.agent == n {
				out, found = append(out, t), true
			}
		}
		if !found {
			return nil, usageError{fmt.Errorf("no hooks for %q: claude or codex", n)}
		}
	}
	return out, nil
}

func (c *cmd) hookStatus(t hookTarget) string {
	path := filepath.Join(t.dir(), t.file)
	doc, err := readJSONObject(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "not installed (" + path + " does not exist)"
		}
		return "unreadable: " + err.Error()
	}
	have := 0
	for _, ev := range t.events {
		if hasOurHook(doc, ev, t.agent) {
			have++
		}
	}
	switch have {
	case 0:
		return "not installed (" + path + ")"
	case len(t.events):
		return "installed (" + path + ")"
	default:
		return fmt.Sprintf("partly installed, %d of %d events; run install again (%s)", have, len(t.events), path)
	}
}

func (c *cmd) changeHooks(t hookTarget, install, dryRun bool) error {
	dir := t.dir()
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		if !install {
			fmt.Fprintf(c.cfg.Stdout, "%s: nothing to remove (%s does not exist)\n", t.label, dir)
			return nil
		}
		return fmt.Errorf("%s does not exist; run %s once first", dir, t.agent)
	}
	path := filepath.Join(dir, t.file)
	doc, err := readJSONObject(path)
	if errors.Is(err, os.ErrNotExist) {
		doc, err = &object{}, nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w (left unchanged)", path, err)
	}
	before, _ := doc.MarshalJSON()
	hooks, err := doc.child("hooks")
	if err != nil {
		return fmt.Errorf("%s: %w (left unchanged)", path, err)
	}
	for _, ev := range t.events {
		if err := removeOurHooks(hooks, ev, t.agent); err != nil {
			return fmt.Errorf("%s: %w (left unchanged)", path, err)
		}
		if install {
			entry := &object{}
			if t.toolEvents[ev] {
				entry.set("matcher", "*")
			}
			entry.set("hooks", []any{map[string]any{"type": "command", "command": c.hookCommand(t.agent), "timeout": 5}})
			list, _ := hooks.get(ev).([]any)
			hooks.set(ev, append(list, entry))
		}
	}
	if hooks.empty() {
		doc.remove("hooks")
	}
	after, _ := doc.MarshalJSON()
	verb := map[bool]string{true: "installed", false: "removed"}[install]
	if bytes.Equal(before, after) {
		fmt.Fprintf(c.cfg.Stdout, "%s: hooks already %s (%s)\n", t.label, verb, path)
	} else if dryRun {
		fmt.Fprintf(c.cfg.Stdout, "%s: would write %s:\n%s\n", t.label, path, indentJSON(after))
	} else {
		if err := writeFileKeepingMode(path, indentJSON(after)); err != nil {
			return err
		}
		fmt.Fprintf(c.cfg.Stdout, "%s: hooks %s (%s)\n", t.label, verb, path)
	}
	if install && t.enable != nil {
		return t.enable(dir, dryRun, c.cfg.Stdout)
	}
	return nil
}

// hasOurHook reports whether event's list holds LaForge's command.
func hasOurHook(doc *object, event, agent string) bool {
	hooks, _ := doc.get("hooks").(*object)
	if hooks == nil {
		return false
	}
	list, _ := hooks.get(event).([]any)
	for _, e := range list {
		if eo, ok := e.(*object); ok {
			inner, _ := eo.get("hooks").([]any)
			for _, h := range inner {
				if ours(h, agent) {
					return true
				}
			}
		}
	}
	return false
}

func ours(h any, agent string) bool {
	ho, ok := h.(*object)
	if !ok {
		return false
	}
	cmd, _ := ho.get("command").(string)
	return strings.Contains(cmd, hookMark+agent) && strings.Contains(cmd, "LAFORGE_SESSION")
}

// removeOurHooks takes LaForge's commands out of event's list, and any
// entry they leave empty; other commands stay where they are.
func removeOurHooks(hooks *object, event, agent string) error {
	v := hooks.get(event)
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("hooks.%s is not a list", event)
	}
	var kept []any
	for _, e := range list {
		eo, ok := e.(*object)
		if !ok {
			kept = append(kept, e)
			continue
		}
		inner, ok := eo.get("hooks").([]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		var rest []any
		for _, h := range inner {
			if !ours(h, agent) {
				rest = append(rest, h)
			}
		}
		if len(rest) == len(inner) {
			kept = append(kept, e)
		} else if len(rest) > 0 {
			eo.set("hooks", rest)
			kept = append(kept, eo)
		}
	}
	if len(kept) == 0 {
		hooks.remove(event)
	} else {
		hooks.set(event, kept)
	}
	return nil
}

// enableCodexHooks sets hooks = true under [features] in config.toml,
// leaving the rest of the file as it is.
func enableCodexHooks(dir string, dryRun bool, out io.Writer) error {
	path := filepath.Join(dir, "config.toml")
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(raw)
	updated := setTOMLFeature(text, "hooks")
	if updated == text {
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "Codex: would set hooks = true under [features] in %s\n", path)
		return nil
	}
	if err := writeFileKeepingMode(path, []byte(updated)); err != nil {
		return err
	}
	fmt.Fprintf(out, "Codex: hooks switched on in %s\n", path)
	return nil
}

// setTOMLFeature makes key = true in the top-level [features] table.
func setTOMLFeature(text, key string) string {
	lines := strings.Split(text, "\n")
	inFeatures, header := false, -1
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			inFeatures = trim == "[features]"
			if inFeatures && header < 0 {
				header = i
			}
			continue
		}
		if inFeatures {
			if k, v, ok := strings.Cut(trim, "="); ok && strings.TrimSpace(k) == key {
				if strings.TrimSpace(strings.SplitN(v, "#", 2)[0]) == "true" {
					return text
				}
				lines[i] = key + " = true"
				return strings.Join(lines, "\n")
			}
		}
	}
	if header >= 0 {
		lines = append(lines[:header+1], append([]string{key + " = true"}, lines[header+1:]...)...)
		return strings.Join(lines, "\n")
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if text != "" {
		text += "\n"
	}
	return text + "[features]\n" + key + " = true\n"
}

func writeFileKeepingMode(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".laforge-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// object is a JSON object that keeps its keys in order, so a settings file
// LaForge edits reads as it did, apart from the edit.
type object struct {
	keys   []string
	values map[string]any
}

func (o *object) get(k string) any {
	if o.values == nil {
		return nil
	}
	return o.values[k]
}

func (o *object) set(k string, v any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, ok := o.values[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.values[k] = v
}

func (o *object) remove(k string) {
	if _, ok := o.values[k]; !ok {
		return
	}
	delete(o.values, k)
	for i, key := range o.keys {
		if key == k {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *object) empty() bool { return len(o.keys) == 0 }

// child is the object at k, made when missing.
func (o *object) child(k string) (*object, error) {
	switch v := o.get(k).(type) {
	case nil:
		c := &object{}
		o.set(k, c)
		return c, nil
	case *object:
		return v, nil
	default:
		return nil, fmt.Errorf("%q is not an object", k)
	}
}

// MarshalJSON writes the object with its keys in order. Strings are written
// without HTML escaping (encoding/json would turn a hook's && into \u0026).
func (o *object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	err := encodeValue(&b, o)
	return b.Bytes(), err
}

func encodeValue(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case *object:
		b.WriteByte('{')
		for i, k := range t.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encodeValue(b, k); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := encodeValue(b, t.values[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := encodeValue(b, e); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		o := &object{}
		for _, k := range sortedKeys(t) {
			o.set(k, t[k])
		}
		return encodeValue(b, o)
	default:
		enc := json.NewEncoder(b)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(t); err != nil {
			return err
		}
		b.Truncate(b.Len() - 1) // Encode's newline
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func readJSONObject(path string) (*object, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return &object{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeValue(dec)
	if err != nil {
		return nil, fmt.Errorf("not JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("not JSON: more after the object")
	}
	o, ok := v.(*object)
	if !ok {
		return nil, errors.New("not a JSON object")
	}
	return o, nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := &object{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				o.set(k, v)
			}
			_, err := dec.Token()
			return o, err
		case '[':
			list := []any{}
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			}
			_, err := dec.Token()
			return list, err
		}
		return nil, fmt.Errorf("unexpected %v", t)
	default:
		return t, nil
	}
}

func indentJSON(raw []byte) []byte {
	var b bytes.Buffer
	if json.Indent(&b, raw, "", "  ") != nil {
		return raw
	}
	b.WriteByte('\n')
	return b.Bytes()
}
