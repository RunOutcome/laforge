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

const skinUsage = "skin list | install SOURCE | update [NAME] | remove NAME | use NAME"

// skin is `la skin list|install SOURCE|update [NAME]|remove NAME|use NAME`.
func (c *cmd) skin(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "install":
		if len(args) != 2 {
			return usageError{errors.New("skin install SOURCE (a folder, owner/repo[@ref][//folder] or a github.com link)")}
		}
		return c.skinInstall(args[1])
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
