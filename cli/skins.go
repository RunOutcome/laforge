// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/RunOutcome/laforge/forge"
)

const skinUsage = "skin list | new NAME [DIR] | check [--strict] DIR | browse [COLLECTION] | install SOURCE|COLLECTION[/NAME]… | collections [add LINK | remove ID] | update [NAME] | remove NAME | use NAME"

// skin is `la skin list|install SOURCE|update [NAME]|remove NAME|use NAME`.
func (c *cmd) skin(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "install":
		if len(args) < 2 {
			return usageError{errors.New("skin install SOURCE (a folder, owner/repo[@ref][//folder] or a github.com link) | COLLECTION[/NAME]…")}
		}
		if done, err := c.collectionInstall(args[1:]); done {
			return err
		}
		if len(args) != 2 {
			return usageError{errors.New("skin install takes one source (or collection skins: official/NAME …)")}
		}
		return c.skinInstall(args[1])
	case "new":
		if len(args) < 2 || len(args) > 3 {
			return usageError{errors.New("skin new NAME [DIR]")}
		}
		dir := args[1]
		if len(args) == 3 {
			dir = args[2]
		}
		if err := forge.NewSkin(dir, args[1], ""); err != nil {
			return err
		}
		fmt.Fprintf(c.cfg.Stdout, "made %s in %s (skin.json, skin.css, README.md)\nnext: edit skin.css, then %s skin check %s && %s skin install %s\n", args[1], dir, c.cfg.Prog, dir, c.cfg.Prog, dir)
		return nil
	case "check":
		strict := false
		var dirs []string
		for _, a := range args[1:] {
			if a == "--strict" {
				strict = true
			} else {
				dirs = append(dirs, a)
			}
		}
		if len(dirs) != 1 {
			return usageError{errors.New("skin check [--strict] DIR")}
		}
		res, err := forge.CheckSkin(dirs[0])
		if err != nil {
			return fmt.Errorf("%s would not install: %v", dirs[0], err)
		}
		fmt.Fprintf(c.cfg.Stdout, "%s (%s, %s layout) installs\n", res.Skin.Name, res.Skin.Label, res.Skin.Layout)
		for _, w := range res.Warnings {
			fmt.Fprintf(c.cfg.Stdout, "  warning: %s\n", w)
		}
		if strict && len(res.Warnings) > 0 {
			return exitCode(1)
		}
		return nil
	case "browse":
		if len(args) > 2 {
			return usageError{errors.New("skin browse [COLLECTION]")}
		}
		id := "official"
		if len(args) == 2 {
			id = args[1]
		}
		return c.skinBrowse(id)
	case "collections", "collection":
		return c.skinCollections(args[1:])
	case "update":
		if len(args) > 2 {
			return usageError{errors.New("skin update [NAME]")}
		}
		return c.skinUpdate(args[1:])
	case "list", "ls":
		f, err := c.apiClient()
		if err != nil {
			return err
		}
		var out struct {
			Default string       `json:"default"`
			Skins   []forge.Skin `json:"skins"`
		}
		if err := f.do(http.MethodGet, "/api/skins", nil, &out); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(c.cfg.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "\tNAME\tLABEL\tLAYOUT\tFROM")
		for _, sk := range out.Skins {
			mark, from := "", "installed"
			if sk.Name == out.Default {
				mark = "*"
			}
			if sk.Builtin {
				from = "built in"
			} else if sk.Source != "" {
				from = sk.Source
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", mark, sk.Name, sk.Label, sk.Layout, from)
		}
		return tw.Flush()
	case "use", "remove", "rm":
		if len(args) != 2 {
			return usageError{fmt.Errorf("skin %s NAME", args[0])}
		}
		f, err := c.apiClient()
		if err != nil {
			return err
		}
		if args[0] == "use" {
			return f.do(http.MethodPut, "/api/skins/default", map[string]string{"name": args[1]}, nil)
		}
		return f.do(http.MethodDelete, "/api/skins/"+args[1], nil, nil)
	}
	return usageError{errors.New(skinUsage)}
}

// slowClient is the running forge with a timeout long enough for it to
// fetch from GitHub, or nil when no forge runs.
func (c *cmd) slowClient() *forgeClient {
	if s := c.running(); s != nil {
		return &forgeClient{state: s, http: &http.Client{Timeout: 3 * time.Minute}}
	}
	return nil
}

// skinInstall installs every skin in a source. A folder goes straight into
// the home; a GitHub source goes through the forge when it runs (so it
// fetches), else is fetched here.
func (c *cmd) skinInstall(arg string) error {
	src, err := forge.ParseSkinSource(arg)
	if err != nil {
		return err
	}
	var res forge.InstallResult
	if f := c.slowClient(); src.GitHub() && f != nil {
		err = f.do(http.MethodPost, "/api/skins/install", map[string]string{"source": arg}, &res)
	} else {
		res, err = forge.InstallSource(c.home, src)
	}
	if err != nil {
		return err
	}
	for _, sk := range res.Installed {
		fmt.Fprintf(c.cfg.Stdout, "installed %s (%s, %s layout)\n", sk.Name, sk.Label, sk.Layout)
	}
	if len(res.Installed) == 1 {
		fmt.Fprintf(c.cfg.Stdout, "choose it in the palette, or: %s skin use %s\n", c.cfg.Prog, res.Installed[0].Name)
	} else if len(res.Installed) > 1 {
		fmt.Fprintf(c.cfg.Stdout, "choose one in the palette, or: %s skin use NAME\n", c.cfg.Prog)
	}
	return skinFailures(res.Failed)
}

