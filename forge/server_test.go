// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// These tests drive real shells (/bin/sh); Windows panes are covered by the
// terminal package's ConPTY tests.
//go:build !windows

package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	// one profile that is certainly installed: /bin/sh as "shell"
	raw, _ := json.Marshal([]Profile{{Name: "shell", Label: "Shell", Command: "/bin/sh"}})
	if err := os.WriteFile(filepath.Join(home, "profiles.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(Options{Home: home, Token: "secret", Cwd: t.TempDir(), Version: "test", Web: fstest.MapFS{"index.html": {Data: []byte("forge")}}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		srv.Shutdown(2 * time.Second)
		ts.Close()
	})
	return srv, ts
}

func call(t *testing.T, ts *httptest.Server, method, path, token string, body any) *http.Response {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, ts.URL+path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestTheAPIRefusesWithoutTheToken(t *testing.T) {
	_, ts := newTestServer(t)
	for _, path := range []string{"/api/sessions", "/api/profiles", "/api/layout"} {
		if r := call(t, ts, "GET", path, "", nil); r.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without token: %d, want 401", path, r.StatusCode)
		}
		if r := call(t, ts, "GET", path, "wrong", nil); r.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s with a wrong token: %d, want 401", path, r.StatusCode)
		}
	}
	if r := call(t, ts, "GET", "/", "", nil); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("page without token: %d, want 401", r.StatusCode)
	}
	if r := call(t, ts, "GET", "/api/health", "", nil); r.StatusCode != http.StatusOK {
		t.Errorf("health is open: %d", r.StatusCode)
	}
}

func TestTheLinkTokenSetsACookieThatOpensThePage(t *testing.T) {
	_, ts := newTestServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(ts.URL + "/?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("token link: %d, want 302", resp.StatusCode)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName || !cookies[0].HttpOnly {
		t.Fatalf("cookie: %+v", cookies)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/", nil)
	req.AddCookie(cookies[0])
	page, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("page with cookie: %d", page.StatusCode)
	}
	if bad, _ := client.Get(ts.URL + "/?token=nope"); bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong link token: %d, want 401", bad.StatusCode)
	}
}

func TestASessionStreamsItsOutputAndTakesInput(t *testing.T) {
	_, ts := newTestServer(t)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "shell", Cols: 80, Rows: 24})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d", r.StatusCode)
	}
	var pane Pane
	if err := json.NewDecoder(r.Body).Decode(&pane); err != nil {
		t.Fatal(err)
	}
	if pane.Profile != "shell" || pane.Title != "Shell" {
		t.Fatalf("pane: %+v", pane)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/" + pane.ID + "/stream"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	msg, _ := json.Marshal(clientMessage{Type: "input", Data: "echo forge-$((40+2))\rexit\r"})
	if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
		t.Fatal(err)
	}
	var seen bytes.Buffer
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			t.Fatalf("read: %v (seen %q)", err, seen.String())
		}
		seen.Write(data)
	}
	if !strings.Contains(seen.String(), "forge-42") {
		t.Fatalf("output %q lacks forge-42", seen.String())
	}
	// once ended, the listing says so
	r = call(t, ts, "GET", "/api/sessions", "secret", nil)
	var panes []Pane
	_ = json.NewDecoder(r.Body).Decode(&panes)
	if len(panes) != 1 || panes[0].State != "complete" {
		t.Fatalf("after exit: %+v", panes)
	}
}

func TestAnUnknownOrUninstalledProfileIsRefused(t *testing.T) {
	srv, ts := newTestServer(t)
	if r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "nope"}); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown profile: %d", r.StatusCode)
	}
	raw, _ := json.Marshal([]Profile{{Name: "ghost", Command: "laforge-command-that-does-not-exist"}})
	_ = os.WriteFile(filepath.Join(srv.opts.Home, "profiles.json"), raw, 0o600)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "ghost"})
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)
	if r.StatusCode != http.StatusBadRequest || body["error"] != "profile_unavailable" {
		t.Fatalf("uninstalled profile: %d %v", r.StatusCode, body)
	}
}

func TestAFolderThatDoesNotExistIsRefused(t *testing.T) {
	_, ts := newTestServer(t)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "shell", Cwd: "/no/such/folder"})
	if r.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing folder: %d", r.StatusCode)
	}
}

func TestTheLayoutIsKeptAsGiven(t *testing.T) {
	_, ts := newTestServer(t)
	if r := call(t, ts, "GET", "/api/layout", "secret", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("empty layout: %d", r.StatusCode)
	}
	layout := map[string]any{"version": 1, "active": "t1", "tabs": []any{map[string]any{"id": "t1"}}}
	if r := call(t, ts, "PUT", "/api/layout", "secret", layout); r.StatusCode != http.StatusNoContent {
		t.Fatalf("put: %d", r.StatusCode)
	}
	r := call(t, ts, "GET", "/api/layout", "secret", nil)
	var back map[string]any
	_ = json.NewDecoder(r.Body).Decode(&back)
	if back["active"] != "t1" {
		t.Fatalf("layout back: %v", back)
	}
	req, _ := http.NewRequest("PUT", ts.URL+"/api/layout", strings.NewReader("{not json"))
	req.Header.Set("Authorization", "Bearer secret")
	bad, _ := http.DefaultClient.Do(req)
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad layout: %d", bad.StatusCode)
	}
}

