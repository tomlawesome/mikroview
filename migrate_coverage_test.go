// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

// TestMigrationCoversAllConfigPathFields is #372's recurrence guard
// applied to #537's list.
//
// migratedStores (migrate_cli.go) is the second hand-maintained list of
// (name, path) pairs in this package, and the first one -- backedUpStores
// -- silently fell three fields behind config.Config for months (#372:
// Watchlist's three stores missing from every backup, no error, no log
// line). A list of the same shape has the same failure available to it,
// so it gets the same guard: walk config.Config by reflection, and
// require every string field named *Path or *File to be either carried
// by migratedStores or listed with a reason on excludedFromMigration.
//
// Wider than TestBackupCoversAllConfigPathFields, which walks *Path
// only. A migration is about everything living on one mount rather than
// about stores specifically, so the operator-supplied files -- TLS
// certificate, key, Postgres DSN -- are decisions this list has to have
// made rather than fields it is entitled to have never heard of.
//
// Proof this catches drift, not just passes: see the sibling test
// TestMigrationCoverageGuardActuallyFailsOnDrift below.
func TestMigrationCoversAllConfigPathFields(t *testing.T) {
	markers, cfg := markConfigPathFields()

	if len(markers) == 0 {
		t.Fatal("test setup: reflection found no *Path/*File fields on config.Config -- the walk is broken")
	}

	carried := map[string]bool{}
	for _, s := range migratedStores(cfg) {
		carried[s.Path] = true
	}

	for name, marker := range markers {
		if carried[marker] {
			continue
		}
		if _, ok := excludedFromMigration[name]; ok {
			continue
		}
		t.Errorf("config.%s is a *Path/*File field with no migration decision recorded: it is not "+
			"carried by migratedStores and not listed on excludedFromMigration. Add it to one -- with "+
			"a reason if excluding it. A store left behind by -migrate-data is data the operator "+
			"believes they moved", name)
	}

	// The reverse: an exclusion naming a field that no longer exists is a
	// stale claim rather than a decision, and reads as coverage.
	for name := range excludedFromMigration {
		if _, ok := markers[name]; !ok {
			t.Errorf("excludedFromMigration lists %q but no such *Path/*File field exists on "+
				"config.Config -- stale entry, remove it", name)
		}
	}
}

// TestMigrationCoverageGuardActuallyFailsOnDrift runs the guard's own
// logic against a deliberately short list, so the guard is known to fail
// when it should rather than assumed to.
//
// The guard above passes today whether or not it works: a test that has
// never failed proves nothing about the drift it claims to catch. This
// reproduces #372's exact shape -- a config field no entry carries -- and
// requires the check to name it.
func TestMigrationCoverageGuardActuallyFailsOnDrift(t *testing.T) {
	markers, cfg := markConfigPathFields()

	// The real list, minus the recovery pepper: the pepper is the entry
	// whose loss is quietest, since every recovery key silently stops
	// verifying and nothing says so until someone needs one.
	drifted := []migratedStore{}
	for _, s := range migratedStores(cfg) {
		if s.Path == markers["Auth.RecoveryPepperPath"] {
			continue
		}
		drifted = append(drifted, s)
	}

	uncovered := uncoveredFields(markers, drifted, excludedFromMigration)
	if len(uncovered) != 1 || uncovered[0] != "Auth.RecoveryPepperPath" {
		t.Fatalf("the coverage check did not catch a dropped store: reported %v, want "+
			"[Auth.RecoveryPepperPath]. The guard in TestMigrationCoversAllConfigPathFields "+
			"is not actually checking anything", uncovered)
	}

	// And the other direction: dropping an exclusion has to be caught
	// too, or a file could quietly become nobody's decision.
	trimmed := map[string]string{}
	for name, reason := range excludedFromMigration {
		if name == "TLS.CertFile" {
			continue
		}
		trimmed[name] = reason
	}
	uncovered = uncoveredFields(markers, migratedStores(cfg), trimmed)
	if len(uncovered) != 1 || uncovered[0] != "TLS.CertFile" {
		t.Fatalf("the coverage check did not catch a dropped exclusion: reported %v, want "+
			"[TLS.CertFile]", uncovered)
	}
}

// uncoveredFields is the coverage rule itself, extracted so the guard and
// the proof that the guard works run the same code rather than two
// similar-looking pieces of code.
func uncoveredFields(markers map[string]string, stores []migratedStore, excluded map[string]string) []string {
	carried := map[string]bool{}
	for _, s := range stores {
		carried[s.Path] = true
	}
	var uncovered []string
	for name, marker := range markers {
		if carried[marker] {
			continue
		}
		if _, ok := excluded[name]; ok {
			continue
		}
		uncovered = append(uncovered, name)
	}
	return uncovered
}

// markConfigPathFields writes a unique marker into every *Path/*File
// string field on a zero config.Config and returns the markers by dotted
// field name, so coverage can be checked by value without migratedStores
// having to report field names it does not know.
func markConfigPathFields() (map[string]string, config.Config) {
	var cfg config.Config
	markers := map[string]string{}

	var walk func(rv reflect.Value, prefix string)
	walk = func(rv reflect.Value, prefix string) {
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if field.PkgPath != "" {
				continue // unexported; config.Config has none, but stay honest
			}
			fv := rv.Field(i)
			name := prefix + field.Name
			switch fv.Kind() {
			case reflect.Struct:
				walk(fv, name+".")
			case reflect.String:
				if strings.HasSuffix(field.Name, "Path") || strings.HasSuffix(field.Name, "File") {
					marker := "537-marker:" + name
					fv.SetString(marker)
					markers[name] = marker
				}
			}
		}
	}
	walk(reflect.ValueOf(&cfg).Elem(), "")
	return markers, cfg
}
