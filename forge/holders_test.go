// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package forge

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/RunOutcome/laforge/holder"
)

// forgeAt starts a forge on home with the given holders, as a restart would.
func forgeAt(t *testing.T, home string, holders *holder.Launcher) (*Server, *httptest.Server) {
	t.Helper()
	srv, err := NewServer(Options{Home: home, Token: "secret", Cwd: home, Version: "test", Holders: holders, Web: fstest.MapFS{"index.html": {Data: []byte("forge")}}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return srv, ts
}

func shortHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "lff")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func profilesAt(t *testing.T, home string, ps ...Profile) {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	raw, _ := json.Marshal(ps)
	if err := os.WriteFile(filepath.Join(home, "profiles.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func panesOf(t *testing.T, ts *httptest.Server) []Pane {
	t.Helper()
	var ps []Pane
	r := call(t, ts, "GET", "/api/sessions", "secret", nil)
	_ = json.NewDecoder(r.Body).Decode(&ps)
	return ps
}

// A held pane outlives its forge: the next forge on the same home adopts it
// with its id, title, output, status and agent session, and it still takes
// input; closing it there ends its holder.
func TestAHeldPaneOutlivesItsForge(t *testing.T) {
	home := shortHome(t)
	holders := &holder.Launcher{Dir: filepath.Join(home, "h"), InProcess: true}
	profilesAt(t, home, Profile{Name: "fake", Label: "Fake", Command: "/bin/sh", Agent: true,
		Args: []string{"-c", `echo before-restart; printf '\033]7501;state=working:app=fake\033\\'; while read l; do echo "said $l"; done`}})
	srv1, ts1 := forgeAt(t, home, holders)
	r := call(t, ts1, "POST", "/api/sessions", "secret", StartRequest{Profile: "fake", Title: "keeper"})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	if !pane.Held {
		t.Fatalf("not held: %+v", pane)
	}
	_, read1, _ := streamOf(t, ts1, pane.ID)
	read1("before-restart")
	call(t, ts1, "POST", "/api/sessions/"+pane.ID+"/agent", "secret", map[string]string{"agent": "fake", "sessionId": "abc-123"})
	call(t, ts1, "POST", "/api/sessions/"+pane.ID+"/status", "secret", map[string]any{"state": "blocked", "kind": "question", "msg": "Which one?", "source": "hook"})
	call(t, ts1, "PATCH", "/api/sessions/"+pane.ID, "secret", map[string]string{"title": "renamed"})
	time.Sleep(100 * time.Millisecond)
	srv1.Detach() // the forge goes; the pane does not
	ts1.Close()

	srv2, ts2 := forgeAt(t, home, holders)
	defer srv2.Shutdown(2 * time.Second)
	ps := panesOf(t, ts2)
	if len(ps) != 1 || ps[0].ID != pane.ID || ps[0].Title != "renamed" || !ps[0].Held || ps[0].AgentSession == nil || ps[0].AgentSession.SessionID != "abc-123" {
		t.Fatalf("adopted %+v", ps)
	}
	if !ps[0].StartedAt.Equal(pane.StartedAt) {
		t.Fatalf("started %v, was %v", ps[0].StartedAt, pane.StartedAt)
	}
	p := waitFor(t, ts2, func(p Pane) bool { return p.Status != nil && p.Status.State == "blocked" })
	if p.Status.Msg != "Which one?" || p.Status.Source != "hook" {
		t.Fatalf("status %+v", p.Status)
	}
	conn, read2, ctx := streamOf(t, ts2, pane.ID)
	read2("before-restart")
	send(t, conn, ctx, "hi\r")
	read2("said hi")
	call(t, ts2, "DELETE", "/api/sessions/"+pane.ID, "secret", nil)
	deadline := time.Now().Add(6 * time.Second)
	for holder.Alive(holders.Dir, pane.ID) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if holder.Alive(holders.Dir, pane.ID) {
		t.Fatal("closing the pane left its holder")
	}
	if _, err := os.Stat(filepath.Join(home, "panes", pane.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("record left: %v", err)
	}
}

// An agent that exits while no forge runs still leaves a shell: the next
// forge reads the exit its holder kept.
func TestAnAgentExitWhileTheForgeIsAwayIsNotLost(t *testing.T) {
	home := shortHome(t)
	holders := &holder.Launcher{Dir: filepath.Join(home, "h"), InProcess: true}
	profilesAt(t, home, Profile{Name: "fake", Label: "Fake", Command: "/bin/sh", Agent: true, Args: []string{"-c", "read l; exit 4"}})
	srv1, ts1 := forgeAt(t, home, holders)
	r := call(t, ts1, "POST", "/api/sessions", "secret", StartRequest{Profile: "fake"})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	srv1.Detach()
	ts1.Close()
	c, err := holder.Dial(holders.Dir, pane.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Write([]byte("x\n")) // the agent exits with no forge watching
	time.Sleep(300 * time.Millisecond)
	c.Detach()
	srv2, ts2 := forgeAt(t, home, holders)
	defer srv2.Shutdown(2 * time.Second)
	p := waitFor(t, ts2, func(p Pane) bool { return p.Profile == "shell" })
	if p.After != "fake" || p.AgentExit == nil || *p.AgentExit != 4 || p.Title != "Shell" {
		t.Fatalf("%+v", p)
	}
}

// Panes whose programs are gone are offered to resume; resuming one starts
// its profile in its folder with the agent's session.
func TestALostPaneResumesItsAgentSession(t *testing.T) {
	home := shortHome(t)
	work := filepath.Join(home, "work")
	os.Mkdir(work, 0o700)
	profilesAt(t, home, Profile{Name: "fake", Label: "Fake", Command: "/bin/sh", Agent: true, Args: []string{"-c", `echo "args:[$*] in:[$PWD]"; sleep 30`, "fake"}, Resume: []string{"--resume", "{session}"}})
	srv1, ts1 := forgeAt(t, home, nil) // no holders: the panes end with the forge
	r := call(t, ts1, "POST", "/api/sessions", "secret", StartRequest{Profile: "fake", Cwd: work, Title: "mine"})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	call(t, ts1, "POST", "/api/sessions/"+pane.ID+"/agent", "secret", map[string]string{"agent": "fake", "sessionId": "sess-9"})
	if r := call(t, ts1, "POST", "/api/sessions/"+pane.ID+"/agent", "secret", map[string]string{"agent": "fake", "sessionId": "x; rm -rf ~"}); r.StatusCode != 400 {
		t.Fatalf("shell characters in a session id: %d", r.StatusCode)
	}
	srv1.Shutdown(2 * time.Second)
	ts1.Close()

	srv2, ts2 := forgeAt(t, home, nil)
	defer srv2.Shutdown(2 * time.Second)
	var lost []LostPane
	_ = json.NewDecoder(call(t, ts2, "GET", "/api/lost", "secret", nil).Body).Decode(&lost)
	if len(lost) != 1 || lost[0].ID != pane.ID || !lost[0].Resumable || lost[0].Title != "mine" || lost[0].AgentSession.SessionID != "sess-9" {
		t.Fatalf("lost %+v", lost)
	}
	r = call(t, ts2, "POST", "/api/sessions", "secret", StartRequest{Profile: "fake", Resume: pane.ID})
	var again Pane
	_ = json.NewDecoder(r.Body).Decode(&again)
	if r.StatusCode != 201 || again.Title != "mine" || again.Cwd != work {
		t.Fatalf("resume: %d %+v", r.StatusCode, again)
	}
	_, read, _ := streamOf(t, ts2, again.ID)
	out := read("in:[")
	if !strings.Contains(out, "args:[--resume sess-9]") {
		t.Fatalf("%q", out)
	}
	_ = json.NewDecoder(call(t, ts2, "GET", "/api/lost", "secret", nil).Body).Decode(&lost)
	if len(lost) != 0 {
		t.Fatalf("still lost: %+v", lost)
	}
	if r := call(t, ts2, "POST", "/api/sessions", "secret", StartRequest{Profile: "fake", Resume: pane.ID}); r.StatusCode != 404 {
		t.Fatalf("resumed twice: %d", r.StatusCode)
	}
}

func waitFor(t *testing.T, ts *httptest.Server, ok func(Pane) bool) Pane {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []Pane
	for time.Now().Before(deadline) {
		last = panesOf(t, ts)
		for _, p := range last {
			if ok(p) {
				return p
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("no pane reached the state: %+v", last)
	return Pane{}
}
