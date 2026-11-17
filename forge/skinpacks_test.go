// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The built-in skins pass the checks under the new manifest.
func TestTheBuiltinSkinsParse(t *testing.T) {
	for _, name := range []string{"classic", "console"} {
		sub, _ := fs.Sub(builtinSkins, "skins/"+name)
		sk, err := ReadSkin(sub)
		if err != nil || sk.Name != name {
			t.Fatalf("%s: %+v %v", name, sk, err)
		}
	}
	// a built-in skin that brings a terminal palette brings one for each mode
	sub, _ := fs.Sub(builtinSkins, "skins/console")
	if sk, _ := ReadSkin(sub); sk.Terminal.Dark["background"] == "" || sk.Terminal.Light["background"] == "" {
		t.Fatalf("console palettes: %+v", sk.Terminal)
	}
}

// Every source form parses to the same parts; bad ones are refused.
func TestSkinSourcesParse(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		in   string
		want SkinSource
	}{
		{"owner/repo", SkinSource{Owner: "owner", Repo: "repo"}},
		{"github:owner/repo", SkinSource{Owner: "owner", Repo: "repo"}},
		{"owner/repo@v2", SkinSource{Owner: "owner", Repo: "repo", Ref: "v2"}},
		{"owner/my.repo@feature/x", SkinSource{Owner: "owner", Repo: "my.repo", Ref: "feature/x"}},
		{"owner/repo//skins/neon", SkinSource{Owner: "owner", Repo: "repo", Subdir: "skins/neon"}},
		{"github:owner/repo@v2//themes", SkinSource{Owner: "owner", Repo: "repo", Ref: "v2", Subdir: "themes"}},
		{"https://github.com/owner/repo", SkinSource{Owner: "owner", Repo: "repo"}},
		{"https://github.com/owner/repo.git/", SkinSource{Owner: "owner", Repo: "repo"}},
		{"https://github.com/owner/repo/tree/main", SkinSource{Owner: "owner", Repo: "repo", Ref: "main"}},
		{"https://github.com/owner/repo/tree/v1.2/skins/neon", SkinSource{Owner: "owner", Repo: "repo", Ref: "v1.2", Subdir: "skins/neon"}},
		{dir, SkinSource{Dir: dir}},
	}
	for _, c := range cases {
		got, err := ParseSkinSource(c.in)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v %v, want %+v", c.in, got, err, c.want)
		}
	}
	// a relative path that starts with . is a folder, made absolute
	if got, err := ParseSkinSource("./pack"); err != nil || got.GitHub() || !filepath.IsAbs(got.Dir) {
		t.Errorf("./pack: %+v %v", got, err)
	}
	// the recorded form reads back
	for _, s := range []SkinSource{{Owner: "o", Repo: "r", Subdir: "skins/a"}, {Owner: "o", Repo: "r"}, {Dir: dir}} {
		if got, err := ParseSkinSource(s.String()); err != nil || !reflect.DeepEqual(got, s) {
			t.Errorf("%s reads back as %+v %v", s, got, err)
		}
	}
	for _, bad := range []string{"", "owner", "owner/", "/x" + "\x00", "owner/repo//../x", "owner/..", "owner/repo@-x", "owner/repo@a..b",
		"https://gitlab.com/owner/repo", "http://github.com/owner/repo", "https://github.com/owner", "https://github.com/owner/repo/blob/main/x",
		"https://evil.com/github.com/owner/repo", "ftp://github.com/owner/repo", "https://user@github.com/owner/repo", "own er/repo"} {
		if got, err := ParseSkinSource(bad); err == nil && got.GitHub() {
			t.Errorf("%q parsed: %+v", bad, got)
		}
	}
}

