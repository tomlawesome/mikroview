// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/matchlog"
)

// Issue #407 deleted watchlist.Store. Promote is now a plain method on
// *Entry (invert.go), so the tests below drive it directly rather than
// through a store's Upsert/Get round trip -- there is no store left in
// this package to round-trip through. RecordObservation*, SetObserving*
// and TestObserveAndPermittedSurviveRestart moved to
// internal/engine/definitions_expectations_test.go, against
// engine.DefinitionsStore, which is what now persists an entry's
// Observed/Permitted state. TestPromoteErrors's not-found/not-inverted
// cases moved there too, as a property of UpdateExpectation (the door
// that now applies a mutation like Promote to a stored entry) rather than
// of Promote itself -- Promote, as a plain method, has no id to look up
// and no Invert flag to refuse on; that policing is the caller's job now.

func inverted(id string, observed []ObservedDest) Entry {
	return Entry{
		ID:        id,
		Invert:    true,
		Observing: true,
		Source:    matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"},
		Observed:  observed,
	}
}

func TestPromoteMovesFromObservedToPermitted(t *testing.T) {
	e := inverted("e1", []ObservedDest{
		{DestIP: "10.0.0.5", Port: 8883},
		{DestIP: "10.0.0.6", Port: 443},
	})

	e.Promote([]PermittedDest{{DestIP: "10.0.0.5", Port: 8883}})

	if len(e.Permitted) != 1 || e.Permitted[0].DestIP != "10.0.0.5" || e.Permitted[0].Port != 8883 {
		t.Errorf("Permitted = %+v, want the promoted pair", e.Permitted)
	}
	if len(e.Observed) != 1 || e.Observed[0].DestIP != "10.0.0.6" {
		t.Errorf("Observed = %+v, want only the un-promoted pair left", e.Observed)
	}
}

// Promoting something never observed is a legitimate, deliberate choice
// -- not every permitted destination has to come from the review list.
func TestPromoteAllowsAPairNeverObserved(t *testing.T) {
	e := inverted("e1", nil)

	e.Promote([]PermittedDest{{DestIP: "1.2.3.4", Port: 443}})

	if len(e.Permitted) != 1 {
		t.Errorf("Permitted = %+v, want the pair promoted despite never being observed", e.Permitted)
	}
}

func TestPromoteIsIdempotent(t *testing.T) {
	e := inverted("e1", nil)
	pair := []PermittedDest{{DestIP: "1.2.3.4", Port: 443}}

	e.Promote(pair)
	e.Promote(pair)

	if len(e.Permitted) != 1 {
		t.Errorf("Permitted has %d entries after promoting the same pair twice, want 1", len(e.Permitted))
	}
}

func TestPromoteDoesNotChangeObserving(t *testing.T) {
	e := inverted("e1", nil) // Observing: true

	e.Promote([]PermittedDest{{DestIP: "1.2.3.4", Port: 443}})

	if !e.Observing {
		t.Error("Promote must not change Observing -- that is the caller's job")
	}
}
