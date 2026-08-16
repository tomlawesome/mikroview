// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"context"
	"io"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"
)

// serveTCPForTest binds an ephemeral port and serves it, returning the
// address and a stop func -- the same setup the tests above do inline.
func serveTCPForTest(t *testing.T, out chan RawMessage) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ServeTCP(ctx, ln, out)
	return ln.Addr().String(), cancel
}

func TestServeTCPFramesOnNewlines(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 4)
	go ServeTCP(ctx, ln, out)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("line one\nline two\n")); err != nil {
		t.Fatal(err)
	}

	want := []string{"line one", "line two"}
	for _, w := range want {
		select {
		case raw := <-out:
			if string(raw.Data) != w {
				t.Errorf("Data = %q, want %q", raw.Data, w)
			}
			if raw.SourceIP != "127.0.0.1" {
				t.Errorf("SourceIP = %q, want 127.0.0.1", raw.SourceIP)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for line %q", w)
		}
	}
}

func TestServeTCPHandlesMultipleConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 8)
	go ServeTCP(ctx, ln, out)

	for i := 0; i < 2; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("hello\n")); err != nil {
			t.Fatal(err)
		}
	}

	received := 0
	for received < 2 {
		select {
		case <-out:
			received++
		case <-time.After(2 * time.Second):
			t.Fatalf("only received %d/2 messages", received)
		}
	}
}

func TestServeTCPRejectsBeyondConnectionLimit(t *testing.T) {
	orig := maxTCPConnections
	maxTCPConnections = 1
	defer func() { maxTCPConnections = orig }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 4)
	go ServeTCP(ctx, ln, out)

	// Holds its slot open (never sends a full line) so the second
	// connection below has to be rejected rather than just queued behind
	// a fast-finishing first one.
	holder, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if _, err := holder.Write([]byte("keep me open")); err != nil {
		t.Fatal(err)
	}

	// Give the accept loop time to register the first connection's slot
	// before dialing the second.
	time.Sleep(50 * time.Millisecond)

	rejected, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer rejected.Close()

	// A rejected connection is closed immediately by the server; the
	// client observes this as EOF on read (possibly after the write
	// below succeeds into the OS send buffer before the close lands).
	rejected.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, readErr := rejected.Read(buf)
	if readErr == nil {
		t.Fatal("expected the over-limit connection to be closed by the server, got a successful read")
	}
}

func TestServeTCPClosesIdleConnection(t *testing.T) {
	origTimeoutNS := tcpIdleTimeoutNS.Swap(int64(100 * time.Millisecond))
	defer tcpIdleTimeoutNS.Store(origTimeoutNS)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 4)
	go ServeTCP(ctx, ln, out)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Send nothing and wait past the idle timeout -- the server should
	// close its side, which this end observes as EOF.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	if readErr == nil {
		t.Fatal("expected the idle connection to be closed by the server, got a successful read")
	}
}

// TestServeTCPDoesNotLeakGoroutinesOnOrdinaryDisconnect proves
// handleTCPConn's watcher goroutine actually exits when a connection
// ends normally, not only on process/ctx shutdown -- before the fix,
// every idle-timeout disconnect (exercised here) leaked one goroutine
// forever, since the watcher only ever selected on ctx.Done().
func TestServeTCPDoesNotLeakGoroutinesOnOrdinaryDisconnect(t *testing.T) {
	origTimeoutNS := tcpIdleTimeoutNS.Swap(int64(20 * time.Millisecond))
	defer tcpIdleTimeoutNS.Store(origTimeoutNS)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 4)
	go ServeTCP(ctx, ln, out)

	baseline := goroutineCountSettled(t)

	const n = 20
	for i := 0; i < n; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		// Idle until the server closes its side (20ms timeout above),
		// then close this end too -- an ordinary, non-adversarial
		// disconnect, the routine case this bug affected.
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		conn.Read(make([]byte, 1))
		conn.Close()
	}

	after := goroutineCountSettled(t)
	if after > baseline+2 { // small slack for scheduler/GC noise, not per-connection growth
		t.Errorf("goroutine count grew from %d to %d after %d ordinary disconnects -- suggests a per-connection leak", baseline, after, n)
	}
}

// goroutineCountSettled polls runtime.NumGoroutine() until it stops
// shrinking (or a timeout), since a just-finished goroutine's stack
// isn't necessarily reclaimed the instant it returns.
func goroutineCountSettled(t *testing.T) int {
	t.Helper()
	last := runtime.NumGoroutine()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		runtime.GC()
		cur := runtime.NumGoroutine()
		if cur >= last {
			return cur
		}
		last = cur
	}
	return last
}

