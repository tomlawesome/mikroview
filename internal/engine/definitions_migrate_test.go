// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/persist"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// writeJSON marshals v and saves it as a fresh document (expect=0) --
// the test-side equivalent of a source store's very first persist,
// standing in for internal/detect.SettingsStore/internal/watchlist.Store
// actually having written it.
func writeJSON(t *testing.T, b persist.Backend, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}
	if _, err := b.Save(context.Background(), data, 0); err != nil {
		t.Fatalf("seeding %s: %v", b.Describe(), err)
	}
}

func writeRaw(t *testing.T, b persist.Backend, data []byte) {
	t.Helper()
	if _, err := b.Save(context.Background(), data, 0); err != nil {
		t.Fatalf("seeding %s: %v", b.Describe(), err)
	}
}

func loadBytes(t *testing.T, b persist.Backend) []byte {
	t.Helper()
	snap, err := b.Load(context.Background())
	if err != nil {
		t.Fatalf("Load %s: %v", b.Describe(), err)
	}
	return snap.Payload
}

func definitionsExist(t *testing.T, b persist.Backend) bool {
	t.Helper()
	snap, err := b.Load(context.Background())
	if err != nil {
		t.Fatalf("Load %s: %v", b.Describe(), err)
	}
	return snap.Exists
}

// --- shape 1: empty sources ---------------------------------------

func TestMigrateDefinitionsEmptySources(t *testing.T) {
	eachBackend(t, "migrate-empty", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()

		migrated, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err != nil {
			t.Fatalf("MigrateDefinitions: %v", err)
		}
		if !migrated {
			t.Fatal("expected migration to run against empty sources")
		}

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		list := s.List()
		if len(list) != len(shippedDetectors) {
			t.Fatalf("got %d definitions, want %d (the shipped baseline only)", len(list), len(shippedDetectors))
		}
		for _, sd := range list {
			if !sd.Available {
				t.Errorf("shipped definition %q came back unavailable", sd.Definition.ID)
			}
			if !sd.Definition.Enabled {
				t.Errorf("shipped definition %q should default enabled", sd.Definition.ID)
			}
			if sd.Definition.Provenance.Origin != ProvenanceShipped {
				t.Errorf("shipped definition %q has provenance %q", sd.Definition.ID, sd.Definition.Provenance.Origin)
			}
		}
	})
}

// --- shape 2: default sources (explicit defaults on disk) ---------

func TestMigrateDefinitionsDefaultSources(t *testing.T) {
	eachBackend(t, "migrate-default", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()
		writeJSON(t, settingsBackend, DefaultDetectorSettings())
		writeJSON(t, wlBackend, migrateWatchlistFile{})

		migrated, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err != nil {
			t.Fatalf("MigrateDefinitions: %v", err)
		}
		if !migrated {
			t.Fatal("expected migration to run")
		}

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.List()) != len(shippedDetectors) {
			t.Fatalf("got %d definitions, want %d", len(s.List()), len(shippedDetectors))
		}
		got, ok := s.Get("port_scan")
		if !ok || !got.Available || !got.Definition.Enabled {
			t.Fatalf("port_scan = %+v, %v", got, ok)
		}
		if got.Definition.Params["threshold"].(float64) != 15 {
			t.Errorf("port_scan threshold = %v, want the DefaultConfig() value 15", got.Definition.Params["threshold"])
		}
	})
}

// --- shape 3: heavily customised -----------------------------------

