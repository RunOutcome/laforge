// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package forge

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func startReq(t *testing.T, ts *httptest.Server, req StartRequest) Pane {
	t.Helper()
	r := call(t, ts, "POST", "/api/sessions", "secret", req)
	var p Pane
	_ = json.NewDecoder(r.Body).Decode(&p)
	if r.StatusCode != 201 {
		t.Fatalf("start %+v: %d %+v", req, r.StatusCode, p)
	}
	return p
}

func paneByID(t *testing.T, ts *httptest.Server, id string) (Pane, bool) {
	for _, p := range panesOf(t, ts) {
		if p.ID == id {
			return p, true
		}
	}
	return Pane{}, false
}

// A child ends with its parent unless it outlives it; the parent is told
// when a child is blocked and when it ends, once per change.
func TestChildrenEndWithTheirParentAndReportToIt(t *testing.T) {
	defer func(q time.Duration) { noteQuiet = q }(noteQuiet)
	noteQuiet = 50 * time.Millisecond
	_, ts := newTestServer(t)
	parent := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", "while read l; do echo \"heard: $l\"; done"}, Title: "parent"})
	_, readParent, _ := streamOf(t, ts, parent.ID)
	child := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", "read l"}, Title: "worker", Parent: parent.ID, Notes: true})
	keeper := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", "sleep 30"}, Title: "keeper", Parent: parent.ID, OutliveParent: true})
	if child.Parent != parent.ID || !child.Notes || !keeper.Outlive {
		t.Fatalf("child %+v keeper %+v", child, keeper)
	}
	if r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Argv: []string{"true"}, Parent: "s-none"}); r.StatusCode != 400 {
		t.Fatalf("unknown parent: %d", r.StatusCode)
	}
	call(t, ts, "POST", "/api/sessions/"+child.ID+"/status", "secret", map[string]any{"state": "blocked", "msg": "Which file?"})
	out := readParent(`heard: [laforge] child "worker"`)
	if !strings.Contains(out, `heard: [laforge] child "worker" (`+child.ID+`) is blocked: Which file?. Read it with: la read `+child.ID) {
		t.Fatalf("note: %q", out)
	}
	// the same status again is not a new note
	call(t, ts, "POST", "/api/sessions/"+child.ID+"/status", "secret", map[string]any{"state": "blocked", "msg": "Which file?"})
	call(t, ts, "POST", "/api/sessions/"+child.ID+"/input", "secret", map[string]any{"text": "x", "enter": true})
	out = readParent("is ended")
	out = readParent("heard: [laforge] child \"worker\" (" + child.ID + ") is ended")
	if strings.Count(out, "heard: [laforge] child \"worker\" ("+child.ID+") is blocked") != 1 {
		t.Fatalf("noted twice: %q", out)
	}
	// the parent ends: the keeper stays
	worker2 := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", "sleep 30"}, Title: "w2", Parent: parent.ID})
	call(t, ts, "DELETE", "/api/sessions/"+parent.ID+"?force=1", "secret", nil)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := paneByID(t, ts, worker2.ID); !ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, ok := paneByID(t, ts, worker2.ID); ok {
		t.Fatal("a child outlived its parent")
	}
	if p, ok := paneByID(t, ts, keeper.ID); !ok || p.EndedAt != nil {
		t.Fatalf("the keeper ended: %+v %v", p, ok)
	}
}

// A send waits for the person typing in the pane to stop; Enter comes on
// its own after the text.
func TestASendWaitsForThePersonTyping(t *testing.T) {
	defer func(q time.Duration) { noteQuiet = q }(noteQuiet)
	noteQuiet = 400 * time.Millisecond
	_, ts := newTestServer(t)
	p := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", "while read l; do echo \"line=[$l]\"; done"}})
	conn, read, ctx := streamOf(t, ts, p.ID)
	send(t, conn, ctx, "half")
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	call(t, ts, "POST", "/api/sessions/"+p.ID+"/input", "secret", map[string]any{"text": " typed", "enter": true})
	if took := time.Since(start); took < 300*time.Millisecond {
		t.Fatalf("sent over the person after %v", took)
	}
	read("line=[half typed]")
}

// Output reads back as text: escapes gone, a carriage return overwriting.
func TestOutputReadsAsText(t *testing.T) {
	_, ts := newTestServer(t)
	p := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", `printf '\033[31mred\033[0m\n10%%\r50%%\r100%%\n\033]7501;state=done\007last\n'; sleep 5`}})
	_, read, _ := streamOf(t, ts, p.ID)
	read("last")
	r := call(t, ts, "GET", "/api/sessions/"+p.ID+"/output?lines=3", "secret", nil)
	body, _ := io.ReadAll(r.Body)
	if string(body) != "red\n100%\nlast\n" {
		t.Fatalf("%q", body)
	}
	if got := plainText([]byte("a\x1b[2;5Hb\x1b[?25lc\x1b(B")); got != "a\nbc" {
		t.Fatalf("%q", got)
	}
}

// A profile takes extra arguments (a prompt for an agent).
func TestAProfileTakesExtraArguments(t *testing.T) {
	srv, ts := newTestServer(t)
	profilesAt(t, srv.opts.Home, Profile{Name: "echoer", Label: "Echoer", Command: "/bin/sh", Args: []string{"-c", `echo "got:[$*]"; sleep 5`, "echoer"}})
	p := startReq(t, ts, StartRequest{Profile: "echoer", Args: []string{"review", "x.go"}})
	_, read, _ := streamOf(t, ts, p.ID)
	read("got:[review x.go]")
}
