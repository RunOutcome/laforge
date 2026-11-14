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
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/RunOutcome/laforge/forge"
)

// LocalImage is what `make image` builds.
const LocalImage = "laforge:local"

// PublishedImage is where release images are published.
const PublishedImage = "ghcr.io/runoutcome/laforge"

// DefaultImage is the image `la up --container` runs ($LAFORGE_IMAGE to
// change): a release build runs the image of its own version, a development
// build the locally built one.
func DefaultImage() string {
	v := strings.TrimPrefix(Version, "v")
	if !releaseVersion.MatchString(v) {
		return LocalImage
	}
	return PublishedImage + ":" + v
}

// releaseVersion is a tagged release: 1.2.3, or 1.2.3-rc1 / -beta.2 / -alpha.
var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta|rc)[0-9.]*)?$`)

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
		if o.image == LocalImage {
			return fmt.Errorf("there is no image %s; build it with `make image` in the laforge repository (or set LAFORGE_IMAGE)", o.image)
		}
		fmt.Fprintf(c.cfg.Stdout, "Pulling %s…\n", o.image)
		pull := exec.Command(rt, "pull", o.image)
		pull.Stdout, pull.Stderr = c.cfg.Stderr, c.cfg.Stderr
		if err := pull.Run(); err != nil {
			return fmt.Errorf("could not pull %s: %w", o.image, err)
		}
	}
	if containerRunning(rt, nil) {
		return fmt.Errorf("a %s container is running that this host's LaForge did not start (no %s); `%s down --container` replaces it", ContainerName, c.containerStatePath(), c.cfg.Prog)
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
	switch {
	case rt == "podman":
		// rootless podman: the host user is uid 1000 (forge) inside, so
		// /workspace is writable and new files are the host user's
		args = append(args, "--userns=keep-id:uid=1000,gid=1000")
	case runtime.GOOS == "linux" && os.Getuid() != 1000:
		fmt.Fprintf(c.cfg.Stderr, "note: the container's user is uid 1000 and you are uid %d; files it writes in %s will be owned by uid 1000\n", os.Getuid(), o.dir)
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
