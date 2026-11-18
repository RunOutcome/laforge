// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/RunOutcome/laforge/forge"
)

const configUsage = "config [--json] | config get KEY | config set KEY VALUE | config unset cwd | config profile [list] | config profile add [--label L] [--description D] [--agent] [--env K=V]… NAME COMMAND [ARGS…] | config profile remove NAME"

// config is `la config`: the forge's settings, the same keys as GET and PUT
// /api/config. It goes through the running forge, else reads and writes the
// home itself.
func (c *cmd) config(args []string) error {
	if len(args) == 0 || args[0] == "list" || args[0] == "ls" || args[0] == "--json" {
		asJSON := len(args) > 0 && args[0] == "--json" || len(args) > 1 && args[1] == "--json"
		return c.configList(asJSON)
	}
	switch args[0] {
	case "get":
		if len(args) != 2 {
			return c.usage("config get KEY")
		}
		return c.configGet(args[1])
	case "set":
		if len(args) != 3 {
			return c.usage("config set KEY VALUE")
		}
		return c.configSet(args[1], args[2])
	case "unset":
		if len(args) != 2 || args[1] != "cwd" {
			return c.usage("config unset cwd (the other settings are set, not unset)")
		}
		return c.configSet("cwd", "")
	case "profile", "profiles":
		return c.configProfile(args[1:])
	}
	return c.usage(configUsage)
}

// usage says how a config command goes (a usageError alone prints nothing).
func (c *cmd) usage(how string) error {
	fmt.Fprintf(c.cfg.Stderr, "usage: %s %s\n", c.cfg.Prog, how)
	return usageError{errors.New(how)}
}

