// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeGate is an EnrolmentGate test double: allowed lists which hosts
// are already sourceIp/acceptedIp, enrolled tracks which host TryEnrol
// most recently accepted (and so becomes allowed from then on, exactly
// as device.Registry's own TryEnrol/Allowed pair behaves), and refused
// counts every line handed to Refuse.
type fakeGate struct {
	mu       sync.Mutex
	allowed  map[string]bool
	token    string
	refused  []string
	enrolled []string
}

func (g *fakeGate) Allowed(host string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.allowed[host]
}

func (g *fakeGate) TryEnrol(host string, line []byte) bool {
	if g.token == "" || !bytes.Contains(line, []byte("mikroview-enrol "+g.token)) {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.allowed == nil {
		g.allowed = map[string]bool{}
	}
	g.allowed[host] = true
	g.enrolled = append(g.enrolled, host)
	g.token = "" // single-use, same contract as the real registry
	return true
}

func (g *fakeGate) Refuse(host string, line []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refused = append(g.refused, host)
}

// TestGateAllowsAConfiguredSourceWithNoTokenNeeded is the "no gate, or
// already allowed" fast path: a line from an address the gate already
// treats as allowed reaches the ingest channel unconditionally.
func TestGateAllowsAConfiguredSourceWithNoTokenNeeded(t *testing.T) {
	g := &fakeGate{allowed: map[string]bool{"127.0.0.1": true}}
	SetEnrolmentGate(g)
	t.Cleanup(func() { SetEnrolmentGate(nil) })

	out := make(chan RawMessage, 4)
	addr, stop := serveTCPForTest(t, out)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("hello\n"))

	select {
	case raw := <-out:
		if string(raw.Data) != "hello" {
			t.Errorf("Data = %q, want %q", raw.Data, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the allowed line")
	}
}

// TestGateRefusesAnUnenrolledLineAndKeepsTheConnectionOpen is issue
// #1281's core listener behaviour: a line from an address that is
// neither allowed nor carrying a valid enrolment marker never reaches
// the ingest channel, is counted via Refuse, and -- critically -- the
// connection is not dropped, so a legitimate router that has not yet
// enrolled is not locked out by its own unrecognised lines.
func TestGateRefusesAnUnenrolledLineAndKeepsTheConnectionOpen(t *testing.T) {
	g := &fakeGate{token: "abcdefghijklmnopqrst"}
	SetEnrolmentGate(g)
	t.Cleanup(func() { SetEnrolmentGate(nil) })

	out := make(chan RawMessage, 4)
	addr, stop := serveTCPForTest(t, out)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("ordinary firewall line\n"))
	conn.Write([]byte(`<30>Jan  1 00:00:00 r mikroview-enrol abcdefghijklmnopqrst` + "\n"))
	conn.Write([]byte("now allowed\n"))

	select {
	case raw := <-out:
		if string(raw.Data) != "now allowed" {
			t.Errorf("Data = %q, want the line sent after enrolment", raw.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the post-enrolment line")
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.refused) != 1 {
		t.Errorf("refused = %v, want exactly the one ordinary line refused before enrolment", g.refused)
	}
	if len(g.enrolled) != 1 {
		t.Errorf("enrolled = %v, want exactly one enrolment", g.enrolled)
	}
}

// TestNilGateAllowsEverything pins the default: a process that never
// calls SetEnrolmentGate (every test but these, and any build that
// somehow starts without main.go wiring one) behaves exactly as this
// package always did before #1281.
func TestNilGateAllowsEverything(t *testing.T) {
	SetEnrolmentGate(nil)

	out := make(chan RawMessage, 4)
	addr, stop := serveTCPForTest(t, out)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.Write([]byte("anything at all\n"))

	select {
	case raw := <-out:
		if string(raw.Data) != "anything at all" {
			t.Errorf("Data = %q, want the line through unconditionally", raw.Data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the line")
	}
}
