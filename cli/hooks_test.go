// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The user's own settings, with a hook of theirs that uses &&.
const userSettings = `{
  "model": "opus",
  "permissions": {
    "allow": [
      "Bash(go test:*)"
    ]
  },
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "say done && echo <ok>"
          }
        ]
      }
    ]
  },
  "zeta": 1.50
}
`

func run(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Main(Config{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errb}, args)
	return code, out.String(), errb.String()
}

// Install adds LaForge's command to each event and keeps everything else as
// it was, keys in order; a second install changes nothing; uninstall gives
// the file back exactly.
func TestHooksInstallKeepsTheUsersSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("LAFORGE_HOME", t.TempDir())
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(userSettings), 0o640)

	if code, out, errs := run(t, "", "hooks", "install", "claude"); code != 0 {
		t.Fatalf("install: %d %s %s", code, out, errs)
	}
	raw, _ := os.ReadFile(path)
	text := string(raw)
	for _, want := range []string{`"say done && echo <ok>"`, `"zeta": 1.50`, "report --hook claude", `"matcher": "*"`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %s in\n%s", want, text)
		}
	}
	if strings.Index(text, `"model"`) > strings.Index(text, `"permissions"`) || strings.Index(text, `"hooks"`) > strings.Index(text, `"zeta"`) {
		t.Errorf("keys moved:\n%s", text)
	}
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct{ Command string }
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Hooks["Stop"]) != 2 || doc.Hooks["Stop"][0].Hooks[0].Command != "say done && echo <ok>" {
		t.Errorf("the user's Stop hook is not first and kept: %+v", doc.Hooks["Stop"])
	}
	if len(doc.Hooks) != 8 || doc.Hooks["PreToolUse"][0].Matcher != "*" || doc.Hooks["SessionStart"][0].Matcher != "" {
		t.Errorf("events: %+v", doc.Hooks)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	if _, out, _ := run(t, "", "hooks", "install", "claude"); !strings.Contains(out, "already installed") {
		t.Errorf("second install: %s", out)
	}
	if _, out, _ := run(t, "", "hooks", "status", "claude"); !strings.Contains(out, "installed (") || strings.Contains(out, "not") {
		t.Errorf("status: %s", out)
	}
	if code, _, errs := run(t, "", "hooks", "uninstall", "claude"); code != 0 {
		t.Fatalf("uninstall: %s", errs)
	}
	if raw, _ := os.ReadFile(path); string(raw) != userSettings {
		t.Errorf("uninstall did not give the file back:\n%s", raw)
	}
}

// A settings file LaForge cannot read is left alone; a missing agent is
// named; --dry-run writes nothing.
func TestHooksRefuseWhatTheyCannotEditSafely(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	t.Setenv("CODEX_HOME", filepath.Join(dir, "no-codex"))
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"hooks": [1]`), 0o600)
	if code, _, errs := run(t, "", "hooks", "install", "claude"); code != 1 || !strings.Contains(errs, "left unchanged") {
		t.Fatalf("broken json: %d %s", code, errs)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"hooks": [1]` {
		t.Fatal("changed a file it could not read")
	}
	if code, _, errs := run(t, "", "hooks", "install", "codex"); code != 1 || !strings.Contains(errs, "run codex once first") {
		t.Fatalf("no codex: %d %s", code, errs)
	}
	os.WriteFile(path, []byte(`{}`), 0o600)
	if _, out, _ := run(t, "", "hooks", "install", "--dry-run", "claude"); !strings.Contains(out, "would write") {
		t.Fatalf("dry run: %s", out)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{}` {
		t.Fatal("dry run wrote")
	}
}

// Codex needs hooks switched on under [features]; the rest of config.toml stays.
func TestCodexHooksAreSwitchedOn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	os.WriteFile(filepath.Join(dir, "config.toml"), []byte("model = \"gpt-5\"\n\n[features]\nweb = true\nhooks = false\n\n[profiles.x]\nhooks = false\n"), 0o600)
	if code, _, errs := run(t, "", "hooks", "install", "codex"); code != 0 {
		t.Fatal(errs)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	if string(raw) != "model = \"gpt-5\"\n\n[features]\nweb = true\nhooks = true\n\n[profiles.x]\nhooks = false\n" {
		t.Fatalf("%s", raw)
	}
	for in, want := range map[string]string{
		"":                       "[features]\nhooks = true\n",
		"a = 1":                  "a = 1\n\n[features]\nhooks = true\n",
		"[features]\nx = 1\n":    "[features]\nhooks = true\nx = 1\n",
		"[features]\nhooks=true": "[features]\nhooks=true",
	} {
		if got := setTOMLFeature(in, "hooks"); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "hooks.json")); err != nil {
		t.Fatal(err)
	}
}

// Each agent event means one status, or nothing.
func TestHookEventsMeanStatuses(t *testing.T) {
	cases := []struct {
		ev         string
		state, msg string
		kind       string
	}{
		{`{"hook_event_name":"SessionStart","source":"startup"}`, "idle", "", ""},
		{`{"hook_event_name":"UserPromptSubmit","prompt":"hi"}`, "working", "", ""},
		{`{"hook_event_name":"PreToolUse","tool_name":"Bash"}`, "working", "", ""},
		{`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"git   push\n origin"}}`, "blocked", "Bash: git push origin", "permission"},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude needs your permission to use Bash"}`, "blocked", "Claude needs your permission to use Bash", "permission"},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt","message":"waiting"}`, "", "", ""},
		{`{"hook_event_name":"Stop"}`, "done", "", ""},
		{`{"hook_event_name":"SessionEnd"}`, "clear", "", ""},
		{`{"hook_event_name":"SubagentStop"}`, "", "", ""},
	}
	for _, c := range cases {
		var ev hookEvent
		json.Unmarshal([]byte(c.ev), &ev)
		r, ok := hookReport("claude", ev)
		if !ok {
			if c.state != "" {
				t.Errorf("%s: nothing", c.ev)
			}
			continue
		}
		if r.State != c.state || r.Msg != c.msg || r.Kind != c.kind || r.Source != "hook" || r.App != "claude" {
			t.Errorf("%s: %+v", c.ev, r)
		}
	}
}

