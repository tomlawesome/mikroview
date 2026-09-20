// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"strings"
	"time"
)

// The operator-facing half of the upgrade framework (#1240,
// docs/decisions/upgrade-framework.md). main reads <data>/version at
// boot -- the build that last ran on this data directory -- and hands
// it here before the marker is overwritten. What is kept is the
// crossing itself: which version was replaced, by which, when that was
// noticed, and whether an admin has since said they have dealt with it.
//
// Persisted beside the address for the same two reasons: the notice has
// to outlive a restart (an unacknowledged upgrade is still unfinished
// work the next morning), and the acknowledgement is the instance's,
// not the browser's -- one admin pressing done settles it for every
// session, which a dismissal held in a browser could never do.

// maxVersion caps the version strings this will store, mirroring
// maxAddress: both ends are mikroview's own (the build stamp and the
// marker file it wrote itself), so this is a backstop against a
// corrupted or hand-edited marker reaching the persisted document, not
// input validation.
const maxVersion = 64

// Upgrade is one crossing from one build version to another.
//
// AcknowledgedAt/By is the admin who pressed done, resolved server-side
// from the session like every Mark's actor. Zero until then, which is
// what Acknowledged reads.
type Upgrade struct {
	Previous       string    `json:"previous"`
	Current        string    `json:"current"`
	NoticedAt      time.Time `json:"noticedAt"`
	AcknowledgedAt time.Time `json:"acknowledgedAt,omitzero"`
	AcknowledgedBy string    `json:"acknowledgedBy,omitempty"`
}

// Acknowledged reports whether an admin has said they have dealt with
// this upgrade.
func (u Upgrade) Acknowledged() bool { return !u.AcknowledgedAt.IsZero() }

// NoteUpgrade records that this boot replaced previous with current,
// reporting whether anything was written.
//
// Nothing is written for a first install (no previous version) or an
// ordinary restart (previous == current): neither is a crossing, and
// the notice has nothing to say about either.
//
// Re-noting the same crossing is deliberately a no-op rather than a
// refresh. Every restart after an upgrade calls this with the same pair
// until the marker moves again, and rewriting the record each time
// would reset both the moment it was noticed and -- far worse -- an
// acknowledgement the operator already gave, so the notice would come
// back from the dead on the next restart.
func (s *Store) NoteUpgrade(previous, current string, now time.Time) bool {
	previous, current = strings.TrimSpace(previous), strings.TrimSpace(current)
	if previous == "" || current == "" || previous == current {
		return false
	}
	if len(previous) > maxVersion || len(current) > maxVersion {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upgrade != nil && s.upgrade.Previous == previous && s.upgrade.Current == current {
		return false
	}
	s.upgrade = &Upgrade{Previous: previous, Current: current, NoticedAt: now}
	s.persistLocked()
	return true
}

// Upgrade returns the crossing this instance is still living with, and
// whether there is one at all. An acknowledged crossing is still
// returned: the acknowledgement is part of the answer, not an erasure
// of it, and the API serves both.
func (s *Store) Upgrade() (Upgrade, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.upgrade == nil {
		return Upgrade{}, false
	}
	return *s.upgrade, true
}

// AcknowledgeUpgrade records that actor has dealt with the upgrade --
// the notice's `done`. Reports the resulting record and whether there
// was an upgrade to acknowledge, so a caller can refuse to write an
// audit entry for a click against nothing.
//
// Idempotent: a second done keeps the first admin's name and time,
// because it is the first one who actually did the work. Persists
// immediately, same reasoning as NoteMark.
func (s *Store) AcknowledgeUpgrade(actor string, now time.Time) (Upgrade, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upgrade == nil {
		return Upgrade{}, false
	}
	if s.upgrade.Acknowledged() {
		return *s.upgrade, true
	}
	if len(actor) > maxNote {
		actor = actor[:maxNote]
	}
	u := *s.upgrade
	u.AcknowledgedAt = now
	u.AcknowledgedBy = actor
	s.upgrade = &u
	s.persistLocked()
	return u, true
}
