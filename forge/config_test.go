// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

func configServer(t *testing.T, home string) *httptest.Server {
	t.Helper()
	srv, err := NewServer(Options{Home: home, Token: "secret", Cwd: t.TempDir(), Version: "test", Port: 4321, Web: fstest.MapFS{"index.html": {Data: []byte("forge")}}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		srv.Shutdown(time.Second)
		ts.Close()
	})
	return ts
}

func configCall(t *testing.T, ts *httptest.Server, method, path string, body any, out any) int {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case nil:
	case string:
		raw = []byte(b)
	default:
		raw, _ = json.Marshal(b)
	}
	req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestConfigShowsEverySettingAndWhatTheForgeIs(t *testing.T) {
	home := t.TempDir()
	ts := configServer(t, home)
	var c ForgeConfig
	if code := configCall(t, ts, "GET", "/api/config", nil, &c); code != 200 {
		t.Fatal(code)
	}
	if c.Skin != "classic" || !c.Scenes || c.CwdSaved || c.Cwd == "" || c.Version != "test" || c.Port != 4321 || c.Home != home || len(c.ReadOnly) == 0 || len(c.Profiles) == 0 {
		t.Fatalf("%+v", c)
	}
}

// A PUT changes the keys it names and keeps the rest; a new server on the
// same home has them.
func TestAPartialPutChangesOnlyItsKeysAndOutlivesTheServer(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	ts := configServer(t, home)
	var c ForgeConfig
	if code := configCall(t, ts, "PUT", "/api/config", map[string]any{"scenes": false}, &c); code != 200 || c.Scenes || c.Skin != "classic" {
		t.Fatalf("%d %+v", code, c)
	}
	if code := configCall(t, ts, "PUT", "/api/config", map[string]any{"cwd": dir, "skin": "console"}, &c); code != 200 {
		t.Fatal(code)
	}
	if c.Cwd != dir || !c.CwdSaved || c.Skin != "console" || c.Scenes {
		t.Fatalf("%+v", c)
	}
	// the old files are still where la skin and older clients look
	if raw, _ := os.ReadFile(filepath.Join(home, "skin")); string(raw) != "console\n" {
		t.Errorf("skin file: %q", raw)
	}
	if _, err := os.Stat(filepath.Join(home, scenesOffFile)); err != nil {
		t.Error("scenes-off is missing")
	}
	var skins struct{ Default string }
	configCall(t, ts, "GET", "/api/skins", nil, &skins)
	if skins.Default != "console" {
		t.Errorf("GET /api/skins default %q", skins.Default)
	}

	next := configServer(t, home)
	var again ForgeConfig
	configCall(t, next, "GET", "/api/config", nil, &again)
	if again.Cwd != dir || !again.CwdSaved || again.Skin != "console" || again.Scenes {
		t.Fatalf("after a restart: %+v", again)
	}
	// clearing the saved folder gives back where the forge was started
	configCall(t, next, "PUT", "/api/config", map[string]any{"cwd": ""}, &again)
	if again.CwdSaved || again.Cwd == dir {
		t.Fatalf("cleared: %+v", again)
	}
}

// la up in a folder sets this run's folder only; a saved one wins.
func TestAnUnsavedCwdIsThisRunsAndLosesToASavedOne(t *testing.T) {
	home := t.TempDir()
	run, saved := t.TempDir(), t.TempDir()
	ts := configServer(t, home)
	var c ForgeConfig
	configCall(t, ts, "PUT", "/api/config", map[string]any{"cwd": run, "save": false}, &c)
	if c.Cwd != run || c.CwdSaved {
		t.Fatalf("%+v", c)
	}
	if _, err := os.Stat(filepath.Join(home, configFile)); err == nil {
		t.Error("an unsaved cwd was saved")
	}
	configCall(t, ts, "PUT", "/api/config", map[string]any{"cwd": saved}, &c)
	configCall(t, ts, "PUT", "/api/config", map[string]any{"cwd": run, "save": false}, &c)
	if c.Cwd != saved {
		t.Fatalf("the saved folder lost: %+v", c)
	}
}

