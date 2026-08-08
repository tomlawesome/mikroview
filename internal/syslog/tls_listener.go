// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"context"
	"crypto/tls"
	"net"
)

// ListenTLS binds addr and serves RouterOS's remote-protocol=tls syslog
// (issue #188) until ctx is done, presenting cert -- the same
// certificate the HTTPS listener uses (see internal/servertls), not a
// second one. The router already imports mikroview's generated CA to
// verify HTTPS ingest; this is that same trust step, not a new one.
//
// This buys confidentiality and mikroview authenticating itself to the
// router. It does NOT authenticate the sender: RouterOS's logging action
// has no client-certificate option (verified against a real router --
// see docs/decisions/routeros-ingest-spike.md), so anything able to
// reach the port can still connect and inject log lines, exactly as
// with plaintext today.
//
// The whole implementation is wrapping a tls.Listener around a plain
// one and handing it to ServeTCP: a tls.Listener satisfies net.Listener,
// which is all ServeTCP asks for, so the connection cap, the per-source
// cap, the idle timeout, and the framing (see handleTCPConn's own
// comment) all apply unchanged. A TLS handshake failure -- a scanner
// speaking plain TCP at the port, or a client that never sends
// anything -- surfaces as an error from the connection's first Read
// inside handleTCPConn, so it fails fast and frees its slot rather than
// hanging; tls.Listener.Accept itself never blocks on the handshake, it
// is negotiated lazily on first use.
func ListenTLS(ctx context.Context, addr string, cert tls.Certificate, out chan<- RawMessage) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return ServeTLS(ctx, ln, cert, out)
}

// ServeTLS wraps an already-bound ln in TLS and serves it via ServeTCP.
// Split from ListenTLS, mirroring ServeTCP's own split from ListenTCP, so
// tests can bind an ephemeral port and learn its address before dialing
// it rather than guessing one and racing another process for it.
func ServeTLS(ctx context.Context, ln net.Listener, cert tls.Certificate, out chan<- RawMessage) error {
	// MinVersion set explicitly rather than left to the zero value's
	// default -- this is a server default that has shifted across Go
	// versions before, and a listener carrying router config across the
	// network is exactly the kind of thing that should not depend on
	// which Go release built it.
	tlsLn := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	return ServeTCP(ctx, tlsLn, out)
}