func TestMigrateDefinitionsHeavilyCustomised(t *testing.T) {
	eachBackend(t, "migrate-custom", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()

		settings := map[string]DetectorSettings{
			"port_scan": {Enabled: false},
			"critical_port": {
				Enabled: true,
				Scope: Scope{
					Hosts:     []string{"10.0.0.0/24", "192.168.1.5"},
					HostsMode: ListModeDeny,
					Ports:     []int{22, 3389},
					PortsMode: ListModeAllow,
				},
			},
			"rule_spike": {
				Enabled: true,
				Scope:   Scope{Rules: []string{"lan-in", "wan-in"}, RulesMode: ListModeAllow},
			},
		}
		writeJSON(t, settingsBackend, settings)

		entries := []*watchlist.Entry{
			{
				ID:        "wl-1",
				Name:      "Printer only from office VLAN",
				Source:    matchlogIdentity("aa:bb:cc:dd:ee:ff", ""),
				DestIP:    "192.168.5.20",
				Ports:     []int{9100, 515},
				CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			},
			{
				ID:         "wl-2",
				Name:       "Any critical port from guest VLAN",
				Ports:      []int{22, 23, 3389},
				SourceList: watchlist.AddressListRef{Device: "core-router", List: "guest-vlan"},
				CreatedAt:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
			},
			{
				ID:        "wl-3",
				Name:      "NAS should only reach backup host",
				Source:    matchlogIdentity("", "10.0.0.50"),
				Invert:    true,
				Observing: false,
				Permitted: []watchlist.PermittedDest{{DestIP: "10.0.0.99", Port: 443}},
				CreatedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			},
		}
		writeJSON(t, wlBackend, migrateWatchlistFile{Entries: entries})

		migrated, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err != nil {
			t.Fatalf("MigrateDefinitions: %v", err)
		}
		if !migrated {
			t.Fatal("expected migration to run")
		}

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}

		if got, _ := s.Get("port_scan"); got.Definition.Enabled {
			t.Error("port_scan should have migrated as disabled")
		}
		cp, ok := s.Get("critical_port")
		if !ok || len(cp.Definition.Scope.Hosts) != 2 || cp.Definition.Scope.HostsMode != ListModeDeny {
			t.Errorf("critical_port scope did not migrate: %+v", cp.Definition.Scope)
		}
		rs, ok := s.Get("rule_spike")
		if !ok || len(rs.Definition.Scope.Rules) != 2 {
			t.Errorf("rule_spike scope did not migrate: %+v", rs.Definition.Scope)
		}

		wl1, ok := s.Get("wl-1")
		if !ok || !wl1.Available {
			t.Fatalf("wl-1 missing or unavailable: %+v", wl1)
		}
		if wl1.Definition.Kind != KindDeclarative || wl1.Definition.Provenance.Origin != ProvenanceCustom {
			t.Errorf("wl-1 kind/provenance = %v/%v, want declarative/custom", wl1.Definition.Kind, wl1.Definition.Provenance.Origin)
		}
		if wl1.Definition.Name != "Printer only from office VLAN" {
			t.Errorf("wl-1 name = %q", wl1.Definition.Name)
		}
		destIP, _ := wl1.Definition.Params["destIp"].([]any)
		if len(destIP) != 1 || destIP[0] != "192.168.5.20" {
			t.Errorf("wl-1 destIp = %v", wl1.Definition.Params["destIp"])
		}

		wl2, ok := s.Get("wl-2")
		if !ok {
			t.Fatal("wl-2 missing")
		}
		listDevice, _ := wl2.Definition.Params["sourceListDevice"].([]any)
		if len(listDevice) != 1 || listDevice[0] != "core-router" {
			t.Errorf("wl-2 sourceListDevice = %v", wl2.Definition.Params["sourceListDevice"])
		}

		wl3, ok := s.Get("wl-3")
		if !ok {
			t.Fatal("wl-3 missing")
		}
		if wl3.Definition.Kind != KindProgrammatic || wl3.Definition.Provenance.Origin != ProvenanceShipped {
			t.Errorf("wl-3 kind/provenance = %v/%v, want programmatic/shipped", wl3.Definition.Kind, wl3.Definition.Provenance.Origin)
		}
		permittedJSON, _ := wl3.Definition.Params["permittedJSON"].([]any)
		if len(permittedJSON) != 1 || !strings.Contains(permittedJSON[0].(string), "10.0.0.99") {
			t.Errorf("wl-3 permittedJSON = %v", wl3.Definition.Params["permittedJSON"])
		}
	})
}