// TestPerSourceConnectionCap is the regression test for single-source
// slot exhaustion. The global cap alone is exhaustible by one host: the
// idle timeout resets on every line, so an attacker trickling traffic
// holds every slot indefinitely and locks out all real routers -- the
// tool goes blind while still looking healthy.
func TestPerSourceConnectionCap(t *testing.T) {
	prev := maxTCPConnectionsPerSource.Load()
	maxTCPConnectionsPerSource.Store(3)
	t.Cleanup(func() { maxTCPConnectionsPerSource.Store(prev) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 128)
	go ServeTCP(ctx, ln, out)

	addr := ln.Addr().String()
	var held []net.Conn
	t.Cleanup(func() {
		for _, c := range held {
			c.Close()
		}
	})

	// Open more than the per-source cap from one address. Each holds the
	// connection open without completing a line, exactly as the
	// exhaustion attack would.
	for i := 0; i < perSourceLimit()+4; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			continue
		}
		held = append(held, c)
	}

	// The listener closes connections past the cap. Confirm at least one
	// was refused by finding a connection that reads EOF promptly.
	refused := 0
	for _, c := range held {
		c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 1)
		if _, err := c.Read(buf); err == io.EOF {
			refused++
		}
	}
	if refused == 0 {
		t.Errorf("opened %d connections from one source with a cap of %d, but none were refused",
			len(held), perSourceLimit())
	}
}

