// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Theme packs: a skin source is a folder or a GitHub repository holding one
// skin (skin.json at its root) or many (each immediate subfolder with a
// skin.json, or the same under skins/). Installing a source installs every
// skin found in it; one bad skin does not stop the others.

// codeloadBase is where GitHub serves repository tarballs. Tests point it
// at a local server; nothing else may.
var codeloadBase = "https://codeload.github.com"

// githubAPIBase serves a private repository's tarball to a signed-in
// request (it redirects to codeload with a short-lived link). Tests point it
// at a local server.
var githubAPIBase = "https://api.github.com"

// githubToken is the user's GitHub token for private skin repositories:
// $GH_TOKEN, $GITHUB_TOKEN, else the GitHub CLI's login. It is sent only to
// GitHub, and only after an anonymous fetch was refused.
var githubToken = func() string {
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

const (
	maxPackDownload = 32 << 20  // the tarball as fetched
	maxPackExtract  = 128 << 20 // what it unpacks to
	packFetchTime   = 2 * time.Minute
	sourceFile      = ".source.json"
)

// SourceRecord is an installed skin's <home>/skins/<name>/.source.json: what
// it was installed from, so it can be updated.
type SourceRecord struct {
	Source      string    `json:"source"`
	Ref         string    `json:"ref,omitempty"`
	InstalledAt time.Time `json:"installedAt"`
}

func readSourceRecord(dir string) (SourceRecord, bool) {
	var rec SourceRecord
	raw, err := os.ReadFile(filepath.Join(dir, sourceFile))
	if err != nil || json.Unmarshal(raw, &rec) != nil || rec.Source == "" {
		return SourceRecord{}, false
	}
	return rec, true
}

// SkinSource is a parsed install source: a local folder, or a GitHub
// repository at a ref with an optional subfolder.
type SkinSource struct {
	Dir    string // a local folder (absolute), when not GitHub
	Owner  string
	Repo   string
	Ref    string // "" means HEAD
	Subdir string // slash-separated, relative, "" for the root
}

// GitHub reports a GitHub source.
func (s SkinSource) GitHub() bool { return s.Dir == "" }

// String is the source as ParseSkinSource reads it back: the folder, or
// github:owner/repo[//subdir] (the ref is recorded on its own).
func (s SkinSource) String() string {
	if !s.GitHub() {
		return s.Dir
	}
	out := "github:" + s.Owner + "/" + s.Repo
	if s.Subdir != "" {
		out += "//" + s.Subdir
	}
	return out
}

func (s SkinSource) ref() string {
	if s.Ref == "" {
		return "HEAD"
	}
	return s.Ref
}

// at is the source of a skin found at rel inside this one.
func (s SkinSource) at(rel string) SkinSource {
	if rel == "." || rel == "" {
		return s
	}
	if !s.GitHub() {
		s.Dir = filepath.Join(s.Dir, filepath.FromSlash(rel))
		return s
	}
	s.Subdir = path.Join(s.Subdir, rel)
	return s
}

var (
	ghOwner = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	ghRepo  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	ghRef   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
)

// ParseSkinSource reads a source: a local folder path; owner/repo,
// github:owner/repo or owner/repo@ref, each with an optional //subdir; or
// https://github.com/owner/repo[/tree/<ref>[/<subdir>]]. A path that names
// a folder on disk, or starts with /, ., ~ or a drive, is a folder.
func ParseSkinSource(src string) (SkinSource, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return SkinSource{}, errors.New("name a skin source: a folder, owner/repo or a github.com link")
	}
	if strings.Contains(src, "://") {
		return parseGitHubURL(src)
	}
	if isLocalPath(src) {
		dir := src
		if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
			home, err := os.UserHomeDir()
			if err != nil {
				return SkinSource{}, err
			}
			dir = filepath.Join(home, dir[1:])
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return SkinSource{}, err
		}
		return SkinSource{Dir: abs}, nil
	}
	short := strings.TrimPrefix(src, "github:")
	var s SkinSource
	if i := strings.Index(short, "//"); i >= 0 {
		sub, err := cleanSubdir(short[i+2:])
		if err != nil {
			return SkinSource{}, err
		}
		s.Subdir, short = sub, short[:i]
	}
	if i := strings.Index(short, "@"); i >= 0 {
		s.Ref, short = short[i+1:], short[:i]
		if !validRef(s.Ref) {
			return SkinSource{}, fmt.Errorf("%s: ref %q: a branch, tag or commit", src, s.Ref)
		}
	}
	owner, repo, ok := strings.Cut(short, "/")
	if !ok || !ghOwner.MatchString(owner) || !validRepo(repo) {
		return SkinSource{}, fmt.Errorf("%s is not a folder or owner/repo", src)
	}
	s.Owner, s.Repo = owner, repo
	return s, nil
}

