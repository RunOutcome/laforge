// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package terminal

import (
	"bytes"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// Program status, as a program reports it about itself.
//
// A program (an agent CLI, or a hook it runs) says what it is doing with
// OSC 7501, the Program Status Protocol herdr-core and Herdr read:
//
//	ESC ] 7501 ; state=<word>[:key=value…] (BEL | ESC \)
//
// Keys: state (idle, working, done, blocked, error; clear removes records),
// kind (permission, question or auth; blocked only), progress (0..100;
// working or blocked), id (a child record, "a/b"; empty is the root), app
// (the program's name for itself), title and msg (base64). Unknown keys are
// ignored; a report with an unknown state is dropped. LaForge keeps what was
// said exactly: it never guesses a status from screen text, and a program
// that reports nothing has no status. The same records are what a hook sets
// through the forge's API (Session.Report), so there is one meaning for both.

// Status words.
const (
	StatusIdle    = "idle"
	StatusWorking = "working"
	StatusDone    = "done"
	StatusBlocked = "blocked"
	StatusError   = "error"
	statusClear   = "clear"
)

var statusWords = map[string]bool{StatusIdle: true, StatusWorking: true, StatusDone: true, StatusBlocked: true, StatusError: true, statusClear: true}
var blockedKinds = map[string]bool{"permission": true, "question": true, "auth": true}

// maxRecords bounds the child records one session keeps.
const maxRecords = 256

// maxOSC bounds a status report's length; a longer one is not a report.
const maxOSC = 8 * 1024

// Status is one status record, in the program's own words.
type Status struct {
	State    string `json:"state"`
	Kind     string `json:"kind,omitempty"`
	Progress *int   `json:"progress,omitempty"`
	App      string `json:"app,omitempty"`
	Title    string `json:"title,omitempty"`
	Msg      string `json:"msg,omitempty"`
	// Source is "hook" for a report a hook sent through the API; empty for
	// the program's own OSC 7501.
	Source string `json:"source,omitempty"`
}

// hookSilence is how long a hook's "working" holds with no output from the
// pane. A hook cannot see every way a turn ends (Claude Code runs none when
// Esc interrupts it), but a working agent draws: its spinner and timer keep
// output coming. A program's own report is never second-guessed.
const hookSilence = 20 * time.Second

// Report is a status record and the record it is about ("" is the root).
type Report struct {
	ID string `json:"id,omitempty"`
	Status
}

// Validate reports whether r is a report LaForge keeps: a known state, and
// kind and progress only where they mean something. It normalises them.
func (r *Report) Validate() bool {
	r.State = strings.TrimSpace(r.State)
	if !statusWords[r.State] {
		return false
	}
	if r.Source != "" && r.Source != "hook" {
		r.Source = "hook"
	}
	if r.State != StatusBlocked || !blockedKinds[r.Kind] {
		r.Kind = ""
	}
	if r.Progress != nil && (r.State != StatusWorking && r.State != StatusBlocked || *r.Progress < 0 || *r.Progress > 100) {
		r.Progress = nil
	}
	if len(r.ID) > 256 || len(r.App) > 256 || len(r.Title) > 1024 || len(r.Msg) > 4096 {
		return false
	}
	return true
}

// parseReport reads the payload of an OSC 7501 (what follows "7501;").
func parseReport(payload []byte) (Report, bool) {
	var r Report
	for _, field := range strings.Split(string(payload), ":") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "state":
			r.State = value
		case "kind":
			r.Kind = value
		case "progress":
			if n, err := strconv.Atoi(value); err == nil {
				r.Progress = &n
			}
		case "id":
			r.ID = value
		case "app":
			r.App = value
		case "title":
			r.Title = decode64(value)
		case "msg":
			r.Msg = decode64(value)
		case "src":
			r.Source = value
		}
	}
	return r, r.Validate()
}

func decode64(s string) string {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return strings.ToValidUTF8(string(b), "�")
		}
	}
	return ""
}

// Encode is the report as OSC 7501, terminated by ST.
func (r Report) Encode() string {
	fields := []string{"state=" + r.State}
	add := func(k, v string) {
		if v != "" {
			fields = append(fields, k+"="+v)
		}
	}
	add("id", r.ID)
	add("kind", r.Kind)
	if r.Progress != nil {
		add("progress", strconv.Itoa(*r.Progress))
	}
	add("app", r.App)
	add("title", base64.StdEncoding.EncodeToString([]byte(r.Title)))
	add("msg", base64.StdEncoding.EncodeToString([]byte(r.Msg)))
	add("src", r.Source) // LaForge's own key; other readers ignore it
	return "\x1b]7501;" + strings.Join(fields, ":") + "\x1b\\"
}

// statusScanner finds OSC 7501 reports in a stream that arrives in pieces:
// a report cut between two reads is held until its end arrives. It never
// changes the stream; the reports stay in the output (terminals ignore an
// OSC they do not know).
type statusScanner struct {
	pending []byte // the start of an unfinished OSC 7501, from ESC ]
}

var oscStart = []byte("\x1b]7501;")

