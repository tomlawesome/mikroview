// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomlawesome/mikroview/internal/logging"
)

var tcpLog = logging.New("syslog-tcp")

// RawMessage is one received syslog line, before envelope parsing,
// together with the metadata (source IP, receive time) that only the
// listener can supply.
type RawMessage struct {
	SourceIP string
	Data     []byte
	RecvTime time.Time
}

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

// reservedFraction is how much of maxTCPConnections is held back for
// routers the operator declared under `devices:` in config.yaml, as a
// divisor: 4 means a quarter.
//
// The per-source cap alone bounds one address, not thirty-two. With a
// global 256 and 8 per source, 32 addresses fill the listener --
// trivial for a single host with a routed IPv6 /64 -- and the idle
// timeout resets on every read, so holding them costs almost nothing. A
// real router dialling in afterwards is accepted and immediately closed,
// its lines never reaching the pipeline: a total monitoring blackout
// whose only trace was a repeated container-log WARN.
//
// Reserving capacity for declared devices is the same answer
// device.Registry already gives to the same class of problem, in its own
// words: configured devices "are never counted against this cap or
// evicted by it ... losing one to a flood of forged packets would be the
// attack succeeding by another route."
//
// A quarter (64 of 256) is far more than the handful of connections a
// declared fleet needs, while leaving 192 for discovery -- which is
// still the normal path, since a device does not have to be declared to
// be monitored. Owner decision on #285 finding 8.
const reservedFraction = 4

// configuredSources holds the normalised source addresses of declared
// devices. Set once at startup by SetConfiguredSources, before any
// listener starts; an atomic pointer rather than a plain map so a
// later call cannot race the accept loop reading it.
var configuredSources atomic.Pointer[map[string]bool]

// SetConfiguredSources declares which source addresses belong to
// routers listed under `devices:` in config.yaml. Those addresses may
// use the reserved portion of the connection pool; everything else is
// held to the unreserved remainder. Call once at startup.
//
// An empty or unset list means no reservation at all, which is the
// correct behaviour for a deployment that declares no devices: every
// source is equally unknown, so holding capacity back for nobody would
// only shrink the pool.
func SetConfiguredSources(addrs []string) {
	m := make(map[string]bool, len(addrs))
	for _, a := range addrs {
		if h := normaliseHost(a); h != "" {
			m[h] = true
		}
	}
	configuredSources.Store(&m)
}

// OnConnection is called once per accepted syslog connection, with the
// source host. Nil unless set. It exists for the setup wizard (#320),
// which has to distinguish "the router cannot reach me at all" from
// "it is connected but no rule is logging yet" -- and only the
// connection itself separates those. A package-level hook rather than a
// ServeTCP parameter, matching SetConfiguredSources above: this is
// process-wide observation, not per-listener configuration.
//
// Called on the accept path, so it must not block.
var OnConnection atomic.Pointer[func(host string)]

// SetOnConnection installs the hook. Call once at startup.
func SetOnConnection(fn func(host string)) {
	OnConnection.Store(&fn)
}

func noteConnection(host string) {
	if fn := OnConnection.Load(); fn != nil && *fn != nil {
		(*fn)(host)
	}
}

func isConfiguredSource(host string) bool {
	m := configuredSources.Load()
	return m != nil && (*m)[host]
}

// reservedSlots is how many of maxTCPConnections only declared devices
// may occupy. Zero when nothing is declared -- see SetConfiguredSources.
func reservedSlots() int {
	m := configuredSources.Load()
	if m == nil || len(*m) == 0 {
		return 0
	}
	if r := maxTCPConnections / reservedFraction; r > 0 {
		return r
	}
	return 1
}

// Listener saturation counters, for the UI. A blackout that only ever
// reached a container log is a blackout nobody sees -- which is the
// worst property this failure had.
var (
	tcpInUse              atomic.Int64
	tcpRejected           atomic.Uint64
	tcpRejectedConfigured atomic.Uint64
)

