// SPDX-License-Identifier: AGPL-3.0-only

package dossier

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/ipscope"
)

// Assumed API: SetHomeScope(*ipscope.Scope), package state like the
// engine's. A networks.home peer (#1481) is inside, so the dossier must
// label it local, as the browser does.
func TestPeerScopeTreatsAConfiguredHomeRangeAsLocal(t *testing.T) {
	t.Cleanup(func() { SetHomeScope(nil) })
	if got := scopeOf("198.51.100.66"); got != "internet" {
		t.Fatalf("with no home scope, scopeOf(198.51.100.66) = %q, want internet", got)
	}
	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	SetHomeScope(sc)
	if got := scopeOf("198.51.100.66"); got != "local" {
		t.Errorf("with home 198.51.100.0/24, scopeOf(198.51.100.66) = %q, want local", got)
	}
	if got := scopeOf("203.0.113.44"); got != "internet" {
		t.Errorf("scopeOf(203.0.113.44) = %q, want internet with the home scope set", got)
	}
}
