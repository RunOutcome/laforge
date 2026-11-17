// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Collections: places to get skins. A collection is a skin source (a GitHub
// repository, or a folder) holding one skin or many, with an optional
// collection.json naming them. Two are built in: "official", the skins
// released with LaForge (its repository's skins/ folder), and "library",
// more skins (RunOutcome/laforge-skins). Anyone can add others by link;
// they are kept in <home>/collections.json.

// Collection is a place to get skins.
type Collection struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	Description string    `json:"description,omitempty"`
	Builtin     bool      `json:"builtin"`
	AddedAt     time.Time `json:"addedAt,omitzero"`
}

// Available is a skin a collection offers.
type Available struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Author      string `json:"author,omitempty"`
	Layout      string `json:"layout,omitempty"`
	Path        string `json:"path"`
	Installed   bool   `json:"installed"`
	// Problem is why the skin would not install (it fails the checks).
	Problem string `json:"problem,omitempty"`
}

// builtinCollections are always offered and cannot be removed.
var builtinCollections = []Collection{
	{ID: "official", Name: "LaForge official", Source: "github:RunOutcome/laforge//skins", Builtin: true,
		Description: "The skins released with LaForge."},
	{ID: "library", Name: "LaForge library", Source: "github:RunOutcome/laforge-skins", Builtin: true,
		Description: "More skins from the LaForge community."},
}

// collectionIndex is a collection's optional collection.json.
type collectionIndex struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Skins       []struct {
		Name, Label, Description, Path string
	} `json:"skins"`
}

// collectionCache keeps what a collection offers for a while, so browsing
// does not download it each time.
type collectionCache struct {
	mu   sync.Mutex
	list map[string]cachedOffer
}

type cachedOffer struct {
	at     time.Time
	index  collectionIndex
	offers []Available
}

const collectionFresh = 10 * time.Minute

var collectionSlug = regexp.MustCompile(`[^a-z0-9]+`)

func (s *Server) collectionsPath() string { return filepath.Join(s.opts.Home, "collections.json") }

func (s *Server) userCollections() []Collection {
	var out []Collection
	if raw, err := os.ReadFile(s.collectionsPath()); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// collections are the built-in collections, then the user's.
func (s *Server) collections() []Collection {
	return append(append([]Collection{}, builtinCollections...), s.userCollections()...)
}

func (s *Server) collection(id string) (Collection, bool) {
	for _, c := range s.collections() {
		if c.ID == id {
			return c, true
		}
	}
	return Collection{}, false
}

// offers is what a collection holds, from the cache unless refresh.
func (s *Server) offers(c Collection, refresh bool) (collectionIndex, []Available, error) {
	s.collCache.mu.Lock()
	if hit, ok := s.collCache.list[c.ID]; ok && !refresh && time.Since(hit.at) < collectionFresh {
		s.collCache.mu.Unlock()
		return hit.index, s.markInstalled(hit.offers), nil
	}
	s.collCache.mu.Unlock()
	src, err := ParseSkinSource(c.Source)
	if err != nil {
		return collectionIndex{}, nil, err
	}
	root, cleanup, err := fetchSource(src)
	if err != nil {
		return collectionIndex{}, nil, err
	}
	defer cleanup()
	var idx collectionIndex
	if raw, err := os.ReadFile(filepath.Join(root, "collection.json")); err == nil {
		if err := json.Unmarshal(raw, &idx); err != nil {
			return collectionIndex{}, nil, fmt.Errorf("collection.json: %v", err)
		}
	}
	described := map[string]int{}
	for i, e := range idx.Skins {
		described[e.Name] = i
	}
	offers := []Available{}
	for _, rel := range FindSkins(root) {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		a := Available{Name: skinNameAt(dir, rel), Path: rel}
		if sk, err := ReadSkin(os.DirFS(dir)); err == nil {
			a.Label, a.Description, a.Author, a.Layout = sk.Label, sk.Description, sk.Author, sk.Layout
		} else {
			a.Problem = err.Error()
			a.Label = a.Name
		}
		if i, ok := described[a.Name]; ok {
			if idx.Skins[i].Label != "" {
				a.Label = idx.Skins[i].Label
			}
			if idx.Skins[i].Description != "" {
				a.Description = idx.Skins[i].Description
			}
		}
		offers = append(offers, a)
	}
	s.collCache.mu.Lock()
	if s.collCache.list == nil {
		s.collCache.list = map[string]cachedOffer{}
	}
	s.collCache.list[c.ID] = cachedOffer{at: time.Now(), index: idx, offers: offers}
	s.collCache.mu.Unlock()
	return idx, s.markInstalled(offers), nil
}

func (s *Server) markInstalled(in []Available) []Available {
	have := s.skins()
	out := make([]Available, len(in))
	for i, a := range in {
		_, a.Installed = have[a.Name]
		out[i] = a
	}
	return out
}

// listCollections is GET /api/skins/collections.
func (s *Server) listCollections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.collections())
}

