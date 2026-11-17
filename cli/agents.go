// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/RunOutcome/laforge/forge"
	"github.com/coder/websocket"
)

// The commands an agent (or a person) uses to run panes from a pane:
// run, ls, send, read, wait, events, close. They reach the forge named by
// LAFORGE_HOME, which every pane is given.

type labelFlags map[string]string

func (l labelFlags) String() string { return "" }
func (l labelFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return errors.New("a label is key=value")
	}
	l[k] = val
	return nil
}

func (c *cmd) apiClient() (*forgeClient, error) {
	if s := c.running(); s != nil {
		return &forgeClient{state: s, http: &http.Client{Timeout: 30 * time.Second}}, nil
	}
	return nil, fmt.Errorf("LaForge is not running; start it with %s up", c.cfg.Prog)
}

func (f *forgeClient) panes() ([]forge.Pane, error) {
	var ps []forge.Pane
	return ps, f.do(http.MethodGet, "/api/sessions", nil, &ps)
}

// resolve finds a pane by id, or by a title only one pane has.
func (f *forgeClient) resolve(name string) (forge.Pane, error) {
	ps, err := f.panes()
	if err != nil {
		return forge.Pane{}, err
	}
	var found []forge.Pane
	for _, p := range ps {
		if p.ID == name {
			return p, nil
		}
		if p.Title == name {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return forge.Pane{}, fmt.Errorf("no pane %q (la ls lists them)", name)
	default:
		return forge.Pane{}, fmt.Errorf("%d panes are called %q; use an id (la ls)", len(found), name)
	}
}

// run is `la run [flags] [--] PROFILE|COMMAND [ARGS…]`.
func (c *cmd) run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	bg := fs.Bool("bg", false, "start it and return its id")
	name := fs.String("name", "", "the pane's name")
	cwd := fs.String("cwd", "", "folder to run in (default: this one)")
	outlive := fs.Bool("outlive-parent", false, "keep running when the pane that started it ends")
	noNotes := fs.Bool("no-notes", false, "do not tell this pane when it needs attention or ends")
	waitMax := fs.Duration("wait-max", 180*time.Second, "without --bg: return after this long, leaving it running (0: wait to the end)")
	labels := labelFlags{}
	fs.Var(labels, "label", "key=value, a name of the starter's own (repeatable)")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	argv := fs.Args()
	if len(argv) == 0 {
		return usageError{errors.New("run what? a profile (claude, codex, shell…) or a command")}
	}
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	dir := *cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dir, _ = filepath.Abs(dir)
	parent := os.Getenv("LAFORGE_SESSION")
	req := forge.StartRequest{Cwd: dir, Title: *name, Parent: parent, OutliveParent: *outlive, Notes: parent != "" && !*noNotes, Labels: labels, Cols: 120, Rows: 32}
	var profiles []forge.Profile
	_ = f.do(http.MethodGet, "/api/profiles", nil, &profiles)
	for _, p := range profiles {
		if p.Name == argv[0] && p.Available {
			req.Profile, req.Args = p.Name, argv[1:]
		}
	}
	if req.Profile == "" {
		req.Argv = argv
	}
	if parent != "" {
		if _, err := f.resolve(parent); err != nil {
			req.Parent, req.Notes = "", false // not a pane of this forge
		}
	}
	var p forge.Pane
	if err := f.do(http.MethodPost, "/api/sessions", req, &p); err != nil {
		return err
	}
	if *bg {
		fmt.Fprintln(c.cfg.Stdout, p.ID)
		return nil
	}
	return c.follow(f, p, *waitMax)
}

// follow streams a pane's output to stdout until it ends, then exits with
// its code; after waitMax (or Ctrl-C) it returns and leaves it running.
func (c *cmd) follow(f *forgeClient, p forge.Pane, waitMax time.Duration) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if waitMax > 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, waitMax)
		defer stop()
	}
	url := "ws" + strings.TrimPrefix(f.state.URL, "http") + "/api/sessions/" + p.ID + "/stream"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + f.state.Token}}})
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 22)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			if ctx.Err() != nil {
				fmt.Fprintf(c.cfg.Stderr, "\n%s: %s still runs. Read it: %[1]s read %[2]s; wait for it: %[1]s wait %[2]s\n", c.cfg.Prog, p.ID)
				return nil
			}
			return err
		}
		c.cfg.Stdout.Write(data)
	}
	ended, err := f.resolve(p.ID)
	if err == nil && ended.ExitCode != nil && *ended.ExitCode != 0 {
		return exitCode(*ended.ExitCode)
	}
	return nil
}

