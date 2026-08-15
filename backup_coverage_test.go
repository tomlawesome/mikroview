// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/config"
)

// TestBackupCoversAllConfigPathFields is #372's recurrence guard.
//
// backedUpStores was a hand-maintained list of nine (Name, Path) pairs
// that silently fell three fields behind config.Config: Watchlist's
// StorePath, SuggestionsStorePath and MatchLogPath were never added, so
// `-backup` dropped an operator's entire watchlist configuration with no
// error, no log line, and no sign anything was missing until a `-restore`
// came up short. Nothing caught that drift because nothing compared
// backedUpStores against config.Config itself.
//
// This test does that comparison. It walks config.Config by reflection,
// finds every string field whose name ends in "Path" -- the naming
// convention every persisted-document field in this package already
// follows (StorePath, TokensStorePath, RecoveryKeysPath, MatchLogPath,
// DBPath, ...) -- and requires each one to be either carried by
// backedUpStores or listed, with a reason, on excludedFromBackup
// (backup_cli.go). A field satisfying neither fails the test: the same
// silent drift #372 found, but now caught at build time instead of by an
// operator during a disaster recovery.
//
// Proof this actually catches the drift: with backedUpStores' three
// Watchlist entries removed, this test fails with exactly the three
// missing fields named (verified by hand while developing this fix --
// `git stash` the backedUpStores addition, run this test, see it fail,
// `git stash pop`).
func TestBackupCoversAllConfigPathFields(t *testing.T) {
	var cfg config.Config

	// markers maps each *Path field's dotted name (e.g.
	// "Watchlist.MatchLogPath") to a unique value written into that field
	// before backedUpStores runs, so presence can be checked by value
	// rather than by needing backedUpStores to expose field names itself.
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
				if strings.HasSuffix(field.Name, "Path") {
					marker := "372-marker:" + name
					fv.SetString(marker)
					markers[name] = marker
				}
			}
		}
	}
	walk(reflect.ValueOf(&cfg).Elem(), "")

	if len(markers) == 0 {
		t.Fatal("test setup: reflection found no *Path fields on config.Config -- the walk is broken")
	}

	carried := map[string]bool{}
	for _, s := range backedUpStores(cfg) {
		carried[s.Path] = true
	}

	for name, marker := range markers {
		if carried[marker] {
			continue
		}
		if _, ok := excludedFromBackup[name]; ok {
			continue
		}
		t.Errorf("config.%s is a *Path field with no backup decision recorded: it is not carried by "+
			"backedUpStores and not listed on excludedFromBackup. Add it to one -- with a reason if "+
			"excluding it. This is exactly the drift #372 found (three watchlist stores silently "+
			"missing from every backup)", name)
	}

	// The reverse direction matters too: an excludedFromBackup entry
	// naming a field that no longer exists is a stale, unverifiable
	// claim rather than a real exclusion decision.
	for name := range excludedFromBackup {
		if _, ok := markers[name]; !ok {
			t.Errorf("excludedFromBackup lists %q but no such *Path field exists on config.Config -- "+
				"stale entry, remove it", name)
		}
	}
}
