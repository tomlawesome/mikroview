// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"errors"
	"testing"

	"github.com/tomlawesome/mikroview/internal/matchlog"
)

// This file was store_test.go before issue #407 deleted watchlist.Store:
// its storage tests (upsert/get, delete, list, restart survival) moved to
// internal/engine/definitions_expectations_test.go, against the store
// that now holds entries. What is left here is ValidateEntry's own
// contract -- the write-boundary rules Store.Upsert used to enforce
// before every write, now exported so the one store that holds entries
// can call it without re-deriving the rules (see ValidateEntry's own doc
// comment in watchlist.go).

func TestValidateEntryRejectsEmptyID(t *testing.T) {
	err := ValidateEntry(Entry{Ports: []int{22}})
	if !errors.Is(err, ErrInvalidEntry) {
		t.Errorf("ValidateEntry with no ID = %v, want ErrInvalidEntry", err)
	}
}

func TestValidateEntryRejectsNoPorts(t *testing.T) {
	err := ValidateEntry(Entry{ID: "e1"})
	if !errors.Is(err, ErrNoPorts) {
		t.Errorf("ValidateEntry with no ports = %v, want ErrNoPorts", err)
	}
}

func TestValidateEntryRejectsInvertedWithNoSource(t *testing.T) {
	err := ValidateEntry(Entry{ID: "e1", Invert: true})
	if !errors.Is(err, ErrInvertedRequiresSource) {
		t.Errorf("ValidateEntry(inverted, no source) = %v, want ErrInvertedRequiresSource", err)
	}
}

// An inverted entry needs no Ports -- confirming the branch in
// ValidateEntry actually skips ErrNoPorts rather than merely not hitting
// it by luck.
func TestValidateEntryAllowsInvertedWithNoPorts(t *testing.T) {
	err := ValidateEntry(Entry{ID: "e1", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}})
	if err != nil {
		t.Errorf("ValidateEntry(inverted, no ports) = %v, want nil", err)
	}
}

func TestValidateEntryRejectsInvalidText(t *testing.T) {
	cases := []Entry{
		{ID: "e1", Ports: []int{22}, Name: "bad\x00null"},
		{ID: "e1", Ports: []int{22}, Name: "bad\x01control"},
		{ID: "e1", Ports: []int{22}, DestIP: "1.2.3.4\x00"},
	}
	for _, e := range cases {
		if err := ValidateEntry(e); !errors.Is(err, ErrInvalidText) {
			t.Errorf("ValidateEntry(%+v) = %v, want ErrInvalidText", e, err)
		}
	}
}

// #806: a Boundary naming an interface with no chain is refused -- a
// rule's In/OutInterface has no meaning apart from the chain it was
// matched on, so this shape could never be looked up against a pushed
// rule table.
func TestValidateEntryRejectsInterfaceWithoutChain(t *testing.T) {
	cases := []Entry{
		{ID: "e1", Ports: []int{22}, Boundary: Boundary{InInterface: "ether1"}},
		{ID: "e1", Ports: []int{22}, Boundary: Boundary{OutInterface: "bridge1"}},
	}
	for _, e := range cases {
		if err := ValidateEntry(e); !errors.Is(err, ErrBoundaryRequiresChain) {
			t.Errorf("ValidateEntry(%+v) = %v, want ErrBoundaryRequiresChain", e, err)
		}
	}
}

// A chain alone, or a chain with an interface, is a valid boundary --
// confirming the refusal above is about an interface with no chain, not
// about the boundary field existing at all.
func TestValidateEntryAllowsChainScopedBoundary(t *testing.T) {
	cases := []Entry{
		{ID: "e1", Ports: []int{22}, Boundary: Boundary{Chain: "forward"}},
		{ID: "e1", Ports: []int{22}, Boundary: Boundary{Chain: "forward", InInterface: "ether1", OutInterface: "bridge1"}},
	}
	for _, e := range cases {
		if err := ValidateEntry(e); err != nil {
			t.Errorf("ValidateEntry(%+v) = %v, want nil", e, err)
		}
	}
}
