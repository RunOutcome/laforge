// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Scenes: a skin may carry one animated scene, code that draws with a
// library the forge bundles (PixiJS 8). The code lives under the skin's
// scene/ folder and runs only after the user allows it; the allowance is
// pinned to a hash of that code, so changed code is off until allowed
// again. A scene runs in a frame the page sandboxes and the forge serves
// with a sandbox policy too: an opaque origin with no cookies, storage, API
// or network past its own files. It sees pane statuses and positions,
// never content.

// SkinScene is skin.json's "scene".
type SkinScene struct {
	Library string `json:"library"`
	Entry   string `json:"entry"`
	Slot    string `json:"slot,omitempty"`
}

// sceneLibraries are the libraries a scene may name, as "id@major".
var sceneLibraries = map[string]bool{"pixi@8": true}

var sceneCode = map[string]bool{".js": true, ".mjs": true}

//go:embed scenehost/host.js
var sceneHostJS []byte

//go:embed scenehost/pixi-8.22.0.min.mjs
var pixiJS []byte

//go:embed scenehost/pixi-8.22.0-unsafe-eval.min.js
var pixiUnsafeEvalJS []byte

// codeFile records a user's allowance of a skin's code: {hash, allowedAt}.
const codeFile = ".code.json"

// scenesOffFile, when present in the home, turns every scene off.
const scenesOffFile = "scenes-off"

// checkScene checks a manifest's scene against the skin's files.
func checkScene(fsys fs.FS, sc *SkinScene) error {
	if !sceneLibraries[sc.Library] {
		return fmt.Errorf("skin.json: scene.library %q: one LaForge bundles (pixi@8)", sc.Library)
	}
	if !fs.ValidPath(sc.Entry) || !strings.HasPrefix(sc.Entry, "scene/") || !sceneCode[path.Ext(sc.Entry)] {
		return fmt.Errorf("skin.json: scene.entry %q: a .js or .mjs file under scene/", sc.Entry)
	}
	if _, err := fs.Stat(fsys, sc.Entry); err != nil {
		return fmt.Errorf("skin.json: scene.entry %q is not in the skin", sc.Entry)
	}
	switch sc.Slot {
	case "":
		sc.Slot = "background"
	case "background":
	default:
		return fmt.Errorf("skin.json: scene.slot %q: background", sc.Slot)
	}
	return nil
}

