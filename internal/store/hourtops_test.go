// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"testing"
	"time"
)

// mkTopEvent is mkEvent (ring_test.go) plus the fields HourTops actually
// reads -- SrcHostName and a distinct DstPort -- kept separate so
// ring_test.go's callers are unaffected by fields they never asked for.
func mkTopEvent(t time.Time, srcIP, srcHostName string, srcPort int, dstPort int) Event {
	return Event{
		Time:        t,
		ReceivedAt:  t,
		DeviceID:    "core",
		Action:      ActionAccept,
		Protocol:    "TCP",
		SrcIP:       srcIP,
		SrcHostName: srcHostName,
		SrcPort:     srcPort,
		DstIP:       "1.2.3.4",
		DstPort:     dstPort,
		Chain:       "forward",
		Raw:         "raw line",
	}
}

func TestHourTopsOnEmptyBufferIsHonestlyBlankNotZero(t *testing.T) {
	s := New(100, time.Hour)
	tops := s.HourTops()
	if len(tops) != timeSeriesMinutes {
		t.Fatalf("len(tops) = %d, want %d", len(tops), timeSeriesMinutes)
	}
	for i, top := range tops {
		// Nothing was ever evicted from an empty buffer -- every minute
		// is honestly known to hold nothing, not merely unknown.
		if !top.Complete {
			t.Errorf("minute %d: Complete = false on an empty buffer, want true (nothing to have evicted)", i)
		}
		if top.Talker != "" || top.Port != "" {
			t.Errorf("minute %d: Talker=%q Port=%q, want both empty on an empty buffer", i, top.Talker, top.Port)
		}
	}
}

// TestHourTopsReportsTheWinnerForAFullyCoveredMinute is the ordinary
// case: a minute the ring has not had to evict anything from gets a
// real answer, using lib/whisperStats.ts's own talker/port fallback
// (hostname over raw address, dest port over source port).
func TestHourTopsReportsTheWinnerForAFullyCoveredMinute(t *testing.T) {
	s := New(100, time.Hour)
	now := time.Now()

	// An old anchor event, comfortably before the target minute, so the
	// ring's oldest-held event can never be mistaken for having evicted
	// anything out of the target minute below.
	s.Insert(mkTopEvent(now.Add(-55*time.Minute), "10.0.0.9", "", 0, 22))

	target := now.Add(-5 * time.Minute)
	s.Insert(mkTopEvent(target, "10.0.0.5", "nas", 5000, 443))
	s.Insert(mkTopEvent(target.Add(2*time.Second), "10.0.0.5", "nas", 5001, 443))
	// A single quieter event from a different talker/port in the same
	// minute -- present so the winner is a genuine majority, not the
	// only entry.
	s.Insert(mkTopEvent(target.Add(3*time.Second), "10.0.0.6", "", 5002, 8080))

	tops := s.HourTops()
	targetMinute := target.Unix() / 60
	var found *HourTop
	for i := range tops {
		if tops[i].Time.Unix()/60 == targetMinute {
			found = &tops[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no bucket for the target minute in the returned axis")
	}
	if !found.Complete {
		t.Fatalf("target minute Complete = false, want true (nothing evicted from it)")
	}
	if found.Talker != "nas" {
		t.Errorf("Talker = %q, want %q (SrcHostName, 2 events beating the other talker's 1)", found.Talker, "nas")
	}
	if found.Port != "443" {
		t.Errorf("Port = %q, want %q (DstPort, 2 events beating the other port's 1)", found.Port, "443")
	}
}

// TestHourTopsMarksAnEvictedMinuteIncompleteRatherThanUndercounting is
// #644's own honesty requirement: once the ring buffer no longer holds
// every event from a minute, that minute must read as an absence, never
// as a plausible-looking count computed from whichever fragment
// survived.
func TestHourTopsMarksAnEvictedMinuteIncompleteRatherThanUndercounting(t *testing.T) {
	// Capacity 1 makes eviction unambiguous: the second Insert below
	// evicts the first outright.
	s := New(1, time.Hour)
	now := time.Now()

	evictedMinute := now.Add(-3 * time.Minute)
	s.Insert(mkTopEvent(evictedMinute, "10.0.0.5", "nas", 5000, 443))
	// Evicts the event above -- capacity 1 held only ever one at a time.
	s.Insert(mkTopEvent(now, "10.0.0.6", "phone", 5001, 8080))

	tops := s.HourTops()
	targetMinute := evictedMinute.Unix() / 60
	var found *HourTop
	for i := range tops {
		if tops[i].Time.Unix()/60 == targetMinute {
			found = &tops[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no bucket for the evicted minute in the returned axis")
	}
	if found.Complete {
		t.Fatalf("Complete = true for a minute the ring no longer holds any event from")
	}
	if found.Talker != "" || found.Port != "" {
		t.Errorf("Talker=%q Port=%q on an incomplete minute, want both blank rather than a number "+
			"derived from data that no longer exists", found.Talker, found.Port)
	}
}

// TestTopOfBreaksTiesByTheLowerLabel pins topOf's tie-break: Go
// deliberately randomises map iteration order, so a correct
// implementation must return the same winner regardless of that order
// -- see topOf's own doc comment for why this is a pairwise running
// max rather than a sort.
func TestTopOfBreaksTiesByTheLowerLabel(t *testing.T) {
	got := topOf(map[string]uint64{"nas": 2, "cam-porch": 2, "phone": 1})
	if got != "cam-porch" {
		t.Errorf("topOf = %q, want %q (lower label wins a count tie)", got, "cam-porch")
	}
}

func TestTopOfOnEmptyCountsIsBlank(t *testing.T) {
	if got := topOf(nil); got != "" {
		t.Errorf("topOf(nil) = %q, want empty", got)
	}
}

func TestTalkerKeyPrefersHostNameOverRawAddress(t *testing.T) {
	e := mkTopEvent(time.Now(), "10.0.0.5", "nas", 0, 0)
	if got := talkerKey(e); got != "nas" {
		t.Errorf("talkerKey = %q, want the resolved host name %q", got, "nas")
	}
	e.SrcHostName = ""
	if got := talkerKey(e); got != "10.0.0.5" {
		t.Errorf("talkerKey = %q, want the raw address %q once no host name is configured", got, "10.0.0.5")
	}
}

func TestPortKeyPrefersDestinationFallsBackToSource(t *testing.T) {
	e := mkTopEvent(time.Now(), "10.0.0.5", "", 1234, 443)
	if got := portKey(e); got != "443" {
		t.Errorf("portKey = %q, want the destination port %q", got, "443")
	}
	e.DstPort = 0
	if got := portKey(e); got != "1234" {
		t.Errorf("portKey = %q, want the source port %q once there is no destination one", got, "1234")
	}
	e.SrcPort = 0
	if got := portKey(e); got != "" {
		t.Errorf("portKey = %q, want empty when neither port is set", got)
	}
}
