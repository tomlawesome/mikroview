package syslog

import (
	"bufio"
	"context"
	"fmt"
	"net"
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

// ServeTCP accepts connections on an already-bound ln, framing each one's
// messages on newlines — unlike UDP, a TCP byte stream has no inherent
// per-message boundary, so RouterOS's remote-protocol=tcp output must be
// newline-delimited to be split back into individual log lines. Split from
// ListenTCP so tests can bind an ephemeral port and learn its address
// before dialing it.
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

		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				defer logging.Recover(tcpLog)
				handleTCPConn(ctx, conn, out)
			}()
		default:
			// At capacity: reject immediately rather than queuing, so the
			// accept loop itself never blocks waiting for a slot to free up.
			tcpLog.Warn(fmt.Sprintf("connection limit (%d) reached, rejecting %s", maxTCPConnections, conn.RemoteAddr()))
			conn.Close()
		}
	}
}

func handleTCPConn(ctx context.Context, conn net.Conn, out chan<- RawMessage) {
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 16*1024), 64*1024)
	conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
	for scanner.Scan() {
		conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
		line := scanner.Bytes()
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