// ListenerStats is a snapshot of syslog listener saturation.
type ListenerStats struct {
	InUse                 int    `json:"inUse"`
	Capacity              int    `json:"capacity"`
	ReservedForConfigured int    `json:"reservedForConfigured"`
	Rejected              uint64 `json:"rejected"`
	// RejectedConfigured counts refusals of a *declared* router. Any
	// value above zero means mikroview turned away a device the operator
	// told it to watch, which is the condition worth surfacing rather
	// than saturation on its own.
	RejectedConfigured uint64 `json:"rejectedConfigured"`
	// Dropped counts syslog messages discarded because the ingest
	// channel was full -- real router records that were received and
	// then thrown away. Previously this happened with no counter and no
	// log line at all.
	Dropped uint64 `json:"dropped"`
	// Oversized counts continuation reads discarded from a message
	// larger than the 64 KiB per-message limit. Above zero means
	// something is sending log lines no RouterOS device produces.
	Oversized uint64 `json:"oversized"`
}

// Stats reports current listener saturation. Safe to call at any time.
func Stats() ListenerStats {
	return ListenerStats{
		InUse:                 int(tcpInUse.Load()),
		Capacity:              maxTCPConnections,
		ReservedForConfigured: reservedSlots(),
		Rejected:              tcpRejected.Load(),
		RejectedConfigured:    tcpRejectedConfigured.Load(),
		Dropped:               tcpDropped.Load(),
		Oversized:             tcpOversized.Load(),
	}
}

func noteRejected(host string) {
	tcpRejected.Add(1)
	if isConfiguredSource(host) {
		tcpRejectedConfigured.Add(1)
	}
}

// One gate per rejection reason, same interval as the ingest-queue drop
// log. This port is unauthenticated by design and RouterOS itself
// retries every few seconds, so an un-gated per-attempt WARN hands
// whoever is being rejected -- a locked-out router, or exactly the
// undeclared-source flood reservedFraction defends against -- the
// ability to write log lines at connection-attempt rate (#322).
// Per-reason rather than shared, so a flood of undeclared sources
// can't suppress the line about a *declared* router being turned away.
var (
	perSourceRejectGate  = logging.NewLimiter(ingestDropLogInterval)
	unreservedRejectGate = logging.NewLimiter(ingestDropLogInterval)
	globalRejectGate     = logging.NewLimiter(ingestDropLogInterval)
)

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
	// undeclaredInUse counts live connections from sources NOT listed
	// under devices: in config.yaml -- what the reservation bounds.
	// Guarded by perSourceMu alongside the map it moves with.
	undeclaredInUse := 0

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
		configured := isConfiguredSource(host)

		// An undeclared source may only take slots outside the portion
		// reserved for declared devices; a declared one may take any
		// free slot. Without this a flood of undeclared sources fills
		// the pool and the operator's own routers are locked out -- see
		// reservedFraction.
		unreservedCap := maxTCPConnections - reservedSlots()
		perSourceMu.Lock()
		atCap := perSource[host] >= perSourceLimit()
		outOfUnreserved := !configured && undeclaredInUse >= unreservedCap
		if !atCap && !outOfUnreserved {
			perSource[host]++
			if !configured {
				undeclaredInUse++
			}
		}
		perSourceMu.Unlock()
		if atCap {
			noteRejected(host)
			if total, ok := perSourceRejectGate.Allow(); ok {
				tcpLog.Warn(fmt.Sprintf("per-source connection limit (%d) reached -- rejecting %s (%d such rejections since start)", perSourceLimit(), host, total))
			}
			conn.Close()
			continue
		}
		if outOfUnreserved {
			noteRejected(host)
			if total, ok := unreservedRejectGate.Allow(); ok {
				tcpLog.Warn(fmt.Sprintf(
					"undeclared sources are using all %d unreserved connection slots (%d of %d held for routers listed under devices: in config.yaml) -- rejecting %s (%d such rejections since start)",
					unreservedCap, reservedSlots(), maxTCPConnections, host, total))
			}
			conn.Close()
			continue
		}

		release := func() {
			perSourceMu.Lock()
			if perSource[host]--; perSource[host] <= 0 {
				delete(perSource, host)
			}
			if !configured {
				undeclaredInUse--
			}
			perSourceMu.Unlock()
		}

		select {
		case slots <- struct{}{}:
			noteConnection(host)
			tcpInUse.Add(1)
			go func() {
				defer func() { <-slots }()
				defer tcpInUse.Add(-1)
				defer release()
				defer logging.Recover(tcpLog)
				handleTCPConn(ctx, conn, out)
			}()
		default:
			noteRejected(host)
			release()
			// At capacity: reject immediately rather than queuing, so the
			// accept loop itself never blocks waiting for a slot to free up.
			if total, ok := globalRejectGate.Allow(); ok {
				tcpLog.Warn(fmt.Sprintf("connection limit (%d) reached -- rejecting %s (%d such rejections since start)", maxTCPConnections, conn.RemoteAddr(), total))
			}
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
		return normaliseHost(host)
	}
	return normaliseHost(addr)
}

