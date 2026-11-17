// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Package cli is the la command tree as a library. The la binary runs it as
// its root; a host such as Outcome mounts it under its own name (`out la`)
// by calling Main with a Config that says how to re-invoke itself.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/RunOutcome/laforge/forge"
)

// Version is set at build time (-ldflags "-X .../cli.Version=v0.1.0").
var Version = "dev"

// DefaultPort is where the forge listens when it is free.
const DefaultPort = 4646

// Config says who is running the command tree.
type Config struct {
	// Prog is the name shown in usage and messages: "la" or "out la".
	Prog string
	// Reexec is the argument prefix that reaches this tree when the current
	// executable is run again: nil for la, []string{"la"} for out.
	Reexec []string
	// Stdin, Stdout and Stderr default to the process's own.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// State is what a running forge writes to <home>/forge.json.
type State struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	Version   string    `json:"version"`
	Home      string    `json:"home"`
	StartedAt time.Time `json:"startedAt"`
}

// Home is the forge's folder: $LAFORGE_HOME, else ~/.laforge/forge.
func Home() string {
	if h := os.Getenv("LAFORGE_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".laforge-forge"
	}
	return filepath.Join(home, ".laforge", "forge")
}

// Main runs one command and returns the exit code.
func Main(cfg Config, args []string) int {
	if cfg.Prog == "" {
		cfg.Prog = "la"
	}
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}
	if len(args) == 0 {
		usage(cfg)
		return 0
	}
	c := &cmd{cfg: cfg, home: Home()}
	var err error
	switch args[0] {
	case "up", "start":
		err = c.up(args[1:])
	case "down", "stop":
		err = c.down(args[1:])
	case "status":
		err = c.status(args[1:])
	case "open":
		err = c.open(args[1:])
	case "serve":
		err = c.serve(args[1:])
	case "report":
		err = c.report(args[1:])
	case "hooks":
		err = c.hooks(args[1:])
	case "notices", "licenses", "licences":
		fmt.Fprint(cfg.Stdout, forge.Notices())
	case "version", "--version", "-v":
		fmt.Fprintf(cfg.Stdout, "laforge %s\n", Version)
	case "help", "--help", "-h":
		usage(cfg)
	default:
		fmt.Fprintf(cfg.Stderr, "%s: unknown command %q\n", cfg.Prog, args[0])
		usage(cfg)
		return 2
	}
	if err != nil {
		var u usageError
		if errors.As(err, &u) {
			return 2
		}
		fmt.Fprintf(cfg.Stderr, "%s: %v\n", cfg.Prog, err)
		return 1
	}
	return 0
}

type usageError struct{ error }

func usage(cfg Config) {
	fmt.Fprintf(cfg.Stdout, `LaForge: many shells and agents, side by side, in your browser.

Usage:
  %[1]s up [--port N] [--cwd DIR] [--no-open] [--foreground]
                     start the forge (or find the running one) and open it
  %[1]s up --container [--image IMG]
                     the same, in a container with agent CLIs bundled,
                     this folder (or --cwd) mounted at /workspace
  %[1]s open         open the running forge in the browser
  %[1]s status       show the running forge and its panes
  %[1]s down [--container]
                     stop the forge and every pane in it
  %[1]s hooks install [claude|codex]
                     let Claude Code and Codex report their status to LaForge
                     (also: hooks status, hooks uninstall)
  %[1]s report STATE [--kind K] [--msg M]
                     say what this pane's program is doing: idle, working,
                     done, blocked, error or clear (programs can also send
                     OSC 7501 themselves)
  %[1]s version
  %[1]s notices      third-party licences of what la includes

The forge keeps running when the browser closes; panes reattach when you open
it again. Its files are in %[2]s ($LAFORGE_HOME to change).
`, cfg.Prog, Home())
}

type cmd struct {
	cfg  Config
	home string
}

func (c *cmd) statePath() string { return filepath.Join(c.home, "forge.json") }

