// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
)

var udpLog = logging.New("syslog-udp")

// RawMessage is one received syslog datagram/line, before envelope
// parsing, together with the metadata (source IP, receive time) that only
// the listener can supply.
type RawMessage struct {
	SourceIP string
	Data     []byte
	RecvTime time.Time
}

// ListenUDP binds addr and serves it until ctx is done.
func ListenUDP(ctx context.Context, addr string, out chan<- RawMessage) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	return ServeUDP(ctx, conn, out)
}

// ServeUDP reads datagrams from an already-bound conn and pushes one
// RawMessage per datagram onto out — a UDP datagram is exactly one syslog
// message, so no additional framing is needed. Split from ListenUDP so
// tests can bind an ephemeral port themselves and learn its address before
// sending test datagrams.
//
// If out is full, the message is dropped rather than blocking the receive
// loop: a stalled downstream consumer must never cause the socket's kernel
// receive buffer to back up and start silently dropping RouterOS traffic
// at the network layer instead, where we'd have no chance to react at all.
func ServeUDP(ctx context.Context, conn net.PacketConn, out chan<- RawMessage) error {
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 16*1024)
	var tempDelay time.Duration
	for {
		n, remote, err := conn.ReadFrom(buf)
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
			udpLog.Warn(fmt.Sprintf("read error: %v; retrying in %v", err, tempDelay))
			time.Sleep(tempDelay)
			continue
		}
		tempDelay = 0
		handleUDPDatagramRecovered(buf[:n], remote, out)
	}
}

// handleUDPDatagramRecovered isolates panic recovery to a single
// datagram -- see logging.Recover's doc comment for why this can't
// just be a defer at the top of ServeUDP's loop (it would end the
// whole listener goroutine after the first bad datagram instead of
// just dropping that one).
func handleUDPDatagramRecovered(buf []byte, remote net.Addr, out chan<- RawMessage) {
	defer logging.Recover(udpLog)

	data := make([]byte, len(buf))
	copy(data, buf)
	host, _, _ := net.SplitHostPort(remote.String())

	select {
	case out <- RawMessage{SourceIP: host, Data: data, RecvTime: time.Now()}:
	default:
	}
}
