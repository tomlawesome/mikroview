// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObservationsAreReported(t *testing.T) {
	s := New()
	now := time.Unix(1_000_000, 0)

	s.NoteCAFetch("192.0.2.1", now)
	s.NoteSyslogConnection("192.0.2.1", now.Add(time.Minute))
	s.NoteSyslogConnection("192.0.2.1", now.Add(2*time.Minute))
	s.NoteEvent("edge", true, now)
	s.NoteEvent("edge", false, now)

	sources, devices := s.Snapshot()
	if len(sources) != 1 || sources[0].Source != "192.0.2.1" {
		t.Fatalf("sources = %+v, want one entry for 192.0.2.1", sources)
	}
	if sources[0].CAFetchedAt == nil || sources[0].SyslogFirstSeenAt == nil {
		t.Errorf("missing observations: %+v", sources[0])
	}
	// First stays first: it is what says "this step completed", while
	// Last says "it is still working".
	if got := *sources[0].SyslogFirstSeenAt; !got.Equal(now.Add(time.Minute)) {
		t.Errorf("SyslogFirstSeenAt = %v, want the first connection", got)
	}
	if got := *sources[0].SyslogLastSeenAt; !got.Equal(now.Add(2 * time.Minute)) {
		t.Errorf("SyslogLastSeenAt = %v, want the most recent connection", got)
	}
	if len(devices) != 1 || devices[0].Events != 2 || devices[0].Decoded != 1 {
		t.Errorf("devices = %+v, want edge with 2 events and 1 decoded", devices)
	}
}

// Events arriving with none decoded is the half-finished setup the
// wizard exists to name: rules log, but without the log-prefix
// convention, so every row shows action "unknown".
func TestUndecodedEventsAreDistinguishableFromNoEvents(t *testing.T) {
	s := New()
	now := time.Unix(1_000_000, 0)
	for i := 0; i < 5; i++ {
		s.NoteEvent("edge", false, now)
	}
	_, devices := s.Snapshot()
	if len(devices) != 1 || devices[0].Events != 5 || devices[0].Decoded != 0 {
		t.Fatalf("devices = %+v, want 5 events and 0 decoded", devices)
	}
}

// Keys are source addresses, so an unauthenticated flood must not grow
// these maps without bound.
func TestMapsAreBounded(t *testing.T) {
	s := New()
	now := time.Unix(1_000_000, 0)
	for i := 0; i < maxSources*3; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		s.NoteCAFetch(ip, now.Add(time.Duration(i)*time.Second))
		s.NoteSyslogConnection(ip, now.Add(time.Duration(i)*time.Second))
		s.NoteEvent(ip, true, now.Add(time.Duration(i)*time.Second))
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, n := range map[string]int{
		"caFetched":  len(s.caFetched),
		"syslogSeen": len(s.syslogSeen),
		"prefixes":   len(s.prefixes),
	} {
		if n > maxSources {
			t.Errorf("%s grew to %d, cap is %d", name, n, maxSources)
		}
	}
}

// Eviction takes the least-recently-seen, so a flood does not erase the
// router the operator is actually setting up.
func TestEvictionKeepsTheRecentlyActive(t *testing.T) {
	s := New()
	base := time.Unix(1_000_000, 0)
	s.NoteSyslogConnection("the-real-router", base)

	for i := 0; i < maxSources*2; i++ {
		// Older than the real router, so they are evicted first...
		s.NoteSyslogConnection(fmt.Sprintf("10.1.%d.%d", i/256, i%256), base.Add(-time.Hour))
		// ...and the real router keeps being seen.
		s.NoteSyslogConnection("the-real-router", base.Add(time.Duration(i)*time.Second))
	}

	sources, _ := s.Snapshot()
	for _, o := range sources {
		if o.Source == "the-real-router" {
			return
		}
	}
	t.Error("the active router was evicted by a flood of stale sources")
}

// TestMarksReplaceRatherThanAccumulate: a step has exactly one outcome
// at a time. Changing one's mind about a step is not a second claim
// about it, and a ledger that showed both would be reporting a
// contradiction as history.
func TestMarksReplaceRatherThanAccumulate(t *testing.T) {
	s := New()
	now := time.Now()

	if _, ok := s.NoteMark(3, MarkSkipped, "tom", "no events yet", now); !ok {
		t.Fatal("NoteMark refused a valid skip")
	}
	if _, ok := s.NoteMark(3, MarkForced, "tom", "still no events", now.Add(time.Minute)); !ok {
		t.Fatal("NoteMark refused a valid force")
	}

	marks := s.Marks()
	if len(marks) != 1 {
		t.Fatalf("Marks() = %d entries, want 1", len(marks))
	}
	if marks[0].Outcome != MarkForced {
		t.Errorf("outcome = %q, want the later decision (%q)", marks[0].Outcome, MarkForced)
	}
}