// TestTCPUnterminatedMessageIsIngested is the case every other test in
// this file misses, and the one that matters: RouterOS sends each
// message as a bare payload with no trailing newline and no octet
// count.
//
// The listener previously read with a bufio.Scanner on the default
// ScanLines split, so Scan() blocked on a delimiter that never arrived
// and the connection was accepted, held, and silently discarded --
// zero events from a real router (#202). The tests passed throughout,
// because they all fed newline-delimited input, which is what a
// well-behaved sender does and not what RouterOS does.
func TestTCPUnterminatedMessageIsIngested(t *testing.T) {
	out := make(chan RawMessage, 8)
	addr, stop := serveTCPForTest(t, out)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Exactly what a router puts on the wire: no \n, no length prefix.
	if _, err := conn.Write([]byte("firewall,info bare-message-no-newline")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case msg := <-out:
		if got := string(msg.Data); got != "firewall,info bare-message-no-newline" {
			t.Errorf("Data = %q, want the message verbatim", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("an un-terminated message was never ingested -- this is #202, and it means a real RouterOS router feeds nothing over TCP")
	}
}

// TestTCPNewlineDelimitedStillWorks guards the other half. A
// conventional syslog sender does terminate its lines and may pack
// several into one write; fixing RouterOS's shape must not regress it.
func TestTCPNewlineDelimitedStillWorks(t *testing.T) {
	out := make(chan RawMessage, 8)
	addr, stop := serveTCPForTest(t, out)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("first\nsecond\r\nthird\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	for _, want := range []string{"first", "second", "third"} {
		select {
		case msg := <-out:
			if got := string(msg.Data); got != want {
				t.Errorf("Data = %q, want %q", got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("never received %q", want)
		}
	}
}

// readOneMessage dials addr, runs send (which must deliver exactly one
// logical message down the resulting connection), and returns the first
// RawMessage that arrives on out. Shared by the #415 regression tests
// below, which need the same dial-send-receive shape for both a
// fragmented and an unfragmented delivery.
func readOneMessage(t *testing.T, addr string, out <-chan RawMessage, send func(conn net.Conn)) RawMessage {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	send(conn)

	select {
	case msg := <-out:
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the message")
		return RawMessage{}
	}
}

// TestTCPFragmentedMessageReassemblesAcrossPartialReads is the
// regression test for #415: the read loop used to recognise a message
// as continuing into the next read only when the current one exactly
// filled the 64KB buffer. A message well under that size which still
// arrived fragmented across several non-full reads was never
// recognised as one message -- each fragment, carrying no framing of
// its own, was parsed as its own line. Observed concretely against the
// real TLS listener: a single ~65KB line produced 3 stray undecoded
// events instead of one real one.
//
// This fails on the pre-fix code: none of the five writes below fill
// the 64KB buffer, so each landed as its own garbage event rather than
// the read loop ever recognising a continuation.
func TestTCPFragmentedMessageReassemblesAcrossPartialReads(t *testing.T) {
	out := make(chan RawMessage, 8)
	addr, stop := serveTCPForTest(t, out)
	defer stop()

	// Long enough that five roughly-equal pieces are each a plausible
	// single TCP/TLS segment on their own -- the same shape a long
	// address-list or NAT-detail line takes in the wild -- while
	// staying comfortably under maxTCPMessageBytes so the oversized
	// path (covered separately below) never engages.
	line := "D|frag-test| forward: in:ether1 out:bridge1, proto TCP (SYN), " +
		"198.51.100.5:1024->203.0.113.9:443, detail=" + strings.Repeat("x", 20000)

	// Baseline: the same line delivered whole, in one write. What the
	// fragmented delivery below must match byte for byte.
	baseline := readOneMessage(t, addr, out, func(conn net.Conn) {
		if _, err := conn.Write([]byte(line + "\n")); err != nil {
			t.Fatalf("baseline write: %v", err)
		}
	})
	if string(baseline.Data) != line {
		t.Fatalf("baseline itself is wrong: got %d bytes, want %d matching the source line", len(baseline.Data), len(line))
	}

	// The same line, delivered across several writes with a short pause
	// between each -- enough for the kernel to hand the reader what has
	// arrived so far as its own short Read(), which is exactly the
	// shape that fragmented a single message into several reads for the
	// real bug. None of these individual reads fill the 64KB buffer,
	// and none but the last contains the terminating newline.
	fragmented := readOneMessage(t, addr, out, func(conn net.Conn) {
		full := line + "\n"
		const parts = 5
		chunkLen := len(full)/parts + 1
		for i := 0; i < len(full); i += chunkLen {
			end := i + chunkLen
			if end > len(full) {
				end = len(full)
			}
			if _, err := conn.Write([]byte(full[i:end])); err != nil {
				t.Fatalf("fragment write: %v", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
	})

	if !bytes.Equal(fragmented.Data, baseline.Data) {
		t.Errorf("fragmented delivery produced %d bytes, want %d bytes byte-identical to the unfragmented baseline",
			len(fragmented.Data), len(baseline.Data))
	}

	select {
	case extra := <-out:
		t.Errorf("expected exactly one event for the fragmented message, got an extra %d-byte event -- the message was shredded into more than one", len(extra.Data))
	case <-time.After(200 * time.Millisecond):
	}
}

// TestTCPOversizedMessageFragmentedAcrossManyReadsStaysBounded is the
// oversize-path counterpart to the regression test above. It targets
// specifically what #415 changed about this path: pending now crosses
// the maxTCPMessageBytes cap by accumulation across several reads,
// rather than by one read happening to exactly fill the buffer the way
// TestOversizedMessageYieldsOneEventNotSeveral's net.Pipe delivery does.
// The lead-up to the cap here is deliberately fragmented across several
// small real-socket writes to exercise that accumulation; once the
// message is already known to be over the limit, discarding its
// continuation is unchanged from before #415 (see handleTCPConn's
// comment) and is exactly what the net.Pipe test already covers, so
// that part is sent as a single write here rather than re-testing it.
func TestTCPOversizedMessageFragmentedAcrossManyReadsStaysBounded(t *testing.T) {
	out := make(chan RawMessage, 32)
	addr, stop := serveTCPForTest(t, out)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The lead-up to the cap, with no newline anywhere -- the shape a
	// RouterOS sender would produce, just far too large -- delivered in
	// several small paced writes so the kernel hands the reader
	// genuinely separate, non-full reads and pending has to cross
	// maxTCPMessageBytes by accumulation, not by one read filling the
	// buffer.
	leadUp := strings.Repeat("A", maxTCPMessageBytes)
	const parts = 6
	chunkLen := len(leadUp)/parts + 1
	for i := 0; i < len(leadUp); i += chunkLen {
		end := i + chunkLen
		if end > len(leadUp) {
			end = len(leadUp)
		}
		if _, err := conn.Write([]byte(leadUp[i:end])); err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The continuation past the cap -- still part of the same
	// over-limit message, discarded rather than parsed. One write, since
	// the discard path's own read-boundary handling is unchanged by
	// #415 and already covered by the net.Pipe test above.
	if _, err := conn.Write([]byte(strings.Repeat("A", 20000))); err != nil {
		t.Fatalf("continuation write: %v", err)
	}
	// A pause before the normal message, so it lands as its own read
	// rather than risking coalescing with the oversized message's final
	// discarded fragment.
	time.Sleep(20 * time.Millisecond)
	if _, err := conn.Write([]byte("D|wan-in|forward: proto TCP, 192.0.2.1:1->198.51.100.1:80\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got []string
	deadline := time.After(5 * time.Second)
	for len(got) < 2 {
		select {
		case m := <-out:
			got = append(got, string(m.Data))
		case <-deadline:
			lengths := make([]int, len(got))
			for i, g := range got {
				lengths[i] = len(g)
			}
			t.Fatalf("timed out; received %d messages with lengths %v", len(got), lengths)
		}
	}

	select {
	case extra := <-out:
		t.Errorf("expected exactly 2 events (one truncated oversized, one normal), got a 3rd of %d bytes -- the oversized message was split into garbage instead of staying bounded", len(extra.Data))
	case <-time.After(300 * time.Millisecond):
	}

	if len(got[0]) != maxTCPMessageBytes {
		t.Errorf("first message is %d bytes, want the %d-byte cap -- the start of the oversized message, delivered once", len(got[0]), maxTCPMessageBytes)
	}
	if got[1] != "D|wan-in|forward: proto TCP, 192.0.2.1:1->198.51.100.1:80" {
		t.Errorf("second message = %q, want the normal line that followed", got[1])
	}
	if Stats().Oversized == 0 {
		t.Error("the discarded continuation was not counted")
	}
}
