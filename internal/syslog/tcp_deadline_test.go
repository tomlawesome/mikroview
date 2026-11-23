// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// deadlineFailConn is a connection whose SetReadDeadline fails and which
// records every Read, so a test can assert the handler never reads from a
// connection it could not put an idle deadline on (#1462).
type deadlineFailConn struct {
	scriptedConn
	rmu    sync.Mutex
	reads  int
	closed chan struct{}
}

func (c *deadlineFailConn) Read(b []byte) (int, error) {
	c.rmu.Lock()
	c.reads++
	c.rmu.Unlock()
	return 0, net.ErrClosed
}

func (c *deadlineFailConn) SetReadDeadline(time.Time) error { return errors.New("deadline refused") }

func (c *deadlineFailConn) Close() error {
	c.scriptedConn.Close()
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (c *deadlineFailConn) readCount() int {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	return c.reads
}

func TestHandleTCPConnReturnsWithoutReadingWhenSetReadDeadlineFails(t *testing.T) {
	c := &deadlineFailConn{closed: make(chan struct{})}
	out := make(chan RawMessage, 4)
	handleTCPConn(context.Background(), c, out)
	if n := c.readCount(); n != 0 {
		t.Fatalf("handler read from a connection whose SetReadDeadline failed: %d Read calls, want 0", n)
	}
	select {
	case <-c.closed:
	default:
		t.Fatal("connection was not closed")
	}
}

// oneConnListener hands out a single connection, then blocks until closed.
type oneConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.conn })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *oneConnListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}
func (l *oneConnListener) Addr() net.Addr { return scriptAddr("192.0.2.1:514") }

func TestServeTCPReleasesSlotWhenSetReadDeadlineFails(t *testing.T) {
	base := tcpInUse.Load()
	c := &deadlineFailConn{closed: make(chan struct{})}
	ln := &oneConnListener{conn: c, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan RawMessage, 4)
	go ServeTCP(ctx, ln, out)

	select {
	case <-c.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("connection was never closed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for tcpInUse.Load() != base {
		if time.Now().After(deadline) {
			t.Fatalf("slot not released: tcpInUse = %d, want %d", tcpInUse.Load(), base)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := c.readCount(); n != 0 {
		t.Fatalf("%d Read calls after SetReadDeadline failed, want 0", n)
	}
}