// A source holds one skin at its root, or many in subfolders or under skins/.
func TestPacksAreFound(t *testing.T) {
	one := skinDir(t, map[string]string{"skin.json": `{"name":"a"}`, "b/skin.json": `{"name":"b"}`})
	if got := FindSkins(one); !reflect.DeepEqual(got, []string{"."}) {
		t.Errorf("one: %v", got)
	}
	many := skinDir(t, map[string]string{"a/skin.json": `{}`, "b/skin.json": `{}`, "docs/readme.md": "", ".git/skin.json": `{}`, "c/d/skin.json": `{}`})
	if got := FindSkins(many); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("many: %v", got)
	}
	under := skinDir(t, map[string]string{"README.md": "", "skins/a/skin.json": `{}`, "skins/b/skin.json": `{}`, "skins/notes.md": ""})
	if got := FindSkins(under); !reflect.DeepEqual(got, []string{"skins/a", "skins/b"}) {
		t.Errorf("skins/: %v", got)
	}
	if got := FindSkins(skinDir(t, map[string]string{"README.md": ""})); len(got) != 0 {
		t.Errorf("none: %v", got)
	}
}

type tarEntry struct {
	name, body, link string
	kind             byte
}

func tarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: 0o644, Size: int64(len(e.body)), Typeflag: e.kind, Linkname: e.link}
		if e.kind == 0 {
			h.Typeflag = tar.TypeReg
		}
		if h.Typeflag != tar.TypeReg {
			h.Size = 0
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			h = &tar.Header{Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "abc123"}}
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte(e.body))
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// The tarball unpacks files and folders only, inside its folder.
func TestTarExtractionIsSafe(t *testing.T) {
	good := tarGz(t, []tarEntry{{name: "pax_global_header", kind: tar.TypeXGlobalHeader}, {name: "repo-abc/", kind: tar.TypeDir}, {name: "repo-abc/skin.json", body: `{}`}})
	dest := t.TempDir()
	if top, err := extractTarGz(bytes.NewReader(good), dest); err != nil || top != "repo-abc" {
		t.Fatalf("good: %q %v", top, err)
	}
	if _, err := os.Stat(filepath.Join(dest, "repo-abc", "skin.json")); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]tarEntry{
		"dotdot":   {{name: "repo/../../escape.css", body: "x"}},
		"absolute": {{name: "/tmp/escape.css", body: "x"}},
		"symlink":  {{name: "repo/link.css", kind: tar.TypeSymlink, link: "/etc/passwd"}},
		"hardlink": {{name: "repo/link.css", kind: tar.TypeLink, link: "repo/a"}},
		"device":   {{name: "repo/dev", kind: tar.TypeChar}},
	}
	for name, entries := range cases {
		parent := t.TempDir()
		dest := filepath.Join(parent, "out")
		os.Mkdir(dest, 0o700)
		if _, err := extractTarGz(bytes.NewReader(tarGz(t, entries)), dest); err == nil {
			t.Errorf("%s: extracted", name)
		}
		if _, err := os.Lstat(filepath.Join(dest, "repo", "link.css")); err == nil {
			t.Errorf("%s: a link was made", name)
		}
		if _, err := os.Stat(filepath.Join(parent, "escape.css")); err == nil {
			t.Errorf("%s: wrote outside", name)
		}
	}
}