func isLocalPath(src string) bool {
	if filepath.IsAbs(src) || strings.HasPrefix(src, ".") || strings.HasPrefix(src, "~") || strings.HasPrefix(src, "/") ||
		strings.HasPrefix(src, `\`) || filepath.VolumeName(src) != "" {
		return true
	}
	if strings.HasPrefix(src, "github:") {
		return false
	}
	info, err := os.Stat(src)
	return err == nil && info.IsDir()
}

func parseGitHubURL(src string) (SkinSource, error) {
	u, err := url.Parse(src)
	if err != nil {
		return SkinSource{}, fmt.Errorf("%s: %v", src, err)
	}
	host := strings.ToLower(u.Host)
	if u.Scheme != "https" || (host != "github.com" && host != "www.github.com") || u.User != nil {
		return SkinSource{}, fmt.Errorf("%s: only https://github.com links are accepted", src)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return SkinSource{}, fmt.Errorf("%s: a link to a repository, github.com/owner/repo", src)
	}
	s := SkinSource{Owner: parts[0], Repo: strings.TrimSuffix(parts[1], ".git")}
	if !ghOwner.MatchString(s.Owner) || !validRepo(s.Repo) {
		return SkinSource{}, fmt.Errorf("%s: a link to a repository, github.com/owner/repo", src)
	}
	rest := parts[2:]
	if len(rest) > 0 {
		if rest[0] != "tree" || len(rest) < 2 {
			return SkinSource{}, fmt.Errorf("%s: a repository or a folder in one (…/tree/<ref>/<folder>)", src)
		}
		s.Ref = rest[1]
		if !validRef(s.Ref) {
			return SkinSource{}, fmt.Errorf("%s: ref %q: a branch, tag or commit", src, s.Ref)
		}
		if len(rest) > 2 {
			sub, err := cleanSubdir(strings.Join(rest[2:], "/"))
			if err != nil {
				return SkinSource{}, err
			}
			s.Subdir = sub
		}
	}
	return s, nil
}

func validRepo(r string) bool { return ghRepo.MatchString(r) && r != "." && r != ".." }

func validRef(r string) bool {
	return ghRef.MatchString(r) && !strings.Contains(r, "..") && !strings.HasPrefix(r, "-") && !strings.HasPrefix(r, "/")
}

func cleanSubdir(sub string) (string, error) {
	sub = strings.Trim(sub, "/")
	if sub == "" {
		return "", nil
	}
	for _, part := range strings.Split(sub, "/") {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `\:`) {
			return "", fmt.Errorf("folder %q: a folder inside the repository", sub)
		}
	}
	return sub, nil
}

// FindSkins lists the skin folders in a source folder, relative to it with
// slashes: "." when skin.json is at its root, else each immediate subfolder
// with a skin.json and each folder under skins/ with one.
func FindSkins(root string) []string {
	if isFile(filepath.Join(root, "skin.json")) {
		return []string{"."}
	}
	var out []string
	for _, base := range []string{"", "skins"} {
		entries, _ := os.ReadDir(filepath.Join(root, base))
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			rel := path.Join(base, e.Name())
			if isFile(filepath.Join(root, filepath.FromSlash(rel), "skin.json")) {
				out = append(out, rel)
			}
		}
	}
	sort.Strings(out)
	return out
}

func isFile(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.Mode().IsRegular()
}

// SkinFailure is one skin in a source that did not install.
type SkinFailure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// InstallResult is what installing a source did, skin by skin.
type InstallResult struct {
	Installed []Skin        `json:"installed"`
	Failed    []SkinFailure `json:"failed"`
}

// InstallSource installs every skin in a source into home/skins. The error
// is for a source that cannot be fetched or holds no skin; a skin that does
// not install is in the result's Failed.
func InstallSource(home string, src SkinSource) (InstallResult, error) {
	root, cleanup, err := fetchSource(src)
	if err != nil {
		return InstallResult{}, err
	}
	defer cleanup()
	found := FindSkins(root)
	if len(found) == 0 {
		return InstallResult{}, fmt.Errorf("%s holds no skin: no skin.json at its root, in a subfolder or under skins/", src)
	}
	res := InstallResult{Installed: []Skin{}, Failed: []SkinFailure{}}
	seen := map[string]string{}
	for _, rel := range found {
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if sk, err := ReadSkin(os.DirFS(dir)); err == nil {
			if first, dup := seen[sk.Name]; dup {
				res.Failed = append(res.Failed, SkinFailure{rel, fmt.Sprintf("%s is named %s, as %s is", rel, sk.Name, first)})
				continue
			}
			seen[sk.Name] = rel
		}
		at := src.at(rel)
		sk, err := installSkin(home, dir, &SourceRecord{Source: at.String(), Ref: recordedRef(at)})
		if err != nil {
			res.Failed = append(res.Failed, SkinFailure{rel, err.Error()})
			continue
		}
		res.Installed = append(res.Installed, sk)
	}
	return res, nil
}

func recordedRef(s SkinSource) string {
	if !s.GitHub() {
		return ""
	}
	return s.ref()
}

// UpdateSkin reinstalls an installed skin from its recorded source.
func UpdateSkin(home, name string) (Skin, error) {
	if !skinName.MatchString(name) {
		return Skin{}, fmt.Errorf("there is no skin %s", name)
	}
	rec, ok := readSourceRecord(filepath.Join(home, "skins", name))
	if !ok {
		return Skin{}, fmt.Errorf("%s has no recorded source; install it again", name)
	}
	src, err := ParseSkinSource(rec.Source)
	if err != nil {
		return Skin{}, err
	}
	if src.GitHub() && rec.Ref != "HEAD" {
		src.Ref = rec.Ref
	}
	root, cleanup, err := fetchSource(src)
	if err != nil {
		return Skin{}, err
	}
	defer cleanup()
	sk, err := ReadSkin(os.DirFS(root))
	if err != nil {
		return Skin{}, err
	}
	if sk.Name != name {
		return Skin{}, fmt.Errorf("%s now holds the skin %s, not %s", src, sk.Name, name)
	}
	return installSkin(home, root, &SourceRecord{Source: src.String(), Ref: recordedRef(src)})
}

// fetchSource gives the source as a local folder: a folder as it is, a
// GitHub repository downloaded and unpacked into a temporary folder that
// cleanup removes.
func fetchSource(src SkinSource) (dir string, cleanup func(), err error) {
	if !src.GitHub() {
		info, err := os.Stat(src.Dir)
		if err != nil || !info.IsDir() {
			return "", nil, fmt.Errorf("%s is not a folder", src.Dir)
		}
		return src.Dir, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "laforge-skins-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(tmp) }
	top, err := downloadRepo(src, tmp)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	dir = filepath.Join(tmp, top, filepath.FromSlash(src.Subdir))
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		cleanup()
		return "", nil, fmt.Errorf("%s/%s has no folder %s at %s", src.Owner, src.Repo, src.Subdir, src.ref())
	}
	return dir, cleanup, nil
}

// downloadRepo fetches the repository's tarball from codeload and unpacks
// it into dest, returning the archive's single top folder.
func downloadRepo(src SkinSource, dest string) (string, error) {
	base, err := url.Parse(codeloadBase)
	if err != nil {
		return "", err
	}
	api, err := url.Parse(githubAPIBase)
	if err != nil {
		return "", err
	}
	allowed := map[string]bool{"codeload.github.com": true, "github.com": true, "api.github.com": true,
		strings.ToLower(base.Host): true, strings.ToLower(api.Host): true}
	client := &http.Client{
		Timeout: packFetchTime,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !allowed[strings.ToLower(req.URL.Host)] || req.URL.Scheme != base.Scheme {
				return fmt.Errorf("refused a redirect to %s", req.URL.Host)
			}
			req.Header.Del("Authorization") // the token is for the API alone; codeload's link carries its own
			return nil
		},
	}
	segs := []string{url.PathEscape(src.Owner), url.PathEscape(src.Repo), "tar.gz"}
	for _, p := range strings.Split(src.ref(), "/") {
		segs = append(segs, url.PathEscape(p))
	}
	resp, err := client.Get(strings.TrimSuffix(codeloadBase, "/") + "/" + strings.Join(segs, "/"))
	if err != nil {
		return "", fmt.Errorf("fetch %s/%s: %v", src.Owner, src.Repo, err)
	}
	signedIn := false
	if resp.StatusCode == http.StatusNotFound {
		// private, or not there: ask again signed in, if the user is
		if token := githubToken(); token != "" {
			resp.Body.Close()
			apiSegs := []string{"repos", url.PathEscape(src.Owner), url.PathEscape(src.Repo), "tarball"}
			if src.ref() != "HEAD" { // the API's default branch is no ref at all
				apiSegs = append(apiSegs, segs[3:]...)
			}
			req, _ := http.NewRequest(http.MethodGet, strings.TrimSuffix(githubAPIBase, "/")+"/"+strings.Join(apiSegs, "/"), nil)
			req.Header.Set("Authorization", "Bearer "+token) // CheckRedirect drops it before codeload
			req.Header.Set("Accept", "application/vnd.github+json")
			if resp, err = client.Do(req); err != nil {
				return "", fmt.Errorf("fetch %s/%s: %v", src.Owner, src.Repo, err)
			}
			signedIn = true
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		hint := "is it public (or are you signed in: gh auth login, or $GH_TOKEN), and the ref right?"
		if signedIn {
			hint = "your GitHub login cannot read it, or the ref is wrong"
		}
		return "", fmt.Errorf("GitHub answered %s for %s/%s at %s: %s", resp.Status, src.Owner, src.Repo, src.ref(), hint)
	}
	body := &capReader{r: resp.Body, left: maxPackDownload}
	return extractTarGz(body, dest)
}

// capReader fails once more than left bytes are read.
type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return n, fmt.Errorf("the download is larger than %d MB", maxPackDownload>>20)
	}
	return n, err
}

// extractTarGz unpacks a gzipped tarball into dest: folders and regular
// files only. An absolute path, a .. or a link or device entry refuses the
// whole archive. It returns the archive's top folder ("" if it has none).
func extractTarGz(r io.Reader, dest string) (string, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return "", fmt.Errorf("the download is not a tarball: %v", err)
	}
	tr := tar.NewReader(gz)
	var total int64
	tops := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("the tarball: %v", err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name := strings.ReplaceAll(h.Name, `\`, "/")
		if strings.HasPrefix(name, "/") || filepath.IsAbs(h.Name) || filepath.VolumeName(h.Name) != "" {
			return "", fmt.Errorf("the tarball holds an absolute path, %s", h.Name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." {
				return "", fmt.Errorf("the tarball holds a path out of its folder, %s", h.Name)
			}
		}
		clean := path.Clean(name)
		if clean == "." {
			continue
		}
		tops[strings.SplitN(clean, "/", 2)[0]] = true
		target := filepath.Join(dest, filepath.FromSlash(clean))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return "", err
			}
		case tar.TypeReg:
			total += h.Size
			if total > maxPackExtract {
				return "", fmt.Errorf("the repository unpacks to more than %d MB", maxPackExtract>>20)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return "", err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(f, io.LimitReader(tr, h.Size))
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("the tarball holds a link or device, %s; a skin source holds files and folders only", h.Name)
		}
	}
	if len(tops) == 1 {
		for t := range tops {
			if info, err := os.Stat(filepath.Join(dest, t)); err == nil && info.IsDir() {
				return t, nil
			}
		}
	}
	return "", nil
}

// installSkins is POST /api/skins/install {source}: {installed, failed}.
func (s *Server) installSkins(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "input", "The body is not JSON.")
		return
	}
	src, err := ParseSkinSource(body.Source)
	if err != nil {
		writeError(w, http.StatusBadRequest, "source", err.Error())
		return
	}
	res, err := InstallSource(s.opts.Home, src)
	if err != nil {
		writeError(w, http.StatusBadRequest, "source", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// updateSkin is POST /api/skins/{name}/update: reinstall from its source.
func (s *Server) updateSkin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	src, ok := s.skins()[name]
	switch {
	case !ok:
		writeError(w, http.StatusNotFound, "no_skin", "There is no skin "+name+".")
		return
	case src.skin.Builtin:
		writeError(w, http.StatusConflict, "builtin", name+" ships with LaForge and updates with it.")
		return
	case src.skin.Source == "":
		writeError(w, http.StatusConflict, "no_source", name+" has no recorded source; install it again.")
		return
	}
	sk, err := UpdateSkin(s.opts.Home, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "source", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sk)
}
