// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package forge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/RunOutcome/laforge/terminal"
)

// Profile is one kind of pane the forge can start: a shell or an agent CLI.
type Profile struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	Args        []string `json:"args,omitempty"`
	Env         []string `json:"env,omitempty"`
	Agent       bool     `json:"agent"`
	// Resume are the arguments, after Args, that continue one of the agent's
	// own sessions; {session} is its id.
	Resume []string `json:"resume,omitempty"`
	// Available and Reason are filled in when profiles are listed.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// Builtin is a profile every forge offers; Custom is one saved in
	// profiles.json (added, or changing a built-in).
	Builtin bool `json:"builtin,omitempty"`
	Custom  bool `json:"custom,omitempty"`
}

// BuiltinProfiles are the panes every forge offers. The shell is the user's
// own login shell; each agent is its platform's own CLI, run unchanged with
// its own login.
func BuiltinProfiles() []Profile {
	shell := LoginShell()
	return []Profile{
		{Name: "shell", Label: "Shell", Description: "Your login shell", Command: shell, Args: loginArgs()},
		{Name: "claude", Label: "Claude Code", Description: "Anthropic Claude Code", Command: "claude", Agent: true, Resume: []string{"--resume", "{session}"}},
		{Name: "codex", Label: "Codex", Description: "OpenAI Codex CLI", Command: "codex", Agent: true, Resume: []string{"resume", "{session}"}},
		{Name: "antigravity", Label: "Antigravity", Description: "Google Antigravity CLI", Command: "agy", Agent: true},
		{Name: "qwen", Label: "Qwen Code", Description: "Alibaba Qwen Code", Command: "qwen", Agent: true},
		{Name: "copilot", Label: "Copilot", Description: "GitHub Copilot CLI", Command: "copilot", Agent: true},
		{Name: "grok", Label: "Grok", Description: "xAI Grok CLI", Command: "grok", Agent: true},
		{Name: "opencode", Label: "opencode", Description: "opencode agent", Command: "opencode", Agent: true},
		{Name: "ollama", Label: "Ollama", Description: "Local model runner", Command: "ollama", Args: []string{"run", "llama3.2"}, Agent: true},
	}
}

// loadProfiles is the built-ins, with any in <home>/profiles.json added or
// replacing a built-in of the same name.
func loadProfiles(home string) ([]Profile, error) {
	profiles := BuiltinProfiles()
	raw, err := os.ReadFile(filepath.Join(home, "profiles.json"))
	if errors.Is(err, os.ErrNotExist) {
		return profiles, nil
	}
	if err != nil {
		return nil, err
	}
	var extra []Profile
	if err := json.Unmarshal(raw, &extra); err != nil {
		return nil, err
	}
	for _, p := range extra {
		if p.Name == "" || p.Command == "" {
			continue
		}
		if p.Label == "" {
			p.Label = p.Name
		}
		replaced := false
		for i := range profiles {
			if profiles[i].Name == p.Name {
				profiles[i], replaced = p, true
			}
		}
		if !replaced {
			profiles = append(profiles, p)
		}
	}
	return profiles, nil
}

// withAvailability marks each profile installed or not on this machine.
func withAvailability(profiles []Profile) []Profile {
	out := make([]Profile, len(profiles))
	for i, p := range profiles {
		if _, ok := terminal.LookupCommand(p.Command); ok {
			p.Available = true
		} else {
			p.Reason = p.Command + " is not installed on this machine."
		}
		out[i] = p
	}
	return out
}