func skinFailures(failed []forge.SkinFailure) error {
	if len(failed) == 0 {
		return nil
	}
	lines := make([]string, len(failed))
	for i, f := range failed {
		lines[i] = f.Path + ": " + f.Reason
	}
	if len(failed) == 1 {
		return errors.New(lines[0])
	}
	return fmt.Errorf("%d skins did not install:\n  %s", len(failed), strings.Join(lines, "\n  "))
}

// skinUpdate reinstalls one installed skin, or every one with a recorded
// source, from where it came: through the forge when it runs, else here.
func (c *cmd) skinUpdate(names []string) error {
	f := c.slowClient()
	if len(names) == 0 {
		if f != nil {
			var out struct {
				Skins []forge.Skin `json:"skins"`
			}
			if err := f.do(http.MethodGet, "/api/skins", nil, &out); err != nil {
				return err
			}
			for _, sk := range out.Skins {
				if !sk.Builtin && sk.Source != "" {
					names = append(names, sk.Name)
				}
			}
		} else {
			entries, _ := os.ReadDir(filepath.Join(c.home, "skins"))
			for _, e := range entries {
				if _, err := os.Stat(filepath.Join(c.home, "skins", e.Name(), ".source.json")); e.IsDir() && err == nil {
					names = append(names, e.Name())
				}
			}
		}
		sort.Strings(names)
		if len(names) == 0 {
			fmt.Fprintln(c.cfg.Stdout, "no installed skin has a recorded source")
			return nil
		}
	}
	var failed []forge.SkinFailure
	for _, name := range names {
		var sk forge.Skin
		var err error
		if f != nil {
			err = f.do(http.MethodPost, "/api/skins/"+name+"/update", map[string]string{}, &sk)
		} else {
			sk, err = forge.UpdateSkin(c.home, name)
		}
		if err != nil {
			failed = append(failed, forge.SkinFailure{Path: name, Reason: err.Error()})
			continue
		}
		fmt.Fprintf(c.cfg.Stdout, "updated %s from %s\n", sk.Name, sk.Source)
	}
	return skinFailures(failed)
}

// collections is the forge's skin collections, or nil without a forge.
func (c *cmd) collections(f *forgeClient) []forge.Collection {
	var cs []forge.Collection
	if f == nil || f.do(http.MethodGet, "/api/skins/collections", nil, &cs) != nil {
		return nil
	}
	return cs
}

// collectionInstall installs from a collection when every argument names
// one (official, official/paper, fans/neon …); done is false when they are
// a source instead.
func (c *cmd) collectionInstall(args []string) (done bool, err error) {
	f := c.slowClient()
	ids := map[string]bool{}
	for _, col := range c.collections(f) {
		ids[col.ID] = true
	}
	byCollection := map[string][]string{}
	var order []string
	for _, a := range args {
		id, name, _ := strings.Cut(a, "/")
		if !ids[id] || strings.Contains(name, "/") {
			if len(args) > 1 && f != nil {
				return true, fmt.Errorf("%s is not COLLECTION or COLLECTION/NAME (collections: %s skin collections)", a, c.cfg.Prog)
			}
			return false, nil
		}
		if _, seen := byCollection[id]; !seen {
			order = append(order, id)
			byCollection[id] = []string{}
		}
		if name != "" {
			byCollection[id] = append(byCollection[id], name)
		}
	}
	var failed []forge.SkinFailure
	for _, id := range order {
		var res forge.InstallResult
		if err := f.do(http.MethodPost, "/api/skins/install", map[string]any{"collection": id, "names": byCollection[id]}, &res); err != nil {
			return true, err
		}
		for _, sk := range res.Installed {
			fmt.Fprintf(c.cfg.Stdout, "installed %s (%s) from %s\n", sk.Name, sk.Label, id)
		}
		failed = append(failed, res.Failed...)
	}
	return true, skinFailures(failed)
}

// skinBrowse lists what a collection offers.
func (c *cmd) skinBrowse(id string) error {
	f := c.slowClient()
	if f == nil {
		return errors.New("browsing a collection needs the forge running: " + c.cfg.Prog + " up")
	}
	var offers []forge.Available
	if err := f.do(http.MethodGet, "/api/skins/collections/"+id+"/skins", nil, &offers); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(c.cfg.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "\tNAME\tLABEL\tLAYOUT\tNOTE")
	for _, a := range offers {
		mark, note := "", a.Description
		if a.Installed {
			mark = "✓"
		}
		if a.Problem != "" {
			note = "will not install: " + a.Problem
		}
		if len(note) > 70 {
			note = note[:67] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", mark, a.Name, a.Label, a.Layout, note)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(c.cfg.Stdout, "install: %s skin install %s/NAME  (or %s for all)\n", c.cfg.Prog, id, id)
	return nil
}

// skinCollections is `la skin collections [add LINK | remove ID]`.
func (c *cmd) skinCollections(args []string) error {
	f := c.slowClient()
	if f == nil {
		return errors.New("collections need the forge running: " + c.cfg.Prog + " up")
	}
	switch {
	case len(args) == 0 || args[0] == "list":
		tw := tabwriter.NewWriter(c.cfg.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tSOURCE")
		for _, col := range c.collections(f) {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", col.ID, col.Name, col.Source)
		}
		return tw.Flush()
	case args[0] == "add" && len(args) == 2:
		var col forge.Collection
		if err := f.do(http.MethodPost, "/api/skins/collections", map[string]string{"source": args[1]}, &col); err != nil {
			return err
		}
		fmt.Fprintf(c.cfg.Stdout, "added %s (%s); browse it: %s skin browse %s\n", col.ID, col.Name, c.cfg.Prog, col.ID)
		return nil
	case (args[0] == "remove" || args[0] == "rm") && len(args) == 2:
		return f.do(http.MethodDelete, "/api/skins/collections/"+args[1], nil, nil)
	}
	return usageError{errors.New("skin collections [add LINK | remove ID]")}
}
