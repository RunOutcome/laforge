// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"text/tabwriter"

	"github.com/RunOutcome/laforge/forge"
)

// skin is `la skin list|install DIR|remove NAME|use NAME`.
func (c *cmd) skin(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "install":
		if len(args) != 2 {
			return usageError{errors.New("skin install DIR")}
		}
		dir, _ := filepath.Abs(args[1])
		sk, err := forge.InstallSkin(Home(), dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(c.cfg.Stdout, "installed %s (%s, %s layout); choose it in the palette, or: %s skin use %s\n", sk.Name, sk.Label, sk.Layout, c.cfg.Prog, sk.Name)
		return nil
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
	return usageError{errors.New("skin list | install DIR | remove NAME | use NAME")}
}
