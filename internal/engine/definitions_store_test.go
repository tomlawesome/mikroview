// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// --- test backends -------------------------------------------------

// pgTestDSN/pgNewTestPool duplicate internal/watchlist/characterization_test.go's
// own helpers of the same name rather than importing them (they are
// unexported there) -- that file's own doc comment gives the precedent
// this follows: "this package already keeps its own small, independent
// copies of things other packages also have."
func pgTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("MIKROVIEW_TEST_POSTGRES not set -- skipping the Postgres half of this test")
	}
	return dsn
}

func pgWithSchema(dsn, schema string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func pgNewTestPool(t *testing.T) *persist.Pool {
	t.Helper()
	ctx := context.Background()

	schema := "test_defs_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	var b strings.Builder
	for _, r := range schema {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	schema = b.String()
	if len(schema) > 63 {
		schema = schema[:63]
	}

	setup, err := persist.OpenPool(ctx, pgTestDSN(t))
	if err != nil {
		t.Fatalf("OpenPool (setup): %v", err)
	}
	if _, err := setup.Raw().Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
		t.Fatalf("dropping schema: %v", err)
	}
	if _, err := setup.Raw().Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("creating schema: %v", err)
	}
	setup.Close()

	p, err := persist.OpenPool(ctx, pgWithSchema(pgTestDSN(t), schema))
	if err != nil {
		t.Fatalf("OpenPool: %v", err)
	}
	t.Cleanup(p.Close)
	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := persist.OpenPool(context.Background(), pgTestDSN(t))
		if err != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.Raw().Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	})
	return p
}

// eachBackend runs run against a fresh file backend and, when
// MIKROVIEW_TEST_POSTGRES is set, a fresh Postgres backend too -- the
// same "test the contract once, against both backends" shape
// internal/persist/contract_test.go uses, applied to this package's own
// store/migration tests per issue #404's requirement that the migration
// matrix run "on BOTH backends."
func eachBackend(t *testing.T, name string, run func(t *testing.T, newBackend func() persist.Backend)) {
	t.Helper()

	t.Run(name+"/file", func(t *testing.T) {
		dir := t.TempDir()
		n := 0
		run(t, func() persist.Backend {
			n++
			return persist.NewFileBackend(filepath.Join(dir, name+"-"+strconv.Itoa(n)+".json"))
		})
	})

	t.Run(name+"/postgres", func(t *testing.T) {
		p := pgNewTestPool(t)
		n := 0
		run(t, func() persist.Backend {
			n++
			return persist.NewPostgresBackend(p, name+"_"+strconv.Itoa(n))
		})
	})
}

func flushDefinitionsForTest(t *testing.T, s *DefinitionsStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

// --- basic open contract --------------------------------------------

func TestOpenDefinitionsStoreEmptyPathIsUsable(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatalf("OpenDefinitionsStore(\"\"): %v", err)
	}
	d := NewDefinition("custom", IntentExpectation, KindDeclarative)
	d.Provenance = Provenance{Origin: ProvenanceCustom}
	if err := s.Upsert(d); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, ok := s.Get(d.ID)
	if !ok || !got.Available || got.Definition.Name != "custom" {
		t.Fatalf("Get after Upsert = %+v, %v", got, ok)
	}
}

func TestOpenDefinitionsStoreMissingFileIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.json")
	s, err := OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("OpenDefinitionsStore on a missing file: %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Errorf("expected an empty store, got %v", got)
	}
}

func TestOpenDefinitionsStoreCorruptFileFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDefinitionsStore(path); err == nil {
		t.Fatal("expected OpenDefinitionsStore to refuse a corrupt document")
	}
}

// --- round trip, copy-on-read ----------------------------------------