// --- shape 4: inverted entry mid-observation ------------------------

func TestMigrateDefinitionsInvertedEntryMidObservation(t *testing.T) {
	eachBackend(t, "migrate-mid-observe", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()

		firstSeen := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
		lastSeen := time.Date(2026, 5, 3, 14, 30, 0, 0, time.UTC)
		entries := []*watchlist.Entry{
			{
				ID:        "wl-observing",
				Name:      "New IoT device egress",
				Source:    matchlogIdentity("11:22:33:44:55:66", ""),
				Invert:    true,
				Observing: true, // still mid-observation -- must not be reset
				Permitted: []watchlist.PermittedDest{{DestIP: "8.8.8.8", Port: 443}},
				Observed: []watchlist.ObservedDest{
					{DestIP: "203.0.113.5", Port: 443, FirstSeen: firstSeen, LastSeen: lastSeen, Count: 42},
					{DestIP: "203.0.113.6", Port: 8443, FirstSeen: firstSeen, LastSeen: firstSeen, Count: 1},
				},
			},
		}
		writeJSON(t, wlBackend, migrateWatchlistFile{Entries: entries})

		if _, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend); err != nil {
			t.Fatalf("MigrateDefinitions: %v", err)
		}

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := s.Get("wl-observing")
		if !ok || !got.Available {
			t.Fatalf("wl-observing missing or unavailable: %+v", got)
		}
		observing, _ := got.Definition.Params["observing"].(bool)
		if !observing {
			t.Error("Observing=true was not carried over -- an in-progress observation period must not be reset")
		}
		observedRaw, _ := got.Definition.Params["observedJSON"].([]any)
		if len(observedRaw) != 1 {
			t.Fatalf("observedJSON = %v", got.Definition.Params["observedJSON"])
		}
		var observed []watchlist.ObservedDest
		if err := json.Unmarshal([]byte(observedRaw[0].(string)), &observed); err != nil {
			t.Fatalf("decoding observedJSON: %v", err)
		}
		if len(observed) != 2 || observed[0].Count != 42 || observed[1].DestIP != "203.0.113.6" {
			t.Errorf("Observed state did not survive intact: %+v", observed)
		}
		if !observed[0].FirstSeen.Equal(firstSeen) || !observed[0].LastSeen.Equal(lastSeen) {
			t.Errorf("Observed timestamps did not survive intact: %+v", observed[0])
		}
	})
}

// --- shape 5: an unrecognized detector name -------------------------

func TestMigrateDefinitionsUnrecognizedDetectorNameIsPreservedUnavailable(t *testing.T) {
	eachBackend(t, "migrate-unknown-detector", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()

		settings := map[string]DetectorSettings{
			"port_scan":            {Enabled: true},
			string("future_thing"): {Enabled: true, Scope: Scope{Hosts: []string{"10.0.0.1"}}},
		}
		writeJSON(t, settingsBackend, settings)

		migrated, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err != nil {
			t.Fatalf("MigrateDefinitions should succeed on an unrecognized-but-well-formed detector name: %v", err)
		}
		if !migrated {
			t.Fatal("expected migration to run")
		}

		s, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.List()) != len(shippedDetectors)+1 {
			t.Fatalf("got %d definitions, want %d (12 shipped + 1 preserved-unavailable)", len(s.List()), len(shippedDetectors)+1)
		}
		got, ok := s.Get("legacy-detector:future_thing")
		if !ok {
			t.Fatal("the unrecognized detector's settings were dropped instead of preserved")
		}
		if got.Available {
			t.Error("expected the unrecognized detector's entry to be marked unavailable")
		}
		if got.Definition.Params["legacyDetectorName"] != "future_thing" {
			t.Errorf("legacyDetectorName = %v", got.Definition.Params["legacyDetectorName"])
		}

		// Never dropped on any write path: Upsert something unrelated,
		// reopen, confirm it is still there.
		other := NewDefinition("unrelated", IntentDetection, KindDeclarative)
		other.Provenance = Provenance{Origin: ProvenanceCustom}
		if err := s.Upsert(other); err != nil {
			t.Fatal(err)
		}
		flushDefinitionsForTest(t, s)
		s2, err := OpenDefinitionsStoreWithBackend(defsBackend)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := s2.Get("legacy-detector:future_thing"); !ok {
			t.Fatal("the unrecognized detector's entry was dropped by an unrelated write")
		}
	})
}