// forgeConfig is the running forge's config, or the home's when none runs
// (live false: no version, host or port of a forge).
func (c *cmd) forgeConfig() (cfg forge.ForgeConfig, live bool, err error) {
	if f, ferr := c.apiClient(); ferr == nil {
		err = f.do(http.MethodGet, "/api/config", nil, &cfg)
		return cfg, true, err
	}
	set, err := forge.ReadSettings(c.home)
	if err != nil {
		return cfg, false, err
	}
	host, _ := os.Hostname()
	return forge.ForgeConfig{Settings: set, Version: Version, Host: host, User: os.Getenv("USER"), Home: c.home, ReadOnly: forge.ReadOnlyKeys}, false, nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// configValue is one key's value as la config prints it.
func configValue(cfg forge.ForgeConfig, key string) (string, bool) {
	switch key {
	case "cwd":
		return cfg.Cwd, true
	case "skin":
		return cfg.Skin, true
	case "scenes":
		return onOff(cfg.Scenes), true
	case "version":
		return cfg.Version, true
	case "host":
		return cfg.Host, true
	case "user":
		return cfg.User, true
	case "port":
		if cfg.Port == 0 {
			return "", true
		}
		return strconv.Itoa(cfg.Port), true
	case "home":
		return cfg.Home, true
	}
	return "", false
}

func (c *cmd) configList(asJSON bool) error {
	cfg, live, err := c.forgeConfig()
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(c.cfg.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cfg)
	}
	if !live {
		fmt.Fprintf(c.cfg.Stdout, "LaForge is not running: the settings saved in %s\n\n", c.home)
	}
	tw := tabwriter.NewWriter(c.cfg.Stdout, 0, 4, 2, ' ', 0)
	for _, k := range forge.SettingKeys {
		v, _ := configValue(cfg, k)
		note := ""
		if k == "cwd" {
			switch {
			case cfg.CwdSaved:
				note = "saved"
			case live:
				note = "where la up ran; not saved"
			default:
				v, note = "-", "not saved: where la up runs"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", k, v, note)
	}
	for _, k := range forge.ReadOnlyKeys {
		v, _ := configValue(cfg, k)
		if v == "" {
			if live || k != "port" {
				continue
			}
			v = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\tread-only\n", k, v)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(c.cfg.Stdout)
	return c.printProfiles(cfg.Profiles)
}

func (c *cmd) printProfiles(profiles []forge.Profile) error {
	tw := tabwriter.NewWriter(c.cfg.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tLABEL\tCOMMAND\tKIND\tFROM\tINSTALLED")
	for _, p := range profiles {
		kind, from, inst := "shell", "built in", "yes"
		if p.Agent {
			kind = "agent"
		}
		if p.Custom && p.Builtin {
			from = "yours (changes a built-in)"
		} else if p.Custom || !p.Builtin {
			from = "yours"
		}
		if !p.Available {
			inst = "no"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Label, strings.Join(append([]string{p.Command}, p.Args...), " "), kind, from, inst)
	}
	return tw.Flush()
}

func (c *cmd) configGet(key string) error {
	cfg, _, err := c.forgeConfig()
	if err != nil {
		return err
	}
	if key == "profiles" {
		for _, p := range cfg.Profiles {
			fmt.Fprintln(c.cfg.Stdout, p.Name)
		}
		return nil
	}
	v, ok := configValue(cfg, key)
	if !ok {
		return fmt.Errorf("there is no setting %q; the settings are %s (and, read-only, %s, profiles)", key, strings.Join(forge.SettingKeys, ", "), strings.Join(forge.ReadOnlyKeys, ", "))
	}
	fmt.Fprintln(c.cfg.Stdout, v)
	return nil
}

// patchFor turns la config set KEY VALUE into the API's patch.
func patchFor(key, value string) (forge.ConfigPatch, error) {
	var p forge.ConfigPatch
	switch key {
	case "cwd":
		dir := value
		if dir != "" && dir != "~" && !strings.HasPrefix(dir, "~/") {
			// a relative folder is this shell's, not the forge's
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
		}
		p.Cwd = &dir
	case "skin":
		p.Skin = &value
	case "scenes":
		var on bool
		switch strings.ToLower(value) {
		case "on", "true", "yes", "1":
			on = true
		case "off", "false", "no", "0":
		default:
			return p, errors.New("scenes is on or off")
		}
		p.Scenes = &on
	case "profiles":
		return p, errors.New("profiles change with: la config profile add|remove")
	default:
		for _, k := range forge.ReadOnlyKeys {
			if k == key {
				return p, fmt.Errorf("%s is read-only", key)
			}
		}
		return p, fmt.Errorf("there is no setting %q; the settings are %s", key, strings.Join(forge.SettingKeys, ", "))
	}
	return p, nil
}

func (c *cmd) configSet(key, value string) error {
	p, err := patchFor(key, value)
	if err != nil {
		return err
	}
	var cfg forge.ForgeConfig
	if f, ferr := c.apiClient(); ferr == nil {
		if err := f.do(http.MethodPut, "/api/config", p, &cfg); err != nil {
			return err
		}
	} else {
		if err := forge.ApplyConfig(c.home, p); err != nil {
			return err
		}
		if cfg.Settings, err = forge.ReadSettings(c.home); err != nil {
			return err
		}
	}
	v, _ := configValue(cfg, key)
	if key == "cwd" && value == "" {
		fmt.Fprintln(c.cfg.Stdout, "cwd is not saved: new panes start where la up runs")
		return nil
	}
	fmt.Fprintf(c.cfg.Stdout, "%s = %s\n", key, v)
	return nil
}

type envFlags []string

func (e *envFlags) String() string { return strings.Join(*e, ",") }
func (e *envFlags) Set(v string) error {
	if k, _, ok := strings.Cut(v, "="); !ok || k == "" {
		return errors.New("an env entry is NAME=value")
	}
	*e = append(*e, v)
	return nil
}

func (c *cmd) configProfile(args []string) error {
	if len(args) == 0 || args[0] == "list" || args[0] == "ls" {
		cfg, _, err := c.forgeConfig()
		if err != nil {
			return err
		}
		return c.printProfiles(cfg.Profiles)
	}
	switch args[0] {
	case "add", "set":
		fs := flag.NewFlagSet("config profile add", flag.ContinueOnError)
		fs.SetOutput(c.cfg.Stderr)
		label := fs.String("label", "", "the name the picker shows (default: NAME)")
		desc := fs.String("description", "", "one line under the label")
		agent := fs.Bool("agent", false, "it is an agent CLI (it reports status, its pane becomes a shell when it exits)")
		var env envFlags
		fs.Var(&env, "env", "NAME=value for its environment (repeatable)")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError{err}
		}
		rest := fs.Args()
		if len(rest) < 2 {
			return c.usage("config profile add [--label L] [--description D] [--agent] [--env K=V]… NAME COMMAND [ARGS…]")
		}
		p := forge.Profile{Name: rest[0], Label: *label, Description: *desc, Command: rest[1], Args: rest[2:], Env: env, Agent: *agent}
		var saved forge.Profile
		if f, err := c.apiClient(); err == nil {
			if err := f.do(http.MethodPut, "/api/profiles/"+url.PathEscape(p.Name), p, &saved); err != nil {
				return err
			}
		} else if saved, err = forge.SaveProfile(c.home, p); err != nil {
			return err
		}
		fmt.Fprintf(c.cfg.Stdout, "saved profile %s (%s)\n", saved.Name, saved.Label)
		if !saved.Available {
			fmt.Fprintf(c.cfg.Stdout, "  warning: %s\n", saved.Reason)
		}
		return nil
	case "remove", "rm":
		if len(args) != 2 {
			return c.usage("config profile remove NAME")
		}
		var err error
		if f, ferr := c.apiClient(); ferr == nil {
			err = f.do(http.MethodDelete, "/api/profiles/"+url.PathEscape(args[1]), nil, nil)
		} else {
			err = forge.RemoveProfile(c.home, args[1])
			switch {
			case errors.Is(err, forge.ErrBuiltinProfile):
				err = fmt.Errorf("%s is built in; it cannot be removed, only changed", args[1])
			case errors.Is(err, forge.ErrNoProfile):
				err = fmt.Errorf("there is no profile %s of yours", args[1])
			}
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(c.cfg.Stdout, "removed profile %s\n", args[1])
		return nil
	}
	return c.usage(configUsage)
}
