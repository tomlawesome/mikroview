// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
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
