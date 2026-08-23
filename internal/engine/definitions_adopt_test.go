// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// AdoptWatchlistEntries is the upgrade path for issue #407's deletion of
// internal/watchlist.Store, and the tests below are the proof that
// deleting a store nobody can write to any more does not quietly delete
// an operator's data with it.
//
// The case that makes this necessary is specific and easy to miss:
// MigrateDefinitions only ever runs while the definitions document does
// not exist. A deployment that upgraded during #404-#406 therefore
// already has one -- and went on creating watchlist entries in the old
// store afterwards, because that store was still the operator-facing
// entry set until this issue. Those entries are in no definitions
// document anywhere, and without adoption the upgrade that deletes the
// store loses them with no error and no warning: an entry set that is
// simply smaller than it was, which is the "absence of detection
// presented as absence of threat" failure #380's first item describes.

// seedWatchlistDocument writes a watchlist document the way
// internal/watchlist.Store used to, so these tests exercise the real
// on-disk shape rather than a hand-built approximation of it.
func seedWatchlistDocument(t *testing.T, b persist.Backend, entries ...watchlist.Entry) {
	t.Helper()
	ptrs := make([]*watchlist.Entry, 0, len(entries))
	for i := range entries {
		e := entries[i]
		ptrs = append(ptrs, &e)
	}
	writeJSON(t, b, migrateWatchlistFile{Entries: ptrs})
}

// upgradeEntries is one of each shape an upgrading deployment can have,
// including the state that must survive intact: an inverted entry
// part-way through its observation period, with destinations already
// promoted and others still under review.
func upgradeEntries() []watchlist.Entry {
	created := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	seen := time.Date(2026, 8, 15, 22, 15, 0, 0, time.UTC)
	return []watchlist.Entry{
		{
			ID: "plain", Name: "watch ssh", Ports: []int{22, 2222},
			DestIP: "10.0.0.5", CreatedAt: created,
		},
		{
			ID: "listed", Name: "watch mgmt list", Ports: []int{8291},
			SourceList: watchlist.AddressListRef{Device: "core", List: "mgmt"}, CreatedAt: created,
		},
		{
			ID: "device", Name: "camera egress", Invert: true, Observing: true,
			Source:                 matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
			IncludeStructuralNoise: true,
			Permitted:              []watchlist.PermittedDest{{DestIP: "198.51.100.10", Port: 443}},
			Observed: []watchlist.ObservedDest{
				{DestIP: "198.51.100.20", Port: 80, FirstSeen: created, LastSeen: seen, Count: 41},
			},
			CreatedAt: created,
		},
	}
}

// TestAdoptWatchlistEntriesCarriesAnUpgradeIntact is the headline
// guarantee: after an upgrade, every entry is still there, and so is
// everything an operator accumulated inside it. The observed candidate
// list matters most -- it is days of a device's traffic fingerprint that
// cannot be recreated by re-running anything, and it is the one thing a
// naive "the entries are all present" check would not notice losing.
func TestAdoptWatchlistEntriesCarriesAnUpgradeIntact(t *testing.T) {
	eachBackend(t, "adopt-upgrade", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, wlBackend := newBackend(), newBackend()

		// The deployment already has a definitions document -- the state
		// MigrateDefinitions refuses to touch, and therefore the exact
		// state this function exists for.
		if _, err := MigrateDefinitions(context.Background(), defsBackend, newBackend(), newBackend()); err != nil {
			t.Fatalf("MigrateDefinitions: %v", err)
		}
		seedWatchlistDocument(t, wlBackend, upgradeEntries()...)
		before := loadBytes(t, wlBackend)

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		adopted, err := AdoptWatchlistEntries(context.Background(), s, wlBackend)
		if err != nil {
			t.Fatalf("AdoptWatchlistEntries: %v", err)
		}
		if adopted != 3 {
			t.Fatalf("adopted %d entries, want 3", adopted)
		}
		// #474: adoptRaw's persistLocked hands the write to the store's
		// write-behind writer and returns -- it does not wait for the
		// save to land. Without this flush, that save can still be in
		// flight when eachBackend's t.TempDir() is removed at the end of
		// this subtest, racing RemoveAll (see flushDefinitionsForTest's
		// own doc comment for the established pattern this follows).
		flushDefinitionsForTest(t, s)

		entries, err := s.ListExpectations()
		if err != nil {
			t.Fatalf("ListExpectations: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("the definitions store holds %d expectation(s) after the upgrade, want 3", len(entries))
		}

		byID := make(map[string]watchlist.Entry, len(entries))
		for _, e := range entries {
			byID[e.ID] = e
		}

		plain := byID["plain"]
		if plain.Name != "watch ssh" || len(plain.Ports) != 2 || plain.DestIP != "10.0.0.5" {
			t.Errorf("the plain entry came through as %+v, want its name, both ports and its destination intact", plain)
		}
		if listed := byID["listed"]; listed.SourceList.Device != "core" || listed.SourceList.List != "mgmt" {
			t.Errorf("the address-list scoping was lost: %+v", listed.SourceList)
		}

		device := byID["device"]
		if !device.Invert || !device.Observing {
			t.Errorf("the inverted entry lost its mode: invert=%t observing=%t", device.Invert, device.Observing)
		}
		if !device.IncludeStructuralNoise {
			t.Error("the inverted entry lost its structural-noise opt-in")
		}
		if len(device.Permitted) != 1 || device.Permitted[0].DestIP != "198.51.100.10" || device.Permitted[0].Port != 443 {
			t.Errorf("the promoted destinations were lost: %+v", device.Permitted)
		}
		if len(device.Observed) != 1 {
			t.Fatalf("the observation period was reset: %+v", device.Observed)
		}
		if got := device.Observed[0]; got.DestIP != "198.51.100.20" || got.Port != 80 || got.Count != 41 {
			t.Errorf("the observed candidate came through as %+v, want 198.51.100.20:80 seen 41 times", got)
		}
		if !device.CreatedAt.Equal(upgradeEntries()[2].CreatedAt) {
			t.Errorf("CreatedAt = %v, want the entry's original creation time", device.CreatedAt)
		}

		// The source is never touched: an operator who has not yet run a
		// clean backup after upgrading still has every entry recoverable
		// from where it always was.
		if after := loadBytes(t, wlBackend); !bytes.Equal(before, after) {
			t.Error("the watchlist document was modified -- adoption reads it and writes only the definitions document")
		}
	})
}

