// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"fmt"
	"testing"
	"time"
)

func TestObservationsAreReported(t *testing.T) {
	s := New()
	now := time.Unix(1_000_000, 0)

	s.NoteCAFetch("192.0.2.1", now)
	s.NoteSyslogConnection("192.0.2.1", now.Add(time.Minute))
	s.NoteSyslogConnection("192.0.2.1", now.Add(2*time.Minute))
	s.NoteEvent("edge", true, now)
	s.NoteEvent("edge", false, now)

	sources, devices := s.Snapshot()
	if len(sources) != 1 || sources[0].Source != "192.0.2.1" {
		t.Fatalf("sources = %+v, want one entry for 192.0.2.1", sources)
	}
	if sources[0].CAFetchedAt == nil || sources[0].SyslogFirstSeenAt == nil {
		t.Errorf("missing observations: %+v", sources[0])
	}
	// First stays first: it is what says "this step completed", while
	// Last says "it is still working".
	if got := *sources[0].SyslogFirstSeenAt; !got.Equal(now.Add(time.Minute)) {
		t.Errorf("SyslogFirstSeenAt = %v, want the first connection", got)
	}
	if got := *sources[0].SyslogLastSeenAt; !got.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("SyslogLastSeenAt = %v, want the most recent connection", got)
	}
	if len(devices) != 1 || devices[0].Events != 2 || devices[0].Decoded != 1 {
		t.Errorf("devices = %+v, want edge with 2 events and 1 decoded", devices)
	}
}

// Events arriving with none decoded is the half-finished setup the
// wizard exists to name: rules log, but without the log-prefix
// convention, so every row shows action "unknown".
func TestUndecodedEventsAreDistinguishableFromNoEvents(t *testing.T) {
	s := New()
	now := time.Unix(1_000_000, 0)
	for i := 0; i < 5; i++ {
		s.NoteEvent("edge", false, now)
	}
	_, devices := s.Snapshot()
	if len(devices) != 1 || devices[0].Events != 5 || devices[0].Decoded != 0 {
		t.Fatalf("devices = %+v, want 5 events and 0 decoded", devices)
	}
}

// Keys are source addresses, so an unauthenticated flood must not grow
// these maps without bound.
func TestMapsAreBounded(t *testing.T) {
	s := New()
	now := time.Unix(1_000_000, 0)
	for i := 0; i < maxSources*3; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		s.NoteCAFetch(ip, now.Add(time.Duration(i)*time.Second))
		s.NoteSyslogConnection(ip, now.Add(time.Duration(i)*time.Second))
		s.NoteEvent(ip, true, now.Add(time.Duration(i)*time.Second))
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, n := range map[string]int{
		"caFetched":  len(s.caFetched),
		"syslogSeen": len(s.syslogSeen),
		"prefixes":   len(s.prefixes),
	} {
		if n > maxSources {
			t.Errorf("%s grew to %d, cap is %d", name, n, maxSources)
		}
	}
}

// Eviction takes the least-recently-seen, so a flood does not erase the
// router the operator is actually setting up.
func TestEvictionKeepsTheRecentlyActive(t *testing.T) {
	s := New()
	base := time.Unix(1_000_000, 0)
	s.NoteSyslogConnection("the-real-router", base)

	for i := 0; i < maxSources*2; i++ {
		// Older than the real router, so they are evicted first...
		s.NoteSyslogConnection(fmt.Sprintf("10.1.%d.%d", i/256, i%256), base.Add(-time.Hour))
		// ...and the real router keeps being seen.
		s.NoteSyslogConnection("the-real-router", base.Add(time.Duration(i)*time.Second))
	}

	sources, _ := s.Snapshot()
	for _, o := range sources {
		if o.Source == "the-real-router" {
			return
		}
	}
	t.Error("the active router was evicted by a flood of stale sources")
}