// exitCode makes Main exit with a program's own code, quietly.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

func (c *cmd) ls(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	children := fs.Bool("children", false, "only this pane's children")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	ps, err := f.panes()
	if err != nil {
		return err
	}
	if *children {
		me := os.Getenv("LAFORGE_SESSION")
		var mine []forge.Pane
		for _, p := range ps {
			if me != "" && p.Parent == me {
				mine = append(mine, p)
			}
		}
		ps = mine
	}
	if *asJSON {
		return json.NewEncoder(c.cfg.Stdout).Encode(ps)
	}
	tw := tabwriter.NewWriter(c.cfg.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPARENT\tSTATE\tAGENT")
	for _, p := range ps {
		parent := p.Parent
		if parent == "" {
			parent = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Title, parent, p.State, agentLine(p))
	}
	return tw.Flush()
}

func (c *cmd) send(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	noEnter := fs.Bool("no-enter", false, "type the text without pressing Enter")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return usageError{err}
	}
	if len(positional) < 2 {
		return usageError{errors.New("send PANE TEXT")}
	}
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	f.http.Timeout = 90 * time.Second // a send waits for a person's half-typed line
	p, err := f.resolve(positional[0])
	if err != nil {
		return err
	}
	text := strings.Join(positional[1:], " ")
	return f.do(http.MethodPost, "/api/sessions/"+p.ID+"/input", map[string]any{"text": text, "enter": !*noEnter}, nil)
}

func (c *cmd) read(args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	lines := fs.Int("lines", 100, "how many of the last lines")
	raw := fs.Bool("raw", false, "the retained bytes, escape sequences and all")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return usageError{err}
	}
	if len(positional) != 1 {
		return usageError{errors.New("read PANE")}
	}
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	p, err := f.resolve(positional[0])
	if err != nil {
		return err
	}
	q := fmt.Sprintf("?lines=%d", *lines)
	if *raw {
		q = "?raw=1"
	}
	req, _ := http.NewRequest(http.MethodGet, f.state.URL+"/api/sessions/"+p.ID+"/output"+q, nil)
	req.Header.Set("Authorization", "Bearer "+f.state.Token)
	resp, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, err = io.Copy(c.cfg.Stdout, resp.Body)
	return err
}

