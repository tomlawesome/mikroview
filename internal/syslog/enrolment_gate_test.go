// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/device"
)

// fakeGate is an EnrolmentGate test double: allowed lists which hosts
// are already sourceIp/acceptedIp, enrolled tracks which host TryEnrol
// most recently accepted (and so becomes allowed from then on, exactly
// as device.Registry's own TryEnrol/Allowed pair behaves), refused
// counts every line handed to Refuse, expecting is the single host
// AcceptsConnectionFrom answers true for (a real registry ties this to
// the address a pending token was minted for; the fake takes it as a
// field instead so a test can set it independently), and refusedConns
// counts every host handed to RefuseConnection.
type fakeGate struct {
	mu           sync.Mutex
	allowed      map[string]bool
	token        string
	expecting    string
	refused      []string
	refusedConns []string
	enrolled     []string
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

func (g *fakeGate) EnrolLine(line []byte) bool {
	return bytes.Contains(line, []byte("mikroview-enrol "))
}

func (g *fakeGate) Refuse(host string, line []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refused = append(g.refused, host)
}

func (g *fakeGate) AcceptsConnectionFrom(host string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.expecting != "" && g.expecting == host
}

func (g *fakeGate) RefuseConnection(host string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.refusedConns = append(g.refusedConns, host)
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
	// expecting 127.0.0.1 because this test's connection is from an
	// address with nothing yet Allowed -- the connection gate
	// (TestConnectionFromUnknownAddressWithNoTokenPendingIsRefusedAtAccept,
	// below) would otherwise refuse it before a single line is read.
	// Since #1291 the gate opens for the one address a token was minted
	// for, so the fake has to name it rather than wave everyone through.
	g := &fakeGate{token: "abcdefghijklmnopqrst", expecting: "127.0.0.1"}
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

// TestConnectionFromUnknownAddressWithNoTokenPendingIsRefusedAtAccept is
// issue #1281's connection gate: a source that is neither Allowed nor
// covered by any pending enrolment token never gets past accept -- the
// server closes the connection immediately, before it can send a
// single byte (let alone a TLS ClientHello or a syslog line), and the
// refusal is recorded via RefuseConnection rather than Refuse.
func TestConnectionFromUnknownAddressWithNoTokenPendingIsRefusedAtAccept(t *testing.T) {
	g := &fakeGate{}
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

	// A refused connection is closed immediately by the server; the
	// client observes this as a read error, the same convention
	// TestServeTCPRejectsBeyondConnectionLimit uses for the connection
	// cap's own rejection.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, readErr := conn.Read(buf); readErr == nil {
		t.Fatal("expected the connection to be closed by the server, got a successful read")
	}

	select {
	case raw := <-out:
		t.Errorf("got %+v on the ingest channel, want nothing from a connection refused at accept", raw)
	case <-time.After(100 * time.Millisecond):
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.refusedConns) != 1 || g.refusedConns[0] != "127.0.0.1" {
		t.Errorf("refusedConns = %v, want exactly [%q]", g.refusedConns, "127.0.0.1")
	}
	if len(g.refused) != 0 {
		t.Errorf("refused (line-level) = %v, want none -- this was refused at accept, before any line", g.refused)
	}
}

// TestConnectionFromAllowedAddressIsAcceptedWithNoTokenPending is the
// declared/enrolled half of issue #1281's connection gate: an address
// already Allowed connects normally even when no token is pending,
// so an ordinary already-enrolled router is never affected by whether
// some other device happens to have a token pending.
func TestConnectionFromAllowedAddressIsAcceptedWithNoTokenPending(t *testing.T) {
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

// TestGateRefusesATokenRedeemedFromAnAddressAnotherDeviceHolds drives the
// real registry through gateAllows, not TryEnrol directly: Allowed used
// to short-circuit the gate before TryEnrol ran, so a token minted for
// an address some other router already held was never refused -- the
// marker passed as the holder's traffic, the claimant stayed pending
// with nothing under refused senders, and the operator had nothing to
// go on.
func TestGateRefusesATokenRedeemedFromAnAddressAnotherDeviceHolds(t *testing.T) {
	r := device.NewRegistry(nil)
	now := time.Now()
	for _, id := range []string{"held", "claimant"} {
		if _, err := r.Create(id, id, now); err != nil {
			t.Fatal(err)
		}
	}
	token, _, err := r.MintEnrolment("held", "10.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.TryEnrol("10.0.0.1", []byte("mikroview-enrol "+token)) {
		t.Fatal("setup: held did not enrol")
	}
	SetEnrolmentGate(r)
	t.Cleanup(func() { SetEnrolmentGate(nil) })

	token, _, err = r.MintEnrolment("claimant", "10.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(`<30>Jan  1 00:00:00 router mikroview-enrol ` + token)
	if gateAllows("10.0.0.1", line) {
		t.Error("gateAllows() = true, want the marker refused rather than passed as held's traffic")
	}
	for _, info := range r.List() {
		if info.ID == "claimant" && info.AcceptedIP != "" {
			t.Errorf("claimant AcceptedIP = %q, want unenrolled", info.AcceptedIP)
		}
	}
	refused := r.Refused()
	if len(refused) != 1 || refused[0].Address != "10.0.0.1" {
		t.Errorf("Refused() = %+v, want the contested address counted once", refused)
	}
	// An ordinary line from the holder still passes.
	if !gateAllows("10.0.0.1", []byte("<30>Jan  1 00:00:00 router firewall,info fwd: in:ether1")) {
		t.Error("gateAllows() = false for held's own traffic")
	}
}
