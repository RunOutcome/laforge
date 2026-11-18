// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Skins: how the forge looks, installable. A skin is a folder with a
// skin.json and its files: stylesheets, fonts and images, nothing that
// runs. Built-in skins ship in the binary (forge/skins); installed ones live
// in <home>/skins/<name> (la skin install). The forge serves a skin's files
// at /skins/<name>/<file>; the client loads its stylesheet. Its layout only
// suggests where the spaces show; the user's spaces setting decides. The forge's default skin is <home>/skin;
// each browser may choose its own.

//go:embed all:skins
var builtinSkins embed.FS

// Skin is a skin's manifest (skin.json) as the forge serves it.
type Skin struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Author      string `json:"author,omitempty"`
	License     string `json:"license,omitempty"`
	// Layout is where the skin suggests the spaces show, used while the user
	// has not chosen (the spaces setting): "classic" (tabs on the title
	// line) or "rail" (a rail of spaces and agents at the left).
	Layout string `json:"layout,omitempty"`
	// CSS are the skin's stylesheets, in order, relative to its folder.
	CSS []string `json:"css,omitempty"`
	// Terminal is the terminal palette for each mode. Every skin draws both
	// light and dark; a mode left out keeps the client's own palette.
	Terminal SkinTerminal `json:"terminal"`
	// Builtin marks a skin the binary ships (it cannot be removed).
	Builtin bool `json:"builtin"`
	// Source is where an installed skin came from (a folder, or
	// github:owner/repo//subdir), from its .source.json; empty for a
	// built-in or a skin installed without one.
	Source string `json:"source,omitempty"`
	// Scene is an animated scene the skin carries: code under scene/ that
	// draws with a library LaForge bundles, run only once allowed (scenes.go).
	Scene *SkinScene `json:"scene,omitempty"`
	// CodeHash, CodeAllowed and SceneURL are the forge's: the scene code's
	// hash, whether the user allows it as it is, and where its frame loads
	// (empty unless allowed and scenes are on).
	CodeHash    string `json:"codeHash,omitempty"`
	CodeAllowed bool   `json:"codeAllowed,omitempty"`
	SceneURL    string `json:"sceneUrl,omitempty"`
}

// SkinTerminal is a skin's terminal palettes, one per mode (xterm ITheme
// keys: background, foreground, cursor, selectionBackground, black …
// brightWhite).
type SkinTerminal struct {
	Dark  map[string]string `json:"dark,omitempty"`
	Light map[string]string `json:"light,omitempty"`
}

var skinName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// skinFileTypes are what a skin may carry: looks, never code.
var skinFileTypes = map[string]string{
	".css": "text/css; charset=utf-8", ".json": "application/json",
	".woff2": "font/woff2", ".woff": "font/woff", ".ttf": "font/ttf", ".otf": "font/otf",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp",
	".gif": "image/gif", ".svg": "image/svg+xml",
	".md": "text/plain; charset=utf-8", ".txt": "text/plain; charset=utf-8",
}

var terminalKeys = map[string]bool{"background": true, "foreground": true, "cursor": true, "cursorAccent": true,
	"selectionBackground": true, "selectionForeground": true, "black": true, "red": true, "green": true,
	"yellow": true, "blue": true, "magenta": true, "cyan": true, "white": true, "brightBlack": true,
	"brightRed": true, "brightGreen": true, "brightYellow": true, "brightBlue": true, "brightMagenta": true,
	"brightCyan": true, "brightWhite": true}

