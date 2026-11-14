// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"bytes"
	"context"
	"encoding/json"
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

	r = call(t, ts, "GET", "/api/sessions", "secret", nil)
	var panes []Pane
	_ = json.NewDecoder(r.Body).Decode(&panes)
	if len(panes) != 1 || panes[0].Title != "Shell" || panes[0].Profile != "shell" || panes[0].After != "fake" || panes[0].State == "complete" {
		t.Fatalf("after the agent: %+v", panes)
	}
}
