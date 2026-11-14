// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RunOutcome/laforge/forge"
)

// DefaultImage is the image `la up --container` runs ($LAFORGE_IMAGE to change).
const DefaultImage = "laforge:local"

// ContainerName is the one forge container per machine.
const ContainerName = "laforge"

// ContainerState is what the host keeps about the forge container in
// <home>/container.json: enough to give a link without asking the container.
type ContainerState struct {
	Image     string    `json:"image"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	Workspace string    `json:"workspace"`
	Runtime   string    `json:"runtime"`
	StartedAt time.Time `json:"startedAt"`
}

func (s *ContainerState) url() string  { return fmt.Sprintf("http://127.0.0.1:%d", s.Port) }
func (s *ContainerState) link() string { return s.url() + "/?token=" + s.Token }

func (c *cmd) containerStatePath() string { return filepath.Join(c.home, "container.json") }

// runtimeCommand is docker, else podman.
func runtimeCommand() (string, error) {
	for _, name := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", errors.New("neither docker nor podman is installed")
}

func (c *cmd) readContainerState() *ContainerState {
	raw, err := os.ReadFile(c.containerStatePath())
	if err != nil {
		return nil
	}
	var s ContainerState
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}

// containerRunning reports whether the forge container is up and answering.
func containerRunning(rt string, s *ContainerState) bool {
	out, err := exec.Command(rt, "inspect", "-f", "{{.State.Running}}", ContainerName).Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return false
	}
	if s == nil {
		return true
	}
	resp, err := (&http.Client{Timeout: 800 * time.Millisecond}).Get(s.url() + "/api/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

type containerUp struct {
	port   int
	dir    string
	image  string
	noOpen bool
}

func (c *cmd) upContainer(o containerUp) error {
	rt, err := runtimeCommand()
	if err != nil {
		return err
	}
	if s := c.readContainerState(); s != nil && containerRunning(rt, s) {
		if s.Workspace != o.dir {
			fmt.Fprintf(c.cfg.Stdout, "LaForge's container is running with %s at /workspace (not %s).\n  %s down --container, then up again, to change it.\n", s.Workspace, o.dir, c.cfg.Prog)
		}
		fmt.Fprintf(c.cfg.Stdout, "LaForge is running in a container (%s)\n  %s\n", s.Image, s.link())
		if !o.noOpen {
			openBrowser(s.link())
		}
		return nil
	}
	if err := exec.Command(rt, "image", "inspect", o.image).Run(); err != nil {
		return fmt.Errorf("there is no image %s; build it with `make image` in the laforge repository (or set LAFORGE_IMAGE)", o.image)
	}
	_ = exec.Command(rt, "rm", "-f", ContainerName).Run() // a stopped one from before
	if err := os.MkdirAll(c.home, 0o700); err != nil {
		return err
	}
	s := ContainerState{Image: o.image, Port: o.port, Token: forge.NewToken(), Workspace: o.dir, Runtime: rt, StartedAt: time.Now().UTC()}
	args := []string{"run", "-d", "--name", ContainerName, "--restart", "unless-stopped",
		"-p", fmt.Sprintf("127.0.0.1:%d:4646", o.port),
		"-v", "laforge-home:/home/forge",
		"-v", o.dir + ":/workspace",
		"-e", "LAFORGE_TOKEN=" + s.Token,
	}
	// API keys for the agents, if the user keeps them in <home>/agents.env
	if env := filepath.Join(c.home, "agents.env"); fileExists(env) {
		args = append(args, "--env-file", env)
	}
	args = append(args, o.image)
	if out, err := exec.Command(rt, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s run: %v: %s", rt, err, strings.TrimSpace(string(out)))
	}
	raw, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(c.containerStatePath(), raw, 0o600); err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if containerRunning(rt, &s) {
			fmt.Fprintf(c.cfg.Stdout, "LaForge is up in a container (%s, %s at /workspace)\n  %s\n", o.image, o.dir, s.link())
			if !o.noOpen {
				openBrowser(s.link())
			}
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	logs, _ := exec.Command(rt, "logs", "--tail", "20", ContainerName).CombinedOutput()
	return fmt.Errorf("the container did not answer:\n%s", logs)
}

func (c *cmd) downContainer() error {
	rt, err := runtimeCommand()
	if err != nil {
		return err
	}
	if err := exec.Command(rt, "inspect", ContainerName).Run(); err != nil {
		fmt.Fprintln(c.cfg.Stdout, "LaForge's container is not running.")
		return nil
	}
	if out, err := exec.Command(rt, "rm", "-f", ContainerName).CombinedOutput(); err != nil {
		return fmt.Errorf("%s rm: %v: %s", rt, err, strings.TrimSpace(string(out)))
	}
	_ = os.Remove(c.containerStatePath())
	fmt.Fprintln(c.cfg.Stdout, "LaForge's container stopped. Its home (logins, installed agents) stays in the volume laforge-home.")
	return nil
}

// containerLine is the status line for the container, or "".
func (c *cmd) containerLine() string {
	rt, err := runtimeCommand()
	if err != nil {
		return ""
	}
	s := c.readContainerState()
	if s == nil || !containerRunning(rt, s) {
		return ""
	}
	return fmt.Sprintf("LaForge in a container (%s), %s, /workspace = %s\n  %s", s.Image, s.url(), s.Workspace, s.link())
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