// TestMarksAreOrderedByStep pins the read order, because the ledger
// renders in the order it reads and Go map iteration is deliberately
// random.
func TestMarksAreOrderedByStep(t *testing.T) {
	s := New()
	now := time.Now()
	for _, step := range []int{4, 1, 5, 2} {
		if _, ok := s.NoteMark(step, MarkSkipped, "tom", "", now); !ok {
			t.Fatalf("NoteMark refused step %d", step)
		}
	}
	var got []int
	for _, m := range s.Marks() {
		got = append(got, m.Step)
	}
	want := []int{1, 2, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("Marks() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Marks() = %v, want %v", got, want)
		}
	}
}

// TestMarksRefuseWhatTheyCannotDescribe. The step numbers and outcomes
// are the ratified design's, not free text: a mark on step 9 describes
// nothing, and neither the ledger nor the audit log should carry it.
// TestMarkAcceptsTheWizardsSixthStep covers #1267: maxStep was still 5
// with a comment claiming "five steps, per the ratified design", but
// round 45 (#394) added a sixth ("Back up the router") -- see
// frontend/src/lib/setupsteps.ts's STEP_TITLES, the six-entry list this
// package's step count must match. NoteMark refused any skip or force
// decision on that sixth step until maxStep caught up.
func TestMarkAcceptsTheWizardsSixthStep(t *testing.T) {
	s := New()
	if _, ok := s.NoteMark(6, MarkSkipped, "tom", "", time.Now()); !ok {
		t.Error("NoteMark refused step 6, but the wizard's setupsteps.ts carries six steps")
	}
}

func TestMarksRefuseWhatTheyCannotDescribe(t *testing.T) {
	s := New()
	now := time.Now()
	for _, tc := range []struct {
		name    string
		step    int
		outcome MarkOutcome
	}{
		{"step zero", 0, MarkSkipped},
		{"step past the last", maxStep + 1, MarkSkipped},
		{"outcome that is not a decision", 1, MarkOutcome("done")},
		{"empty outcome", 1, MarkOutcome("")},
	} {
		if _, ok := s.NoteMark(tc.step, tc.outcome, "tom", "", now); ok {
			t.Errorf("%s: NoteMark accepted step %d outcome %q", tc.name, tc.step, tc.outcome)
		}
	}
	if n := len(s.Marks()); n != 0 {
		t.Errorf("Marks() = %d entries, want 0", n)
	}
}

// TestMarkNoteIsBounded. The note is free text from a client, and the
// store is in memory for the life of the process -- an unbounded field
// is storage, not a record.
func TestMarkNoteIsBounded(t *testing.T) {
	s := New()
	long := strings.Repeat("x", maxNote*3)
	m, ok := s.NoteMark(1, MarkForced, "tom", long, time.Now())
	if !ok {
		t.Fatal("NoteMark refused a valid mark")
	}
	if len(m.Note) != maxNote {
		t.Errorf("note length = %d, want it capped at %d", len(m.Note), maxNote)
	}
}

// TestSetAddressRejectsEmptyAndOverlong covers SetAddress's own
// backstop: charset and structure are validSetupAddress's job in
// internal/api (#1095), checked before this is ever reached, but an
// empty value (there is no "clear the address" operation) or an
// absurdly long one must not reach the persisted document either way.
func TestSetAddressRejectsEmptyAndOverlong(t *testing.T) {
	s := New()
	if s.SetAddress("") {
		t.Error("SetAddress accepted an empty address")
	}
	if s.SetAddress(strings.Repeat("a", maxAddress+1)) {
		t.Error("SetAddress accepted a value past maxAddress")
	}
	if s.Address() != "" {
		t.Errorf("Address() = %q, want empty -- both attempts above should have been refused", s.Address())
	}
	if !s.SetAddress(strings.Repeat("a", maxAddress)) {
		t.Error("SetAddress refused a value exactly at maxAddress")
	}
}

