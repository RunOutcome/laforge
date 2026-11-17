// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package forge

import (
	"context"

	"bufio"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"strings"
	"testing"
	"time"
)

// events follows GET /api/events and returns a function that waits for the
// next event matching ok.
func events(t *testing.T, url string) func(ok func(name string, ev Event) bool) Event {
	t.Helper()
	req, _ := http.NewRequest("GET", url+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("events: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	type frame struct {
		name string
		data string
	}
	frames := make(chan frame, 64)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		var f frame
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				f.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				f.data = strings.TrimPrefix(line, "data: ")
			case line == "" && f.name != "":
				frames <- f
				f = frame{}
			}
		}
		close(frames)
	}()
	first := <-frames
	if first.name != "panes" {
		t.Fatalf("first event %q", first.name)
	}
	return func(ok func(string, Event) bool) Event {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case f, open := <-frames:
				if !open {
					t.Fatal("events ended")
				}
				var ev Event
				_ = json.Unmarshal([]byte(f.data), &ev)
				if ok(f.name, ev) {
					return ev
				}
			case <-deadline:
				t.Fatal("no such event")
			}
		}
	}
}

// An agent's own OSC 7501 reaches its pane's view and the event stream; a
// viewer marking it seen turns a done agent's pane seen until it says
// something new; when the agent exits and the pane becomes a shell, the
// agent's status goes with it.
func TestAnAgentsStatusReachesItsPaneAndTheEvents(t *testing.T) {
	srv, ts := newTestServer(t)
	next := events(t, ts.URL)
	pane := startFake(t, srv, ts, `printf '\033]7501;state=working:app=fake\033\\'; read l; printf '\033]7501;state=done\007'; read l`, "")
	conn, _, ctx := streamOf(t, ts, pane.ID)
	ev := next(func(name string, ev Event) bool {
		return name == "pane.changed" && ev.Pane.ID == pane.ID && ev.Pane.Status != nil && ev.Pane.Status.State == "working"
	})
	if ev.Pane.Status.App != "fake" || ev.Pane.Seen || ev.Seq == 0 {
		t.Fatalf("%+v", ev.Pane)
	}
	send(t, conn, ctx, "\n")
	next(func(name string, ev Event) bool { return ev.Pane.Status != nil && ev.Pane.Status.State == "done" })
	if r := call(t, ts, "POST", "/api/sessions/"+pane.ID+"/seen", "secret", nil); r.StatusCode != 200 {
		t.Fatalf("seen: %d", r.StatusCode)
	}
	next(func(name string, ev Event) bool { return ev.Pane.Seen && ev.Pane.Status.State == "done" })
	// a new word makes it unseen again
	r := call(t, ts, "POST", "/api/sessions/"+pane.ID+"/status", "secret", map[string]any{"state": "blocked", "kind": "permission", "msg": "Run make?"})
	var p Pane
	_ = json.NewDecoder(r.Body).Decode(&p)
	if r.StatusCode != 200 || p.Seen || p.Status.Kind != "permission" || p.Status.Msg != "Run make?" {
		t.Fatalf("report: %d %+v", r.StatusCode, p)
	}
	send(t, conn, ctx, "\n")
	ev = next(func(name string, ev Event) bool { return ev.Pane.Profile == "shell" && ev.Pane.Status == nil })
	if ev.Pane.Turn != 1 {
		t.Fatalf("turn %d", ev.Pane.Turn)
	}
}

// A report the forge cannot keep is refused with a reason, and a closed
// pane's last view is announced.
func TestBadReportsAreRefusedAndClosingIsAnnounced(t *testing.T) {
	_, ts := newTestServer(t)
	next := events(t, ts.URL)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "shell"})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	for _, body := range []any{map[string]any{"state": "thinking"}, map[string]any{}, "x"} {
		if r := call(t, ts, "POST", "/api/sessions/"+pane.ID+"/status", "secret", body); r.StatusCode != 400 {
			t.Errorf("%v: %d", body, r.StatusCode)
		}
	}
	if r := call(t, ts, "POST", "/api/sessions/nope/status", "secret", map[string]any{"state": "idle"}); r.StatusCode != 404 {
		t.Errorf("unknown pane: %d", r.StatusCode)
	}
	if r := call(t, ts, "POST", "/api/sessions/"+pane.ID+"/status", "", map[string]any{"state": "idle"}); r.StatusCode != 401 {
		t.Errorf("no token: %d", r.StatusCode)
	}
	call(t, ts, "DELETE", "/api/sessions/"+pane.ID+"?force=1", "secret", nil)
	next(func(name string, ev Event) bool { return name == "pane.closed" && ev.Pane.ID == pane.ID })
}

// Every pane knows its forge: LAFORGE_HOME is the forge's folder.
func TestPanesAreToldTheirForge(t *testing.T) {
	srv, ts := newTestServer(t)
	pane := startFake(t, srv, ts, `echo home=[$LAFORGE_HOME] session=[$LAFORGE_SESSION]`, "")
	_, read, _ := streamOf(t, ts, pane.ID)
	out := read("This pane is a shell now")
	if !strings.Contains(out, "home=["+srv.opts.Home+"]") || !strings.Contains(out, "session=["+pane.ID+"]") {
		t.Fatalf("%q", out)
	}
}

func send(t *testing.T, conn *websocket.Conn, ctx context.Context, data string) {
	t.Helper()
	msg, _ := json.Marshal(clientMessage{Type: "input", Data: data})
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
}
