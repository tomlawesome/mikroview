// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/servertls"
)

// testCert generates an in-memory certificate the same way servertls.Load
// does for mikroview's own zero-config default -- no disk I/O, since
// StorePath is left empty.
func testCert(t *testing.T) tls.Certificate {
	t.Helper()
	cert, _, _, err := servertls.Load(servertls.Config{})
	if err != nil {
		t.Fatalf("generating test cert: %v", err)
	}
	return cert
}

// serveTLSForTest binds an ephemeral port and serves it via ServeTLS,
// mirroring serveTCPForTest in tcp_listener_test.go.
func serveTLSForTest(t *testing.T, out chan RawMessage) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go ServeTLS(ctx, ln, FixedCertificate(testCert(t)), out)
	return ln.Addr().String(), cancel
}

// dialTLSInsecure bounds the handshake with a real dialer timeout rather
// than tls.Dial's unbounded default -- against a server that silently
// doesn't speak TLS at all (the failure shape a broken ServeTLS would
// produce), an unbounded handshake blocks forever instead of failing,
// which would hang the whole test run rather than reporting a clear
// failure. Caught by mutation-testing this file's own claim.
func dialTLSInsecure(t *testing.T, addr string) *tls.Conn {
	t.Helper()
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("dial tls: %v", err)
	}
	return conn
}

// TestServeTLSFramesLikeServeTCP proves ServeTLS is a genuine wrapper
// around ServeTCP, not a parallel reimplementation: RouterOS's bare,
// unterminated message shape (#202) must be ingested here exactly as it
// is over plain TCP.
func TestServeTLSFramesLikeServeTCP(t *testing.T) {
	out := make(chan RawMessage, 4)
	addr, stop := serveTLSForTest(t, out)
	defer stop()

	conn := dialTLSInsecure(t, addr)
	defer conn.Close()

	if _, err := conn.Write([]byte("firewall,info bare-message-no-newline")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case msg := <-out:
		if got := string(msg.Data); got != "firewall,info bare-message-no-newline" {
			t.Errorf("Data = %q, want the message verbatim", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("an unterminated message was never ingested over TLS")
	}
}

// TestServeTLSRejectsPlaintext proves a handshake failure -- a scanner,
// or anything speaking plain TCP at the port -- doesn't hang or crash the
// listener, and produces no message.
func TestServeTLSRejectsPlaintext(t *testing.T) {
	out := make(chan RawMessage, 4)
	addr, stop := serveTLSForTest(t, out)
	defer stop()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("firewall,info not-a-tls-handshake\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case msg := <-out:
		t.Fatalf("expected no message from a plaintext connection, got %q", msg.Data)
	case <-time.After(300 * time.Millisecond):
	}

	// The listener itself must still be serving other connections --
	// the failed handshake above must not have wedged the accept loop.
	conn2 := dialTLSInsecure(t, addr)
	defer conn2.Close()
	if _, err := conn2.Write([]byte("still-alive")); err != nil {
		t.Fatalf("write after plaintext rejection: %v", err)
	}
	select {
	case msg := <-out:
		if string(msg.Data) != "still-alive" {
			t.Errorf("Data = %q, want %q", msg.Data, "still-alive")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener stopped accepting connections after a failed handshake")
	}
}

// TestServeTLSStopsOnContextCancel proves ServeTLS returns and closes its
// listener when ctx is cancelled, the same lifetime contract ServeTCP
// already has (ListenTLS delegates to it directly).
func TestServeTLSStopsOnContextCancel(t *testing.T) {
	out := make(chan RawMessage, 4)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ServeTLS(ctx, ln, FixedCertificate(testCert(t)), out) }()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("ServeTLS returned %v after cancel, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ServeTLS did not return after context cancellation")
	}

	if _, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		t.Error("expected the listener to be closed after context cancellation, dial succeeded")
	}
}