var cssColor = regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|rgba?\([0-9., %]+\))$`)

// maxSkinBytes bounds a skin folder.
const maxSkinBytes = 16 << 20

// ReadSkin checks a skin folder and returns its manifest. Every file must
// be a type a skin may carry, every stylesheet it names must be there, and
// it must fit maxSkinBytes.
func ReadSkin(fsys fs.FS) (Skin, error) {
	raw, err := fs.ReadFile(fsys, "skin.json")
	if err != nil {
		return Skin{}, errors.New("a skin needs a skin.json")
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return Skin{}, fmt.Errorf("skin.json: %v", err)
	}
	if _, ok := keys["mode"]; ok {
		return Skin{}, errors.New("skin.json: skins draw both light and dark; remove mode")
	}
	for _, k := range []string{"builtin", "source", "codeHash", "codeAllowed", "sceneUrl"} {
		if _, ok := keys[k]; ok {
			return Skin{}, fmt.Errorf("skin.json: %s is the forge's to set; remove it", k)
		}
	}
	if t, ok := keys["terminal"]; ok {
		var modes map[string]json.RawMessage
		if json.Unmarshal(t, &modes) != nil {
			return Skin{}, errors.New(`skin.json: terminal is {"dark": {…}, "light": {…}}`)
		}
		for k := range modes {
			if k != "dark" && k != "light" {
				return Skin{}, fmt.Errorf(`skin.json: terminal.%s: terminal is {"dark": {…}, "light": {…}}, a palette per mode`, k)
			}
		}
	}
	var sk Skin
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sk); err != nil {
		return Skin{}, fmt.Errorf("skin.json: %v", err)
	}
	if !skinName.MatchString(sk.Name) {
		return Skin{}, fmt.Errorf("skin.json: name %q: lower-case letters, digits and dashes, at most 40", sk.Name)
	}
	if sk.Label == "" {
		sk.Label = sk.Name
	}
	switch sk.Layout {
	case "":
		sk.Layout = "classic"
	case "classic", "rail":
	default:
		return Skin{}, fmt.Errorf("skin.json: layout %q: classic or rail", sk.Layout)
	}
	for mode, palette := range map[string]map[string]string{"dark": sk.Terminal.Dark, "light": sk.Terminal.Light} {
		for k, v := range palette {
			if !terminalKeys[k] || !cssColor.MatchString(v) {
				return Skin{}, fmt.Errorf("skin.json: terminal.%s.%s %q: a known key and a #hex or rgb() colour", mode, k, v)
			}
		}
	}
	var total int64
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: a skin may not hold links", p)
		}
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil // .DS_Store and the like are left out
		}
		ext := strings.ToLower(path.Ext(p))
		if sceneCode[ext] {
			if sk.Scene == nil || !strings.HasPrefix(p, "scene/") {
				return fmt.Errorf("%s: code only under scene/, in a skin that declares a scene", p)
			}
		} else if _, ok := skinFileTypes[ext]; !ok {
			return fmt.Errorf("%s: a skin carries stylesheets, fonts and images only (and a scene's code)", p)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return Skin{}, err
	}
	if total > maxSkinBytes {
		return Skin{}, fmt.Errorf("the skin is %d MB; at most %d", total>>20, maxSkinBytes>>20)
	}
	if sk.Scene != nil {
		if err := checkScene(fsys, sk.Scene); err != nil {
			return Skin{}, err
		}
	}
	for _, c := range sk.CSS {
		if !fs.ValidPath(c) || path.Ext(c) != ".css" {
			return Skin{}, fmt.Errorf("skin.json: css %q: a .css file in the skin", c)
		}
		if _, err := fs.Stat(fsys, c); err != nil {
			return Skin{}, fmt.Errorf("skin.json: css %q is not in the skin", c)
		}
	}
	return sk, nil
}

type skinSource struct {
	skin Skin
	fsys fs.FS
}

func (s *Server) skinsDir() string { return filepath.Join(s.opts.Home, "skins") }

// skins are the built-in skins, then the installed ones; an installed skin
// does not replace a built-in of the same name.
func (s *Server) skins() map[string]skinSource {
	out := map[string]skinSource{}
	entries, _ := fs.ReadDir(builtinSkins, "skins")
	for _, e := range entries {
		sub, err := fs.Sub(builtinSkins, "skins/"+e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		if sk, err := ReadSkin(sub); err == nil && sk.Name == e.Name() {
			sk.Builtin = true
			out[sk.Name] = skinSource{sk, sub}
		}
	}
	if s.opts.Home == "" {
		return out
	}
	installed, _ := os.ReadDir(s.skinsDir())
	for _, e := range installed {
		if !e.IsDir() || out[e.Name()].skin.Builtin {
			continue
		}
		sub := os.DirFS(filepath.Join(s.skinsDir(), e.Name()))
		if sk, err := ReadSkin(sub); err == nil && sk.Name == e.Name() {
			if rec, ok := readSourceRecord(filepath.Join(s.skinsDir(), e.Name())); ok {
				sk.Source = rec.Source
			}
			out[sk.Name] = skinSource{sk, sub}
		}
	}
	return out
}

// defaultSkin is the forge's chosen skin, or "classic".
func (s *Server) defaultSkin() string {
	if s.opts.Home != "" {
		if raw, err := os.ReadFile(filepath.Join(s.opts.Home, "skin")); err == nil {
			if name := strings.TrimSpace(string(raw)); skinName.MatchString(name) {
				return name
			}
		}
	}
	return "classic"
}

// listSkins is GET /api/skins: {default, skins}.
func (s *Server) listSkins(w http.ResponseWriter, r *http.Request) {
	all := s.skins()
	list := make([]Skin, 0, len(all))
	for _, src := range all {
		sk := src.skin
		s.sceneState(&sk, src.fsys)
		list = append(list, sk)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Builtin != list[j].Builtin {
			return list[i].Builtin
		}
		return list[i].Name < list[j].Name
	})
	def := s.defaultSkin()
	if _, ok := all[def]; !ok {
		def = "classic"
	}
	writeJSON(w, http.StatusOK, map[string]any{"default": def, "skins": list, "scenes": ScenesOn(s.opts.Home)})
}

// setDefaultSkin is PUT /api/skins/default {name}.
func (s *Server) setDefaultSkin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	if _, ok := s.skins()[body.Name]; !ok {
		writeError(w, http.StatusNotFound, "no_skin", "There is no skin "+body.Name+".")
		return
	}
	if err := os.WriteFile(filepath.Join(s.opts.Home, "skin"), []byte(body.Name+"\n"), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "save", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"default": body.Name})
}

// removeSkin is DELETE /api/skins/{name}: an installed skin only.
func (s *Server) removeSkin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	src, ok := s.skins()[name]
	switch {
	case !ok:
		writeError(w, http.StatusNotFound, "no_skin", "There is no skin "+name+".")
		return
	case src.skin.Builtin:
		writeError(w, http.StatusConflict, "builtin", name+" ships with LaForge and cannot be removed.")
		return
	}
	if err := os.RemoveAll(filepath.Join(s.skinsDir(), name)); err != nil {
		writeError(w, http.StatusInternalServerError, "remove", err.Error())
		return
	}
	if s.defaultSkin() == name {
		_ = os.Remove(filepath.Join(s.opts.Home, "skin"))
	}
	w.WriteHeader(http.StatusNoContent)
}

// skinFile is GET /skins/{name}/{file...}: one of a skin's files, served
// as what it is and never as a page (an SVG cannot run script here).
func (s *Server) skinFile(w http.ResponseWriter, r *http.Request) {
	src, ok := s.skins()[r.PathValue("name")]
	file := r.PathValue("file")
	ctype := skinFileTypes[strings.ToLower(path.Ext(file))]
	if !ok || !fs.ValidPath(file) || ctype == "" || strings.Contains("/"+file, "/.") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(src.fsys, file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; sandbox")
	h.Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

// InstallSkin copies a checked skin folder into home/skins/<name>,
// replacing an installed skin of that name. A built-in name is refused.
// It records the folder as the skin's source.
func InstallSkin(home, dir string) (Skin, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Skin{}, err
	}
	return installSkin(home, abs, &SourceRecord{Source: abs})
}

// installSkin is InstallSkin with the source to record in the skin's
// .source.json (none when rec is nil).
func installSkin(home, dir string, rec *SourceRecord) (Skin, error) {
	sk, err := ReadSkin(os.DirFS(dir))
	if err != nil {
		return Skin{}, err
	}
	if _, err := fs.Stat(builtinSkins, "skins/"+sk.Name); err == nil {
		return Skin{}, fmt.Errorf("%s is a built-in skin; give yours another name", sk.Name)
	}
	root := filepath.Join(home, "skins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Skin{}, err
	}
	tmp, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Skin{}, err
	}
	defer os.RemoveAll(tmp)
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if strings.HasPrefix(d.Name(), ".") && rel != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(tmp, rel), 0o700)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tmp, rel), data, 0o600)
	})
	if err != nil {
		return Skin{}, err
	}
	if rec != nil {
		r := *rec
		r.InstalledAt = time.Now().UTC()
		raw, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(filepath.Join(tmp, sourceFile), append(raw, '\n'), 0o600); err != nil {
			return Skin{}, err
		}
		sk.Source = r.Source
	}
	dest := filepath.Join(root, sk.Name)
	if sk.Scene != nil {
		// an allowance carries over only while the code is the same
		sk.CodeHash = SceneCodeHash(os.DirFS(tmp))
		if prev := readAllowance(dest); prev != "" && prev == sk.CodeHash {
			if raw, err := os.ReadFile(filepath.Join(dest, codeFile)); err == nil {
				_ = os.WriteFile(filepath.Join(tmp, codeFile), raw, 0o600)
				sk.CodeAllowed = true
			}
		}
	}
	old := dest + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return Skin{}, err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Rename(old, dest)
		return Skin{}, err
	}
	_ = os.RemoveAll(old)
	return sk, nil
}
