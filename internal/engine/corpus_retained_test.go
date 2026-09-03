// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeRetained is the on-disk half, held in memory: a day name mapped
// to that day's events, oldest first, exactly as the real files are
// read back.
type fakeRetained struct {
	days     []string
	events   map[string][]store.Event
	failOn   string
	listErr  error
	readDays []string // records which days were actually opened
}

func (f *fakeRetained) Days() ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.days, nil
}

func (f *fakeRetained) ReplayDay(day string, cutoff time.Time, visit func(store.Event)) (int, error) {
	f.readDays = append(f.readDays, day)
	if day == f.failOn {
		return 0, errors.New("frame did not open -- wrong key, or the file has been altered")
	}
	n := 0
	for _, e := range f.events[day] {
		if !cutoff.IsZero() && !e.ReceivedAt.Before(cutoff) {
			continue
		}
		n++
		visit(e)
	}
	return n, nil
}

func retainedEvent(at time.Time, src string) store.Event {
	return store.Event{Time: at, ReceivedAt: at, SrcIP: src, DstIP: "10.0.0.1", DstPort: 22, Action: store.ActionDrop}
}

// The seam: disk first, memory second, oldest first throughout, and an
// event held by both halves visited exactly once.
func TestRetainedCorpusJoinsDiskAndRingWithoutGapOrDuplicate(t *testing.T) {
	base := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	s := store.New(1000, 72*time.Hour)

	// Three events in memory, the oldest of them 10 minutes old.
	ringStart := base.Add(24 * time.Hour)
	for i := range 3 {
		s.Insert(retainedEvent(ringStart.Add(time.Duration(i)*time.Minute), fmt.Sprintf("10.2.0.%d", i)))
	}

	// Disk holds two older days, plus one event the ring also holds --
	// the overlap every real deployment has.
	older := retainedEvent(base, "10.1.0.1")
	newer := retainedEvent(base.Add(12*time.Hour), "10.1.0.2")
	overlap := retainedEvent(ringStart, "10.2.0.0")
	f := &fakeRetained{
		days: []string{"2026-09-01", "2026-09-02"},
		events: map[string][]store.Event{
			"2026-09-01": {older},
			"2026-09-02": {newer, overlap},
		},
	}

	c := NewRetainedCorpus(s, f)
	var got []store.Event
	w := c.Replay(func(e store.Event) { got = append(got, e) })

	if len(got) != 5 {
		t.Fatalf("visited %d events, want 5 (2 from disk, 3 from the ring, the overlap once)", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].ReceivedAt.Before(got[i-1].ReceivedAt) {
			t.Fatalf("events are not oldest first at %d: %v then %v", i, got[i-1].ReceivedAt, got[i].ReceivedAt)
		}
	}
	seen := map[string]int{}
	for _, e := range got {
		seen[e.SrcIP+e.ReceivedAt.String()]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("event %s visited %d times -- the seam duplicated it", k, n)
		}
	}
	if !w.Start.Equal(base) {
		t.Errorf("window starts at %v, want the oldest retained event %v", w.Start, base)
	}
	if w.Count != 5 {
		t.Errorf("window count = %d, want 5", w.Count)
	}
	if w.Truncated {
		t.Error("window reports truncated with everything read")
	}
}

// With nothing in memory there is no overlap to avoid, so the whole
// history is read.
func TestRetainedCorpusReadsEverythingWhenTheRingIsEmpty(t *testing.T) {
	s := store.New(100, time.Hour)
	base := time.Now().Add(-24 * time.Hour)
	f := &fakeRetained{
		days: []string{"2026-09-02"},
		events: map[string][]store.Event{
			"2026-09-02": {retainedEvent(base, "10.1.0.1"), retainedEvent(base.Add(time.Minute), "10.1.0.2")},
		},
	}
	c := NewRetainedCorpus(s, f)
	n := 0
	w := c.Replay(func(store.Event) { n++ })
	if n != 2 || w.Count != 2 {
		t.Fatalf("visited %d events, want 2", n)
	}
}

// The budget is spent on the newest days. Dropping the oldest shortens
// the window honestly; dropping the newest would leave a hole between
// the last day read and the ring, and report it as continuous.
func TestRetainedCorpusDropsOldestDaysNotNewest(t *testing.T) {
	restore := maxCorpusEvents
	maxCorpusEvents = 3
	t.Cleanup(func() { maxCorpusEvents = restore })

	s := store.New(100, time.Hour)
	base := time.Now().Add(-72 * time.Hour)
	f := &fakeRetained{
		days: []string{"2026-09-01", "2026-09-02", "2026-09-03"},
		events: map[string][]store.Event{
			"2026-09-01": {retainedEvent(base, "oldest")},
			"2026-09-02": {retainedEvent(base.Add(24*time.Hour), "middle")},
			"2026-09-03": {retainedEvent(base.Add(48*time.Hour), "newest-a"), retainedEvent(base.Add(49*time.Hour), "newest-b")},
		},
	}
	c := NewRetainedCorpus(s, f)
	var got []store.Event
	w := c.Replay(func(e store.Event) { got = append(got, e) })

	if len(got) != 3 {
		t.Fatalf("visited %d events, want the 3 the budget allows", len(got))
	}
	if got[0].SrcIP != "middle" || got[2].SrcIP != "newest-b" {
		t.Errorf("kept %q..%q, want middle..newest-b -- the oldest day should be the one dropped", got[0].SrcIP, got[2].SrcIP)
	}
	if !w.Truncated {
		t.Error("a shortened window must report itself truncated")
	}
	for _, day := range f.readDays {
		if day == "2026-09-01" {
			t.Error("opened the oldest day after the budget was spent")
		}
	}
}

// A day that will not open stops the walk. Skipping over it would put a
// hole in the middle of the window and report it as continuous.
func TestRetainedCorpusStopsAtAnUnreadableDay(t *testing.T) {
	s := store.New(100, time.Hour)
	base := time.Now().Add(-72 * time.Hour)
	f := &fakeRetained{
		days:   []string{"2026-09-01", "2026-09-02", "2026-09-03"},
		failOn: "2026-09-02",
		events: map[string][]store.Event{
			"2026-09-01": {retainedEvent(base, "oldest")},
			"2026-09-03": {retainedEvent(base.Add(48*time.Hour), "newest")},
		},
	}
	c := NewRetainedCorpus(s, f)
	var got []store.Event
	w := c.Replay(func(e store.Event) { got = append(got, e) })

	if len(got) != 1 || got[0].SrcIP != "newest" {
		t.Fatalf("visited %v, want only the day newer than the unreadable one", got)
	}
	if !w.Truncated {
		t.Error("a window cut short by an unreadable day must report itself truncated")
	}
	for _, day := range f.readDays {
		if day == "2026-09-01" {
			t.Error("kept reading past the unreadable day -- that would leave a hole in the window")
		}
	}
}

// A history that cannot even be listed leaves the ring's own window,
// marked truncated rather than passed off as the whole corpus.
func TestRetainedCorpusSurvivesAnUnlistableHistory(t *testing.T) {
	s := store.New(100, time.Hour)
	s.Insert(retainedEvent(time.Now().Add(-time.Minute), "10.2.0.1"))
	f := &fakeRetained{listErr: errors.New("permission denied")}

	c := NewRetainedCorpus(s, f)
	n := 0
	w := c.Replay(func(store.Event) { n++ })
	if n != 1 {
		t.Fatalf("visited %d events, want the 1 in the ring", n)
	}
	if !w.Truncated {
		t.Error("an unreadable history must not be reported as no history")
	}
}
