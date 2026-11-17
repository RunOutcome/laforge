// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package holder

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/RunOutcome/laforge/terminal"
)

// Client is the forge's connection to one holder. It is a terminal.PTY:
// reads are the held terminal's output (its retained ring first, then its
// status records, then live output), writes are input, Wait is the
// program's exit.
type Client struct {
	c  net.Conn
	mu sync.Mutex // one writer at a time

	out      *io.PipeReader
	outW     *io.PipeWriter
	first    View
	viewMu   sync.Mutex
	view     View
	ended    chan struct{}
	endOnce  sync.Once
	lost     error
	exits    chan int
	exitOnce sync.Once
	onView   func(View)
}

// ErrHolderLost is the end of a pane whose holder went away (killed, or the
// machine restarted) rather than reporting its program's exit.
var ErrHolderLost = errors.New("the pane's holder process is gone")

// Dial connects to pane id's holder in dir and starts streaming its output
// from the beginning of what it retains. onView, when not nil, is called
// with every view the holder sends after the first.
func Dial(dir, id string, onView func(View)) (*Client, error) {
	nc, err := net.DialTimeout("unix", Socket(dir, id), 2*time.Second)
	if err != nil {
		return nil, err
	}
	_ = nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	typ, payload, err := readFrame(nc)
	if err != nil || typ != frameView {
		nc.Close()
		if err == nil {
			err = errors.New("holder: no view")
		}
		return nil, err
	}
	_ = nc.SetReadDeadline(time.Time{})
	var v View
	if err := json.Unmarshal(payload, &v); err != nil {
		nc.Close()
		return nil, err
	}
	r, w := io.Pipe()
	c := &Client{c: nc, out: r, outW: w, first: v, view: v, ended: make(chan struct{}), exits: make(chan int, 1), onView: onView}
	if v.AgentExit != nil {
		c.exitOnce.Do(func() { c.exits <- *v.AgentExit })
	}
	if err := c.send(frameAttach, map[string]int64{"from": 0}); err != nil {
		nc.Close()
		return nil, err
	}
	go c.read()
	return c, nil
}

// First is the view the holder sent when the client connected.
func (c *Client) First() View { return c.first }

// View is the holder's latest view.
func (c *Client) View() View {
	c.viewMu.Lock()
	defer c.viewMu.Unlock()
	return c.view
}

// AgentExit delivers the agent's exit code once the wrapper reports it.
func (c *Client) AgentExit() <-chan int { return c.exits }

func (c *Client) read() {
	defer c.finish(ErrHolderLost)
	for {
		typ, payload, err := readFrame(c.c)
		if err != nil {
			return
		}
		switch typ {
		case frameOutput:
			if _, err := c.outW.Write(payload); err != nil {
				return
			}
		case frameView:
			var v View
			if json.Unmarshal(payload, &v) != nil {
				continue
			}
			c.viewMu.Lock()
			c.view = v
			c.viewMu.Unlock()
			if v.AgentExit != nil {
				code := *v.AgentExit
				c.exitOnce.Do(func() { c.exits <- code })
			}
			if c.onView != nil {
				c.onView(v)
			}
		case frameEnd:
			c.finish(nil)
			return
		}
	}
}

func (c *Client) finish(err error) {
	c.endOnce.Do(func() {
		c.lost = err
		c.outW.Close()
		close(c.ended)
	})
}

func (c *Client) send(typ byte, v any) error {
	var payload []byte
	switch t := v.(type) {
	case []byte:
		payload = t
	case nil:
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		payload = raw
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return writeFrame(c.c, typ, payload)
}

func (c *Client) Read(p []byte) (int, error) { return c.out.Read(p) }

func (c *Client) Write(p []byte) (int, error) {
	select {
	case <-c.ended:
		return 0, io.EOF
	default:
	}
	if err := c.send(frameInput, append([]byte(nil), p...)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close is a no-op: the session closes its terminal when the program ends,
// but the forge still owes the holder its Forget.
func (c *Client) Close() error { return nil }

func (c *Client) Resize(columns, rows uint16) error {
	return c.send(frameResize, map[string]uint16{"cols": columns, "rows": rows})
}

func (c *Client) Wait() (int, error) {
	<-c.ended
	if c.lost != nil {
		return -1, c.lost
	}
	v := c.View()
	if v.Session.ExitCode != nil {
		return *v.Session.ExitCode, nil
	}
	return -1, nil
}

func (c *Client) Signal(force bool) error { return c.send(frameStop, map[string]bool{"force": force}) }

func (c *Client) PID() int { return c.View().Session.PID }

// Report passes a status report on to the holder, which keeps it with the
// program's own (so it outlives this forge).
func (c *Client) Report(r terminal.Report) error { return c.send(frameReport, r) }

// SetMeta gives the holder the forge's record of the pane.
func (c *Client) SetMeta(meta json.RawMessage) error { return c.send(frameMeta, []byte(meta)) }

// Forget tells the holder the forge is done with the pane (it ends the
// program if it still runs, then exits) and closes the connection.
func (c *Client) Forget() {
	_ = c.send(frameForget, nil)
	c.c.Close()
}

// Detach closes the connection and leaves the holder running.
func (c *Client) Detach() { c.c.Close() }

// Ended is closed when the program's end (or the holder's loss) is known.
func (c *Client) Ended() <-chan struct{} { return c.ended }
