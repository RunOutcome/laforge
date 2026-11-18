// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const sceneManifest = `{"name":"tide","css":["skin.css"],"scene":{"library":"pixi@8","entry":"scene/main.js"}}`

func TestASceneIsChecked(t *testing.T) {
	ok := fstest.MapFS{"skin.json": {Data: []byte(sceneManifest)}, "skin.css": {}, "scene/main.js": {Data: []byte("export default () => {}")}, "scene/fish.mjs": {}}
	sk, err := ReadSkin(ok)
	if err != nil || sk.Scene == nil || sk.Scene.Slot != "background" {
		t.Fatalf("%+v %v", sk, err)
	}
	cases := []struct{ name, manifest, file, want string }{
		{"library", strings.Replace(sceneManifest, "pixi@8", "three@0", 1), "scene/main.js", "scene.library"},
		{"entry outside", strings.Replace(sceneManifest, "scene/main.js", "main.js", 1), "main.js", "code only under scene/"},
		{"entry missing", sceneManifest, "scene/other.js", "is not in the skin"},
		{"slot", strings.Replace(sceneManifest, `"entry"`, `"slot":"header","entry"`, 1), "scene/main.js", "scene.slot"},
		{"wasm", sceneManifest, "scene/x.wasm", "stylesheets, fonts and images only"},
		{"forge's field", strings.Replace(sceneManifest, `"css"`, `"sceneUrl":"/x","css"`, 1), "scene/main.js", "forge's to set"},
	}
	for _, c := range cases {
		files := fstest.MapFS{"skin.json": {Data: []byte(c.manifest)}, "skin.css": {}, c.file: {}}
		if c.name == "wasm" {
			files["scene/main.js"] = &fstest.MapFile{}
		}
		if _, err := ReadSkin(files); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
	}
	a, b := SceneCodeHash(ok), SceneCodeHash(fstest.MapFS{"scene/main.js": {Data: []byte("export default () => {}")}, "scene/fish.mjs": {Data: []byte("x")}})
	if a == "" || a == b {
		t.Fatalf("hash %q %q", a, b)
	}
}

func TestAScenesCodeRunsOnlyOnceAllowed(t *testing.T) {
	srv, ts := newTestServer(t)
	home := srv.opts.Home
	dir := filepath.Join(t.TempDir(), "tide")
	os.MkdirAll(filepath.Join(dir, "scene"), 0o755)
	os.WriteFile(filepath.Join(dir, "skin.json"), []byte(sceneManifest), 0o644)
	os.WriteFile(filepath.Join(dir, "skin.css"), []byte(":root[data-skin='tide'][data-mode='dark']{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "scene", "main.js"), []byte("export default () => {}"), 0o644)
	os.WriteFile(filepath.Join(dir, "fish.png"), []byte("png"), 0o644)

	tide := func() Skin {
		var l struct {
			Skins  []Skin `json:"skins"`
			Scenes bool   `json:"scenes"`
		}
		json.NewDecoder(call(t, ts, "GET", "/api/skins", "secret", nil).Body).Decode(&l)
		for _, s := range l.Skins {
			if s.Name == "tide" {
				return s
			}
		}
		t.Fatal("no tide")
		return Skin{}
	}
	install := func(allow bool) {
		if r := call(t, ts, "POST", "/api/skins/install", "secret", map[string]any{"source": dir, "allowCode": allow}); r.StatusCode != 200 {
			t.Fatalf("install %d", r.StatusCode)
		}
	}
	get := func(p string) *http.Response {
		r, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// installed without consent: no URL, and a guessed key loads nothing
	install(false)
	sk := tide()
	if sk.CodeAllowed || sk.SceneURL != "" || sk.CodeHash == "" {
		t.Fatalf("unallowed: %+v", sk)
	}
	if r := get("/scene/" + srv.sceneKey("tide", sk.CodeHash) + "/tide/scene/main.js"); r.StatusCode != 404 {
		t.Fatalf("served unallowed code: %d", r.StatusCode)
	}

	// allowed: the frame, host, library and code load without the cookie,
	// sandboxed; a wrong key and the skin's stylesheet do not
	call(t, ts, "POST", "/api/skins/tide/code", "secret", map[string]bool{"allow": true})
	sk = tide()
	if !sk.CodeAllowed || !strings.HasPrefix(sk.SceneURL, "/scene/") {
		t.Fatalf("allowed: %+v", sk)
	}
	for _, f := range []string{"", "_host.js", "_lib/pixi.mjs", "_lib/pixi-unsafe-eval.js", "scene/main.js", "fish.png"} {
		r := get(sk.SceneURL + f)
		csp := r.Header.Get("Content-Security-Policy")
		if r.StatusCode != 200 || !strings.HasPrefix(csp, "sandbox allow-scripts;") || strings.Contains(csp, "unsafe-eval") || r.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%q: %d %q", f, r.StatusCode, csp)
		}
	}
	page, _ := io.ReadAll(get(sk.SceneURL).Body)
	if !strings.Contains(string(page), `data-entry="scene/main.js"`) {
		t.Fatalf("frame: %s", page)
	}
	for _, p := range []string{strings.Replace(sk.SceneURL, "/scene/", "/scene/0", 1), sk.SceneURL + "skin.css", sk.SceneURL + "skin.json/../.code.json", sk.SceneURL + ".code.json"} {
		if r := get(p); r.StatusCode != 404 {
			t.Errorf("%s: %d", p, r.StatusCode)
		}
	}

	// the same code reinstalled keeps its allowance; changed code loses it
	install(false)
	if !tide().CodeAllowed {
		t.Fatal("same code lost its allowance")
	}
	os.WriteFile(filepath.Join(dir, "scene", "main.js"), []byte("export default () => { /* new */ }"), 0o644)
	install(false)
	if sk := tide(); sk.CodeAllowed || sk.SceneURL != "" {
		t.Fatalf("changed code still allowed: %+v", sk)
	}
	if r := get("/scene/" + srv.sceneKey("tide", sk.CodeHash) + "/tide/scene/main.js"); r.StatusCode != 404 {
		t.Fatal("old key still loads")
	}
	install(true)
	if !tide().CodeAllowed {
		t.Fatal("allowCode did not allow")
	}

	// scenes off: no URL even when allowed; back on, it returns
	call(t, ts, "PUT", "/api/skins/scenes", "secret", map[string]bool{"on": false})
	if sk := tide(); sk.SceneURL != "" || !sk.CodeAllowed || ScenesOn(home) {
		t.Fatalf("scenes off: %+v", sk)
	}
	call(t, ts, "PUT", "/api/skins/scenes", "secret", map[string]bool{"on": true})
	call(t, ts, "POST", "/api/skins/tide/code", "secret", map[string]bool{"allow": false})
	if tide().CodeAllowed {
		t.Fatal("disallow did not")
	}
}
