// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

//go:build !windows

package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func viewCall(t *testing.T, url, method, path, token string, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url+path, strings.NewReader(body))
	req.Header.Set("Authorization", "View "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// A share opens one pane to watch: its view, stream and output. Its input is
// ignored, other panes and every change are refused, and it ends when the
// pane closes or it is revoked.
func TestAShareOnlyWatchesOnePane(t *testing.T) {
	_, ts := newTestServer(t)
	shared := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", "echo shared-out; while read l; do echo \"typed:$l\"; done"}})
	other := startReq(t, ts, StartRequest{Argv: []string{"/bin/sh", "-c", "sleep 30"}})
	r := call(t, ts, "POST", "/api/shares", "secret", map[string]string{"pane": shared.ID, "for": "1h"})
	var sh Share
	_ = json.NewDecoder(r.Body).Decode(&sh)
	if r.StatusCode != 201 || sh.Token == "" || sh.Token == "secret" || time.Until(sh.Expires) > time.Hour+time.Minute {
		t.Fatalf("share: %d %+v", r.StatusCode, sh)
	}
	var ps []Pane
	_ = json.NewDecoder(viewCall(t, ts.URL, "GET", "/api/sessions", sh.Token, "").Body).Decode(&ps)
	if len(ps) != 1 || ps[0].ID != shared.ID {
		t.Fatalf("panes through the share: %+v", ps)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/sessions/" + other.ID + "/output"},
		{"GET", "/api/sessions/" + other.ID + "/stream"},
		{"POST", "/api/sessions/" + shared.ID + "/input"},
		{"DELETE", "/api/sessions/" + shared.ID},
		{"POST", "/api/sessions"},
		{"POST", "/api/shares"},
		{"GET", "/api/shares"},
		{"PUT", "/api/layout"},
	} {
		if r := viewCall(t, ts.URL, c.method, c.path, sh.Token, `{"text":"x","enter":true}`); r.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s through a share: %d", c.method, c.path, r.StatusCode)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/sessions/"+shared.ID+"/stream",
		&websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"View " + sh.Token}}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	msg, _ := json.Marshal(clientMessage{Type: "input", Data: "sneaky\r"})
	conn.Write(ctx, websocket.MessageText, msg)
	var seen strings.Builder
	for !strings.Contains(seen.String(), "shared-out") {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		seen.Write(data)
	}
	time.Sleep(300 * time.Millisecond)
	out := call(t, ts, "GET", "/api/sessions/"+shared.ID+"/output", "secret", nil)
	var b strings.Builder
	buf := make([]byte, 4096)
	n, _ := out.Body.Read(buf)
	b.Write(buf[:n])
	if strings.Contains(b.String(), "typed:sneaky") {
		t.Fatal("a share typed into the pane")
	}
	// the page opens with the link; a revoked share opens nothing
	if r := viewCall(t, ts.URL, "GET", "/api/whoami", sh.Token, ""); r.StatusCode != 200 {
		t.Fatalf("whoami: %d", r.StatusCode)
	}
	call(t, ts, "DELETE", "/api/shares/"+sh.Token, "secret", nil)
	if r := viewCall(t, ts.URL, "GET", "/api/sessions", sh.Token, ""); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after revoke: %d", r.StatusCode)
	}
}
