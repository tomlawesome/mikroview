// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"strconv"
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
// atomic.Int64 rather than a plain int for exactly the reason
// maxTCPConnectionsPerSource and tcpIdleTimeoutNS below document: a test
// shrinking this to exercise the rejection path races ServeTCP's accept
// loop still reading it, with no Go-level happens-before edge between
// "the test observed the rejection" and "the accept loop finished
// reading the old value" -- caught by `go test -race -count=2`, which
// failed 3/3 while this was a plain var (issue #380 item 6). Read
// through maxTCPConns(); use of a raw value elsewhere in the process is
// never mutated, so it is a var only to remain test-shrinkable.
var maxTCPConnections atomic.Int64

func init() {
	maxTCPConnections.Store(256)
}

// maxTCPConns reads the current connection ceiling. See maxTCPConnections.
func maxTCPConns() int {
	return int(maxTCPConnections.Load())
}

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

// tcpQuiescence is how long handleTCPConn's read loop waits, once a
// read has left an accumulated message fragment with no newline in it,
// before deciding the sender has actually finished that message rather
// than merely paused between segments of it. Needed only for
// RouterOS-shaped bare messages (#202), which carry no delimiter at
// all -- a newline-terminated message never waits on this, since the
// newline itself resolves it the instant it arrives.
//
// 75ms is comfortably above same-write fragmentation on a LAN (a TLS
// record boundary or ordinary TCP segmentation resolves in
// microseconds in practice -- see #415) and comfortably below any gap
// that would exist between two genuinely distinct log lines, so two
// bare messages sent back to back are never coalesced into one.
const tcpQuiescence = 75 * time.Millisecond

// tcpHeaderCompletionWindow is how much longer the read loop waits once
// tcpQuiescence has already expired but the bytes at the end of pending
// are an RFC3164 header that has not finished arriving (see
// rfc3164HeaderStillArriving).
//
// tcpQuiescence guesses "the sender has finished" from silence alone,
// because a bare RouterOS message offers nothing else to go on. A
// half-arrived header is the one case where the bytes themselves say
// otherwise: a sender that has written "<30>Aug 29 20:52:4" is
// demonstrably in the middle of a message, so treating that silence as
// the end of the *previous* message glues a fragment of the next
// message's header onto a real record -- which mikroview then displays
// as fact (#914). Waiting instead costs nothing when the rest arrives,
// which on a working connection it does.
//
// One second is far above both plausible causes of that gap -- a single
// TCP retransmission (Linux's TCP_RTO_MIN is 200ms) and a read loop
// that lost the CPU on a contended host, which is how #914 first showed
// up -- and far below anything an operator would notice. It is only
// ever reached after a message has already gone quiet mid-header, so it
// adds no latency to ordinary traffic at all.
const tcpHeaderCompletionWindow = time.Second

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

// OnConnection is called once per accepted syslog connection that has
// gone on to complete a TLS handshake (see handleTCPConn), with the
// source host. Nil unless set. It exists for the setup wizard (#320),
// which has to distinguish "the router cannot reach me at all" from
// "it is connected but no rule is logging yet" -- and only a completed
// handshake actually separates those; a bare TCP connect (a LAN port
// scan, a health check, or a router failing the handshake against a
// certificate that doesn't cover its address) does not (#371). A
// package-level hook rather than a ServeTCP parameter, matching
// SetConfiguredSources above: this is process-wide observation, not
// per-listener configuration.
//
// Called from each connection's own goroutine, past its handshake, not
// from the accept loop -- so it may take as long as it likes; it is the
// accept loop itself, in ServeTCP, that must never block.
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
	if r := maxTCPConns() / reservedFraction; r > 0 {
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
		Capacity:              maxTCPConns(),
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
	slots := make(chan struct{}, maxTCPConns())

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
		unreservedCap := maxTCPConns() - reservedSlots()
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
					unreservedCap, reservedSlots(), maxTCPConns(), host, total))
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
				tcpLog.Warn(fmt.Sprintf("connection limit (%d) reached -- rejecting %s (%d such rejections since start)", maxTCPConns(), conn.RemoteAddr(), total))
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