// TestAdoptWatchlistEntriesIsIdempotentAndNeverOverwrites pins the
// property that lets this run on every boot rather than once: an entry
// already in the definitions document is left completely alone. Without
// that, every restart would quietly overwrite whatever an operator had
// since changed with whatever the frozen source document still said --
// an edit that silently reverts overnight is worse than one that never
// applied.
func TestAdoptWatchlistEntriesIsIdempotentAndNeverOverwrites(t *testing.T) {
	eachBackend(t, "adopt-idempotent", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, wlBackend := newBackend(), newBackend()
		seedWatchlistDocument(t, wlBackend, watchlist.Entry{ID: "plain", Name: "watch ssh", Ports: []int{22}})

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		if adopted, err := AdoptWatchlistEntries(context.Background(), s, wlBackend); err != nil || adopted != 1 {
			t.Fatalf("first adoption: adopted=%d err=%v, want 1 and no error", adopted, err)
		}

		// The operator edits it after upgrading, through the definitions
		// API.
		if err := s.UpdateExpectation("plain", func(e *watchlist.Entry) error {
			e.Name = "renamed by the operator"
			e.Ports = []int{22, 2222}
			return nil
		}); err != nil {
			t.Fatalf("UpdateExpectation: %v", err)
		}

		if adopted, err := AdoptWatchlistEntries(context.Background(), s, wlBackend); err != nil || adopted != 0 {
			t.Fatalf("second adoption: adopted=%d err=%v, want 0 and no error", adopted, err)
		}
		// #474: the first adoption and UpdateExpectation both mark the
		// store dirty; the write-behind writer's save for either can
		// still be in flight here. Flush before this subtest's
		// t.TempDir() is removed, same as every other write-behind test
		// in this package (flushDefinitionsForTest/flushForTest).
		flushDefinitionsForTest(t, s)

		e, ok, err := s.GetExpectation("plain")
		if err != nil || !ok {
			t.Fatalf("GetExpectation: ok=%v err=%v", ok, err)
		}
		if e.Name != "renamed by the operator" || len(e.Ports) != 2 {
			t.Errorf("the operator's edit was overwritten by the source document: %+v", e)
		}
	})
}

// TestAdoptWatchlistEntriesFailsClosedOnAnUnreadableSource keeps #378's
// contract on the way through: a watchlist document that exists but
// cannot be parsed is a hard error, never "no entries." Starting with a
// silently empty entry set -- and then persisting that emptiness into
// the definitions document -- is precisely the outcome fail-closed
// opening exists to prevent.
func TestAdoptWatchlistEntriesFailsClosedOnAnUnreadableSource(t *testing.T) {
	eachBackend(t, "adopt-unreadable", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, wlBackend := newBackend(), newBackend()
		writeRaw(t, wlBackend, []byte(`{"entries": [ this is not json`))

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		adopted, err := AdoptWatchlistEntries(context.Background(), s, wlBackend)
		if err == nil {
			t.Fatal("expected an unreadable watchlist document to be refused, not read as empty")
		}
		var startupErr *persist.StartupError
		if !errors.As(err, &startupErr) {
			t.Errorf("error = %v, want a *persist.StartupError so the caller refuses to start", err)
		}
		if adopted != 0 {
			t.Errorf("adopted %d entries from an unreadable document", adopted)
		}
		if definitionsExist(t, defsBackend) {
			t.Error("a definitions document was written despite the source being unreadable")
		}
	})
}

