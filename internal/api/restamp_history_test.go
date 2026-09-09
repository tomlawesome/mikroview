// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/entities"
	"github.com/tomlawesome/mikroview/internal/naming"
	"github.com/tomlawesome/mikroview/internal/store"
)

// fakeRetainedDays is the on-disk half of a corpus held in memory: one
// day's events, returned exactly as the real day files hand them back
// -- names and all, stamped when the event was written.
type fakeRetainedDays struct {
	day    string
	events []store.Event
}

func (f *fakeRetainedDays) Days() ([]string, error) { return []string{f.day}, nil }

func (f *fakeRetainedDays) ReplayDay(day string, cutoff time.Time, visit func(store.Event)) (int, error) {
	if day != f.day {
		return 0, nil
	}
	n := 0
	for _, e := range f.events {
		if !cutoff.IsZero() && !e.ReceivedAt.Before(cutoff) {
			continue
		}
		n++
		visit(e)
	}
	return n, nil
}

// TestReplayRestampsNamesOnEventsReadBackFromDisk is #996's first half:
// a day file holds the names that applied when it was written, so a
// rename made afterwards was invisible to every replay reading those
// events back -- the same staleness #993 fixed for the ring, on the
// surface #993 left behind.
//
// The re-stamp is asserted through the corpus the replay handler
// actually builds (Server.replayCorpus), not through a helper, so the
// test fails if the wrapper is ever dropped from that construction
// site.
func TestReplayRestampsNamesOnEventsReadBackFromDisk(t *testing.T) {
	s := newAuthTestServer(t)
	// The same wiring main.go uses: one entities store behind both the
	// handler and the resolver.
	s.Naming = naming.Resolver{Entities: s.Entities}

	now := time.Now()
	// Written to disk an hour ago, with the names that applied then --
	// "old-name" for the host, nothing for the port or the rule.
	s.History = &fakeRetainedDays{
		day: "2026-09-09",
		events: []store.Event{{
			ID: 1, Time: now.Add(-time.Hour), ReceivedAt: now.Add(-time.Hour),
			DeviceID: "core", SrcIP: "10.0.0.9", DstIP: "10.0.0.1",
			SrcPort: 51000, DstPort: 443,
			RuleLabel: "lan-wan", SrcHostName: "old-name",
		}},
	}
	// One event still in the ring, so the pass covers both halves and
	// the disk half cannot be confused for the memory one.
	s.Store.Insert(store.Event{
		Time: now, ReceivedAt: now, DeviceID: "core",
		SrcIP: "10.0.0.20", DstIP: "10.0.0.1", DstPort: 443,
	})

	// The operator renames the host, names the port and names the rule,
	// all after the day file was written.
	for _, e := range []entities.Entity{
		{Type: entities.TypeHost, Key: "10.0.0.9", Label: "jellyfish"},
		{Type: entities.TypePort, Key: "443", Label: "web"},
		{Type: entities.TypeRule, Key: "lan-wan", Label: "LAN to WAN"},
	} {
		if _, err := s.Entities.Upsert(e); err != nil {
			t.Fatalf("upsert %s:%s: %v", e.Type, e.Key, err)
		}
	}

	var replayed []store.Event
	s.replayCorpus().Replay(func(e store.Event) { replayed = append(replayed, e) })
	if len(replayed) != 2 {
		t.Fatalf("replayed %d events, want 2 (one from disk, one from the ring)", len(replayed))
	}

	// Disk first, then memory -- see engine.RetainedCorpus.
	fromDisk := replayed[0]
	if fromDisk.SrcHostName != "jellyfish" {
		t.Errorf("SrcHostName off disk = %q, want %q -- the rename made after the day file was written", fromDisk.SrcHostName, "jellyfish")
	}
	if fromDisk.DstPortName != "web" {
		t.Errorf("DstPortName off disk = %q, want %q", fromDisk.DstPortName, "web")
	}
	if fromDisk.RuleName != "LAN to WAN" {
		t.Errorf("RuleName off disk = %q, want %q", fromDisk.RuleName, "LAN to WAN")
	}
	// Identity is untouched: this rewrites what is displayed, never what
	// the event was -- see store.Restamp's own contract for the ring.
	if fromDisk.ID != 1 || fromDisk.SrcIP != "10.0.0.9" || fromDisk.DstPort != 443 || fromDisk.RuleLabel != "lan-wan" {
		t.Errorf("identity fields changed on the way off disk: %+v", fromDisk)
	}
}

// TestReplayRestampBlanksANameNothingSuppliesAnyMore pins the delete
// direction, which is the half a "paste the new label in" fix would get
// wrong: with the entity gone, resolution answers "" exactly as it
// would for an event arriving now, so the raw address shows again
// rather than the label the file was written with.
func TestReplayRestampBlanksANameNothingSuppliesAnyMore(t *testing.T) {
	s := newAuthTestServer(t)
	s.Naming = naming.Resolver{Entities: s.Entities}

	now := time.Now()
	s.History = &fakeRetainedDays{
		day: "2026-09-09",
		events: []store.Event{{
			ID: 1, Time: now.Add(-time.Hour), ReceivedAt: now.Add(-time.Hour),
			DeviceID: "core", SrcIP: "10.0.0.9", SrcHostName: "deleted-name",
		}},
	}

	var replayed []store.Event
	s.replayCorpus().Replay(func(e store.Event) { replayed = append(replayed, e) })
	if len(replayed) != 1 {
		t.Fatalf("replayed %d events, want 1", len(replayed))
	}
	if replayed[0].SrcHostName != "" {
		t.Errorf("SrcHostName off disk = %q, want empty -- nothing names 10.0.0.9 any more", replayed[0].SrcHostName)
	}
}