// rfc3164HeaderLen reports how many bytes at the start of data form a
// valid RFC3164 header -- an optional "<PRI>" (validated 0-191 when
// present, same range and same off-by-one-tolerant "<=4 chars between
// the brackets" rule ParseEnvelope uses in envelope.go) followed by a
// "MMM DD HH:MM:SS" timestamp with a real month name and in-range
// digits (bsdTimeLayout, also from envelope.go) -- or -1 if data does
// not begin with one. Deliberately the same shape ParseEnvelope parses,
// so "is this a header" (here) and "how do we read one" (there) can't
// drift apart.
//
// PRI is optional here for the same reason it's optional in
// ParseEnvelope: nothing about finding a boundary requires it. In
// practice every message #614's fix actually splits does carry one --
// remote-log-format=syslog puts it there -- but a header-shaped split
// point shouldn't stop working just because some future sender omits
// it the way ParseEnvelope already tolerates.
func rfc3164HeaderLen(data []byte) int {
	if len(data) == 0 {
		return -1
	}
	i := 0
	if data[0] == '<' {
		end := bytes.IndexByte(data, '>')
		if end <= 0 || end > 4 {
			return -1
		}
		pri, err := strconv.Atoi(string(data[1:end]))
		if err != nil || pri < 0 || pri > 191 {
			return -1
		}
		i = end + 1
	} else if data[0] < 'A' || data[0] > 'Z' {
		// Cheap reject before the structural check below: every month
		// abbreviation bsdTimeLayout can match starts with an uppercase
		// letter, so anything else here can never be a bare (no-PRI)
		// header start.
		return -1
	}
	if len(data)-i < len(bsdTimeLayout) {
		return -1
	}
	if !looksLikeBSDTimestamp(data[i : i+len(bsdTimeLayout)]) {
		return -1
	}
	if _, err := time.Parse(bsdTimeLayout, string(data[i:i+len(bsdTimeLayout)])); err != nil {
		return -1
	}
	return i + len(bsdTimeLayout)
}

// looksLikeBSDTimestamp is a cheap, allocation-free structural
// pre-check for the "MMM DD HH:MM:SS" shape bsdTimeLayout parses --
// separator positions and digit-ness only, not real month names or
// in-range digits (time.Parse still does that; this never rejects
// anything time.Parse would accept). data must already be at least
// len(bsdTimeLayout) bytes -- callers check that first.
//
// It exists because nextHeaderStart calls rfc3164HeaderLen at nearly
// every byte offset in pending, and the single-byte "starts with an
// uppercase letter" check above passes on a long run of any one
// uppercase letter (a real body can contain one, and the oversized
// path's own test fixture does) -- without this, every such position
// paid for a string conversion and a full time.Parse attempt, turning
// one read's scan into work proportional to pending's length instead
// of to the read itself. Measured concretely: with only the one-byte
// check, splitting the accumulation for a 64KB oversized message
// across several small reads made the read loop fall far enough behind
// the writer that reads coalesced past the message-size cap, and
// TestTCPOversizedMessageFragmentedAcrossManyReadsStaysBounded failed.
func looksLikeBSDTimestamp(data []byte) bool {
	isDigit := func(b byte) bool { return b >= '0' && b <= '9' }
	return data[1] >= 'a' && data[1] <= 'z' &&
		data[2] >= 'a' && data[2] <= 'z' &&
		data[3] == ' ' &&
		(data[4] == ' ' || isDigit(data[4])) &&
		isDigit(data[5]) &&
		data[6] == ' ' &&
		isDigit(data[7]) && isDigit(data[8]) &&
		data[9] == ':' &&
		isDigit(data[10]) && isDigit(data[11]) &&
		data[12] == ':' &&
		isDigit(data[13]) && isDigit(data[14])
}

