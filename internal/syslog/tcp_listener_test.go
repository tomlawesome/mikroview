// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"context"
	"io"
	"net"
	"runtime"
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
