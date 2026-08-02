package syslog

import (
	"bufio"
	"context"
	"net"
	"time"
)

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

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		go handleTCPConn(ctx, conn, out)
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
	for scanner.Scan() {
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
