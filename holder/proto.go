// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

// Package holder keeps a terminal running in a process of its own, so the
// forge can stop, restart or update without ending the programs in its
// panes. Each pane's holder owns the pseudo-terminal, its replay ring and
// its status, and listens on a Unix socket only its user can open; the
// forge reaches it through a Client, which is a terminal.PTY, so the forge
// treats a held pane as it treats one of its own.
package holder

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
)

// Frame types. A frame is one type byte, a big-endian uint32 length, and
// that many bytes of payload.
const (
	// forge → holder
	frameAttach = 'A' // {"from":N}: stream output from offset N
	frameInput  = 'I' // bytes for the terminal
	frameResize = 'R' // {"cols":C,"rows":R}
	frameStop   = 'S' // {"force":bool}
	frameReport = 'P' // a terminal.Report
	frameMeta   = 'M' // the forge's record of the pane, kept as given
	frameForget = 'X' // the forge is done with this pane: end it, then exit

	// holder → forge
	frameView   = 'V' // a View
	frameOutput = 'O' // output bytes
	frameEnd    = 'F' // the output ended: the program exited
)

// maxFrame bounds a frame's payload.
const maxFrame = 4 << 20

func writeFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > maxFrame {
		return errors.New("holder: frame too large")
	}
	head := [5]byte{typ}
	binary.BigEndian.PutUint32(head[1:], uint32(len(payload)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func writeJSONFrame(w io.Writer, typ byte, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(w, typ, raw)
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var head [5]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(head[1:])
	if n > maxFrame {
		return 0, nil, errors.New("holder: frame too large")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return head[0], payload, nil
}