// nextHeaderStart returns the offset of the next valid RFC3164 header
// in data at or after from, or -1 if none is found. Linear in
// len(data)-from: rfc3164HeaderLen's cheap first-byte reject only skips
// the expensive time.Parse when the byte in question can't possibly
// start a header at all (most of an ordinary firewall line), not when
// it merely fails to -- a long run of uppercase letters, which any
// legitimate message body can contain, still pays for a time.Parse
// attempt at every one of them. handleTCPConn's caller is what keeps
// this bounded overall: it advances `from` across reads instead of
// rescanning pending's already-checked prefix each time (see
// headerScanned in its own read loop), so a large message built from
// many small reads is scanned once in total, not once per read.
func nextHeaderStart(data []byte, from int) int {
	for i := from; i < len(data); i++ {
		if rfc3164HeaderLen(data[i:]) >= 0 {
			return i
		}
	}
	return -1
}

// rfc3164MaxHeaderBytes bounds how wide a valid RFC3164 header can be:
// "<191>" (5 bytes, the widest legal PRI) plus bsdTimeLayout's 15-byte
// timestamp. handleTCPConn's headerScanned watermark holds back this
// many bytes from the end of what it marks "already scanned" -- a
// header up to this wide could still be forming right at that edge,
// waiting on a later read to complete it.
const rfc3164MaxHeaderBytes = 5 + len(bsdTimeLayout)

// rfc3164HeaderStillArriving reports whether the bytes at the end of
// data are an RFC3164 header that has not finished arriving -- either a
// header with no message after it yet, or one that is itself only
// partly here. Both mean the sender is mid-message, so a silence must
// not be read as the end of whatever sits in front of it.
//
// This is the counterpart to the headerScanned watermark's trailing
// margin, which already holds back rfc3164MaxHeaderBytes-1 bytes from
// "already scanned" on exactly the grounds that a header could still be
// forming there. The eager split loop respected that margin; the
// quiescence flush did not, and emptied pending wholesale -- delivering
// a real record with the first bytes of the next message's header stuck
// on the end of it (#914).
//
// Only the tail is examined. A header further back has either already
// been split on (it was complete) or is not a boundary at all.
func rfc3164HeaderStillArriving(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	// A complete header and nothing else: the message it introduces has
	// not started arriving. A header on its own is never a message.
	if rfc3164HeaderLen(data) == len(data) {
		return true
	}
	// Otherwise a header can only be part-way through arriving if it
	// runs to the very end of data, and it is at most one byte short of
	// rfc3164MaxHeaderBytes wide.
	from := len(data) - (rfc3164MaxHeaderBytes - 1)
	if from < 0 {
		from = 0
	}
	for i := from; i < len(data); i++ {
		if rfc3164HeaderPrefix(data[i:]) {
			return true
		}
	}
	return false
}

// rfc3164HeaderPrefix reports whether data is a proper prefix of a
// header rfc3164HeaderLen would accept -- the same shape, with the tail
// end of it not yet arrived.
//
// How much of a prefix counts as evidence differs by whether the PRI is
// there, and deliberately so, because the cost of being wrong is a
// delayed message. A leading '<' is decisive on its own: RouterOS log
// text does not contain one, so "<", "<3", "<30", "<30>" can only be a
// PRI starting. A bare header has no such marker -- a single capital
// letter ends real message bodies constantly -- so nothing counts until
// the month abbreviation is complete enough for time.Parse to rule on.
func rfc3164HeaderPrefix(data []byte) bool {
	if len(data) == 0 {
		return false
	}

	i := 0
	hasPRI := false
	if data[0] == '<' {
		end := bytes.IndexByte(data, '>')
		if end < 0 {
			// The PRI itself is still arriving: '<' plus at most the
			// three digits of the widest legal value.
			if len(data) > 4 {
				return false
			}
			for _, b := range data[1:] {
				if b < '0' || b > '9' {
					return false
				}
			}
			return true
		}
		if end <= 0 || end > 4 {
			return false
		}
		pri, err := strconv.Atoi(string(data[1:end]))
		if err != nil || pri < 0 || pri > 191 {
			return false
		}
		i = end + 1
		hasPRI = true
	}

	ts := data[i:]
	if len(ts) >= len(bsdTimeLayout) {
		// Wide enough to hold a whole header. If it were a valid one
		// rfc3164HeaderLen would have said so, and nothing here is
		// still on its way.
		return false
	}
	if len(ts) == 0 {
		// "<PRI>" complete, timestamp not started.
		return true
	}
	if !hasPRI && len(ts) < 3 {
		return false
	}
	return bsdTimestampPrefix(ts)
}