// TestWitnessSurvivesRestart is #1221's whole point: a step witnessed
// before the process stops still reads back as witnessed, receipt and
// all, from a fresh Store opened against the same file -- the same
// simulation of a restart persist's own contract tests use, since the
// witness lives in the same on-disk document as marks.
// TestSetBackupTransportRefusesAnythingElse: the store is what must
// never hold a third value (#955), since handleSetupCommands renders a
// script from it and has no "unrenderable" state to fall back on.
func TestSetBackupTransportRefusesAnythingElse(t *testing.T) {
	s := New()
	for _, bad := range []string{"", "SFTP", "ftp", "https ", "scp"} {
		if s.SetBackupTransport(bad) {
			t.Errorf("SetBackupTransport(%q) was accepted", bad)
		}
	}
	if got := s.BackupTransport(); got != BackupTransportSFTP {
		t.Errorf("BackupTransport() = %q, want the sftp default -- every attempt above should have been refused", got)
	}
	if !s.SetBackupTransport(BackupTransportHTTPS) {
		t.Fatal("SetBackupTransport refused https")
	}
	if got := s.BackupTransport(); got != BackupTransportHTTPS {
		t.Errorf("BackupTransport() = %q, want https", got)
	}
}

func TestWitnessSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	now := time.Unix(1_757_000_000, 0)

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := s1.NoteWitnessed(1, "ca.crt fetched by 192.0.2.1", now); !ok {
		t.Fatal("NoteWitnessed refused a valid step")
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	witnessed := s2.Witnessed()
	if len(witnessed) != 1 {
		t.Fatalf("Witnessed() after reopening = %d entries, want 1", len(witnessed))
	}
	w := witnessed[0]
	if w.Step != 1 || w.Outcome != MarkWitnessed {
		t.Errorf("witness = step %d %q, want step 1 witnessed", w.Step, w.Outcome)
	}
	if w.Note != "ca.crt fetched by 192.0.2.1" {
		t.Errorf("witness receipt = %q, want the original receipt to survive", w.Note)
	}
	if !w.At.Equal(now) {
		t.Errorf("witness time = %v, want %v", w.At, now)
	}
}

// TestWitnessDoesNotDisturbMarks pins the two halves of #1221's
// architecture call: a witness never overwrites an operator's mark for
// the same step, and an operator's later decision never erases a
// witness already recorded -- the two are read back together by
// whichever caller decides which wins, not merged here.
func TestWitnessDoesNotDisturbMarks(t *testing.T) {
	s := New()
	now := time.Now()

	if _, ok := s.NoteMark(2, MarkSkipped, "tom", "no router has opened a syslog connection", now); !ok {
		t.Fatal("NoteMark refused a valid skip")
	}
	if _, ok := s.NoteWitnessed(2, "syslog connected from 192.0.2.1", now.Add(time.Minute)); !ok {
		t.Fatal("NoteWitnessed refused a valid step")
	}

	marks := s.Marks()
	if len(marks) != 1 || marks[0].Outcome != MarkSkipped {
		t.Fatalf("Marks() = %+v, want the skip untouched by the witness", marks)
	}
	witnessed := s.Witnessed()
	if len(witnessed) != 1 || witnessed[0].Outcome != MarkWitnessed {
		t.Fatalf("Witnessed() = %+v, want the witness recorded alongside the skip", witnessed)
	}

	// The operator changes their mind after the witness exists -- the
	// witness must still be there afterwards.
	if _, ok := s.NoteMark(2, MarkForced, "tom", "still nothing", now.Add(2*time.Minute)); !ok {
		t.Fatal("NoteMark refused a valid force")
	}
	if witnessed := s.Witnessed(); len(witnessed) != 1 {
		t.Fatalf("Witnessed() after a later mark = %+v, want the witness to survive it", witnessed)
	}
}

// TestSecondWitnessIsANoOp: NoteWitnessed writes once per step. A second
// call for a step already witnessed must neither change the recorded
// receipt nor persist again -- the point made in its own doc comment,
// pinned here via the version counter persistLocked advances on every
// real write.
func TestSecondWitnessIsANoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Unix(1_757_000_000, 0)

	if _, ok := s.NoteWitnessed(3, "2 of 2 events carried a decoded action", now); !ok {
		t.Fatal("NoteWitnessed refused a valid step")
	}
	versionAfterFirst := s.version

	if _, ok := s.NoteWitnessed(3, "5 of 5 events carried a decoded action", now.Add(time.Hour)); !ok {
		t.Fatal("NoteWitnessed refused a valid step")
	}

	if s.version != versionAfterFirst {
		t.Errorf("version advanced from %d to %d on a repeat witness -- the file was rewritten", versionAfterFirst, s.version)
	}
	witnessed := s.Witnessed()
	if len(witnessed) != 1 || witnessed[0].Note != "2 of 2 events carried a decoded action" {
		t.Errorf("witness = %+v, want the first receipt kept, not the second", witnessed)
	}
	if !witnessed[0].At.Equal(now) {
		t.Errorf("witness time = %v, want the first observation's time", witnessed[0].At)
	}
}
