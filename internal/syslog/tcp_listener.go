// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
)

var tcpLog = logging.New("syslog-tcp")

// maxTCPConnections bounds concurrent RouterOS remote-protocol=tcp
// connections. Unlike UDP (a stateless per-datagram receive loop) or the
// WebSocket hub (which already sets deadlines, see internal/api/ws.go),
// TCP has no natural ceiling: without one, an unbounded number of open
// connections -- misbehaving devices, or a scan of the LAN -- would each
// hold a goroutine and a socket open indefinitely.
//
// A var rather than a const so tests can shrink it to exercise the
// rejection path without opening 256+ real sockets.
var maxTCPConnections = 256

// maxTCPConnectionsPerSource caps how many of the global slots any one
// source IP may hold. Without it the global cap alone is exhaustible by
// a single host: the idle timeout resets on every line, so one attacker
// trickling a byte of traffic every few minutes holds all 256 slots
// indefinitely and locks out every real router -- the tool goes blind
// while appearing healthy.
//
// 8 is generous for a legitimate sender (RouterOS opens one connection;
// a few extra covers reconnect churn and NAT'd multi-device sites)
// while making single-source exhaustion impossible.
//
// atomic.Int64 rather than a plain int for exactly the reason
// tcpIdleTimeoutNS above documents: a test shrinking this races the
// accept loop still reading it, with no Go-level happens-before edge
// between "the test observed a connection close" and "the accept loop
// finished with the value". Caught by the race detector when this was
// first written as a plain var.
var maxTCPConnectionsPerSource atomic.Int64

// maxTCPMessageBytes bounds a single read, and so a single message. The
// Scanner this replaced capped its token at 64KiB for the same reason:
// an unbounded read is an unbounded allocation driven by whatever is on
// the other end of the socket.
const maxTCPMessageBytes = 64 * 1024

func init() {
	maxTCPConnectionsPerSource.Store(8)
}

func perSourceLimit() int {
	return int(maxTCPConnectionsPerSource.Load())
}

// tcpIdleTimeoutNS closes a connection that has gone this long without a
// complete line, so a connection that never sends anything (or stalls
// mid-stream) doesn't hold its slot in maxTCPConnections forever. It's an
// idle timeout, not a connection lifetime cap -- reset after every line,
// so an actively-streaming router is never disconnected for staying
// connected too long.
//
// Nanoseconds in an atomic.Int64 rather than a plain time.Duration var:
// tests shrink this below 10 real minutes while a connection's
// handling goroutine (handleTCPConn, below) may still be reading it, and
// there's no Go-level happens-before edge between "the test observed the
// connection close over the socket" and "the goroutine that closed it has
// truly finished" -- only a proven physical ordering, which the race
// detector doesn't trust. Atomic access sidesteps needing that proof.
var tcpIdleTimeoutNS atomic.Int64

func init() {
	tcpIdleTimeoutNS.Store(int64(10 * time.Minute))
}

func tcpIdleTimeout() time.Duration {
	return time.Duration(tcpIdleTimeoutNS.Load())
}

// ListenTCP binds addr and serves it until ctx is done.
func ListenTCP(ctx context.Context, addr string, out chan<- RawMessage) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return ServeTCP(ctx, ln, out)
}

