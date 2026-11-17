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
	"github.com/RunOutcome/laforge/holder"
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
	case "restart":
		err = c.restart(args[1:])
	case "run":
		err = c.run(args[1:])
	case "ls":
		err = c.ls(args[1:])
	case "send":
		err = c.send(args[1:])
	case "read":
		err = c.read(args[1:])
	case "wait":
		err = c.wait(args[1:])
	case "events":
		err = c.events(args[1:])
	case "close":
		err = c.closePane(args[1:])
	case "answer":
		err = c.answer(args[1:])
	case "share":
		err = c.share(args[1:])
	case "skin", "skins":
		err = c.skin(args[1:])
	case "--skill", "skill":
		fmt.Fprint(cfg.Stdout, skill(cfg.Prog))
	case "holder": // started by the forge: one pane's holder process
		err = holder.RunFromStdin(c.holdersDir(), cfg.Stdin)
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
		var code exitCode
		if errors.As(err, &code) {
			return int(code)
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
  %[1]s restart      restart the forge (after an update); panes keep running
  %[1]s down [--container]
                     stop the forge and every pane in it (they can be
                     resumed after the next up)
  %[1]s run [--bg] [--name N] PROFILE|COMMAND [ARGS…]
                     run a pane (from a pane: a child that ends with it)
  %[1]s ls | send PANE TEXT | read PANE | wait PANE… | close PANE
                     list, type into, read, wait for and close panes
  %[1]s answer PANE allow|deny
                     answer the permission a pane's agent waits on
  %[1]s share PANE [--for 2h] | share --list | share --revoke TOKEN
                     a read-only link to watch one pane
  %[1]s skin [list] | skin install SOURCE | skin update [NAME]
  %[1]s skin use NAME | skin remove NAME
                     how the forge looks; SOURCE is a folder, owner/repo[@ref]
                     or a github.com link, holding one skin or many
                     (docs/skins.md to make one)
  %[1]s skin browse [COLLECTION] | skin install official[/NAME]…
  %[1]s skin collections [add LINK | remove ID]
                     get skins from collections: official (released with
                     LaForge), library (more), or any you add by link
  %[1]s --skill      how an agent uses LaForge, in one read
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
	if *foreground {
		serveArgs := []string{"--port", strconv.Itoa(*port), "--cwd", dir}
		if !*noOpen {
			serveArgs = append(serveArgs, "--open")
		}
		return c.serve(serveArgs)
	}
	s, err := c.startDetached(*port, dir, "")
	if err != nil {
		return err
	}
	fmt.Fprintf(c.cfg.Stdout, "LaForge is up (pid %d)\n  %s\n", s.PID, s.link())
	if !*noOpen {
		openBrowser(s.link())
	}
	return nil
}

// startDetached starts `serve` in the background and waits for it to answer.
// token, when given, is the forge's token (a restart keeps the old one).
func (c *cmd) startDetached(port int, dir, token string) (*State, error) {
	serveArgs := []string{"serve", "--port", strconv.Itoa(port), "--cwd", dir}
	if err := os.MkdirAll(c.home, 0o700); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(c.home, "forge.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	old, err := c.readState()
	if err == nil && processAlive(old.PID) {
		return nil, fmt.Errorf("a forge (pid %d) holds %s but does not answer on port %d; stop it (kill %d) or check %s", old.PID, c.statePath(), old.Port, old.PID, logPath)
	}
	if token == "" && err == nil {
		token = old.Token // a forge that died: its browser tabs keep working
	}
	child := exec.Command(exe, append(append([]string{}, c.cfg.Reexec...), serveArgs...)...)
	child.Stdout, child.Stderr, child.Stdin = logFile, logFile, nil
	child.Dir = dir
	if token != "" {
		child.Env = append(os.Environ(), "LAFORGE_TOKEN="+token)
	}
	detach(child)
	if err := child.Start(); err != nil {
		return nil, err
	}
	pid := child.Process.Pid
	_ = child.Process.Release()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.running(); s != nil && s.PID == pid {
			return s, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("the forge did not start; see %s", logPath)
}

// restart replaces the running forge with a new one (this binary, after an
// update) on the same port and token. Held panes keep running and the new
// forge adopts them; an open browser reconnects by itself.
func (c *cmd) restart(args []string) error {
	s := c.running()
	if s == nil {
		return fmt.Errorf("LaForge is not running; start it with %s up", c.cfg.Prog)
	}
	f := &forgeClient{state: s, http: &http.Client{Timeout: 3 * time.Second}}
	var conf struct {
		Cwd string `json:"cwd"`
	}
	_ = f.do(http.MethodGet, "/api/config", nil, &conf)
	if err := f.do(http.MethodPost, "/api/restart", nil, nil); err != nil {
		var api *apiError
		if errors.As(err, &api) && api.status == http.StatusNotFound {
			return fmt.Errorf("the running forge (%s) cannot restart with its panes kept; stop it with %s down and start it again", s.Version, c.cfg.Prog)
		}
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for processAlive(s.PID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(s.PID) {
		return fmt.Errorf("pid %d did not stop", s.PID)
	}
	dir := conf.Cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	next, err := c.startDetached(s.Port, dir, s.Token)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.cfg.Stdout, "LaForge restarted (pid %d → %d, %s), panes kept\n  %s\n", s.PID, next.PID, next.Version, next.link())
	return nil
}

func (c *cmd) holdersDir() string { return filepath.Join(c.home, "holders") }

// holders is the launcher the forge starts panes with: nil (panes in the
// forge's own process) where holders are not supported or LAFORGE_HOLDERS=0.
func (c *cmd) holders() *holder.Launcher {
	if !holder.Supported() || os.Getenv("LAFORGE_HOLDERS") == "0" {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	command := append(append([]string{exe}, c.cfg.Reexec...), "holder")
	return &holder.Launcher{Dir: c.holdersDir(), Command: command}
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
	fmt.Fprintln(tw, "ID\tTITLE\tPROFILE\tSTATE\tAGENT\tFOLDER")
	for _, p := range panes {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.Title, p.Profile, p.State, agentLine(p), p.Cwd)
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
	srv, err := forge.NewServer(forge.Options{Home: c.home, Cwd: *cwd, Version: Version, Token: os.Getenv("LAFORGE_TOKEN"), Holders: c.holders()})
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
	case <-srv.Restarts():
		fmt.Fprintln(c.cfg.Stdout, "LaForge restarting: held panes keep running")
		srv.Detach()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return nil
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

// agentLine is what a pane's program says it is doing, for la status.
func agentLine(p forge.Pane) string {
	if p.Status == nil {
		return "-"
	}
	word := p.Status.State
	if word == "done" && p.Seen {
		word = "idle"
	}
	if p.Status.Msg != "" {
		msg := p.Status.Msg
		if len(msg) > 48 {
			msg = msg[:47] + "…"
		}
		word += ": " + msg
	}
	return word
}