func (c *cmd) readState() (*State, error) {
	raw, err := os.ReadFile(c.statePath())
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// running is the state of a forge that answers its health check as the
// process the state names; nil when none does.
func (c *cmd) running() *State {
	s, err := c.readState()
	if err != nil {
		return nil
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/health", s.Port))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var h struct {
		Product string `json:"product"`
		PID     int    `json:"pid"`
	}
	if json.NewDecoder(resp.Body).Decode(&h) != nil || h.Product != "laforge" || h.PID != s.PID {
		return nil
	}
	return s
}

func (s *State) link() string { return s.URL + "/?token=" + s.Token }

func (c *cmd) up(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	port := fs.Int("port", DefaultPort, "port to listen on (127.0.0.1 only); taken: any free port")
	cwd := fs.String("cwd", "", "folder new panes start in (default: this folder)")
	noOpen := fs.Bool("no-open", false, "do not open the browser")
	foreground := fs.Bool("foreground", false, "run in this terminal instead of in the background")
	container := fs.Bool("container", false, "run the forge in a container (docker or podman), this folder at /workspace")
	image := fs.String("image", envOr("LAFORGE_IMAGE", DefaultImage()), "the image for --container")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	dir := *cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dir, _ = filepath.Abs(dir)
	if *container {
		return c.upContainer(containerUp{port: *port, dir: dir, image: *image, noOpen: *noOpen})
	}

	if s := c.running(); s != nil {
		c.setCwd(s, dir)
		fmt.Fprintf(c.cfg.Stdout, "LaForge is running (pid %d, %s)\n  %s\n", s.PID, s.Version, s.link())
		if !*noOpen {
			openBrowser(s.link())
		}
		return nil
	}
	serveArgs := []string{"serve", "--port", strconv.Itoa(*port), "--cwd", dir}
	if *foreground {
		if !*noOpen {
			serveArgs = append(serveArgs, "--open")
		}
		return c.serve(serveArgs[1:])
	}
	if err := os.MkdirAll(c.home, 0o700); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(c.home, "forge.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	if old, err := c.readState(); err == nil && processAlive(old.PID) {
		return fmt.Errorf("a forge (pid %d) holds %s but does not answer on port %d; stop it (kill %d) or check %s", old.PID, c.statePath(), old.Port, old.PID, logPath)
	}
	_ = os.Remove(c.statePath())
	child := exec.Command(exe, append(append([]string{}, c.cfg.Reexec...), serveArgs...)...)
	child.Stdout, child.Stderr, child.Stdin = logFile, logFile, nil
	child.Dir = dir
	detach(child)
	if err := child.Start(); err != nil {
		return err
	}
	pid := child.Process.Pid
	_ = child.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.running(); s != nil && s.PID == pid {
			fmt.Fprintf(c.cfg.Stdout, "LaForge is up (pid %d)\n  %s\n", s.PID, s.link())
			if !*noOpen {
				openBrowser(s.link())
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("the forge did not start; see %s", logPath)
}

func (c *cmd) setCwd(s *State, dir string) {
	raw, _ := json.Marshal(map[string]string{"cwd": dir})
	req, _ := http.NewRequest(http.MethodPut, s.URL+"/api/config", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	if resp, err := (&http.Client{Timeout: time.Second}).Do(req); err == nil {
		resp.Body.Close()
	}
}

func (c *cmd) open(args []string) error {
	s := c.running()
	if s == nil {
		return fmt.Errorf("LaForge is not running; start it with %s up", c.cfg.Prog)
	}
	fmt.Fprintln(c.cfg.Stdout, s.link())
	openBrowser(s.link())
	return nil
}

func (c *cmd) status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	s := c.running()
	if line := c.containerLine(); line != "" && !*asJSON {
		fmt.Fprintln(c.cfg.Stdout, line)
		if s == nil {
			return nil
		}
	}
	if s == nil {
		if *asJSON {
			fmt.Fprintln(c.cfg.Stdout, `{"running":false}`)
			return nil
		}
		fmt.Fprintf(c.cfg.Stdout, "LaForge is not running. Start it with %s up\n", c.cfg.Prog)
		return nil
	}
	req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var panes []forge.Pane
	if err := json.NewDecoder(resp.Body).Decode(&panes); err != nil {
		return err
	}
	if *asJSON {
		return json.NewEncoder(c.cfg.Stdout).Encode(map[string]any{"running": true, "pid": s.PID, "url": s.URL, "version": s.Version, "panes": panes})
	}
	fmt.Fprintf(c.cfg.Stdout, "LaForge %s, pid %d, %s, up %s\n", s.Version, s.PID, s.URL, time.Since(s.StartedAt).Round(time.Second))
	if len(panes) == 0 {
		fmt.Fprintln(c.cfg.Stdout, "No panes.")
		return nil
	}
	tw := tabwriter.NewWriter(c.cfg.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tPROFILE\tSTATE\tFOLDER")
	for _, p := range panes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Title, p.Profile, p.State, p.Cwd)
	}
	return tw.Flush()
}

func (c *cmd) down(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	container := fs.Bool("container", false, "stop the forge's container")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	if *container {
		return c.downContainer()
	}
	s := c.running()
	if s == nil {
		fmt.Fprintln(c.cfg.Stdout, "LaForge is not running.")
		return nil
	}
	p, err := os.FindProcess(s.PID)
	if err != nil {
		return err
	}
	if err := terminate(p); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if c.running() == nil {
			fmt.Fprintf(c.cfg.Stdout, "LaForge stopped (pid %d).\n", s.PID)
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("pid %d is still running", s.PID)
}

// serve runs the forge in this process until it is signalled.
func (c *cmd) serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(c.cfg.Stderr)
	port := fs.Int("port", DefaultPort, "port")
	cwd := fs.String("cwd", "", "folder new panes start in")
	openIt := fs.Bool("open", false, "open the browser once listening")
	listen := fs.String("listen", envOr("LAFORGE_LISTEN", "127.0.0.1"), "address to listen on (0.0.0.0 in a container)")
	if err := fs.Parse(args); err != nil {
		return usageError{err}
	}
	if s := c.running(); s != nil {
		return fmt.Errorf("LaForge is already running (pid %d, %s)", s.PID, s.URL)
	}
	if err := os.MkdirAll(c.home, 0o700); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(*listen, strconv.Itoa(*port)))
	if err != nil {
		ln, err = net.Listen("tcp", net.JoinHostPort(*listen, "0"))
		if err != nil {
			return err
		}
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	srv, err := forge.NewServer(forge.Options{Home: c.home, Cwd: *cwd, Version: Version, Token: os.Getenv("LAFORGE_TOKEN")})
	if err != nil {
		return err
	}
	state := State{PID: os.Getpid(), Port: actual, URL: fmt.Sprintf("http://127.0.0.1:%d", actual), Token: srv.Token(), Version: Version, Home: c.home, StartedAt: time.Now().UTC()}
	raw, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(c.statePath(), raw, 0o600); err != nil {
		return err
	}
	defer func() {
		// remove the state only while it is still this process's
		if cur, err := c.readState(); err == nil && cur.PID == os.Getpid() {
			os.Remove(c.statePath())
		}
	}()

	httpServer := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- httpServer.Serve(ln) }()
	fmt.Fprintf(c.cfg.Stdout, "LaForge %s listening on %s (pid %d)\n  %s\n", Version, state.URL, state.PID, state.link())
	if *openIt {
		openBrowser(state.link())
	}
	select {
	case <-ctx.Done():
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	fmt.Fprintln(c.cfg.Stdout, "LaForge stopping: ending every pane")
	srv.Shutdown(3 * time.Second)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func openBrowser(url string) {
	if os.Getenv("LAFORGE_NO_BROWSER") != "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
