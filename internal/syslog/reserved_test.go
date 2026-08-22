// SPDX-License-Identifier: AGPL-3.0-only

package syslog

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// The per-source cap bounds one address, not thirty-two. With a global
// 256 and 8 per source, 32 addresses fill the listener -- trivial for a
// single host holding a routed IPv6 /64 -- and because the idle timeout
// resets on every read, holding them costs almost nothing. A real router
// dialling in afterwards was accepted and immediately closed, its lines
// never reaching the pipeline.
//
// Reserving capacity for declared devices is the answer device.Registry
// already gives to the same class of problem. See #285 finding 8.
func TestReservedSlotsOnlyExistWhenDevicesAreDeclared(t *testing.T) {
	t.Cleanup(func() { SetConfiguredSources(nil) })

	SetConfiguredSources(nil)
	if got := reservedSlots(); got != 0 {
		t.Errorf("reservedSlots with nothing declared = %d, want 0 -- holding capacity back for nobody only shrinks the pool", got)
	}

	SetConfiguredSources([]string{"192.0.2.1"})
	want := maxTCPConns() / reservedFraction
	if got := reservedSlots(); got != want {
		t.Errorf("reservedSlots = %d, want %d", got, want)
	}
	if reservedSlots() >= maxTCPConns() {
		t.Error("the reservation consumed the whole pool -- discovery is still the normal path and must keep capacity")
	}
}

// A declared device must be recognised regardless of which form its
// address arrives in: a dual-stack listener reports an IPv4 peer as
// "::ffff:192.0.2.1", while config.yaml holds "192.0.2.1".
func TestConfiguredSourcesMatchAcrossAddressForms(t *testing.T) {
	t.Cleanup(func() { SetConfiguredSources(nil) })
	SetConfiguredSources([]string{" 192.0.2.1 ", "2001:db8::1"})

	for _, host := range []string{"192.0.2.1", "::ffff:192.0.2.1", "2001:db8::1", "2001:0db8:0000::1"} {
		if !isConfiguredSource(normaliseHost(host)) {
			t.Errorf("%q was not recognised as a declared device", host)
		}
	}
	for _, host := range []string{"198.51.100.9", "2001:db8::2", ""} {
		if isConfiguredSource(normaliseHost(host)) {
			t.Errorf("%q was treated as a declared device", host)
		}
	}
}

// Stats is what makes a blackout visible. Before it, the only trace was
// a repeated container-log WARN -- which means visible to nobody.
func TestStatsReportsCapacityAndReservation(t *testing.T) {
	t.Cleanup(func() { SetConfiguredSources(nil) })
	SetConfiguredSources([]string{"192.0.2.1"})

	s := Stats()
	if s.Capacity != maxTCPConns() {
		t.Errorf("Capacity = %d, want %d", s.Capacity, maxTCPConns())
	}
	if s.ReservedForConfigured != reservedSlots() {
		t.Errorf("ReservedForConfigured = %d, want %d", s.ReservedForConfigured, reservedSlots())
	}

	// A refusal of a declared router is the condition worth surfacing,
	// so it is counted separately from refusals in general.
	before := Stats()
	noteRejected("192.0.2.1")
	noteRejected("198.51.100.9")
	after := Stats()
	if after.Rejected != before.Rejected+2 {
		t.Errorf("Rejected = %d, want %d", after.Rejected, before.Rejected+2)
	}
	if after.RejectedConfigured != before.RejectedConfigured+1 {
		t.Errorf("RejectedConfigured = %d, want %d -- only the declared router should count", after.RejectedConfigured, before.RejectedConfigured+1)
	}
}

// The handoff into the ingest pipeline is a non-blocking send: when the
// channel is full the message is discarded, which is right -- blocking
// would stall the whole listener. Doing it *silently* was not. Real
// router records vanished with no log line and no counter anywhere, so
// an operator saw the live view go quiet with nothing to explain it,
// while internal/detect.Enqueue and internal/watchlist's evaluator
// already paired the identical select/default with a counter and a
// rate-limited warning. See #285 finding 9.
func TestIngestDropsAreCounted(t *testing.T) {
	before := Stats().Dropped
	for i := 0; i < 5; i++ {
		noteIngestDrop()
	}
	if got := Stats().Dropped - before; got != 5 {
		t.Errorf("Dropped rose by %d, want 5 -- a discarded router record must leave a trace", got)
	}
}

// A message larger than the read buffer arrives as several full reads.
// Treating each as its own message manufactured two or three events out
// of one, and -- worse than the count being wrong -- each fragment was
// then parsed as a whole log line, producing garbage fields attributed
// to a real device. See #285 finding 18.
func TestOversizedMessageYieldsOneEventNotSeveral(t *testing.T) {
	out := make(chan RawMessage, 16)
	serverConn, clientConn := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleTCPConn(ctx, serverConn, out)
	}()

	// One message of two and a half buffers, with no newline anywhere --
	// the shape a RouterOS sender produces, just far too large -- then a
	// '\n' terminator before the normal message that follows. The
	// terminator is the only honest end-of-run signal a receiver has:
	// an unterminated blob followed by more bytes is, to any receiver,
	// still one message, so ending the discard without one would mean
	// guessing at a boundary the sender never sent.
	oversized := append(bytes.Repeat([]byte("A"), maxTCPMessageBytes*2+maxTCPMessageBytes/2), '\n')
	if _, err := clientConn.Write(oversized); err != nil {
		t.Fatalf("write: %v", err)
	}
	// A normal message afterwards must still arrive: the discard has to
	// end with the oversized message's own terminator, not swallow what
	// follows. Left without a trailing newline of its own so this also
	// exercises the EOF-flush path below, not just the newline path.
	if _, err := clientConn.Write([]byte("D|wan-in|forward: proto TCP, 192.0.2.1:1->198.51.100.1:80")); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got []string
	deadline := time.After(3 * time.Second)
	for len(got) < 2 {
		select {
		case m := <-out:
			got = append(got, string(m.Data))
		case <-deadline:
			t.Fatalf("timed out; received %d messages: %q", len(got), got)
		}
	}
	clientConn.Close()
	<-done

	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2 (one truncated oversized, one normal)", len(got))
	}
	if len(got[0]) != maxTCPMessageBytes {
		t.Errorf("first message is %d bytes, want the %d-byte cap -- the start of the oversized message, delivered once",
			len(got[0]), maxTCPMessageBytes)
	}
	if !strings.HasPrefix(got[1], "D|wan-in|") {
		t.Errorf("second message = %q, want the normal line that followed -- the discard must end with the oversized message", got[1])
	}
	if Stats().Oversized == 0 {
		t.Error("the discarded continuation was not counted")
	}
}
