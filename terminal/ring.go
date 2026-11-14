// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package terminal

import "sync"

// ring is a fixed-size byte buffer holding the most recent terminal output. It
// is what lets a browser refresh restore a tile's contents without the Outcome
// server keeping an unbounded transcript in memory.
type ring struct {
	mutex   sync.RWMutex
	data    []byte
	size    int
	start   int
	length  int
	written int64
}

func newRing(size int) *ring {
	if size <= 0 {
		size = 256 * 1024
	}
	return &ring{data: make([]byte, size), size: size}
}

func (buffer *ring) Write(chunk []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	buffer.written += int64(len(chunk))
	if len(chunk) >= buffer.size {
		copy(buffer.data, chunk[len(chunk)-buffer.size:])
		buffer.start = 0
		buffer.length = buffer.size
		return len(chunk), nil
	}
	for _, value := range chunk {
		position := (buffer.start + buffer.length) % buffer.size
		buffer.data[position] = value
		if buffer.length < buffer.size {
			buffer.length++
		} else {
			buffer.start = (buffer.start + 1) % buffer.size
		}
	}
	return len(chunk), nil
}

// Snapshot returns a copy of the retained output in order.
func (buffer *ring) Snapshot() []byte {
	buffer.mutex.RLock()
	defer buffer.mutex.RUnlock()
	out := make([]byte, buffer.length)
	for index := 0; index < buffer.length; index++ {
		out[index] = buffer.data[(buffer.start+index)%buffer.size]
	}
	return out
}

// Written reports the total bytes ever produced, which the health panel uses to
// spot output storms.
func (buffer *ring) Written() int64 {
	buffer.mutex.RLock()
	defer buffer.mutex.RUnlock()
	return buffer.written
}