// addCollection is POST /api/skins/collections {source}: check the source
// holds skins, then keep it.
func (s *Server) addCollection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	src, err := ParseSkinSource(strings.TrimSpace(body.Source))
	if err != nil {
		writeError(w, http.StatusBadRequest, "source", err.Error())
		return
	}
	for _, c := range s.collections() {
		if c.Source == src.String() {
			writeError(w, http.StatusConflict, "exists", "That collection is already here, as "+c.ID+".")
			return
		}
	}
	c := Collection{Source: src.String(), AddedAt: time.Now().UTC()}
	if src.GitHub() {
		c.Name = src.Owner + "/" + src.Repo
	} else {
		c.Name = filepath.Base(src.Dir)
	}
	c.ID = "pending-" + NewToken()[:8] // offers caches by id; renamed below
	idx, offers, err := s.offers(c, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "source", err.Error())
		return
	}
	if len(offers) == 0 {
		writeError(w, http.StatusBadRequest, "source", src.String()+" holds no skin: no skin.json at its root, in a subfolder or under skins/.")
		return
	}
	if idx.Name != "" {
		c.Name = idx.Name
	}
	c.Description = idx.Description
	base := idx.ID
	if base == "" {
		base = path.Base(strings.TrimSuffix(c.Name, "/"))
	}
	base = strings.Trim(collectionSlug.ReplaceAllString(strings.ToLower(base), "-"), "-")
	if base == "" {
		base = "collection"
	}
	c.ID = base
	for n := 2; ; n++ {
		if _, taken := s.collection(c.ID); !taken {
			break
		}
		c.ID = fmt.Sprintf("%s-%d", base, n)
	}
	s.collCache.mu.Lock()
	for k, v := range s.collCache.list {
		if strings.HasPrefix(k, "pending-") {
			delete(s.collCache.list, k)
			s.collCache.list[c.ID] = v
		}
	}
	s.collCache.mu.Unlock()
	list := append(s.userCollections(), c)
	raw, _ := json.MarshalIndent(list, "", "  ")
	if err := os.WriteFile(s.collectionsPath(), raw, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "save", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// removeCollection is DELETE /api/skins/collections/{id}: the user's own
// only. Skins installed from it stay.
func (s *Server) removeCollection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, ok := s.collection(id)
	switch {
	case !ok:
		writeError(w, http.StatusNotFound, "no_collection", "There is no collection "+id+".")
		return
	case c.Builtin:
		writeError(w, http.StatusConflict, "builtin", id+" comes with LaForge and cannot be removed.")
		return
	}
	var keep []Collection
	for _, u := range s.userCollections() {
		if u.ID != id {
			keep = append(keep, u)
		}
	}
	raw, _ := json.MarshalIndent(keep, "", "  ")
	if err := os.WriteFile(s.collectionsPath(), raw, 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "save", err.Error())
		return
	}
	s.collCache.mu.Lock()
	delete(s.collCache.list, id)
	s.collCache.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// collectionSkins is GET /api/skins/collections/{id}/skins[?refresh=1].
func (s *Server) collectionSkins(w http.ResponseWriter, r *http.Request) {
	c, ok := s.collection(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no_collection", "There is no collection "+r.PathValue("id")+".")
		return
	}
	_, offers, err := s.offers(c, r.URL.Query().Get("refresh") == "1")
	if err != nil {
		writeError(w, http.StatusBadGateway, "source", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, offers)
}

// installFromCollection installs names (all when empty) from a collection.
func (s *Server) installFromCollection(id string, names []string) (InstallResult, error) {
	c, ok := s.collection(id)
	if !ok {
		return InstallResult{}, errors.New("there is no collection " + id)
	}
	src, err := ParseSkinSource(c.Source)
	if err != nil {
		return InstallResult{}, err
	}
	res, err := InstallSkinsFrom(s.opts.Home, src, names)
	if err == nil {
		s.collCache.mu.Lock()
		delete(s.collCache.list, id) // what is installed changed
		s.collCache.mu.Unlock()
	}
	return res, err
}
