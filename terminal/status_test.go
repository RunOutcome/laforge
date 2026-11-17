// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 COR Holdings, LLC

package terminal

import (
	"strings"
	"testing"
	"time"
)

// The reports herdr-core's own parser test sends read the same here
// (ghostty-vt tests.rs program_status_reports_and_title_changes_reach_us).
func TestReportsReadAsHerdrReadsThem(t *testing.T) {
	var sc statusScanner
	got := sc.scan([]byte("\x1b]7501;state=working:app=claude-code:progress=40\x1b\\" +
		"\x1b]7501;state=blocked:kind=permission:id=a/b:msg=QXBwbHk/\x07" +
		"\x1b]7501;state=clear\x1b\\\x1b]7501;state=bogus\x1b\\"))
	if len(got) != 3 {
		t.Fatalf("reports %+v", got)
	}
	if got[0].State != "working" || got[0].App != "claude-code" || got[0].Progress == nil || *got[0].Progress != 40 {
		t.Fatalf("first %+v", got[0])
	}
	if got[1].Kind != "permission" || got[1].ID != "a/b" || got[1].Msg != "Apply?" {
		t.Fatalf("second %+v", got[1])
	}
	if got[2].State != "clear" {
		t.Fatalf("third %+v", got[2])
	}
}

// A report cut anywhere between two reads is found once, whole.
func TestAReportSplitAcrossReadsIsFoundOnce(t *testing.T) {
	stream := "before \x1b[1mbold\x1b[0m \x1b]7501;state=blocked:kind=question:msg=" +
		"V2hpY2ggZmlsZT8=\x1b\\ after \x1b]0;title\x07 end"
	for cut := 0; cut <= len(stream); cut++ {
		var sc statusScanner
		got := append(sc.scan([]byte(stream[:cut])), sc.scan([]byte(stream[cut:]))...)
		if len(got) != 1 || got[0].State != "blocked" || got[0].Kind != "question" || got[0].Msg != "Which file?" {
			t.Fatalf("cut at %d: %+v", cut, got)
		}
	}
	// byte by byte
	var sc statusScanner
	var got []Report
	for i := 0; i < len(stream); i++ {
		got = append(got, sc.scan([]byte{stream[i]})...)
	}
	if len(got) != 1 {
		t.Fatalf("byte by byte: %+v", got)
	}
}

// Fields that mean nothing for the state are dropped; a state LaForge does
// not know is not a report; other OSCs and an unfinished report hold nothing.
func TestReportsAreValidated(t *testing.T) {
	var sc statusScanner
	got := sc.scan([]byte("\x1b]7501;state=working:kind=permission:progress=140\x07" +
		"\x1b]7501;state=idle:progress=3\x07\x1b]7501;nostate\x07\x1b]7501;?\x07\x1b]2;x\x07"))
	if len(got) != 2 || got[0].Kind != "" || got[0].Progress != nil || got[1].Progress != nil {
		t.Fatalf("%+v", got)
	}
	// An endless OSC is let go rather than held forever.
	long := "\x1b]7501;state=working:msg=" + strings.Repeat("A", 3*maxOSC)
	sc = statusScanner{}
	sc.scan([]byte(long))
	if len(sc.pending) > maxOSC {
		t.Fatalf("held %d bytes", len(sc.pending))
	}
}

// Encode writes what parse reads.
func TestEncodeRoundTrips(t *testing.T) {
	p := 7
	r := Report{ID: "x/y", Status: Status{State: "blocked", Kind: "auth", Progress: &p, App: "codex", Title: "Sign in", Msg: "Open the link: ü"}}
	var sc statusScanner
	got := sc.scan([]byte(r.Encode()))
	if len(got) != 1 || got[0].ID != r.ID || got[0].Kind != "auth" || *got[0].Progress != 7 || got[0].Title != r.Title || got[0].Msg != r.Msg {
		t.Fatalf("%+v", got)
	}
}

