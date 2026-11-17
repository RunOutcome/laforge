// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs/openapi.yaml names every route the server registers, and no other:
// the contract clients build against cannot drift from the code.
func TestTheContractNamesEveryRoute(t *testing.T) {
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := os.ReadFile("../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	norm := func(p string) string { return regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(p, "{}") }
	set := func(ms [][]string) []string {
		seen := map[string]bool{}
		for _, m := range ms {
			seen[norm(m[1])] = true
		}
		out := make([]string, 0, len(seen))
		for k := range seen {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	code := set(regexp.MustCompile(`HandleFunc\("[A-Z]+ (/api/[^"]*)"`).FindAllStringSubmatch(string(src), -1))
	doc := set(regexp.MustCompile(`(?m)^  (/api/[^:]*):`).FindAllStringSubmatch(string(spec), -1))
	if strings.Join(code, "\n") != strings.Join(doc, "\n") {
		t.Fatalf("routes in code:\n%s\n\nin docs/openapi.yaml:\n%s", strings.Join(code, "\n"), strings.Join(doc, "\n"))
	}
}