func TestDefinitionsStorePersistenceRoundTrip(t *testing.T) {
	eachBackend(t, "roundtrip", func(t *testing.T, newBackend func() persist.Backend) {
		b := newBackend()

		s1, err := OpenDefinitionsStoreWithBackend(b)
		if err != nil {
			t.Fatal(err)
		}
		d := NewDefinition("Custom rule", IntentExpectation, KindDeclarative)
		d.Provenance = Provenance{Origin: ProvenanceCustom}
		d.Enabled = true
		if err := s1.Upsert(d); err != nil {
			t.Fatal(err)
		}
		flushDefinitionsForTest(t, s1)

		s2, err := OpenDefinitionsStoreWithBackend(b)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := s2.Get(d.ID)
		if !ok {
			t.Fatal("expected the definition to survive a reopen")
		}
		if got.Definition.Name != "Custom rule" || !got.Available {
			t.Errorf("got %+v", got)
		}
	})
}

// testDetectionSpec is the smallest well-formed detection block, for
// tests whose subject is the store rather than the detector. A stored
// custom detection has to carry one (see upsertLocking): it is rebuilt
// from its bytes alone, so without it there would be nothing to rebuild.
func testDetectionSpec() *DetectionSpec {
	return &DetectionSpec{
		Conditions:     []Condition{{Field: FieldDestinationPort, Operator: OpEquals, Values: []string{"22"}}},
		Key:            KeyPerSource,
		Counting:       CountingTotal,
		DetailTemplate: "{Count} events from {SourceAddress}",
	}
}

func TestDefinitionsStoreGetIsADeepCopy(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDefinition("mutate me", IntentDetection, KindDeclarative)
	d.Provenance = Provenance{Origin: ProvenanceCustom}
	d.Detection = testDetectionSpec()
	d.Scope.Hosts = []string{"10.0.0.1"}
	if err := s.Upsert(d); err != nil {
		t.Fatal(err)
	}

	got, _ := s.Get(d.ID)
	got.Definition.Scope.Hosts[0] = "mutated"
	got.Definition.Name = "mutated"

	again, _ := s.Get(d.ID)
	if again.Definition.Scope.Hosts[0] != "10.0.0.1" {
		t.Errorf("mutating a Get result reached back into the store: Hosts = %v", again.Definition.Scope.Hosts)
	}
	if again.Definition.Name != "mutate me" {
		t.Errorf("mutating a Get result reached back into the store: Name = %v", again.Definition.Name)
	}
}

// --- shipped/unavailable immutability ---------------------------------

func shippedDefinition(id string) Definition {
	return Definition{
		ID:         id,
		Name:       id,
		Intent:     IntentDetection,
		Kind:       KindProgrammatic,
		Provenance: Provenance{Origin: ProvenanceShipped},
	}
}

func TestDefinitionsStoreRefusesToDeleteAShippedDefinition(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	d := shippedDefinition("port_scan")
	if err := s.Upsert(d); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("port_scan"); err == nil {
		t.Fatal("expected Delete to refuse a shipped definition")
	}
	if _, ok := s.Get("port_scan"); !ok {
		t.Fatal("the shipped definition was removed despite Delete erroring")
	}
}

func TestDefinitionsStoreRefusesToOverwriteAShippedDefinition(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	d := shippedDefinition("port_scan")
	if err := s.Upsert(d); err != nil {
		t.Fatal(err)
	}
	replacement := NewDefinition("hijacked", IntentDetection, KindDeclarative)
	replacement.ID = "port_scan"
	replacement.Provenance = Provenance{Origin: ProvenanceCustom}
	if err := s.Upsert(replacement); err == nil {
		t.Fatal("expected Upsert to refuse overwriting a shipped definition wholesale")
	}
	got, _ := s.Get("port_scan")
	if got.Definition.Name == "hijacked" {
		t.Fatal("the shipped definition was overwritten despite Upsert erroring")
	}
}