// SceneCodeHash is the sha256 of a skin's scene code: every file under
// scene/, by path then content. Empty when there is none.
func SceneCodeHash(fsys fs.FS) string {
	var files []string
	_ = fs.WalkDir(fsys, "scene", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.HasPrefix(d.Name(), ".") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 {
		return ""
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return ""
		}
		fmt.Fprintf(h, "%s\x00%d\x00", f, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

type codeAllowance struct {
	Hash      string    `json:"hash"`
	AllowedAt time.Time `json:"allowedAt"`
}

func readAllowance(dir string) string {
	var a codeAllowance
	raw, err := os.ReadFile(filepath.Join(dir, codeFile))
	if err != nil || json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return a.Hash
}

// AllowSkinCode records that the user allows an installed skin's scene code
// as it is now; DisallowSkinCode takes that back.
func AllowSkinCode(home, name string) (string, error) {
	dir := filepath.Join(home, "skins", name)
	sk, err := ReadSkin(os.DirFS(dir))
	if err != nil {
		return "", err
	}
	if sk.Scene == nil {
		return "", fmt.Errorf("%s has no scene", name)
	}
	hash := SceneCodeHash(os.DirFS(dir))
	raw, _ := json.MarshalIndent(codeAllowance{hash, time.Now().UTC()}, "", "  ")
	return hash, os.WriteFile(filepath.Join(dir, codeFile), append(raw, '\n'), 0o600)
}

func DisallowSkinCode(home, name string) error {
	err := os.Remove(filepath.Join(home, "skins", name, codeFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ScenesOn reports whether scenes may run in this forge at all.
func ScenesOn(home string) bool {
	_, err := os.Stat(filepath.Join(home, scenesOffFile))
	return err != nil
}

func SetScenes(home string, on bool) error {
	p := filepath.Join(home, scenesOffFile)
	if on {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(p, []byte("scenes are off: la skin scenes on\n"), 0o600)
}

// sceneKey is the capability in a scene's URLs: only a client that may list
// skins learns it, and it changes with the code. The frame cannot send the
// forge's cookie, so the key is what lets it load its files.
func (s *Server) sceneKey(name, hash string) string {
	m := hmac.New(sha256.New, []byte(s.opts.Token))
	m.Write([]byte("scene\x00" + name + "\x00" + hash))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// sceneState fills a skin's code fields: its hash, whether it is allowed,
// and the frame's URL when the scene may run.
func (s *Server) sceneState(sk *Skin, fsys fs.FS) {
	if sk.Scene == nil {
		return
	}
	sk.CodeHash = SceneCodeHash(fsys)
	if sk.Builtin {
		sk.CodeAllowed = true
	} else {
		sk.CodeAllowed = sk.CodeHash != "" && readAllowance(filepath.Join(s.skinsDir(), sk.Name)) == sk.CodeHash
	}
	if sk.CodeAllowed && ScenesOn(s.opts.Home) {
		sk.SceneURL = "/scene/" + s.sceneKey(sk.Name, sk.CodeHash) + "/" + sk.Name + "/"
	}
}

// sceneFile is GET /scene/{key}/{name}/{file...}: the frame, its host and
// library, and the skin's own scene files and images. Every response says
// sandbox, so a scene opened by its URL is as shut in as one in the frame.
func (s *Server) sceneFile(w http.ResponseWriter, r *http.Request) {
	name, file := r.PathValue("name"), r.PathValue("file")
	src, ok := s.skins()[name]
	if !ok || src.skin.Scene == nil || !fs.ValidPath(orDot(file)) || strings.Contains("/"+file, "/.") {
		http.NotFound(w, r)
		return
	}
	sk := src.skin
	s.sceneState(&sk, src.fsys)
	if sk.SceneURL == "" || !hmac.Equal([]byte(r.PathValue("key")), []byte(s.sceneKey(name, sk.CodeHash))) {
		http.NotFound(w, r)
		return
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	self := scheme + "://" + r.Host + sk.SceneURL
	h := w.Header()
	h.Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src "+self+
		"; img-src "+self+" data: blob:; connect-src "+self+"; style-src 'unsafe-inline'; font-src "+self+" data:")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Access-Control-Allow-Origin", "*") // the frame's origin is opaque
	h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	h.Set("Referrer-Policy", "no-referrer")
	var data []byte
	ctype := ""
	switch {
	case file == "":
		ctype = "text/html; charset=utf-8"
		data = []byte(`<!doctype html><html data-library="` + html.EscapeString(sk.Scene.Library) + `" data-entry="` +
			html.EscapeString(sk.Scene.Entry) + `"><head><meta charset="utf-8"><title>scene</title>` +
			`<style>html,body{margin:0;height:100%;overflow:hidden;background:transparent}canvas{display:block}</style>` +
			`<script type="module" src="_host.js"></script></head><body></body></html>`)
		h.Set("Cache-Control", "no-store")
	case file == "_host.js":
		ctype, data = "text/javascript; charset=utf-8", sceneHostJS
	case file == "_lib/pixi.mjs":
		ctype, data = "text/javascript; charset=utf-8", pixiJS
		h.Set("Cache-Control", "public, max-age=86400")
	case file == "_lib/pixi-unsafe-eval.js":
		ctype, data = "text/javascript; charset=utf-8", pixiUnsafeEvalJS
		h.Set("Cache-Control", "public, max-age=86400")
	case strings.HasPrefix(file, "scene/") && sceneCode[path.Ext(file)]:
		ctype = "text/javascript; charset=utf-8"
	default:
		ctype = skinFileTypes[strings.ToLower(path.Ext(file))]
		if ctype == "" || strings.HasSuffix(file, ".css") {
			http.NotFound(w, r)
			return
		}
	}
	if data == nil {
		var err error
		if data, err = fs.ReadFile(src.fsys, file); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	if h.Get("Cache-Control") == "" {
		h.Set("Cache-Control", "no-cache")
	}
	h.Set("Content-Type", ctype)
	_, _ = w.Write(data)
}

func orDot(p string) string {
	if p == "" {
		return "."
	}
	return strings.TrimSuffix(p, "/")
}

// skinCode is POST /api/skins/{name}/code {allow}: allow an installed
// skin's scene code as it is now, or take that back.
func (s *Server) skinCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Allow bool `json:"allow"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	name := r.PathValue("name")
	src, ok := s.skins()[name]
	switch {
	case !ok:
		writeError(w, http.StatusNotFound, "no_skin", "There is no skin "+name+".")
		return
	case src.skin.Builtin:
		writeError(w, http.StatusConflict, "builtin", name+" ships with LaForge; its scene is trusted.")
		return
	case src.skin.Scene == nil:
		writeError(w, http.StatusConflict, "no_scene", name+" has no scene.")
		return
	}
	var err error
	if body.Allow {
		_, err = AllowSkinCode(s.opts.Home, name)
	} else {
		err = DisallowSkinCode(s.opts.Home, name)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save", err.Error())
		return
	}
	sk := s.skins()[name]
	s.sceneState(&sk.skin, sk.fsys)
	writeJSON(w, http.StatusOK, sk.skin)
}

// setScenes is PUT /api/skins/scenes {on}: every scene on or off.
func (s *Server) setScenes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	if err := SetScenes(s.opts.Home, body.On); err != nil {
		writeError(w, http.StatusInternalServerError, "save", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"scenes": body.On})
}

// allowInstalled allows the code of every installed skin with a scene in
// res, for an install the user said allowCode to.
func allowInstalled(home string, res *InstallResult) {
	for i, sk := range res.Installed {
		if sk.Scene != nil {
			if hash, err := AllowSkinCode(home, sk.Name); err == nil {
				res.Installed[i].CodeHash, res.Installed[i].CodeAllowed = hash, true
			}
		}
	}
}