func (c *cmd) closePane(args []string) error {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	force := fs.Bool("force", false, "kill it now")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return usageError{err}
	}
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	for _, name := range positional {
		p, err := f.resolve(name)
		if err != nil {
			return err
		}
		q := ""
		if *force {
			q = "?force=1"
		}
		if err := f.do(http.MethodDelete, "/api/sessions/"+p.ID+q, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// reached reports whether pane p is at the state until names.
func reached(p forge.Pane, until string) bool {
	ended := p.EndedAt != nil
	word := ""
	if p.Status != nil {
		word = p.Status.State
	}
	switch until {
	case "ended", "exit":
		return ended
	case "attention":
		return ended || word == "blocked" || word == "error" || word == "done"
	case "done":
		return ended || word == "done"
	default:
		return word == until
	}
}

// wait is `la wait PANE… [--until STATE]` or `la wait --children`.
func (c *cmd) wait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	until := fs.String("until", "attention", "attention (blocked, error, done or ended), done, blocked, idle, working, error or ended")
	timeout := fs.Duration("timeout", 0, "give up after this long (exit 3)")
	children := fs.Bool("children", false, "wait for every child of this pane")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return usageError{err}
	}
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	f.http.Timeout = 0
	want := map[string]bool{}
	for _, name := range positional {
		p, err := f.resolve(name)
		if err != nil {
			return err
		}
		want[p.ID] = true
	}
	me := os.Getenv("LAFORGE_SESSION")
	if *children && me == "" {
		return errors.New("--children: not in a LaForge pane")
	}
	if len(want) == 0 && !*children {
		return usageError{errors.New("wait for which pane? (or --children)")}
	}
	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.state.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+f.state.Token)
	resp, err := f.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return exitCode(3)
		}
		return err
	}
	defer resp.Body.Close()
	state := map[string]forge.Pane{}
	done := func() bool {
		if *children {
			for _, p := range state {
				if p.Parent == me {
					want[p.ID] = true
				}
			}
			if len(want) == 0 {
				return true
			}
		}
		for id := range want {
			p, ok := state[id]
			if ok && !reached(p, *until) {
				return false
			}
		}
		return true
	}
	report := func() {
		for id := range want {
			if p, ok := state[id]; ok {
				fmt.Fprintf(c.cfg.Stdout, "%s\t%s\t%s\n", p.ID, p.Title, agentLine(p)+endedNote(p))
			} else {
				fmt.Fprintf(c.cfg.Stdout, "%s\tclosed\n", id)
			}
		}
	}
	err = readEvents(resp.Body, func(name string, data []byte) bool {
		switch name {
		case "panes":
			var ps []forge.Pane
			_ = json.Unmarshal(data, &ps)
			for _, p := range ps {
				state[p.ID] = p
			}
		case "pane.changed":
			var ev forge.Event
			_ = json.Unmarshal(data, &ev)
			state[ev.Pane.ID] = ev.Pane
		case "pane.closed":
			var ev forge.Event
			_ = json.Unmarshal(data, &ev)
			p := ev.Pane
			if p.EndedAt == nil {
				now := time.Now()
				p.EndedAt = &now
			}
			state[p.ID] = p
		}
		return !done()
	})
	if ctx.Err() != nil {
		report()
		return exitCode(3)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	report()
	return nil
}

func endedNote(p forge.Pane) string {
	if p.EndedAt == nil {
		return ""
	}
	if p.ExitCode != nil {
		return fmt.Sprintf(" (ended, exit %d)", *p.ExitCode)
	}
	return " (ended)"
}

// readEvents reads server-sent events, calling on for each until it
// returns false or the stream ends.
func readEvents(r io.Reader, on func(name string, data []byte) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	name, data := "", ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && name != "":
			if !on(name, []byte(data)) {
				return nil
			}
			name, data = "", ""
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF
}

func (c *cmd) events(args []string) error {
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	f.http.Timeout = 0
	req, _ := http.NewRequest(http.MethodGet, f.state.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+f.state.Token)
	resp, err := f.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	err = readEvents(resp.Body, func(name string, data []byte) bool {
		fmt.Fprintf(c.cfg.Stdout, "%s %s\n", name, data)
		return true
	})
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// share is `la share PANE [--for D]`, `la share --list`, `la share --revoke TOKEN`.
func (c *cmd) share(args []string) error {
	fs := flag.NewFlagSet("share", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	life := fs.String("for", "24h", "how long the link works (at most 168h)")
	list := fs.Bool("list", false, "list the live links")
	revoke := fs.String("revoke", "", "end a link")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return usageError{err}
	}
	f, err := c.apiClient()
	if err != nil {
		return err
	}
	switch {
	case *revoke != "":
		return f.do(http.MethodDelete, "/api/shares/"+*revoke, nil, nil)
	case *list:
		var shares []forge.Share
		if err := f.do(http.MethodGet, "/api/shares", nil, &shares); err != nil {
			return err
		}
		for _, sh := range shares {
			fmt.Fprintf(c.cfg.Stdout, "%s\t%s\tuntil %s\n", sh.Token, sh.Pane, sh.Expires.Local().Format(time.RFC822))
		}
		return nil
	}
	if len(positional) != 1 {
		return usageError{errors.New("share PANE")}
	}
	p, err := f.resolve(positional[0])
	if err != nil {
		return err
	}
	var sh forge.Share
	if err := f.do(http.MethodPost, "/api/shares", map[string]string{"pane": p.ID, "for": *life}, &sh); err != nil {
		return err
	}
	fmt.Fprintf(c.cfg.Stdout, "%s/?view=%s\n  watches %q only, until %s (revoke: %s share --revoke %s)\n", f.state.URL, sh.Token, p.Title, sh.Expires.Local().Format(time.RFC822), c.cfg.Prog, sh.Token)
	return nil
}