func TestClosingASessionStopsAndForgetsIt(t *testing.T) {
	_, ts := newTestServer(t)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "shell"})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	if r := call(t, ts, "DELETE", "/api/sessions/"+pane.ID, "secret", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("close: %d", r.StatusCode)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r := call(t, ts, "GET", "/api/sessions", "secret", nil)
		var panes []Pane
		_ = json.NewDecoder(r.Body).Decode(&panes)
		if len(panes) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the closed session is still listed")
}

func TestAgentEnvironmentIsNotLeakedIntoPanes(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	_, ts := newTestServer(t)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "shell"})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/" + pane.ID + "/stream"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	msg, _ := json.Marshal(clientMessage{Type: "input", Data: "echo \"cc=[${CLAUDECODE-unset}] id=[$LAFORGE_SESSION]\"\rexit\r"})
	_ = conn.Write(ctx, websocket.MessageText, msg)
	var seen bytes.Buffer
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		seen.Write(data)
	}
	out := seen.String()
	if !strings.Contains(out, "cc=[unset]") || !strings.Contains(out, "id=["+pane.ID+"]") {
		t.Fatalf("environment: %q", out)
	}
}

// When an agent exits, its pane carries on as a shell, relabelled.
func TestAnAgentThatExitsLeavesAShell(t *testing.T) {
	srv, ts := newTestServer(t)
	t.Setenv("SHELL", "/bin/sh")
	raw, _ := json.Marshal([]Profile{{Name: "fake", Label: "Fake Agent", Command: "/bin/sh", Args: []string{"-c", "echo agent-ran; exit 3"}, Agent: true}})
	_ = os.WriteFile(filepath.Join(srv.opts.Home, "profiles.json"), raw, 0o600)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "fake", Cols: 100, Rows: 30})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	if pane.Command != "/bin/sh -c echo agent-ran; exit 3" {
		t.Fatalf("shown command: %q", pane.Command)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/" + pane.ID + "/stream"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var seen bytes.Buffer
	read := func(until string) {
		for !strings.Contains(seen.String(), until) {
			_, data, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("waiting for %q: %v (seen %q)", until, err, seen.String())
			}
			seen.Write(data)
		}
	}
	read("This pane is a shell now")
	if !strings.Contains(seen.String(), "agent-ran") || !strings.Contains(seen.String(), "Fake Agent exited (3)") {
		t.Fatalf("output: %q", seen.String())
	}
	msg, _ := json.Marshal(clientMessage{Type: "input", Data: "echo still-$((1+1))\r"})
	_ = conn.Write(ctx, websocket.MessageText, msg)
	read("still-2")

	p := waitForPane(t, ts, func(p Pane) bool { return p.Profile == "shell" })
	if p.Title != "Shell" || p.After != "fake" || p.AgentExit == nil || *p.AgentExit != 3 || p.State == "complete" {
		t.Fatalf("after the agent: %+v", p)
	}
}