// TestAdoptWatchlistEntriesWritesNothingOnAConversionFailure is the
// one-write-at-the-end half of the #404 migration conventions, applied
// here: every entry is converted before anything is written, so a single
// bad value cannot leave half an entry set adopted and the rest silently
// missing. Reproduced with a value a pre-migration document could
// genuinely contain (a port outside 1-65535) rather than a synthetic
// hook, the same way TestMigrateDefinitionsRefusesOnConversionFailure
// does.
func TestAdoptWatchlistEntriesWritesNothingOnAConversionFailure(t *testing.T) {
	eachBackend(t, "adopt-conversion-failure", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, wlBackend := newBackend(), newBackend()
		seedWatchlistDocument(t, wlBackend,
			watchlist.Entry{ID: "good", Name: "watch ssh", Ports: []int{22}},
			watchlist.Entry{ID: "bad", Name: "impossible port", Ports: []int{70000}},
		)

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		adopted, err := AdoptWatchlistEntries(context.Background(), s, wlBackend)
		if err == nil {
			t.Fatal("expected an unconvertible entry to fail the whole adoption")
		}
		if adopted != 0 {
			t.Errorf("adopted %d entries despite the failure", adopted)
		}
		entries, listErr := s.ListExpectations()
		if listErr != nil {
			t.Fatalf("ListExpectations: %v", listErr)
		}
		if len(entries) != 0 {
			t.Errorf("the store holds %d expectation(s) after a refused adoption, want none -- a partial write is exactly what this refuses", len(entries))
		}
	})
}

// TestAdoptWatchlistEntriesWithNoSourceIsANoop covers the ordinary case
// for every deployment that never had a watchlist document at all: no
// backend, or an absent document, is nothing to do rather than an error
// to refuse over.
func TestAdoptWatchlistEntriesWithNoSourceIsANoop(t *testing.T) {
	eachBackend(t, "adopt-no-source", func(t *testing.T, newBackend func() persist.Backend) {
		s, err := OpenDefinitionsStoreWithBackend(newBackend())
		if err != nil {
			t.Fatal(err)
		}
		if adopted, err := AdoptWatchlistEntries(context.Background(), s, nil); err != nil || adopted != 0 {
			t.Fatalf("no backend: adopted=%d err=%v, want 0 and no error", adopted, err)
		}
		if adopted, err := AdoptWatchlistEntries(context.Background(), s, newBackend()); err != nil || adopted != 0 {
			t.Fatalf("absent document: adopted=%d err=%v, want 0 and no error", adopted, err)
		}
	})
}

// TestAdoptWatchlistEntriesTeardownDoesNotRaceAPendingWrite is #474's
// regression test.
//
// TestAdoptWatchlistEntriesIsIdempotentAndNeverOverwrites failed on PR
// #473's CI (a frontend-only diff) with "TempDir RemoveAll cleanup:
// unlinkat ...: directory not empty" -- t.TempDir()'s own cleanup
// racing an asynchronous write into that directory. The mechanism:
// adoptRaw and UpdateExpectation both call persistLocked, which hands
// the encoded document to the store's write-behind writer (MarkDirty)
// and returns immediately -- nothing waits for that save to actually
// land. Before this issue, neither adopt test flushed before returning,
// so the writer goroutine's save could still be in flight when
// eachBackend's t.TempDir() was removed at subtest teardown.
//
// This is confirmed here, not assumed: run in a tight loop that skips
// go test's own per-subtest scheduling overhead (which is what made the
// race too rare to hit reliably through -race -count=50 against the
// real subtest -- confirmed by running exactly that: 500 iterations, no
// failure), running the same AdoptWatchlistEntries+UpdateExpectation
// sequence the fixed tests above now run, immediately followed by the
// same os.RemoveAll a TempDir cleanup performs. Without a flush in
// between, this reproduces the exact failure this test pins against
// (observed empirically: about 1 failure in 500 runs). With the flush
// -- the fix both tests above now carry -- RemoveAll is unconditionally
// safe, because Flush blocks until the write-behind writer reports
// nothing is dirty any more, i.e. the save has actually completed.
func TestAdoptWatchlistEntriesTeardownDoesNotRaceAPendingWrite(t *testing.T) {
	const iterations = 200
	for i := 0; i < iterations; i++ {
		dir, err := os.MkdirTemp("", "adopt-teardown-race-")
		if err != nil {
			t.Fatal(err)
		}

		defsBackend := persist.NewFileBackend(filepath.Join(dir, "defs.json"))
		wlBackend := persist.NewFileBackend(filepath.Join(dir, "watchlist.json"))
		seedWatchlistDocument(t, wlBackend, watchlist.Entry{ID: "plain", Name: "watch ssh", Ports: []int{22}})

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AdoptWatchlistEntries(context.Background(), s, wlBackend); err != nil {
			t.Fatalf("iteration %d: AdoptWatchlistEntries: %v", i, err)
		}
		if err := s.UpdateExpectation("plain", func(e *watchlist.Entry) error {
			e.Name = "renamed by the operator"
			return nil
		}); err != nil {
			t.Fatalf("iteration %d: UpdateExpectation: %v", i, err)
		}

		// The fix: block until the write-behind writer has actually
		// persisted this change before anything removes the directory
		// it lives in -- see flushDefinitionsForTest.
		flushDefinitionsForTest(t, s)

		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("iteration %d: RemoveAll raced a pending write despite Flush: %v", i, err)
		}
	}
}
