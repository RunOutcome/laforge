// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"archive/tar"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Collections: official and library are built in and cannot be removed; a
// collection added by link is checked, named from its collection.json, and
// offers its skins (with installed and problem marks); skins install from
// it by name or all at once.
func TestSkinCollections(t *testing.T) {
	tarball := tarGz(t, []tarEntry{
		{name: "fans-abc/", kind: tar.TypeDir},
		{name: "fans-abc/collection.json", body: `{"id":"fans","name":"Fan skins","description":"for fun","skins":[{"name":"neon","label":"Neon Nights"}]}`},
		{name: "fans-abc/neon/skin.json", body: `{"name":"neon","label":"Neon","author":"me"}`},
		{name: "fans-abc/ember/skin.json", body: `{"name":"ember"}`},
		{name: "fans-abc/old/skin.json", body: `{"name":"old","mode":"dark"}`},
	})
	cl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/you/fans/tar.gz/HEAD" {
			w.Write(tarball)
			return
		}
		http.NotFound(w, r)
	}))
	defer cl.Close()
	oc, ot := codeloadBase, githubToken
	codeloadBase, githubToken = cl.URL, func() string { return "" }
	defer func() { codeloadBase, githubToken = oc, ot }()

	srv, ts := newTestServer(t)
	var cs []Collection
	json.NewDecoder(call(t, ts, "GET", "/api/skins/collections", "secret", nil).Body).Decode(&cs)
	if len(cs) != 2 || cs[0].ID != "official" || cs[1].ID != "library" || !cs[0].Builtin {
		t.Fatalf("%+v", cs)
	}
	if r := call(t, ts, "DELETE", "/api/skins/collections/official", "secret", nil); r.StatusCode != 409 {
		t.Fatalf("removed official: %d", r.StatusCode)
	}
	r := call(t, ts, "POST", "/api/skins/collections", "secret", map[string]string{"source": "https://github.com/you/fans"})
	var c Collection
	json.NewDecoder(r.Body).Decode(&c)
	if r.StatusCode != 201 || c.ID != "fans" || c.Name != "Fan skins" || c.Source != "github:you/fans" {
		t.Fatalf("%d %+v", r.StatusCode, c)
	}
	if r := call(t, ts, "POST", "/api/skins/collections", "secret", map[string]string{"source": "you/fans"}); r.StatusCode != 409 {
		t.Fatalf("added twice: %d", r.StatusCode)
	}
	if r := call(t, ts, "POST", "/api/skins/collections", "secret", map[string]string{"source": "you/none"}); r.StatusCode != 400 {
		t.Fatalf("a missing repo: %d", r.StatusCode)
	}
	var offers []Available
	json.NewDecoder(call(t, ts, "GET", "/api/skins/collections/fans/skins", "secret", nil).Body).Decode(&offers)
	byName := map[string]Available{}
	for _, a := range offers {
		byName[a.Name] = a
	}
	if len(offers) != 3 || byName["neon"].Label != "Neon Nights" || byName["neon"].Author != "me" || byName["old"].Problem == "" || byName["ember"].Installed {
		t.Fatalf("%+v", offers)
	}
	var res InstallResult
	json.NewDecoder(call(t, ts, "POST", "/api/skins/install", "secret", map[string]any{"collection": "fans", "names": []string{"neon", "nothere"}}).Body).Decode(&res)
	if len(res.Installed) != 1 || res.Installed[0].Name != "neon" || len(res.Failed) != 1 || res.Failed[0].Path != "nothere" {
		t.Fatalf("by name: %+v", res)
	}
	json.NewDecoder(call(t, ts, "GET", "/api/skins/collections/fans/skins", "secret", nil).Body).Decode(&offers)
	for _, a := range offers {
		if a.Name == "neon" && !a.Installed {
			t.Fatal("neon not marked installed")
		}
	}
	json.NewDecoder(call(t, ts, "POST", "/api/skins/install", "secret", map[string]any{"collection": "fans"}).Body).Decode(&res)
	if len(res.Installed) != 2 || len(res.Failed) != 1 { // neon again, ember; old fails its checks
		t.Fatalf("all: %+v", res)
	}
	if r := call(t, ts, "DELETE", "/api/skins/collections/fans", "secret", nil); r.StatusCode != 204 {
		t.Fatalf("remove: %d", r.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(srv.opts.Home, "skins", "ember", "skin.json")); err != nil {
		t.Fatal("removing a collection removed its skins")
	}
	json.NewDecoder(call(t, ts, "GET", "/api/skins/collections", "secret", nil).Body).Decode(&cs)
	if len(cs) != 2 {
		t.Fatalf("after remove: %+v", cs)
	}
}
