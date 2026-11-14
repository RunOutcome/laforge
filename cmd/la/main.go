// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Command la is LaForge.
package main

import (
	"os"

	"github.com/RunOutcome/laforge/cli"
)

func main() { os.Exit(cli.Main(cli.Config{Prog: "la"}, os.Args[1:])) }