// --- shape 6: corrupt sources refuse the whole migration -------------

func TestMigrateDefinitionsCorruptDetectSettingsSourceFailsClosed(t *testing.T) {
	eachBackend(t, "migrate-corrupt-settings", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()
		corrupt := []byte(`{"port_scan": not valid json`)
		writeRaw(t, settingsBackend, corrupt)
		writeJSON(t, wlBackend, migrateWatchlistFile{})

		_, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err == nil {
			t.Fatal("expected MigrateDefinitions to refuse a corrupt detector-settings source")
		}
		if !strings.Contains(err.Error(), "detector settings") {
			t.Errorf("error does not name the failing store: %v", err)
		}
		if definitionsExist(t, defsBackend) {
			t.Error("a definitions document was created despite the migration failing")
		}
		if string(loadBytes(t, settingsBackend)) != string(corrupt) {
			t.Error("the corrupt source's bytes were modified")
		}
	})
}

func TestMigrateDefinitionsCorruptWatchlistSourceFailsClosed(t *testing.T) {
	eachBackend(t, "migrate-corrupt-watchlist", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()
		writeJSON(t, settingsBackend, DefaultDetectorSettings())
		corrupt := []byte(`{"entries": [{`)
		writeRaw(t, wlBackend, corrupt)

		_, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err == nil {
			t.Fatal("expected MigrateDefinitions to refuse a corrupt watchlist source")
		}
		if !strings.Contains(err.Error(), "watchlist") {
			t.Errorf("error does not name the failing store: %v", err)
		}
		if definitionsExist(t, defsBackend) {
			t.Error("a definitions document was created despite the migration failing")
		}
		if string(loadBytes(t, wlBackend)) != string(corrupt) {
			t.Error("the corrupt source's bytes were modified")
		}
	})
}

// TestMigrateDefinitionsRefusesOnConversionFailure is the "failure
// injected PARTWAY through conversion, not only at load" case issue
// #404 asks for. Both sources are well-formed, valid JSON -- persist.Open
// succeeds for both -- but one watchlist entry carries a port number
// outside 1-65535, a value internal/watchlist.Store.Upsert has never
// range-checked (only "at least one port," see ErrNoPorts), so it is a
// realistic value a pre-migration document could actually contain. It
// only fails once convertToDefinitions tries to validate it against
// watchlistNonInvertedParamSchema -- i.e. partway through conversion,
// after every load/parse step already succeeded.
func TestMigrateDefinitionsRefusesOnConversionFailure(t *testing.T) {
	eachBackend(t, "migrate-conversion-fail", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()
		writeJSON(t, settingsBackend, DefaultDetectorSettings())
		entries := []*watchlist.Entry{
			{ID: "wl-ok", Name: "fine", Ports: []int{443}},
			{ID: "wl-bad", Name: "out of range", Ports: []int{99999}},
		}
		wlPayload := migrateWatchlistFile{Entries: entries}
		writeJSON(t, wlBackend, wlPayload)

		before := loadBytes(t, wlBackend)

		_, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err == nil {
			t.Fatal("expected MigrateDefinitions to refuse on a conversion-time failure")
		}
		if definitionsExist(t, defsBackend) {
			t.Error("a definitions document was created despite the migration failing partway through conversion")
		}
		if string(loadBytes(t, wlBackend)) != string(before) {
			t.Error("the watchlist source's bytes were modified by a failed migration")
		}
		if string(loadBytes(t, settingsBackend)) == "" {
			t.Error("test setup: the settings source should still hold its seeded bytes")
		}
	})
}