// TestServeTLSOversizedMessageStaysBoundedAcrossRecords pins the
// discard-continuation heuristic in handleTCPConn's oversized branch to
// newline presence alone, not read size (#379). The buggy version keyed
// "still discarding" off n == len(buf) as well as "no newline yet" -- a
// condition net.Pipe (TestOversizedMessageYieldsOneEventNotSeveral, in
// tcp_listener_test.go) cannot exercise: a net.Pipe Read hands back
// exactly len(buf) bytes for as long as a large Write is still pending,
// so it satisfies the old read-fill heuristic by construction and the
// bug never triggers there. A real TLS transport can't be fooled that
// way -- tls.Conn.Read returns at most one TLS record's plaintext per
// call, well under the 64 KiB buffer, so n == len(buf) never happens on
// a continuation read. Under the old code that reset oversized to false
// on the very first continuation read, and the next chunk of discard
// garbage was re-ingested as a fresh message -- concatenating straight
// onto whatever real line followed it. See #379 item 3.
func TestServeTLSOversizedMessageStaysBoundedAcrossRecords(t *testing.T) {
	out := make(chan RawMessage, 16)
	addr, stop := serveTLSForTest(t, out)
	defer stop()

	conn := dialTLSInsecure(t, addr)
	defer conn.Close()

	// One message of two and a half buffers, with no newline anywhere --
	// written in small chunks so the TLS record layer, not this test,
	// decides how much of it arrives per Read on the server side.
	oversized := bytes.Repeat([]byte("A"), maxTCPMessageBytes*2+maxTCPMessageBytes/2)
	const chunk = 4096
	for i := 0; i < len(oversized); i += chunk {
		end := i + chunk
		if end > len(oversized) {
			end = len(oversized)
		}
		if _, err := conn.Write(oversized[i:end]); err != nil {
			t.Fatalf("write oversized chunk: %v", err)
		}
	}
	if _, err := conn.Write([]byte("\n")); err != nil {
		t.Fatalf("write terminator: %v", err)
	}

	trailing := []byte("D|wan-in|forward: proto TCP, 192.0.2.1:1->198.51.100.1:80\n")
	if _, err := conn.Write(trailing); err != nil {
		t.Fatalf("write trailing line: %v", err)
	}

	var got [][]byte
	deadline := time.After(5 * time.Second)
	for len(got) < 2 {
		select {
		case m := <-out:
			got = append(got, m.Data)
		case <-deadline:
			t.Fatalf("timed out; received %d messages: %q", len(got), got)
		}
	}
	// Give a third, corrupted message (the buggy symptom) a moment to
	// arrive before asserting there isn't one.
	select {
	case m := <-out:
		got = append(got, m.Data)
	case <-time.After(200 * time.Millisecond):
	}

	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2 (one truncated oversized head, one clean trailing line); got %q", len(got), got)
	}
	if len(got[0]) != maxTCPMessageBytes {
		t.Errorf("first message is %d bytes, want the %d-byte cap -- the start of the oversized message, delivered once", len(got[0]), maxTCPMessageBytes)
	}
	wantTrailing := bytes.TrimRight(trailing, "\n")
	if !bytes.Equal(got[1], wantTrailing) {
		t.Errorf("second message = %q, want %q byte-for-byte -- any extra bytes mean discard-tail garbage merged into the real line", got[1], wantTrailing)
	}
	if Stats().Oversized == 0 {
		t.Error("the discarded continuation was not counted")
	}
}

// TestListenTLSBindsByAddress is a thin check that ListenTLS itself (not
// just ServeTLS) binds a real listener at the given address -- the one
// piece ServeTLS-based tests above can't exercise, since they bind their
// own listener and hand it to ServeTLS directly.
func TestListenTLSBindsByAddress(t *testing.T) {
	out := make(chan RawMessage, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ListenTLS(ctx, "127.0.0.1:0", FixedCertificate(testCert(t)), out) }()

	select {
	case err := <-errCh:
		t.Fatalf("ListenTLS returned early: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}
