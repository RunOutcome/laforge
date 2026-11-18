// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

// The forge's settings, one model for the API (GET/PUT /api/config), la
// config and the Settings panel. Each setting keeps the file it always had,
// so older clients and la skin keep working:
//
//	cwd       <home>/config.json  {"cwd": ...}
//	skin      <home>/skin
//	scenes    <home>/scenes-off   (present: off)
//	profiles  <home>/profiles.json (the user's own, over the built-ins)

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const configFile = "config.json"

// SettingKeys are the settings a client may change, in the order la config
// lists them.
var SettingKeys = []string{"cwd", "skin", "scenes"}

// ReadOnlyKeys are what GET /api/config also says about the forge; no client
// changes them.
var ReadOnlyKeys = []string{"version", "host", "user", "port", "home"}

// savedConfig is <home>/config.json.
type savedConfig struct {
	Cwd string `json:"cwd,omitempty"`
}

func readSaved(home string) savedConfig {
	var c savedConfig
	if raw, err := os.ReadFile(filepath.Join(home, configFile)); err == nil {
		_ = json.Unmarshal(raw, &c)
	}
	return c
}

func writeSaved(home string, c savedConfig) error {
	raw, _ := json.MarshalIndent(c, "", "  ")
	return writeFileAtomic(filepath.Join(home, configFile), append(raw, '\n'))
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SavedCwd is the new-pane folder saved in the home, or "" when none is (new
// panes then start where la up ran), or when it is no longer a folder.
func SavedCwd(home string) string {
	dir := readSaved(home).Cwd
	if dir == "" {
		return ""
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// Settings are the forge's settings, as GET /api/config shows them.
type Settings struct {
	// Cwd is the folder new panes start in; CwdSaved says it is the saved
	// one, not only where la up ran.
	Cwd      string    `json:"cwd"`
	CwdSaved bool      `json:"cwdSaved"`
	Skin     string    `json:"skin"`
	Scenes   bool      `json:"scenes"`
	Profiles []Profile `json:"profiles"`
}

// ReadSettings are the settings saved in a home, for la config when no forge
// runs. Cwd is the saved folder only.
func ReadSettings(home string) (Settings, error) {
	profiles, err := ListProfiles(home)
	if err != nil {
		return Settings{}, err
	}
	cwd := SavedCwd(home)
	return Settings{Cwd: cwd, CwdSaved: cwd != "", Skin: homeServer(home).currentSkin(), Scenes: ScenesOn(home), Profiles: profiles}, nil
}

// homeServer is a server of a home only, for what reads the home's skins.
func homeServer(home string) *Server { return &Server{opts: Options{Home: home}} }

func (s *Server) currentSkin() string {
	def := s.defaultSkin()
	if _, ok := s.skins()[def]; !ok {
		def = "classic"
	}
	return def
}

// ConfigPatch is a partial change to the settings: each field left out is
// kept.
type ConfigPatch struct {
	Cwd    *string `json:"cwd,omitempty"`
	Skin   *string `json:"skin,omitempty"`
	Scenes *bool   `json:"scenes,omitempty"`
	// Save false makes Cwd this run's only (la up in a folder); a saved
	// folder still wins over it. Absent or true saves it.
	Save *bool `json:"save,omitempty"`
}

// ConfigError is a refusal of a patch: Code is the API's error code.
type ConfigError struct{ Code, Reason string }

func (e *ConfigError) Error() string { return e.Reason }

func refuse(code, format string, a ...any) error {
	return &ConfigError{Code: code, Reason: fmt.Sprintf(format, a...)}
}

// ParseConfigPatch reads a PUT /api/config body, refusing a key it does not
// know, a read-only key or a value of the wrong type.
func ParseConfigPatch(raw []byte) (ConfigPatch, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return ConfigPatch{}, refuse("input", "The body is not a JSON object.")
	}
	var p ConfigPatch
	for k, v := range keys {
		var err error
		switch k {
		case "cwd":
			err = json.Unmarshal(v, &p.Cwd)
		case "skin":
			err = json.Unmarshal(v, &p.Skin)
		case "scenes":
			err = json.Unmarshal(v, &p.Scenes)
		case "save":
			err = json.Unmarshal(v, &p.Save)
		case "profiles":
			return p, refuse("input", "Profiles change one at a time: PUT or DELETE /api/profiles/{name}.")
		case "cwdSaved", "version", "host", "user", "port", "home", "readOnly":
			return p, refuse("read_only", "%s is read-only.", k)
		default:
			return p, refuse("unknown_key", "There is no setting %q; the settings are %s.", k, strings.Join(SettingKeys, ", "))
		}
		if err != nil || string(v) == "null" {
			return p, refuse("input", "%s has the wrong type.", k)
		}
	}
	return p, nil
}

// checkPatch validates a patch against a home and returns it normalised:
// cwd absolute and expanded, without changing anything.
func checkPatch(home string, p ConfigPatch) (ConfigPatch, error) {
	if p.Cwd != nil {
		dir := expandHome(strings.TrimSpace(*p.Cwd))
		if dir != "" {
			abs, err := filepath.Abs(dir)
			if err != nil {
				return p, refuse("input", "%s is not a folder.", dir)
			}
			if info, err := os.Stat(abs); err != nil || !info.IsDir() {
				return p, refuse("input", "%s is not a folder.", abs)
			}
			dir = abs
		}
		p.Cwd = &dir
	}
	if p.Skin != nil {
		if _, ok := homeServer(home).skins()[*p.Skin]; !ok {
			return p, refuse("no_skin", "There is no skin %s.", *p.Skin)
		}
	}
	return p, nil
}

// ApplyConfig validates the whole patch, then saves each setting it names to
// its file in the home; a refused patch changes nothing. It is what la config
// set does when no forge runs; a forge does it through PUT /api/config.
func ApplyConfig(home string, p ConfigPatch) error {
	p, err := checkPatch(home, p)
	if err != nil {
		return err
	}
	if p.Cwd != nil && (p.Save == nil || *p.Save) {
		c := readSaved(home)
		c.Cwd = *p.Cwd
		if err := writeSaved(home, c); err != nil {
			return err
		}
	}
	if p.Skin != nil {
		if err := os.WriteFile(filepath.Join(home, "skin"), []byte(*p.Skin+"\n"), 0o600); err != nil {
			return err
		}
	}
	if p.Scenes != nil {
		if err := SetScenes(home, *p.Scenes); err != nil {
			return err
		}
	}
	return nil
}

// ForgeConfig is GET /api/config: the settings, and what the forge is.
type ForgeConfig struct {
	Settings
	Version  string   `json:"version"`
	Host     string   `json:"host"`
	User     string   `json:"user"`
	Port     int      `json:"port,omitempty"`
	Home     string   `json:"home"`
	ReadOnly []string `json:"readOnly"`
}

func (s *Server) config(r *http.Request) (ForgeConfig, error) {
	profiles, err := s.profiles()
	if err != nil {
		return ForgeConfig{}, err
	}
	host, _ := os.Hostname()
	port := s.opts.Port
	if port == 0 {
		if _, p, err := net.SplitHostPort(r.Host); err == nil {
			port, _ = strconv.Atoi(p)
		}
	}
	saved := SavedCwd(s.opts.Home)
	return ForgeConfig{
		Settings: Settings{Cwd: s.defaultCwd(), CwdSaved: saved != "", Skin: s.currentSkin(), Scenes: ScenesOn(s.opts.Home), Profiles: profiles},
		Version:  s.opts.Version, Host: host, User: os.Getenv("USER"), Port: port, Home: s.opts.Home, ReadOnly: ReadOnlyKeys,
	}, nil
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.config(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "profiles", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// putConfig is PUT /api/config: a partial change; each key left out is kept.
func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<12))
	if err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body could not be read.")
		return
	}
	p, err := ParseConfigPatch(raw)
	if err == nil {
		p, err = checkPatch(s.opts.Home, p)
	}
	if err == nil {
		s.mu.Lock()
		err = ApplyConfig(s.opts.Home, p)
		if err == nil && p.Cwd != nil && p.Save != nil && !*p.Save && *p.Cwd != "" {
			s.cwd = *p.Cwd
		}
		s.mu.Unlock()
	}
	var refused *ConfigError
	if errors.As(err, &refused) {
		status := http.StatusBadRequest
		if refused.Code == "no_skin" {
			status = http.StatusNotFound
		}
		writeError(w, status, refused.Code, refused.Reason)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save", err.Error())
		return
	}
	s.getConfig(w, r)
}

// ---- profiles ----

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

// ErrBuiltinProfile refuses removing a built-in profile nobody changed.
var ErrBuiltinProfile = errors.New("a built-in profile cannot be removed; change it instead")

// ErrNoProfile is a profile name the forge does not have.
var ErrNoProfile = errors.New("there is no such profile")

// profileEntry is one profile as profiles.json keeps it.
type profileEntry struct {
	Name        string   `json:"name"`
	Label       string   `json:"label,omitempty"`
	Description string   `json:"description,omitempty"`
	Command     string   `json:"command"`
	Args        []string `json:"args,omitempty"`
	Env         []string `json:"env,omitempty"`
	Agent       bool     `json:"agent,omitempty"`
	Resume      []string `json:"resume,omitempty"`
}

func readEntries(home string) ([]profileEntry, error) {
	raw, err := os.ReadFile(filepath.Join(home, "profiles.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []profileEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("profiles.json: %v", err)
	}
	return out, nil
}

func writeEntries(home string, entries []profileEntry) error {
	if entries == nil {
		entries = []profileEntry{}
	}
	raw, _ := json.MarshalIndent(entries, "", "  ")
	return writeFileAtomic(filepath.Join(home, "profiles.json"), append(raw, '\n'))
}

func isBuiltinProfile(name string) bool {
	for _, b := range BuiltinProfiles() {
		if b.Name == name {
			return true
		}
	}
	return false
}

// ListProfiles is every profile with its availability, each marked built in
// and/or custom (saved in profiles.json).
func ListProfiles(home string) ([]Profile, error) {
	p, err := loadProfiles(home)
	if err != nil {
		return nil, err
	}
	entries, _ := readEntries(home)
	custom := map[string]bool{}
	for _, e := range entries {
		custom[e.Name] = true
	}
	out := withAvailability(p)
	for i := range out {
		out[i].Builtin = isBuiltinProfile(out[i].Name)
		out[i].Custom = custom[out[i].Name]
	}
	return out, nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// CheckProfile refuses a profile the forge could not start; a command that
// is not installed is allowed (Available says so).
func CheckProfile(p Profile) error {
	if !profileName.MatchString(p.Name) {
		return refuse("input", "A profile's name is lowercase letters, digits, '.', '_' or '-', up to 40, starting with a letter or digit: %q is not.", p.Name)
	}
	if strings.TrimSpace(p.Command) == "" {
		return refuse("input", "A profile needs a command.")
	}
	if hasControl(p.Command) || hasControl(p.Label) || hasControl(p.Description) {
		return refuse("input", "A profile's command, label and description are one line each.")
	}
	if len(p.Label) > 80 || len(p.Description) > 200 || len(p.Command) > 1024 {
		return refuse("input", "That profile is too long.")
	}
	for _, e := range p.Env {
		if k, _, ok := strings.Cut(e, "="); !ok || k == "" || hasControl(e) {
			return refuse("input", "Each env entry is NAME=value: %q is not.", e)
		}
	}
	for _, a := range append(append([]string{}, p.Args...), p.Resume...) {
		if strings.ContainsAny(a, "\x00") {
			return refuse("input", "An argument holds a NUL.")
		}
	}
	return nil
}

// SaveProfile adds a profile to the home, or replaces the one of its name
// (a built-in included). It returns the profile as listed, with Available
// false and a Reason when its command is not installed.
func SaveProfile(home string, p Profile) (Profile, error) {
	p.Command = strings.TrimSpace(p.Command)
	p.Label = strings.TrimSpace(p.Label)
	if err := CheckProfile(p); err != nil {
		return Profile{}, err
	}
	if p.Label == "" {
		p.Label = p.Name
	}
	entries, err := readEntries(home)
	if err != nil {
		return Profile{}, err
	}
	e := profileEntry{Name: p.Name, Label: p.Label, Description: p.Description, Command: p.Command, Args: p.Args, Env: p.Env, Agent: p.Agent, Resume: p.Resume}
	replaced := false
	for i := range entries {
		if entries[i].Name == p.Name {
			entries[i], replaced = e, true
		}
	}
	if !replaced {
		entries = append(entries, e)
	}
	if err := writeEntries(home, entries); err != nil {
		return Profile{}, err
	}
	return findProfile(home, p.Name)
}

// RemoveProfile removes a profile of the user's: one added goes, one that
// changed a built-in goes back to the built-in.
func RemoveProfile(home, name string) error {
	entries, err := readEntries(home)
	if err != nil {
		return err
	}
	kept := entries[:0]
	found := false
	for _, e := range entries {
		if e.Name == name {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		if isBuiltinProfile(name) {
			return ErrBuiltinProfile
		}
		return ErrNoProfile
	}
	return writeEntries(home, kept)
}

func findProfile(home, name string) (Profile, error) {
	all, err := ListProfiles(home)
	if err != nil {
		return Profile{}, err
	}
	for _, p := range all {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, ErrNoProfile
}

// putProfile is PUT /api/profiles/{name}: add or change one profile.
func (s *Server) putProfile(w http.ResponseWriter, r *http.Request) {
	var p Profile
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not a profile.")
		return
	}
	name := r.PathValue("name")
	if p.Name != "" && p.Name != name {
		writeError(w, http.StatusBadRequest, "input", "The body names another profile than the path.")
		return
	}
	p.Name = name
	s.mu.Lock()
	saved, err := SaveProfile(s.opts.Home, p)
	s.mu.Unlock()
	var refused *ConfigError
	if errors.As(err, &refused) {
		writeError(w, http.StatusBadRequest, refused.Code, refused.Reason)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "save", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// deleteProfile is DELETE /api/profiles/{name}.
func (s *Server) deleteProfile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.mu.Lock()
	err := RemoveProfile(s.opts.Home, name)
	s.mu.Unlock()
	switch {
	case errors.Is(err, ErrBuiltinProfile):
		writeError(w, http.StatusConflict, "builtin", name+" is built in; it cannot be removed, only changed.")
	case errors.Is(err, ErrNoProfile):
		writeError(w, http.StatusNotFound, "no_profile", "There is no profile "+name+" of yours.")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "save", err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