// TestDefinitionsStorePreservesUnknownDefinitionByteForByte is issue
// #404's central guarantee, tested directly against the store (the
// migration-scoped version lives in definitions_migrate_test.go): a
// definition this binary cannot identify -- an unrecognized Kind, the
// "downgrade" case StoredDefinition.Available documents -- survives a
// boot, an unrelated write, and a reopen with its stored value
// completely unchanged.
//
// The seed document is produced the same way a real definitions store
// would produce it -- json.MarshalIndent, exactly what persistLocked
// itself uses -- rather than hand-typed JSON. That matters for what
// "byte-for-byte" can honestly mean here: encoding/json's indent pass
// re-tokenizes every embedded json.RawMessage as part of formatting the
// surrounding document (see persistLocked's own doc comment), so a
// hand-typed compact literal would visibly "change" on the very first
// persist even though nothing about its value moved. A realistic seed --
// what an actual newer-version mikroview would have written -- round-
// trips through that same indent algorithm deterministically and comes
// out identical, which is the meaningful claim: this store never
// mutates, drops, or reformats-as-if-touched an entry it never actually
// touched.
func TestDefinitionsStorePreservesUnknownDefinitionByteForByte(t *testing.T) {
	eachBackend(t, "unknown-def", func(t *testing.T, newBackend func() persist.Backend) {
		b := newBackend()

		unknownValue := `{"id":"future-def","name":"From a newer mikroview","intent":"detection","kind":"quantum_anomaly","enabled":true,"params":{"threshold":9000},"provenance":{"origin":"shipped"}}`
		seed := definitionsDocument{
			Version: definitionsDocumentVersion,
			Definitions: map[string]json.RawMessage{
				"future-def": json.RawMessage(unknownValue),
			},
		}
		seedBytes, err := json.MarshalIndent(seed, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Save(context.Background(), seedBytes, 0); err != nil {
			t.Fatalf("seeding: %v", err)
		}
		// The value to compare against after the round trip: the exact
		// bytes a fresh decode of the realistic seed produces for this
		// entry, not the hand-typed literal above -- MarshalIndent's own
		// re-tokenization already happened once, at seed time, so this
		// is what "unchanged" actually means from here on.
		var seedDoc definitionsDocument
		if err := json.Unmarshal(seedBytes, &seedDoc); err != nil {
			t.Fatal(err)
		}
		unknown := string(seedDoc.Definitions["future-def"])

		s, err := OpenDefinitionsStoreWithBackend(b)
		if err != nil {
			t.Fatalf("boot: %v", err)
		}

		got, ok := s.Get("future-def")
		if !ok {
			t.Fatal("the unknown definition did not survive boot at all")
		}
		if got.Available {
			t.Fatal("expected the unknown-kind definition to be marked unavailable")
		}

		// Write something else -- this is what exercises persistLocked's
		// re-marshal of the whole document, including the untouched
		// unknown entry.
		other := NewDefinition("something else", IntentDetection, KindDeclarative)
		other.Provenance = Provenance{Origin: ProvenanceCustom}
		other.Detection = testDetectionSpec()
		if err := s.Upsert(other); err != nil {
			t.Fatal(err)
		}
		flushDefinitionsForTest(t, s)

		// Reopen fresh -- a new process, not the same in-memory state.
		s2, err := OpenDefinitionsStoreWithBackend(b)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		again, ok := s2.Get("future-def")
		if !ok {
			t.Fatal("the unknown definition did not survive the write+reopen round trip")
		}
		if again.Available {
			t.Fatal("expected the unknown-kind definition to still be marked unavailable")
		}

		// Byte-for-byte: reach past the store's own decode (which would
		// silently normalize field order/whitespace) and compare the raw
		// stored bytes for this one entry directly.
		snap, err := b.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var doc definitionsDocument
		if err := json.Unmarshal(snap.Payload, &doc); err != nil {
			t.Fatal(err)
		}
		gotRaw, ok := doc.Definitions["future-def"]
		if !ok {
			t.Fatal("future-def is missing from the reloaded document entirely")
		}
		if string(gotRaw) != unknown {
			t.Errorf("unknown definition did not survive byte-for-byte:\n got:  %s\n want: %s", gotRaw, unknown)
		}

		// And Delete/Upsert against it are refused, for the same reason.
		if err := s2.Delete("future-def"); err == nil {
			t.Error("expected Delete to refuse an unavailable definition")
		}
		if _, ok := s2.Get("future-def"); !ok {
			t.Error("future-def was removed despite Delete erroring")
		}
	})
}

func TestDefinitionsStoreListIncludesUnavailableDefinitions(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	// Upsert can't create an unavailable entry directly (it validates),
	// so seed one the same way convertDetectSettings' unrecognized-
	// detector path does: an empty Kind.
	s.mu.Lock()
	s.raw["unavailable-1"] = json.RawMessage(`{"id":"unavailable-1","name":"x","intent":"detection","kind":"","provenance":{"origin":"shipped"}}`)
	s.mu.Unlock()

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("List() = %d entries, want 1", len(list))
	}
	if list[0].Available {
		t.Error("expected the seeded entry to be unavailable")
	}
	if list[0].Definition.ID != "unavailable-1" {
		t.Errorf("List() lost the ID of an unavailable entry: %+v", list[0])
	}
}