// The turn counts each time the root leaves working; child records do not
// move it; clear removes a record and its children; the time moves only
// when the root's word changes (herdr-core status.rs, the same rules).
func TestTheStatusBook(t *testing.T) {
	var b statusBook
	now := time.Now()
	at := func(s int) time.Time { return now.Add(time.Duration(s) * time.Second) }
	rep := func(state, id string) Report { return Report{ID: id, Status: Status{State: state}} }
	b.apply(rep("idle", ""), at(0))
	b.apply(rep("working", ""), at(1))
	b.apply(rep("working", ""), at(2))
	if !b.since.Equal(at(1)) {
		t.Fatalf("since %v moved on the same word", b.since)
	}
	b.apply(rep("done", ""), at(3))
	b.apply(rep("working", ""), at(4))
	b.apply(rep("blocked", ""), at(5))
	b.apply(rep("working", "sub"), at(6))
	b.apply(rep("working", "sub/deeper"), at(6))
	b.apply(rep("working", "other"), at(6))
	if b.turn != 2 {
		t.Fatalf("turn %d", b.turn)
	}
	b.apply(rep("clear", "sub"), at(7))
	if len(b.records) != 1 || b.records["other"].State != "working" {
		t.Fatalf("records %+v", b.records)
	}
	b.apply(rep("working", ""), at(8))
	b.reset(at(9))
	if b.turn != 3 || b.root != nil || b.records != nil || !b.reported {
		t.Fatalf("after reset %+v", b)
	}
}

// A program's own OSC 7501 reaches the session's view, a report through
// the API means the same, and the program's exit clears its status.
func TestASessionKeepsWhatItsProgramReports(t *testing.T) {
	posix(t)
	changes := make(chan string, 16)
	session, err := Start("term-status", LaunchSpec{Command: "/bin/sh", Args: []string{"-c",
		`printf '\033]7501;state=working:app=fake\033\\'; read line; printf '\033]7501;state=blocked:kind=permission:msg=UnVuIGl0Pw==\007'; read line`}},
		0, func(s *Session) {
			if st := s.View().Status; st != nil {
				changes <- st.State
			} else {
				changes <- "-"
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	expect := func(want string) {
		t.Helper()
		select {
		case got := <-changes:
			if got != want {
				t.Fatalf("change %q, want %q", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no change to %q", want)
		}
	}
	expect("working")
	if v := session.View(); v.Status.App != "fake" || !v.Reported || v.Since == nil {
		t.Fatalf("view %+v", v.StatusView)
	}
	session.Write([]byte("\n"))
	expect("blocked")
	if v := session.View(); v.Status.Kind != "permission" || v.Status.Msg != "Run it?" || v.Turn != 1 {
		t.Fatalf("view %+v", v.StatusView)
	}
	if changed, err := session.Report(Report{Status: Status{State: "working"}}); !changed || err != nil {
		t.Fatalf("report: %v %v", changed, err)
	}
	expect("working")
	if _, err := session.Report(Report{Status: Status{State: "thinking"}}); err != ErrBadReport {
		t.Fatalf("bad report: %v", err)
	}
	session.Write([]byte("\n"))
	expect("-")
	if !session.Wait(5 * time.Second) {
		t.Fatal("no exit")
	}
	if v := session.View(); v.Status != nil || v.Turn != 2 {
		t.Fatalf("after exit %+v", v.StatusView)
	}
	if _, err := session.Report(Report{Status: Status{State: "idle"}}); err != ErrSessionClosed {
		t.Fatalf("report after exit: %v", err)
	}
}

// A hook's "working" with no output for hookSilence reads as idle (a turn
// the hook never saw end); the program's own "working" is kept as said.
func TestASilentHookWorkingSettlesToIdle(t *testing.T) {
	now := time.Now()
	var b statusBook
	b.apply(Report{Status: Status{State: "working", Source: "hook", App: "claude"}}, now)
	if v := b.view(now, now.Add(hookSilence/2)); v.Status.State != "working" {
		t.Fatalf("too soon: %+v", v.Status)
	}
	if v := b.view(now.Add(hookSilence), now.Add(hookSilence+time.Second)); v.Status.State != "working" {
		t.Fatalf("output came: %+v", v.Status)
	}
	v := b.view(now, now.Add(hookSilence+time.Second))
	if v.Status.State != "idle" || v.Status.App != "claude" || v.Turn != 1 || !v.Since.Equal(now.Add(hookSilence)) {
		t.Fatalf("after silence: %+v turn %d since %v", v.Status, v.Turn, v.Since)
	}
	var own statusBook
	own.apply(Report{Status: Status{State: "working"}}, now)
	if v := own.view(now, now.Add(time.Hour)); v.Status.State != "working" {
		t.Fatalf("program's own: %+v", v.Status)
	}
}