// scan returns the reports that end in chunk.
func (sc *statusScanner) scan(chunk []byte) []Report {
	var reports []Report
	data := chunk
	if len(sc.pending) > 0 {
		data = append(sc.pending, chunk...)
		sc.pending = nil
	}
	for len(data) > 0 {
		i := bytes.IndexByte(data, 0x1b)
		if i < 0 {
			return reports
		}
		rest := data[i:]
		if len(rest) < len(oscStart) && bytes.HasPrefix(oscStart, rest) {
			sc.pending = append([]byte(nil), rest...)
			return reports
		}
		if !bytes.HasPrefix(rest, oscStart) {
			data = rest[1:]
			continue
		}
		body := rest[len(oscStart):]
		end, skip := oscEnd(body)
		if end < 0 {
			if len(rest) <= maxOSC {
				sc.pending = append([]byte(nil), rest...)
			}
			return reports
		}
		if r, ok := parseReport(body[:end]); ok {
			reports = append(reports, r)
		}
		data = body[end+skip:]
	}
	return reports
}

// oscEnd finds the terminator of an OSC body: BEL, or ST (ESC \). It returns
// the body's length and the terminator's, or -1 when the body is unfinished.
// Another ESC that is not ST ends the OSC unfinished (as terminals do):
// the body is reported with a zero-length terminator so it is not held.
func oscEnd(body []byte) (int, int) {
	for i, b := range body {
		switch b {
		case 0x07:
			return i, 1
		case 0x1b:
			if i+1 == len(body) {
				return -1, 0
			}
			if body[i+1] == '\\' {
				return i, 2
			}
			return i, 0
		case 0x18, 0x1a: // CAN, SUB abort the sequence
			return i, 1
		}
		if i >= maxOSC {
			return i, 0
		}
	}
	return -1, 0
}

// statusBook is a session's status records.
type statusBook struct {
	root     *Status
	records  map[string]Status
	since    *time.Time // when the root's state last changed
	turn     int        // times the root left working
	reported bool       // the program ever reported
}

// apply records r and reports whether the root's state changed.
func (b *statusBook) apply(r Report, now time.Time) bool {
	before := b.word()
	wasWorking := before == StatusWorking
	b.reported = true
	switch {
	case r.State == statusClear && r.ID == "":
		b.root, b.records = nil, nil
	case r.State == statusClear:
		child := r.ID + "/"
		for id := range b.records {
			if id == r.ID || strings.HasPrefix(id, child) {
				delete(b.records, id)
			}
		}
	case r.ID == "":
		s := r.Status
		b.root = &s
	default:
		if b.records == nil {
			b.records = map[string]Status{}
		}
		if _, exists := b.records[r.ID]; !exists && len(b.records) >= maxRecords-1 {
			for id := range b.records { // drop one; which matters less than the bound
				delete(b.records, id)
				break
			}
		}
		b.records[r.ID] = r.Status
	}
	return b.settle(before, wasWorking, now)
}

// reset forgets every record: the program ended.
func (b *statusBook) reset(now time.Time) bool {
	before := b.word()
	b.root, b.records = nil, nil
	return b.settle(before, before == StatusWorking, now)
}

func (b *statusBook) settle(before string, wasWorking bool, now time.Time) bool {
	after := b.word()
	if wasWorking && after != StatusWorking {
		b.turn++
	}
	if after != before {
		t := now
		b.since = &t
		return true
	}
	return false
}

func (b *statusBook) word() string {
	if b.root == nil {
		return ""
	}
	return b.root.State
}

// StatusView is what a session's view carries about its status.
type StatusView struct {
	Status   *Status           `json:"status,omitempty"`
	Records  map[string]Status `json:"records,omitempty"`
	Since    *time.Time        `json:"statusSince,omitempty"`
	Turn     int               `json:"turn"`
	Reported bool              `json:"reported"`
}

// view is the records as shown; lastOutput settles a silent hook "working".
func (b *statusBook) view(lastOutput, now time.Time) StatusView {
	if b.root != nil && b.root.Source == "hook" && b.root.State == StatusWorking &&
		now.Sub(lastOutput) > hookSilence && b.since != nil && now.Sub(*b.since) > hookSilence {
		quiet := lastOutput
		if b.since.After(quiet) {
			quiet = *b.since
		}
		b.apply(Report{Status: Status{State: StatusIdle, App: b.root.App, Source: "hook"}}, quiet.Add(hookSilence))
	}
	v := StatusView{Since: b.since, Turn: b.turn, Reported: b.reported}
	if b.root != nil {
		s := *b.root
		v.Status = &s
	}
	if len(b.records) > 0 {
		v.Records = make(map[string]Status, len(b.records))
		for k, s := range b.records {
			v.Records[k] = s
		}
	}
	return v
}

// encodeAll is every record as OSC 7501, the root last, so a reader that
// replays it ends with the records as they are now.
func (b *statusBook) encodeAll() string {
	var out strings.Builder
	for id, s := range b.records {
		out.WriteString(Report{ID: id, Status: s}.Encode())
	}
	if b.root != nil {
		out.WriteString(Report{Status: *b.root}.Encode())
	}
	return out.String()
}