// A hook reports to the pane's forge, records the agent's session on
// SessionStart, ignores sub-agents, prints nothing and exits 0 even with no
// forge at all.
func TestAHookReportsToItsForge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LAFORGE_HOME", home)
	t.Setenv("LAFORGE_SESSION", "s-1")
	var mu sync.Mutex
	var got []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.Header.Get("Authorization")+" "+r.URL.Path+" "+string(body))
		mu.Unlock()
		w.Write([]byte("{}"))
	}))
	defer ts.Close()

	if code, out, errs := run(t, `{"hook_event_name":"Stop"}`, "report", "--hook", "claude"); code != 0 || out != "" || errs != "" {
		t.Fatalf("no forge: %d %q %q", code, out, errs)
	}
	raw, _ := json.Marshal(State{URL: ts.URL, Token: "tok"})
	os.WriteFile(filepath.Join(home, "forge.json"), raw, 0o600)
	run(t, `{"hook_event_name":"SessionStart","session_id":"abc","source":"startup"}`, "report", "--hook", "claude")
	run(t, `{"hook_event_name":"Stop","agent_id":"sub-1"}`, "report", "--hook", "claude")
	if code, out, _ := run(t, `{"hook_event_name":"PermissionRequest","tool_name":"Edit","tool_input":{"file_path":"/x.go"}}`, "report", "--hook", "claude"); code != 0 || out != "" {
		t.Fatalf("%d %q", code, out)
	}
	if code, _, errs := run(t, "", "report", "working", "--msg", "tests"); code != 0 {
		t.Fatal(errs)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{
		`Bearer tok /api/sessions/s-1/agent {"agent":"claude","sessionId":"abc","source":"startup"}`,
		`Bearer tok /api/sessions/s-1/status {"state":"idle","app":"claude","source":"hook"}`,
		`Bearer tok /api/sessions/s-1/status {"state":"blocked","kind":"permission","app":"claude","msg":"Edit: /x.go","source":"hook"}`,
		`Bearer tok /api/sessions/s-1/status {"state":"working","msg":"tests"}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// With --answer-wait, a permission request asks the forge and an answer
// becomes Claude Code's PermissionRequest decision on stdout; no answer
// prints nothing.
func TestAPermissionHookGivesTheAgentTheAnswer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LAFORGE_HOME", home)
	t.Setenv("LAFORGE_SESSION", "s-1")
	decision := "allow"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions/s-1/ask" {
			t.Errorf("path %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["msg"] != "Bash: git push" || body["wait"].(float64) != 30 {
			t.Errorf("body %v", body)
		}
		json.NewEncoder(w).Encode(map[string]string{"decision": decision})
	}))
	defer ts.Close()
	raw, _ := json.Marshal(State{URL: ts.URL, Token: "tok"})
	os.WriteFile(filepath.Join(home, "forge.json"), raw, 0o600)
	ev := `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"git push"}}`
	_, out, _ := run(t, ev, "report", "--hook", "claude", "--answer-wait", "30s")
	if strings.TrimSpace(out) != `{"hookSpecificOutput":{"decision":{"behavior":"allow"},"hookEventName":"PermissionRequest"}}` {
		t.Fatalf("allow: %q", out)
	}
	decision = "deny"
	_, out, _ = run(t, ev, "report", "--hook", "claude", "--answer-wait", "30s")
	if !strings.Contains(out, `"behavior":"deny"`) || !strings.Contains(out, `"message":"Denied in LaForge`) {
		t.Fatalf("deny: %q", out)
	}
	decision = ""
	if _, out, _ = run(t, ev, "report", "--hook", "claude", "--answer-wait", "30s"); out != "" {
		t.Fatalf("no answer: %q", out)
	}
}

// --answer writes the wait into the PermissionRequest hook only, with a
// timeout long enough for it.
func TestHooksInstallWithAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	if code, _, errs := run(t, "", "hooks", "install", "claude", "--answer", "90s"); code != 0 {
		t.Fatal(errs)
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string
				Timeout int
			}
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	json.Unmarshal(raw, &doc)
	pr := doc.Hooks["PermissionRequest"][0].Hooks[0]
	if !strings.Contains(pr.Command, "--answer-wait 1m30s") || pr.Timeout != 105 {
		t.Fatalf("%+v", pr)
	}
	if st := doc.Hooks["Stop"][0].Hooks[0]; strings.Contains(st.Command, "answer") || st.Timeout != 5 {
		t.Fatalf("%+v", st)
	}
}

// la skin install checks a skin and copies it into the forge's home.
func TestSkinInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LAFORGE_HOME", home)
	src := t.TempDir()
	os.WriteFile(filepath.Join(src, "skin.json"), []byte(`{"name":"neon","css":["skin.css"]}`), 0o644)
	os.WriteFile(filepath.Join(src, "skin.css"), []byte(`body{}`), 0o644)
	if code, out, errs := run(t, "", "skin", "install", src); code != 0 || !strings.Contains(out, "installed neon") {
		t.Fatalf("%d %q %q", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(home, "skins", "neon", "skin.css")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "evil.js"), []byte(`x`), 0o644)
	if code, _, errs := run(t, "", "skin", "install", src); code == 0 || !strings.Contains(errs, "fonts and images only") {
		t.Fatalf("installed code: %d %q", code, errs)
	}
}
