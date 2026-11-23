// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"slices"
	"testing"

	"github.com/tomlawesome/mikroview/internal/blocklist"
)

// The two default source lists must agree, the same drift guard
// netclass_default_test.go gives NetClass.Sources -- see its own doc
// comment for the concrete drift (a fresh install silently missing a
// source main.go's wiring never even reads this literal for) that this
// guards against here too.
//
// A separate _test.go file (package config) so the import stays out of
// the package proper and the leaf property (Blocklist above stays
// dependency-free) is unaffected for consumers -- test files never ship.
func TestBlocklistDefaultMatchesBlocklistPackage(t *testing.T) {
	got := defaults().Blocklist.Sources
	want := blocklist.DefaultSources

	if !slices.Equal(got, want) {
		t.Errorf("config default blocklist.sources = %v, blocklist.DefaultSources = %v -- these must agree, since main.go wires the former and internal/blocklist documents the latter", got, want)
	}
}