// --- idempotence ------------------------------------------------------

func TestMigrateDefinitionsSecondBootIsANoOp(t *testing.T) {
	eachBackend(t, "migrate-idempotent", func(t *testing.T, newBackend func() persist.Backend) {
		defsBackend, settingsBackend, wlBackend := newBackend(), newBackend(), newBackend()
		writeJSON(t, settingsBackend, DefaultDetectorSettings())

		migrated, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err != nil || !migrated {
			t.Fatalf("first migration: migrated=%v err=%v", migrated, err)
		}
		firstPayload := loadBytes(t, defsBackend)

		// The source changes after the first migration -- must not be
		// picked up by a second boot.
		mutated := map[string]DetectorSettings{"port_scan": {Enabled: false}}
		if _, err := settingsBackend.Save(context.Background(), mustMarshal(t, mutated), 0); err == nil {
			// This particular Save is expected to conflict (the store
			// already has a document from writeJSON above); either
			// outcome is fine for this test, only the definitions
			// document's stability matters below.
		}

		migrated2, err := MigrateDefinitions(context.Background(), defsBackend, settingsBackend, wlBackend)
		if err != nil {
			t.Fatalf("second migration: %v", err)
		}
		if migrated2 {
			t.Fatal("expected the second boot to be a no-op")
		}
		if string(loadBytes(t, defsBackend)) != string(firstPayload) {
			t.Error("the definitions document changed on a second, supposedly no-op boot")
		}
	})
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// matchlogIdentity builds a watchlist.Entry.Source value -- a small
// helper purely to keep the fixtures above one line each.
func matchlogIdentity(mac, ip string) matchlog.Identity {
	return matchlog.Identity{MAC: mac, IP: ip}
}

// unwritableBackend has a Load that always reports "nothing stored yet"
// and a Save that always fails -- the shape a real FileBackend takes
// when its directory cannot be created (the live-check regression this
// reproduces: a rootless deployment where /var/lib/mikroview cannot be
// created, discovered by actually running mikroview rather than by
// reading the code -- see AGENTS.md's "Run it before you ship it"). A
// small local copy of the failingBackend/unreadableBackend pattern
// internal/persist/writebehind_test.go already uses, rather than
// exporting a test double from that package.
type unwritableBackend struct{}

func (unwritableBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}
func (unwritableBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	return 0, fmt.Errorf("mkdir /var/lib/mikroview: permission denied")
}
func (unwritableBackend) Close() error     { return nil }
func (unwritableBackend) Describe() string { return "unwritable test backend" }

// TestMigrateDefinitionsWriteFailureIsNotFatal is the fix for a real
// defect live-check found (not the unit suite): a destination that
// cannot be written to yet -- unlike an unreadable *source*, or a
// conversion failure -- must not take the whole server down.
// MigrateDefinitions returning a plain error here and main.go treating
// every MigrateDefinitions error as fatal crashed mikroview outright the
// first time this ran against a real, rootless deployment where
// /var/lib/mikroview's default definitions.json path could not be
// created. Neither source is ever touched by MigrateDefinitions, and the
// definitions document still does not exist either way -- there is
// nothing here to protect by refusing to start, unlike an unreadable
// source or a conversion failure, so this must be distinguishable
// (errors.Is against ErrMigrationWriteFailed) rather than indistinguishable
// from those.
func TestMigrateDefinitionsWriteFailureIsNotFatal(t *testing.T) {
	_, err := MigrateDefinitions(context.Background(), unwritableBackend{}, nil, nil)
	if err == nil {
		t.Fatal("expected an error when the destination cannot be written")
	}
	if !errors.Is(err, ErrMigrationWriteFailed) {
		t.Errorf("error is not ErrMigrationWriteFailed: %v", err)
	}
	var startupErr *persist.StartupError
	if errors.As(err, &startupErr) {
		t.Errorf("a write failure must not present as *persist.StartupError (that would make main.go treat it as fatal): %v", err)
	}
}