// ServeTCP accepts connections on an already-bound ln. Unlike UDP, a TCP
// byte stream has no inherent per-message boundary, so each connection is
// framed by handleTCPConn: one read is one message, or several if that
// read contains newlines. RouterOS sends neither newlines nor lengths --
// see #202 and handleTCPConn's comment. Split from ListenTCP so tests can
// bind an ephemeral port and learn its address before dialing it.
func ServeTCP(ctx context.Context, ln net.Listener, out chan<- RawMessage) error {
	defer ln.Close()

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	// A buffered channel used purely as a counting semaphore: acquiring a
	// slot is receiving capacity to send, releasing is the deferred read
	// once the connection's goroutine exits.
	slots := make(chan struct{}, maxTCPConnections)

	// Per-source counts, guarded by its own mutex: the accept loop
	// increments, each connection's goroutine decrements on exit.
	var perSourceMu sync.Mutex
	perSource := make(map[string]int)

	var tempDelay time.Duration
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if tempDelay == 0 {
				tempDelay = 5 * time.Millisecond
			} else {
				tempDelay *= 2
			}
			if max := time.Second; tempDelay > max {
				tempDelay = max
			}
			tcpLog.Warn(fmt.Sprintf("accept error: %v; retrying in %v", err, tempDelay))
			time.Sleep(tempDelay)
			continue
		}
		tempDelay = 0

		host := remoteHost(conn)
		perSourceMu.Lock()
		atCap := perSource[host] >= perSourceLimit()
		if !atCap {
			perSource[host]++
		}
		perSourceMu.Unlock()
		if atCap {
			tcpLog.Warn(fmt.Sprintf("per-source connection limit (%d) reached for %s, rejecting", perSourceLimit(), host))
			conn.Close()
			continue
		}

		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				defer func() {
					perSourceMu.Lock()
					if perSource[host]--; perSource[host] <= 0 {
						delete(perSource, host)
					}
					perSourceMu.Unlock()
				}()
				defer logging.Recover(tcpLog)
				handleTCPConn(ctx, conn, out)
			}()
		default:
			perSourceMu.Lock()
			if perSource[host]--; perSource[host] <= 0 {
				delete(perSource, host)
			}
			perSourceMu.Unlock()
			// At capacity: reject immediately rather than queuing, so the
			// accept loop itself never blocks waiting for a slot to free up.
			tcpLog.Warn(fmt.Sprintf("connection limit (%d) reached, rejecting %s", maxTCPConnections, conn.RemoteAddr()))
			conn.Close()
		}
	}
}

// remoteHost extracts the address part of conn's remote endpoint, so
// per-source accounting keys on the host rather than host:port (every
// connection has a distinct source port, which would defeat the cap
// entirely).
func remoteHost(conn net.Conn) string {
	addr := conn.RemoteAddr().String()
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func handleTCPConn(ctx context.Context, conn net.Conn, out chan<- RawMessage) {
	defer conn.Close()

	// done, closed when this function returns, lets the watcher below
	// exit as soon as *this* connection ends -- not only on process
	// shutdown. Without it, this goroutine leaked on every ordinary
	// disconnect (every idle-timeout, every client reconnect), since it
	// only ever watched ctx.Done() (server lifetime), never this
	// connection's own end. Mirrors internal/api/ws.go's reader
	// goroutine, which already gets this right the same way.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

	// One read is one message, unless the read contains newlines, in
	// which case it is several.
	//
	// This used to be a bufio.Scanner on the default ScanLines split,
	// which meant it ingested nothing at all from a RouterOS router
	// (#202): RouterOS sends each message as a bare payload with no
	// trailing newline and no octet count, so Scan() sat waiting for a
	// delimiter that never arrived. Measured against a real CHR, TCP
	// delivered 0 events where UDP delivered 3 of the same messages, and
	// nothing was logged -- the connection was accepted, held, and
	// silently discarded.
	//
	// Handling both shapes is deliberate. A conventional syslog sender
	// does terminate its lines and may pack several into one read;
	// RouterOS terminates nothing. Splitting on newlines when they are
	// present, and otherwise taking the read whole, serves both without
	// the receiver having to know which kind of sender it has.
	//
	// The honest limitation: with no delimiter there is nothing to
	// recover with if two bare messages ever land in one read -- they
	// would arrive merged rather than lost. That is a property of the
	// protocol, not of this code. Observed behaviour is one message per
	// read, including under a 25-message burst, and the live check
	// asserts it stays that way.
	buf := make([]byte, maxTCPMessageBytes)
	conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
			for _, line := range bytes.Split(buf[:n], []byte{'\n'}) {
				line = bytes.TrimRight(line, "\r")
				if len(line) == 0 {
					continue
				}
				data := make([]byte, len(line))
				copy(data, line)

				select {
				case out <- RawMessage{SourceIP: host, Data: data, RecvTime: time.Now()}:
				default:
				}
			}
		}
		if err != nil {
			return
		}
	}
}
