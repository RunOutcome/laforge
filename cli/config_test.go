// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/RunOutcome/laforge/forge"
)

// With no forge running, la config reads and writes the home itself, in the
// files a forge reads when it starts.
func TestConfigWithoutAForgeWritesTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LAFORGE_HOME", home)
	dir := t.TempDir()

	if code, out, errs := run(t, "", "config"); code != 0 || !strings.Contains(out, "not running") || !strings.Contains(out, "scenes") || !strings.Contains(out, "PROFILE") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	for _, args := range [][]string{{"config", "set", "cwd", dir}, {"config", "set", "scenes", "off"}, {"config", "set", "skin", "console"}} {
		if code, out, errs := run(t, "", args...); code != 0 {
			t.Fatalf("%v: %d %s %s", args, code, out, errs)
		}
	}
	if _, out, _ := run(t, "", "config", "get", "cwd"); strings.TrimSpace(out) != dir {
		t.Errorf("cwd %q", out)
	}
	if _, out, _ := run(t, "", "config", "get", "scenes"); strings.TrimSpace(out) != "off" {
		t.Errorf("scenes %q", out)
	}
	set, _ := forge.ReadSettings(home)
	if set.Cwd != dir || set.Skin != "console" || set.Scenes {
		t.Fatalf("%+v", set)
	}

	// refusals: an unknown key, a read-only one, a bad value, a missing skin
	for _, args := range [][]string{{"config", "set", "colour", "red"}, {"config", "set", "version", "9"}, {"config", "set", "scenes", "maybe"}, {"config", "set", "skin", "nope"}, {"config", "set", "cwd", filepath.Join(home, "nope")}, {"config", "get", "colour"}} {
		if code, _, errs := run(t, "", args...); code != 1 || errs == "" {
			t.Errorf("%v: %d %q", args, code, errs)
		}
	}
	if code, _, errs := run(t, "", "config", "bogus"); code != 2 || !strings.Contains(errs, "usage:") {
		t.Errorf("bogus: %d %q", code, errs)
	}

	if code, out, _ := run(t, "", "config", "profile", "add", "--label", "Later", "later", "no-such-command-laforge", "--flag"); code != 0 || !strings.Contains(out, "warning") {
		t.Fatalf("%d %s", code, out)
	}
	if _, out, _ := run(t, "", "config", "profile"); !strings.Contains(out, "no-such-command-laforge --flag") || !strings.Contains(out, "yours") {
		t.Errorf("list:\n%s", out)
	}
	if code, _, errs := run(t, "", "config", "profile", "remove", "claude"); code != 1 || !strings.Contains(errs, "built in") {
		t.Errorf("remove built-in: %d %s", code, errs)
	}
	if code, _, _ := run(t, "", "config", "profile", "remove", "later"); code != 0 {
		t.Error("remove later")
	}
	if code, _, _ := run(t, "", "config", "unset", "cwd"); code != 0 || forge.SavedCwd(home) != "" {
		t.Error("unset cwd")
	}
}

// With a forge running, la config goes through it: what it sets the forge
// shows at once.
func TestConfigGoesThroughTheRunningForge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LAFORGE_HOME", home)
	dir := t.TempDir()
	srv, err := forge.NewServer(forge.Options{Home: home, Token: "tok", Cwd: t.TempDir(), Version: "test", Web: fstest.MapFS{"index.html": {Data: []byte("x")}}})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() {
		srv.Shutdown(time.Second)
		ts.Close()
	})
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(ts.URL, "http://"))
	var p int
	json.Unmarshal([]byte(port), &p)
	raw, _ := json.Marshal(State{PID: os.Getpid(), Port: p, URL: ts.URL, Token: "tok"})
	os.WriteFile(filepath.Join(home, "forge.json"), raw, 0o600)

	if code, out, errs := run(t, "", "config", "set", "cwd", dir); code != 0 {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	if code, out, _ := run(t, "", "config"); code != 0 || strings.Contains(out, "not running") || !strings.Contains(out, dir) || !strings.Contains(out, "saved") || !strings.Contains(out, "read-only") {
		t.Fatalf("%d\n%s", code, out)
	}
	if _, out, _ := run(t, "", "config", "get", "port"); strings.TrimSpace(out) != port {
		t.Errorf("port %q, want %s", out, port)
	}
	if _, out, _ := run(t, "", "config", "--json"); !strings.Contains(out, `"cwdSaved": true`) {
		t.Errorf("json:\n%s", out)
	}
	if code, _, errs := run(t, "", "config", "set", "port", "1"); code != 1 || !strings.Contains(errs, "read-only") {
		t.Errorf("port: %d %s", code, errs)
	}
	if code, _, errs := run(t, "", "config", "set", "skin", "nope"); code != 1 || !strings.Contains(errs, "no skin") {
		t.Errorf("skin: %d %s", code, errs)
	}
	exe, _ := os.Executable()
	if code, out, errs := run(t, "", "config", "profile", "add", "--agent", "mine", exe); code != 0 || strings.Contains(out, "warning") {
		t.Fatalf("%d %s %s", code, out, errs)
	}
	if _, out, _ := run(t, "", "config", "get", "profiles"); !strings.Contains(out, "mine\n") {
		t.Errorf("profiles:\n%s", out)
	}
	if code, _, errs := run(t, "", "config", "profile", "remove", "nobody"); code != 1 || !strings.Contains(errs, "no profile") {
		t.Errorf("remove nobody: %d %s", code, errs)
	}
	// the home has it too: a forge started next has it
	if forge.SavedCwd(home) != dir {
		t.Error("cwd not saved in the home")
	}
}
