// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func skinDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A skin carries looks only, and says what it is correctly.
func TestASkinIsChecked(t *testing.T) {
	good := `{"name":"neon","label":"Neon","layout":"rail","mode":"dark","css":["skin.css"],"terminal":{"background":"#000000","red":"rgb(255, 0, 0)"}}`
	cases := []struct {
		name  string
		files fstest.MapFS
		want  string
	}{
		{"good", fstest.MapFS{"skin.json": {Data: []byte(good)}, "skin.css": {Data: []byte("a{}")}, "fonts/x.woff2": {}, ".DS_Store": {}}, ""},
		{"no manifest", fstest.MapFS{"skin.css": {}}, "needs a skin.json"},
		{"script", fstest.MapFS{"skin.json": {Data: []byte(good)}, "skin.css": {}, "x.js": {}}, "stylesheets, fonts and images only"},
		{"html", fstest.MapFS{"skin.json": {Data: []byte(good)}, "skin.css": {}, "x.html": {}}, "only"},
		{"bad name", fstest.MapFS{"skin.json": {Data: []byte(`{"name":"../x"}`)}}, "name"},
		{"bad layout", fstest.MapFS{"skin.json": {Data: []byte(`{"name":"x","layout":"grid"}`)}}, "layout"},
		{"missing css", fstest.MapFS{"skin.json": {Data: []byte(`{"name":"x","css":["a.css"]}`)}}, "not in the skin"},
		{"css escapes", fstest.MapFS{"skin.json": {Data: []byte(`{"name":"x","css":["../a.css"]}`)}}, "a .css file"},
		{"bad colour", fstest.MapFS{"skin.json": {Data: []byte(`{"name":"x","terminal":{"red":"url(x)"}}`)}}, "terminal.red"},
		{"unknown key", fstest.MapFS{"skin.json": {Data: []byte(`{"name":"x","script":"a.js"}`)}}, "unknown field"},
	}
	for _, c := range cases {
		sk, err := ReadSkin(c.files)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		case c.want == "" && (sk.Layout != "rail" || sk.Builtin):
			t.Errorf("%s: %+v", c.name, sk)
		}
	}
}

// A skin installs into the home, is listed and served with its files (as
// what they are, never a page), becomes the default, and is removed; a
// built-in cannot be replaced or removed.
func TestASkinInstallsAndIsServed(t *testing.T) {
	srv, ts := newTestServer(t)
	dir := skinDir(t, map[string]string{
		"skin.json": `{"name":"neon","label":"Neon","css":["skin.css"]}`,
		"skin.css":  `@font-face{src:url(fonts/a.woff2)} body{color:#0f0}`, "fonts/a.woff2": "w", "logo.svg": "<svg/>",
	})
	if _, err := InstallSkin(srv.opts.Home, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallSkin(srv.opts.Home, skinDir(t, map[string]string{"skin.json": `{"name":"classic"}`})); err == nil {
		t.Fatal("replaced a built-in")
	}
	var list struct {
		Default string
		Skins   []Skin
	}
	json.NewDecoder(call(t, ts, "GET", "/api/skins", "secret", nil).Body).Decode(&list)
	if list.Default != "classic" || len(list.Skins) < 2 || !list.Skins[0].Builtin || list.Skins[len(list.Skins)-1].Name != "neon" {
		t.Fatalf("%+v", list)
	}
	r := call(t, ts, "GET", "/skins/neon/skin.css", "secret", nil)
	body, _ := io.ReadAll(r.Body)
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/css") || !strings.Contains(string(body), "#0f0") {
		t.Fatalf("css: %d %s %q", r.StatusCode, r.Header.Get("Content-Type"), body)
	}
	if r := call(t, ts, "GET", "/skins/neon/logo.svg", "secret", nil); !strings.Contains(r.Header.Get("Content-Security-Policy"), "sandbox") || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("svg headers: %v", r.Header)
	}
	for _, p := range []string{"/skins/neon/skin.json/../../forge.json", "/skins/neon/nothing.css", "/skins/none/skin.css", "/skins/neon/x.js"} {
		if r := call(t, ts, "GET", p, "secret", nil); r.StatusCode == 200 {
			t.Errorf("%s served", p)
		}
	}
	if r := call(t, ts, "GET", "/skins/neon/skin.css", "wrong", nil); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	call(t, ts, "PUT", "/api/skins/default", "secret", map[string]string{"name": "neon"})
	json.NewDecoder(call(t, ts, "GET", "/api/skins", "secret", nil).Body).Decode(&list)
	if list.Default != "neon" {
		t.Fatalf("default %q", list.Default)
	}
	if r := call(t, ts, "DELETE", "/api/skins/classic", "secret", nil); r.StatusCode != 409 {
		t.Fatalf("removed a built-in: %d", r.StatusCode)
	}
	if r := call(t, ts, "DELETE", "/api/skins/neon", "secret", nil); r.StatusCode != 204 {
		t.Fatalf("remove: %d", r.StatusCode)
	}
	json.NewDecoder(call(t, ts, "GET", "/api/skins", "secret", nil).Body).Decode(&list)
	if list.Default != "classic" {
		t.Fatalf("default after remove %q", list.Default)
	}
}