func TestBadConfigIsRefusedAndChangesNothing(t *testing.T) {
	home := t.TempDir()
	ts := configServer(t, home)
	for _, tc := range []struct {
		body any
		code int
		err  string
	}{
		{`not json`, 400, "input"},
		{`[1]`, 400, "input"},
		{map[string]any{"colour": "red"}, 400, "unknown_key"},
		{map[string]any{"version": "9"}, 400, "read_only"},
		{map[string]any{"port": 1}, 400, "read_only"},
		{map[string]any{"scenes": "yes"}, 400, "input"},
		{map[string]any{"scenes": nil}, 400, "input"},
		{map[string]any{"cwd": filepath.Join(home, "nope")}, 400, "input"},
		{map[string]any{"profiles": []any{}}, 400, "input"},
		// all or nothing: the good scenes does not land with the bad skin
		{map[string]any{"scenes": false, "skin": "no-such-skin"}, 404, "no_skin"},
	} {
		var e struct{ Error, Reason string }
		if code := configCall(t, ts, "PUT", "/api/config", tc.body, &e); code != tc.code || e.Error != tc.err || e.Reason == "" {
			t.Errorf("%v: %d %+v", tc.body, code, e)
		}
	}
	var c ForgeConfig
	configCall(t, ts, "GET", "/api/config", nil, &c)
	if !c.Scenes || c.Skin != "classic" || c.CwdSaved {
		t.Fatalf("a refused PUT changed something: %+v", c)
	}
}

func TestProfilesAreAddedChangedAndRemoved(t *testing.T) {
	home := t.TempDir()
	ts := configServer(t, home)
	exe, _ := os.Executable()

	var p Profile
	if code := configCall(t, ts, "PUT", "/api/profiles/mine", Profile{Command: exe, Args: []string{"-x"}, Agent: true}, &p); code != 200 {
		t.Fatal(code)
	}
	if p.Name != "mine" || p.Label != "mine" || !p.Available || !p.Custom || p.Builtin {
		t.Fatalf("%+v", p)
	}
	// a command that is not installed is kept, with a warning
	if code := configCall(t, ts, "PUT", "/api/profiles/later", Profile{Label: "Later", Command: "no-such-command-laforge"}, &p); code != 200 || p.Available || p.Reason == "" {
		t.Fatalf("%d %+v", code, p)
	}
	// changing a built-in, then taking the change back
	if code := configCall(t, ts, "PUT", "/api/profiles/codex", Profile{Label: "Codex (mine)", Command: "codex", Args: []string{"--full-auto"}, Agent: true}, &p); code != 200 || !p.Builtin || !p.Custom {
		t.Fatalf("%d %+v", code, p)
	}
	var list []Profile
	configCall(t, ts, "GET", "/api/profiles", nil, &list)
	names := map[string]Profile{}
	for _, x := range list {
		names[x.Name] = x
	}
	if names["codex"].Label != "Codex (mine)" || names["mine"].Command != exe || names["later"].Label != "Later" {
		t.Fatalf("%+v", list)
	}
	if code := configCall(t, ts, "DELETE", "/api/profiles/codex", nil, nil); code != 204 {
		t.Fatal(code)
	}
	if p, _ := findProfile(home, "codex"); p.Label != "Codex" || p.Custom {
		t.Fatalf("codex was not given back: %+v", p)
	}
	if code := configCall(t, ts, "DELETE", "/api/profiles/mine", nil, nil); code != 204 {
		t.Fatal(code)
	}
	if _, err := findProfile(home, "mine"); err != ErrNoProfile {
		t.Fatal("mine is still there")
	}
	var e struct{ Error string }
	if code := configCall(t, ts, "DELETE", "/api/profiles/claude", nil, &e); code != 409 || e.Error != "builtin" {
		t.Errorf("built-in: %d %+v", code, e)
	}
	if code := configCall(t, ts, "DELETE", "/api/profiles/nobody", nil, &e); code != 404 || e.Error != "no_profile" {
		t.Errorf("missing: %d %+v", code, e)
	}
	// profiles.json keeps only what the user said, no listing fields
	raw, _ := os.ReadFile(filepath.Join(home, "profiles.json"))
	if bytes.Contains(raw, []byte("available")) || bytes.Contains(raw, []byte("custom")) || !bytes.Contains(raw, []byte(`"later"`)) {
		t.Errorf("profiles.json:\n%s", raw)
	}
}

func TestBadProfilesAreRefused(t *testing.T) {
	ts := configServer(t, t.TempDir())
	for name, body := range map[string]any{
		"Bad%20Name": Profile{Command: "sh"},
		"empty":      Profile{Command: "  "},
		"multi":      Profile{Command: "sh\nrm -rf /"},
		"env":        Profile{Command: "sh", Env: []string{"NOEQUALS"}},
		"other":      Profile{Name: "different", Command: "sh"},
		"notjson":    "{",
	} {
		var e struct{ Error, Reason string }
		if code := configCall(t, ts, "PUT", "/api/profiles/"+name, body, &e); code != 400 || e.Error != "input" {
			t.Errorf("%s: %d %+v", name, code, e)
		}
	}
}