// bsdTimestampPrefix is looksLikeBSDTimestamp for a timestamp that is
// still arriving: the same separator-and-digit positions, checked only
// as far as the bytes present, and shorter than the full layout.
//
// Once three letters are here the month is decidable, and deciding it
// is time.Parse's job rather than this file's -- the same delegation
// rfc3164HeaderLen makes, so "is this a real month" cannot drift
// between the two. The day and time fields are filled in with values
// that always parse, leaving only the month under test.
func bsdTimestampPrefix(ts []byte) bool {
	isDigit := func(b byte) bool { return b >= '0' && b <= '9' }
	for i, b := range ts {
		ok := false
		switch i {
		case 0:
			ok = b >= 'A' && b <= 'Z'
		case 1, 2:
			ok = b >= 'a' && b <= 'z'
		case 3, 6:
			ok = b == ' '
		case 4:
			ok = b == ' ' || isDigit(b)
		case 5, 7, 8, 10, 11, 13, 14:
			ok = isDigit(b)
		case 9, 12:
			ok = b == ':'
		}
		if !ok {
			return false
		}
	}
	if len(ts) < 3 {
		return true
	}
	_, err := time.Parse(bsdTimeLayout, string(ts[:3])+"  1 00:00:00")
	return err == nil
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

	// The idle deadline applies to a TLS handshake exactly as it does to
	// the read loop below -- HandshakeContext performs its own reads and
	// writes on this same conn, and honours whatever deadline is set on
	// it -- so a connection that opens and then never completes (or
	// never attempts) a handshake doesn't hold its slot forever.
	conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))

	// noteConnection fires here, past a completed TLS handshake, not
	// from ServeTCP's accept branch: tls.Listener.Accept negotiates the
	// handshake lazily (see tls_listener.go's own doc comment), so a
	// bare TCP connect -- a LAN port scan, a health check, or a router
	// whose handshake is about to fail against a certificate that
	// doesn't cover its address -- must not satisfy the setup wizard's
	// "syslog connected" step (#371). Gated on a completed handshake,
	// not on the first byte of application data, because a working
	// handshake with no logging rule configured yet is a different,
	// already-distinguished state (see setup.Store.NoteSyslogConnection's
	// doc comment) that this must not collapse into "never connected".
	if tlsConn, ok := conn.(*tls.Conn); ok {
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return
		}
	}
	noteConnection(host)

	// Framing has two shapes, and this loop distinguishes them by
	// content, not by how many Read()s a message happened to arrive in
	// -- that distinction by read-size was the bug (#415, below).
	//
	// A conventional syslog sender terminates every line with \n and may
	// pack several into one read; RouterOS terminates none of them --
	// it sends each message as a bare payload, no trailing newline and
	// no octet count (#202, verified against a real CHR: TCP delivered 0
	// events where UDP delivered 3 of the same messages, because the
	// bufio.Scanner this replaced sat waiting for a delimiter that never
	// arrived). Both shapes are handled by the same accumulator: bytes
	// read are appended to pending and scanned for '\n'. Anything up to
	// a newline is a complete message, however many reads it took to
	// arrive.
	//
	// That "however many reads it took" is what #415 fixed. The
	// previous version only recognised a message as continuing into the
	// next read when the current one exactly filled the buffer -- so a
	// message under the buffer size that still arrived fragmented
	// across several non-full reads (a TLS record boundary, ordinary
	// TCP segmentation under load) had each fragment parsed as its own
	// line: no fragment carries framing of its own, so each produced a
	// garbage, undecoded event in place of the one real one. Measured
	// concretely: a single ~65KB line over the real TLS listener
	// produced 3 stray events before this fix, with none of the
	// individual reads filling the 64KB buffer.
	//
	// A bare RouterOS-shaped message never gets a '\n', so pending alone
	// can never resolve it -- the only signal available is that nothing
	// more arrived for a while. tcpQuiescence is that signal: once a
	// read leaves pending non-empty with no newline in it, the next read
	// uses a short deadline instead of the ordinary idle one, and a
	// timeout there means the sender is done with this message, not
	// merely between segments of it -- so pending is flushed as one
	// complete message. Genuine same-message fragmentation (RouterOS
	// bursts, TLS records arriving back to back) resolves within
	// microseconds on a LAN, comfortably inside the window; the gap
	// between two genuinely distinct bare messages does not, so they are
	// never coalesced into one. See #202 for the burst-handling
	// behaviour this has to keep working, and tcpQuiescence's own
	// comment for the window itself.
	//
	// tcpQuiescence alone is not enough once several bare messages arrive
	// close enough together that the gap between them, not just within
	// one of them, falls inside its window -- a real burst, not
	// fragmentation of a single message. #614: one traffic() call in the
	// live CHR fixture logs an input line and a forward/NAT line for the
	// same packet within the same short window, and on a real router
	// several such pairs land close enough together that quiescence
	// never separates them -- ~10 lines flushed as a single message.
	// Downstream, the parser reads that whole blob as one string and its
	// field extraction is a left-to-right scan, so whichever embedded
	// line's fields are read last is what wins: a stored chain=input
	// event ended up carrying a later forward-chain line's dstPort and
	// NAT annotation, with its own srcPort not even present in its own
	// (2KB-truncated) Raw text -- one coalescing cause, not two separate
	// bugs.
	//
	// remote-log-format=syslog (see live-routeros.sh's setup() and
	// docs/routeros-setup.md) is what makes a real fix possible: RouterOS
	// then gives every message its own RFC3164 header ("<PRI>MMM DD
	// HH:MM:SS HOSTNAME ", verified against a real CHR 7.23.3), so the
	// arrival of the next header inside pending is itself proof the
	// message before it is complete -- no need to wait on quiescence to
	// find that boundary. rfc3164HeaderLen/nextHeaderStart below look for
	// exactly that shape and split eagerly whenever it turns up after the
	// first byte of pending (the header at position 0, if any, belongs to
	// the message still accumulating, not to one that just ended).
	//
	// A sender left on the default remote-log-format has no header
	// anywhere in what it sends -- nothing for this to find -- so it gets
	// no benefit and falls back to exactly what it did before: waiting
	// out tcpQuiescence to resolve the last message of a burst. That is
	// the accepted residual (see the issue's decided-fix comment): there
	// is nothing on that wire to frame on, so nothing here can.
	//
	// A message *larger* than the cap is the other case this has always
	// had to handle: a write bigger than maxTCPMessageBytes with no
	// newline in reach. The first maxTCPMessageBytes are delivered once,
	// truncated (honest -- it is the genuine start of what was sent),
	// and continuation bytes are discarded and counted until a newline
	// ends the run -- with whatever follows that newline in the same
	// read salvaged into pending rather than discarded with it, since
	// it belongs to the next message, not this one. Entry into this
	// state happens by pending crossing the cap (see below) and is
	// unaffected by what follows here: once the cap is already crossed
	// and counted, the only question left is whether this run has
	// reached its terminator, and read size doesn't answer that --
	// under TLS, the only production transport, a single Read can never
	// fill the buffer (tls.Conn hands back at most one record's
	// plaintext, well under the 64 KiB cap), so a discard check that
	// also required a full read reset itself on the very first
	// continuation read and let the next chunk of discard garbage back
	// in as if it were a fresh message, corrupting whatever real line
	// followed it. See #285 finding 18 and #379.
	buf := make([]byte, maxTCPMessageBytes)
	// pending holds bytes read but not yet resolved into a complete
	// message: either the tail of a newline-delimited message still
	// missing its '\n', or the whole of a bare message whose end hasn't
	// been confirmed yet. Left untouched while oversized is true and no
	// terminator has turned up yet -- that path never accumulates, for
	// the memory-bound reason maxTCPMessageBytes exists in the first
	// place; it only receives the salvaged remainder once a terminator
	// does turn up, per the discard branch below.
	var pending []byte
	oversized := false
	// headerScanned is the #614 split loop's cross-read watermark: how
	// far into pending it has already searched for a header with none
	// found, so a message built from many small reads (the oversized
	// path's own lead-up, or any large ordinary message with no header
	// in it at all) is scanned once in total rather than once per read
	// -- see nextHeaderStart's doc comment for why that re-scan cost is
	// real. Reset to 0 anywhere pending's front moves for any reason: a
	// boundary just resolved, so nothing learned about the old buffer's
	// indexing still applies to the new one.
	headerScanned := 0
	// awaitingHeader records that the last deadline set was
	// tcpHeaderCompletionWindow rather than tcpQuiescence, because
	// pending ended in a header that had not finished arriving. It is
	// what tells the two timeouts apart: everywhere else the loop
	// infers which deadline fired from pending being non-empty, and
	// this is the one state that breaks that inference. Cleared by any
	// read that returns bytes -- the wait is over, whatever arrived.
	awaitingHeader := false

	emit := func(data []byte) {
		data = bytes.TrimRight(data, "\r")
		if len(data) == 0 {
			return
		}
		cp := make([]byte, len(data))
		copy(cp, data)

		select {
		case out <- RawMessage{SourceIP: host, Data: cp, RecvTime: time.Now()}:
		default:
			// The ingest channel is full, which means the single ingest
			// goroutine is not keeping up -- a stalled persistence
			// backend, a detector flood, anything downstream. Dropping
			// is right (blocking here would stall the whole listener),
			// but dropping *silently* was not: real router records
			// vanished with no log line and no counter anywhere, so an
			// operator saw the live view go quiet with nothing to
			// explain it.
			//
			// internal/detect.Enqueue and internal/watchlist's evaluator
			// already pair this exact select/default with a counter and
			// a rate-limited warning; this is the one handoff that did
			// not. See #285 finding 9.
			noteIngestDrop()
		}
	}

	conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			awaitingHeader = false
			conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))

			if oversized {
				// Still discarding the tail of a message whose start was
				// already delivered truncated, and whose cap-crossing
				// was already counted on entry. The first newline in
				// this read ends the run -- but only the bytes up to and
				// including it were ever part of the oversized message;
				// anything after it is the start of whatever comes
				// next, salvaged into pending rather than thrown away
				// with the rest of the read. Without that, a terminator
				// that happens to arrive in the same read as the
				// following message's own bytes (one TLS record holding
				// both) would silently destroy that message -- data
				// loss of a genuine event, not merely an over-eager
				// discard. No newline anywhere in this read means it's
				// still all discard, however it happened to be sliced
				// by the network.
				idx := bytes.IndexByte(buf[:n], '\n')
				tcpOversized.Add(1)
				if idx < 0 {
					oversized = true
					if err != nil {
						return
					}
					continue
				}
				oversized = false
				pending = append(pending, buf[idx+1:n]...)
			} else {
				pending = append(pending, buf[:n]...)
			}

			// The cap has to be judged before the newline split, not
			// only after it (#944). The accumulation check further down
			// only sees pending once every terminated line has been
			// emitted, so when a slow reader takes the bytes that cross
			// the cap *and* the terminator after them in one read, the
			// split below delivered the whole over-limit line intact --
			// the cap held only when the reader was quick enough to see
			// it crossed before the newline landed, which is why the
			// test for it flaked on a loaded runner rather than failing.
			// Same outcome as the accumulation path: the first
			// maxTCPMessageBytes delivered once, the rest of that line
			// discarded and counted, whatever follows the terminator
			// kept. No discard state to enter, since the terminator is
			// already in hand.
			for {
				idx := bytes.IndexByte(pending, '\n')
				if idx <= maxTCPMessageBytes {
					break
				}
				emit(pending[:maxTCPMessageBytes])
				tcpOversized.Add(1)
				pending = pending[idx+1:]
				headerScanned = 0
			}

			for {
				idx := bytes.IndexByte(pending, '\n')
				if idx < 0 {
					break
				}
				emit(pending[:idx])
				pending = pending[idx+1:]
				headerScanned = 0
			}

			// #614: eagerly split at each subsequent RFC3164 header found
			// in what's left of pending (see the framing comment above
			// handleTCPConn for why). A header at position 0 belongs to
			// the message still accumulating -- it is not itself a
			// boundary -- so only a *second* header turning up later in
			// pending proves the first message actually ended.
			//
			// The search has to skip past that first header's *entire*
			// span, not merely its first byte: a header's own timestamp
			// text, taken on its own, independently re-matches as a
			// valid bare (no-PRI) header a few bytes later -- "Aug 29
			// 20:52:44" is a legitimate header start whether or not a
			// "<30>" sits in front of it. Searching from offset 1 alone
			// found exactly that false boundary inside the header
			// currently anchoring pending, splitting a real header in
			// two ("<30>" as one message, its own timestamp as the
			// next). Skipping to the end of position 0's header, when it
			// has one, is what keeps that header intact.
			for {
				skip := 1
				if hl := rfc3164HeaderLen(pending); hl > skip {
					skip = hl
				}
				from := skip
				if headerScanned > from {
					from = headerScanned
				}
				next := nextHeaderStart(pending, from)
				if next < 0 {
					// Nothing found from `from` on. Remember that, short
					// of a trailing margin wide enough to hold a header
					// that's only partially arrived and could still
					// complete once more bytes are appended -- so the
					// next read resumes the search there instead of
					// re-scanning bytes this one already ruled out. This
					// is what keeps a large, header-free message (the
					// oversized path's lead-up, in particular) from being
					// rescanned in full on every single read.
					headerScanned = len(pending) - (rfc3164MaxHeaderBytes - 1)
					if headerScanned < skip {
						headerScanned = skip
					}
					break
				}
				emit(pending[:next])
				pending = pending[next:]
				headerScanned = 0
			}

			if len(pending) >= maxTCPMessageBytes {
				// pending itself has crossed the cap with no newline in
				// it: the same "message larger than the limit" case the
				// old full-buffer check caught, reached here by
				// accumulation instead. Deliver the truncated start
				// once, honestly, and drop what's left of this read past
				// the cap -- entering the discard path above for
				// whatever continues it.
				emit(pending[:maxTCPMessageBytes])
				pending = pending[:0]
				oversized = true
				headerScanned = 0
			}
		}

		if err != nil {
			// A pending, not-yet-resolved fragment is flushed rather
			// than silently lost on any error -- a connection that
			// closes right behind its last bare message, or resets
			// mid-stream, still gets what it sent rather than nothing.
			ambiguous := !oversized && len(pending) > 0
			ne, isNetErr := err.(net.Error)
			timedOut := isNetErr && ne.Timeout()

			if ambiguous && timedOut && !awaitingHeader && rfc3164HeaderStillArriving(pending) {
				// tcpQuiescence expired, but pending ends in a header
				// that is still on its way -- so the sender is
				// mid-message, and this silence is not the end of the
				// message in front of it. Flushing here is what glued
				// a fragment of the next message's header onto a real
				// record (#914). Wait out one bounded window instead;
				// if the rest arrives, the ordinary eager split
				// resolves the boundary exactly, and if it never does
				// the next timeout falls through to the flush below.
				awaitingHeader = true
				conn.SetReadDeadline(time.Now().Add(tcpHeaderCompletionWindow))
				continue
			}

			if ambiguous {
				emit(pending)
				pending = pending[:0]
				headerScanned = 0
			}
			awaitingHeader = false
			if timedOut && ambiguous {
				// This was tcpQuiescence's short deadline (or the
				// header-completion window above), not the ordinary
				// idle one: the sender simply finished a bare message
				// rather than going idle. Flushed above; the
				// connection stays open.
				conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
				continue
			}
			// Either a genuine idle timeout with nothing pending to
			// excuse it, or a real error (EOF, reset) -- either way the
			// connection is done.
			return
		}

		if len(pending) > 0 {
			// pending holds an unresolved, newline-less fragment:
			// tighten the deadline so a quiet spell resolves it quickly
			// rather than waiting out the full idle timeout.
			conn.SetReadDeadline(time.Now().Add(tcpQuiescence))
		} else {
			conn.SetReadDeadline(time.Now().Add(tcpIdleTimeout()))
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