// POST /api/skins/install installs every skin in a local pack, reports the
// one that fails, and records each one's source; a bad source is a 400.
func TestTheInstallRouteInstallsAPack(t *testing.T) {
	srv, ts := newTestServer(t)
	pack := skinDir(t, map[string]string{
		"skins/neon/skin.json":  `{"name":"neon","css":["skin.css"],"terminal":{"dark":{"background":"#000"}}}`,
		"skins/neon/skin.css":   `body{}`,
		"skins/ember/skin.json": `{"name":"ember"}`,
		"skins/bad/skin.json":   `{"name":"bad","mode":"dark"}`,
		"skins/evil/skin.json":  `{"name":"evil"}`, "skins/evil/x.js": "alert(1)",
	})
	r := call(t, ts, "POST", "/api/skins/install", "secret", map[string]string{"source": pack})
	var res InstallResult
	json.NewDecoder(r.Body).Decode(&res)
	if r.StatusCode != 200 || len(res.Installed) != 2 || len(res.Failed) != 2 {
		t.Fatalf("%d %+v", r.StatusCode, res)
	}
	if res.Failed[0].Path != "skins/bad" || !strings.Contains(res.Failed[0].Reason, "remove mode") || res.Failed[1].Path != "skins/evil" {
		t.Fatalf("failed: %+v", res.Failed)
	}
	var rec SourceRecord
	raw, _ := os.ReadFile(filepath.Join(srv.opts.Home, "skins", "neon", ".source.json"))
	if json.Unmarshal(raw, &rec) != nil || rec.Source != filepath.Join(pack, "skins", "neon") || rec.InstalledAt.IsZero() {
		t.Fatalf("record: %s", raw)
	}
	var list struct{ Skins []Skin }
	json.NewDecoder(call(t, ts, "GET", "/api/skins", "secret", nil).Body).Decode(&list)
	sources := map[string]string{}
	for _, sk := range list.Skins {
		sources[sk.Name] = sk.Source
	}
	if sources["classic"] != "" || sources["neon"] != rec.Source || sources["ember"] == "" {
		t.Fatalf("sources: %v", sources)
	}
	if r := call(t, ts, "GET", "/skins/neon/.source.json", "secret", nil); r.StatusCode == 200 {
		t.Fatal(".source.json served")
	}
	// update rereads the folder
	os.WriteFile(filepath.Join(pack, "skins", "neon", "skin.json"), []byte(`{"name":"neon","label":"Neon 2","css":["skin.css"]}`), 0o644)
	var sk Skin
	r = call(t, ts, "POST", "/api/skins/neon/update", "secret", nil)
	json.NewDecoder(r.Body).Decode(&sk)
	if r.StatusCode != 200 || sk.Label != "Neon 2" || sk.Source != rec.Source {
		t.Fatalf("update: %d %+v", r.StatusCode, sk)
	}
	if r := call(t, ts, "POST", "/api/skins/classic/update", "secret", nil); r.StatusCode != 409 {
		t.Fatalf("update a built-in: %d", r.StatusCode)
	}
	for _, src := range []string{"", "https://gitlab.com/a/b", filepath.Join(pack, "nothing"), skinDir(t, map[string]string{"README.md": ""})} {
		if r := call(t, ts, "POST", "/api/skins/install", "secret", map[string]string{"source": src}); r.StatusCode != 400 {
			t.Errorf("%q: %d", src, r.StatusCode)
		}
	}
	if r := call(t, ts, "POST", "/api/skins/install", "", map[string]string{"source": pack}); r.StatusCode != 401 {
		t.Fatalf("no token: %d", r.StatusCode)
	}
	req, _ := http.NewRequest("POST", ts.URL+"/api/skins/install", strings.NewReader(`{"source":"`+pack+`"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Origin", "http://127.0.0.1:1")
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 403 {
		t.Fatalf("cross origin: %d", r.StatusCode)
	}
}

// A GitHub source is fetched from codeload, unpacked and installed, at its
// ref and folder; update fetches it again at the same ref.
func TestAGitHubPackInstalls(t *testing.T) {
	tarball := tarGz(t, []tarEntry{
		{name: "pax_global_header", kind: tar.TypeXGlobalHeader},
		{name: "skins-v2/", kind: tar.TypeDir},
		{name: "skins-v2/README.md", body: "# skins"},
		{name: "skins-v2/themes/neon/skin.json", body: `{"name":"neon","css":["skin.css"]}`},
		{name: "skins-v2/themes/neon/skin.css", body: "body{}"},
		{name: "skins-v2/themes/ember/skin.json", body: `{"name":"ember"}`},
	})
	var mu sync.Mutex
	var paths []string
	cl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/you/skins/tar.gz/v2":
			w.Write(tarball)
		case "/you/away/tar.gz/HEAD":
			http.Redirect(w, r, "https://example.com/x.tar.gz", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cl.Close()
	old := codeloadBase
	codeloadBase = cl.URL
	defer func() { codeloadBase = old }()
	oldToken := githubToken
	githubToken = func() string { return "" } // never the real login in tests
	defer func() { githubToken = oldToken }()

	srv, ts := newTestServer(t)
	r := call(t, ts, "POST", "/api/skins/install", "secret", map[string]string{"source": "you/skins@v2//themes"})
	var res InstallResult
	json.NewDecoder(r.Body).Decode(&res)
	if r.StatusCode != 200 || len(res.Installed) != 2 || len(res.Failed) != 0 {
		t.Fatalf("%d %+v", r.StatusCode, res)
	}
	raw, _ := os.ReadFile(filepath.Join(srv.opts.Home, "skins", "neon", ".source.json"))
	var rec SourceRecord
	if json.Unmarshal(raw, &rec) != nil || rec.Source != "github:you/skins//themes/neon" || rec.Ref != "v2" {
		t.Fatalf("record: %s", raw)
	}
	if r := call(t, ts, "GET", "/skins/neon/skin.css", "secret", nil); r.StatusCode != 200 {
		t.Fatalf("css: %d", r.StatusCode)
	}
	if sk, err := UpdateSkin(srv.opts.Home, "neon"); err != nil || sk.Source != rec.Source {
		t.Fatalf("update: %+v %v", sk, err)
	}
	if _, err := InstallSource(t.TempDir(), SkinSource{Owner: "you", Repo: "none"}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing repo: %v", err)
	}
	if _, err := InstallSource(t.TempDir(), SkinSource{Owner: "you", Repo: "away"}); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect off GitHub: %v", err)
	}
	if _, err := InstallSource(t.TempDir(), SkinSource{Owner: "you", Repo: "skins", Ref: "v2", Subdir: "nope"}); err == nil {
		t.Fatal("installed from a missing folder")
	}
	mu.Lock()
	defer mu.Unlock()
	if paths[0] != "/you/skins/tar.gz/v2" {
		t.Fatalf("fetched %v", paths)
	}
}

// A private repository: codeload refuses it anonymously, so the fetch asks
// GitHub's API signed in, which redirects to a short-lived codeload link.
// The token goes only to the API, never to codeload; a public fetch never
// sends it.
func TestAPrivateSkinRepoInstallsSignedIn(t *testing.T) {
	tarball := tarGz(t, []tarEntry{
		{name: "you-private-abc/", kind: tar.TypeDir},
		{name: "you-private-abc/neon/skin.json", body: `{"name":"neon"}`},
		{name: "you-private-abc/ember/skin.json", body: `{"name":"ember"}`},
	})
	var mu sync.Mutex
	var auths []string
	cl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.URL.Path+"="+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path == "/you/private/tar.gz/signed" && r.URL.Query().Get("token") == "short" {
			w.Write(tarball)
			return
		}
		http.NotFound(w, r)
	}))
	defer cl.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, "api"+r.URL.Path+"="+r.Header.Get("Authorization"))
		mu.Unlock()
		if r.URL.Path == "/repos/you/private/tarball" && r.Header.Get("Authorization") == "Bearer tok" {
			http.Redirect(w, r, cl.URL+"/you/private/tar.gz/signed?token=short", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer api.Close()
	oc, oa, ot := codeloadBase, githubAPIBase, githubToken
	codeloadBase, githubAPIBase, githubToken = cl.URL, api.URL, func() string { return "tok" }
	defer func() { codeloadBase, githubAPIBase, githubToken = oc, oa, ot }()

	res, err := InstallSource(t.TempDir(), SkinSource{Owner: "you", Repo: "private"})
	if err != nil || len(res.Installed) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	mu.Lock()
	for _, a := range auths {
		if !strings.HasPrefix(a, "api") && strings.Contains(a, "tok") {
			t.Fatalf("the token reached codeload: %v", auths)
		}
	}
	if len(auths) != 3 || auths[1] != "api/repos/you/private/tarball=Bearer tok" {
		t.Fatalf("requests: %v", auths)
	}
	mu.Unlock()
	githubToken = func() string { return "" }
	if _, err := InstallSource(t.TempDir(), SkinSource{Owner: "you", Repo: "private"}); err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("signed out: %v", err)
	}
}