// TestSetEnabledAndScopeWorksOnAShippedDefinition is the door
// ErrDefinitionImmutable's own message always implied and issue #405
// finally had to open: "disable it instead (Enabled=false)" was
// unfollowable while Upsert refused a shipped definition wholesale and
// nothing else could change one. internal/detect's settings store used
// to be where a detector toggle landed; it is deleted, so this is.
func TestSetEnabledAndScopeWorksOnAShippedDefinition(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := SeedShippedDefinitions(s, nil, DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}

	scope := Scope{Hosts: []string{"203.0.113.0/24"}, HostsMode: ListModeDeny}
	if err := s.SetEnabledAndScope("port_scan", false, scope); err != nil {
		t.Fatalf("SetEnabledAndScope: %v", err)
	}

	got, ok := s.Get("port_scan")
	if !ok {
		t.Fatal("expected port_scan to still exist")
	}
	if got.Definition.Enabled {
		t.Error("expected port_scan to be disabled")
	}
	if len(got.Definition.Scope.Hosts) != 1 || got.Definition.Scope.HostsMode != ListModeDeny {
		t.Errorf("Scope = %+v, want the one just set", got.Definition.Scope)
	}
	// Everything else is untouched -- the narrowness is the point.
	// Params come back JSON-decoded (a float64 where the schema says int
	// -- decodeStored deliberately does not re-validate, see its own doc
	// comment), so this compares the value rather than the Go type.
	if fmt.Sprint(got.Definition.Params["threshold"]) != fmt.Sprint(DefaultShippedDefaults().PortScanThreshold) {
		t.Errorf("Params were disturbed: %+v", got.Definition.Params)
	}
	if got.Definition.Provenance.Origin != ProvenanceShipped {
		t.Errorf("Provenance = %q, want it unchanged", got.Definition.Provenance.Origin)
	}
}

func TestSetEnabledAndScopeRejectsAnUnknownID(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabledAndScope("not_a_definition", false, Scope{}); !errors.Is(err, ErrNoSuchDefinition) {
		t.Errorf("SetEnabledAndScope on an unknown id = %v, want ErrNoSuchDefinition", err)
	}
}

func TestSetEnabledAndScopeRejectsAMalformedScope(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	if err := SeedShippedDefinitions(s, nil, DefaultShippedDefaults()); err != nil {
		t.Fatalf("SeedShippedDefinitions: %v", err)
	}
	err = s.SetEnabledAndScope("port_scan", true, Scope{HostsMode: ListMode("maybe")})
	if err == nil {
		t.Fatal("expected an invalid list mode to be refused")
	}
	if got, _ := s.Get("port_scan"); !got.Definition.Enabled || len(got.Definition.Scope.Hosts) != 0 {
		t.Errorf("a refused change must leave the definition untouched, got %+v", got.Definition)
	}
}

// TestSetEnabledAndScopeRefusesAnUnavailableDefinition pins the one case
// it does refuse: toggling something whose meaning this binary cannot
// read is exactly as unsafe as overwriting it.
func TestSetEnabledAndScopeRefusesAnUnavailableDefinition(t *testing.T) {
	s, err := OpenDefinitionsStore("")
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.raw["mystery"] = json.RawMessage(`{"id":"mystery","kind":"who-knows"}`)
	s.mu.Unlock()

	if err := s.SetEnabledAndScope("mystery", false, Scope{}); !errors.Is(err, ErrDefinitionImmutable) {
		t.Errorf("SetEnabledAndScope on an unavailable definition = %v, want ErrDefinitionImmutable", err)
	}
}