// normaliseHost puts an address into the one form the configured-source
// set is keyed on, so "::ffff:192.0.2.1" from a dual-stack listener and
// "192.0.2.1" from config.yaml are the same device -- the same
// normalisation device.Registry already applies to its own source keys.
func normaliseHost(host string) string {
	host = strings.TrimSpace(host)
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	return host
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
	// Measured, not assumed: a 500-message burst from a real router
	// arrived as 500 events, none lost and none merged. TCP is permitted
	// to coalesce writes, and with no delimiter there would be nothing to
	// split them back apart with -- but RouterOS writes one message per
	// send, and that is what the wire shows. Worth keeping an eye on,
	// not a defect to design around.
	// One further case the above does not cover: a message *larger* than
	// the buffer. A 100 KiB write arrives as two or three full reads,
	// and treating each as its own message manufactured two or three
	// events out of one. Not merely wrong in count -- each fragment was
	// then parsed as though it were a whole log line, so the second and
	// third produced garbage fields attributed to a real device.
	//
	// A read that completely fills the buffer with no newline in it is
	// the signature: no real RouterOS line is 64 KiB. The first such
	// chunk is delivered (truncated, which is honest -- it is the
	// beginning of what was sent) and the continuation is discarded and
	// counted until a short read ends it. See #285 finding 18.
	buf := make([]byte, maxTCPMessageBytes)
	oversized := false
	conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
			full := n == len(buf) && !bytes.Contains(buf[:n], []byte{'\n'})
			if oversized {
				// Still inside the message whose start was already
				// delivered. Drop the continuation rather than parse it.
				tcpOversized.Add(1)
				oversized = full
				if err != nil {
					return
				}
				continue
			}
			oversized = full

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
					// The ingest channel is full, which means the single
					// ingest goroutine is not keeping up -- a stalled
					// persistence backend, a detector flood, anything
					// downstream. Dropping is right (blocking here would
					// stall the whole listener), but dropping *silently*
					// was not: real router records vanished with no log
					// line and no counter anywhere, so an operator saw
					// the live view go quiet with nothing to explain it.
					//
					// internal/detect.Enqueue and internal/watchlist's
					// evaluator already pair this exact select/default
					// with a counter and a rate-limited warning; this is
					// the one handoff that did not. See #285 finding 9.
					noteIngestDrop()
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// ingestDropLogInterval bounds how often a full ingest channel actually
// logs. Sustained overload is exactly the condition this reports, so
// logging every drop would add load at the worst moment -- the same
// reasoning internal/detect's observeQueueDropLogInterval already
// applies to its own queue.
const ingestDropLogInterval = 30 * time.Second

var (
	tcpDropped   atomic.Uint64
	tcpOversized atomic.Uint64
	dropLogGate  = logging.NewLimiter(ingestDropLogInterval)
)

func noteIngestDrop() {
	total := tcpDropped.Add(1)
	if _, ok := dropLogGate.Allow(); ok {
		tcpLog.Warn(fmt.Sprintf(
			"ingest queue full -- %d syslog messages discarded since start; events are arriving faster than they can be processed, or something downstream is stalled",
			total))
	}
}
