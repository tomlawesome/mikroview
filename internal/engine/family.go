// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"sort"
	"strings"
)

// Family is the flag family an operator files a custom detector under --
// the one thing on the conditions editor that colours everything else
// (round 7/8 of #490's settings rounds, ratified on #829).
//
// The seven names are the frontend's own ratified palette
// (frontend/src/lib/flagPalette.ts): six inks the design record assigns
// to the sixteen shipped detectors by what the flag is *about*, plus the
// operator-authored accent every custom detector wore before this field
// existed. They are stored here, rather than derived, because a custom
// detector's subject is known only to its author: nothing in this binary
// can work out whether "garage probes" is a scan or a presence question.
//
// Deliberately a display fact and nothing more. No evaluation, dispatch,
// routing or threshold decision reads it -- a detector filed under
// hostile matches exactly what a detector filed under presence with the
// same conditions matches. That is why the set can be closed here without
// closing anything the engine does: adding a family is a palette change
// on both sides, never a change to what a detector can watch.
//
// Empty is valid and means "not filed": FamilyCustom's ink is what
// familyOf already falls back to, so an older stored definition reads the
// same as it always did.
type Family string

const (
	FamilyHostile  Family = "hostile"
	FamilyScan     Family = "scan"
	FamilyOutbound Family = "outbound"
	FamilyRepeat   Family = "repeat"
	FamilySurge    Family = "surge"
	FamilyPresence Family = "presence"
	FamilyCustom   Family = "custom"
)

var knownFamilies = map[Family]bool{
	FamilyHostile:  true,
	FamilyScan:     true,
	FamilyOutbound: true,
	FamilyRepeat:   true,
	FamilySurge:    true,
	FamilyPresence: true,
	FamilyCustom:   true,
}

// ValidateFamily accepts the empty family (not filed) and the seven
// ratified names, and refuses everything else by listing what it would
// have taken -- the same shape ValidateDetectionDetailTemplate's refusal
// has, for the same reason: an operator reading it should learn what to
// write, not only that they wrote the wrong thing.
func ValidateFamily(f Family) error {
	if f == "" || knownFamilies[f] {
		return nil
	}
	return fmt.Errorf("engine: unknown flag family %q -- the families a detector may be filed under are %s", f, strings.Join(FamilyNames(), ", "))
}

// FamilyNames lists the seven families in sorted order, for an error
// message and for any caller that wants to offer the choice.
func FamilyNames() []string {
	out := make([]string, 0, len(knownFamilies))
	for f := range knownFamilies {
		out = append(out, string(f))
	}
	sort.Strings(out)
	return out
}