// waitForPane polls the only pane until ok holds (the relabel is async).
func waitForPane(t *testing.T, ts *httptest.Server, ok func(Pane) bool) Pane {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []Pane
	for time.Now().Before(deadline) {
		r := call(t, ts, "GET", "/api/sessions", "secret", nil)
		last = nil
		_ = json.NewDecoder(r.Body).Decode(&last)
		if len(last) == 1 && ok(last[0]) {
			return last[0]
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("pane never reached the state: %+v", last)
	return Pane{}
}

func streamOf(t *testing.T, ts *httptest.Server, id string) (*websocket.Conn, func(until string) string, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/sessions/" + id + "/stream"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	var seen bytes.Buffer
	read := func(until string) string {
		for !strings.Contains(seen.String(), until) {
			_, data, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("waiting for %q: %v (seen %q)", until, err, seen.String())
			}
			seen.Write(data)
		}
		return seen.String()
	}
	return conn, read, ctx
}

func startFake(t *testing.T, srv *Server, ts *httptest.Server, script string, title string, env ...string) Pane {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	raw, _ := json.Marshal([]Profile{{Name: "fake", Label: "Fake Agent", Command: "/bin/sh", Args: []string{"-c", script}, Env: env, Agent: true}})
	_ = os.WriteFile(filepath.Join(srv.opts.Home, "profiles.json"), raw, 0o600)
	r := call(t, ts, "POST", "/api/sessions", "secret", StartRequest{Profile: "fake", Title: title, Cols: 100, Rows: 30})
	var pane Pane
	_ = json.NewDecoder(r.Body).Decode(&pane)
	return pane
}

// A Ctrl-C that kills the agent still leaves a shell (the wrapper ignores it).
func TestCtrlCInAnAgentStillLeavesAShell(t *testing.T) {
	srv, ts := newTestServer(t)
	pane := startFake(t, srv, ts, "echo agent-ready; sleep 30", "")
	conn, read, ctx := streamOf(t, ts, pane.ID)
	read("agent-ready")
	msg, _ := json.Marshal(clientMessage{Type: "input", Data: "\x03"})
	_ = conn.Write(ctx, websocket.MessageText, msg)
	read("This pane is a shell now")
	msg, _ = json.Marshal(clientMessage{Type: "input", Data: "echo alive-$((2+3))\r"})
	_ = conn.Write(ctx, websocket.MessageText, msg)
	read("alive-5")
	waitForPane(t, ts, func(p Pane) bool { return p.Profile == "shell" })
}

// A name the user gave survives the agent's exit; the agent's Env stays with the agent.
func TestTheUsersNameAndTheAgentsEnvAreKept(t *testing.T) {
	srv, ts := newTestServer(t)
	pane := startFake(t, srv, ts, "echo key=[$FAKE_KEY]", "api-refactor", "FAKE_KEY=sekrit")
	conn, read, ctx := streamOf(t, ts, pane.ID)
	read("This pane is a shell now")
	msg, _ := json.Marshal(clientMessage{Type: "input", Data: "echo after=[${FAKE_KEY-unset}]\r"})
	_ = conn.Write(ctx, websocket.MessageText, msg)
	out := read("after=[unset]")
	if !strings.Contains(out, "key=[sekrit]") {
		t.Fatalf("the agent did not get its env: %q", out)
	}
	p := waitForPane(t, ts, func(p Pane) bool { return p.Profile == "shell" })
	if p.Title != "api-refactor" {
		t.Fatalf("title: %q", p.Title)
	}
}

// Output that looks like the old in-band marker changes nothing.
func TestAgentOutputCannotFakeTheExit(t *testing.T) {
	srv, ts := newTestServer(t)
	pane := startFake(t, srv, ts, `printf '\033]7770;laforge-shell\007'; echo faked; sleep 30`, "")
	_, read, _ := streamOf(t, ts, pane.ID)
	read("faked")
	time.Sleep(200 * time.Millisecond)
	r := call(t, ts, "GET", "/api/sessions", "secret", nil)
	var panes []Pane
	_ = json.NewDecoder(r.Body).Decode(&panes)
	if len(panes) != 1 || panes[0].Profile != "fake" {
		t.Fatalf("relabelled by output: %+v", panes)
	}
}

// A page on another local port cannot make changes with the forge's cookie.
func TestChangesFromAnotherOriginAreRefused(t *testing.T) {
	_, ts := newTestServer(t)
	post := func(headers map[string]string) int {
		req, _ := http.NewRequest("POST", ts.URL+"/api/sessions", strings.NewReader(`{"profile":"shell"}`))
		req.Header.Set("Content-Type", "text/plain")
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "secret"})
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := post(map[string]string{"Origin": "http://127.0.0.1:3000"}); code != http.StatusForbidden {
		t.Fatalf("other origin: %d, want 403", code)
	}
	if code := post(map[string]string{"Sec-Fetch-Site": "same-site"}); code != http.StatusForbidden {
		t.Fatalf("same-site, not same-origin: %d, want 403", code)
	}
	if code := post(map[string]string{"Origin": ts.URL, "Sec-Fetch-Site": "same-origin"}); code != http.StatusCreated {
		t.Fatalf("own page: %d, want 201", code)
	}
	if code := post(nil); code != http.StatusCreated { // the CLI: no Origin
		t.Fatalf("no origin: %d, want 201", code)
	}
}

// Saves that arrive together never leave a broken layout.
func TestConcurrentLayoutSavesStayWhole(t *testing.T) {
	srv, ts := newTestServer(t)
	done := make(chan int, 20)
	for i := 0; i < 20; i++ {
		go func(i int) {
			body := fmt.Sprintf(`{"version":1,"active":"t%d","tabs":[{"id":"t%d","pad":%q}]}`, i, i, strings.Repeat("x", 20000))
			req, _ := http.NewRequest("PUT", ts.URL+"/api/layout", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer secret")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				done <- 0
				return
			}
			resp.Body.Close()
			done <- resp.StatusCode
		}(i)
	}
	for i := 0; i < 20; i++ {
		if code := <-done; code != http.StatusNoContent {
			t.Fatalf("save: %d", code)
		}
	}
	raw, err := os.ReadFile(filepath.Join(srv.opts.Home, "layout.json"))
	if err != nil || !json.Valid(raw) {
		t.Fatalf("layout after concurrent saves is not JSON (%v)", err)
	}
	left, _ := filepath.Glob(filepath.Join(srv.opts.Home, "layout-*.json"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}
