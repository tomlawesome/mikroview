// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

// The promise of issue #131 is that moving a store onto Postgres changes
// where the bytes live and nothing about how the store behaves. These
// run the same assertions against both backends.
//
// The Postgres side skips itself when MIKROVIEW_TEST_POSTGRES is unset,
// the same convention internal/auth's backend tests use.
func eachSetupBackend(t *testing.T, run func(t *testing.T, open func() *Store)) {
	t.Helper()

	t.Run("file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "setup.json")
		run(t, func() *Store {
			s, err := Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			return s
		})
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("MIKROVIEW_TEST_POSTGRES")
		if dsn == "" {
			t.Skip("MIKROVIEW_TEST_POSTGRES not set")
		}
		pool, err := persist.OpenPool(t.Context(), dsn)
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		t.Cleanup(pool.Close)
		if err := pool.Migrate(t.Context()); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		// A store name unique to this test, so tests do not collide in a
		// shared database.
		name := "setuptest_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
		b := persist.NewPostgresBackend(pool, name)

		// Reset to empty, unconditionally -- a suite that only passes
		// against a database it has never run against before is worse
		// than no suite, because it teaches people to ignore it.
		snap, err := b.Load(t.Context())
		if err != nil {
			t.Fatalf("reading the store before reset: %v", err)
		}
		if _, err := b.Save(t.Context(), []byte(`{"marks":[]}`), snap.Version); err != nil {
			t.Fatalf("resetting the store: %v", err)
		}
		t.Cleanup(func() { _ = b.Close() })

		run(t, func() *Store {
			s, err := OpenWithBackend(b)
			if err != nil {
				t.Fatalf("OpenWithBackend: %v", err)
			}
			return s
		})
	})
}

// TestMarksSurviveARestart is the point of persisting these at all.
//
// The design record makes the record the feature: a forced-past line has
// to keep surfacing in the step list, the audit log, and every empty
// state whose silence it explains. A mark that lived only in memory took
// two of those three surfaces with it on every restart -- and a restart
// is most likely at upgrade, exactly when an operator is looking for the
// explanation.
//
// Deliberately a reopen, not a check that a field was written: "the
// bytes were marshalled" is not the property that matters, "a second
// process reads the same decision back" is.
func TestMarksSurviveARestart(t *testing.T) {
	eachSetupBackend(t, func(t *testing.T, open func() *Store) {
		at := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)

		first := open()
		if _, ok := first.NoteMark(2, MarkForced, "tom", "no router has opened a syslog connection", at); !ok {
			t.Fatal("NoteMark refused a valid force")
		}
		if _, ok := first.NoteMark(4, MarkSkipped, "tom", "no pushed table has arrived", at); !ok {
			t.Fatal("NoteMark refused a valid skip")
		}

		// A second process, reading the same document.
		second := open()
		marks := second.Marks()
		if len(marks) != 2 {
			t.Fatalf("Marks() after reopen = %d, want 2 -- the record did not survive", len(marks))
		}
		if marks[0].Step != 2 || marks[0].Outcome != MarkForced {
			t.Errorf("first mark = step %d %q, want step 2 forced", marks[0].Step, marks[0].Outcome)
		}
		// Who and when have to come back too: a forced-past line that
		// cannot say who decided it explains nothing.
		if marks[0].Actor != "tom" {
			t.Errorf("actor = %q, want tom", marks[0].Actor)
		}
		if !marks[0].At.Equal(at) {
			t.Errorf("at = %v, want %v", marks[0].At, at)
		}
		if !strings.Contains(marks[0].Note, "syslog") {
			t.Errorf("note = %q, want what was not observed", marks[0].Note)
		}
		if marks[1].Step != 4 || marks[1].Outcome != MarkSkipped {
			t.Errorf("second mark = step %d %q, want step 4 skipped", marks[1].Step, marks[1].Outcome)
		}
	})
}

// TestChangedMindSurvivesAsOneMark. A step has exactly one outcome at a
// time in memory; a restart must not resurrect the one it replaced.
func TestChangedMindSurvivesAsOneMark(t *testing.T) {
	eachSetupBackend(t, func(t *testing.T, open func() *Store) {
		now := time.Now()
		first := open()
		first.NoteMark(3, MarkSkipped, "tom", "nothing yet", now)
		first.NoteMark(3, MarkForced, "tom", "still nothing", now.Add(time.Minute))

		marks := open().Marks()
		if len(marks) != 1 {
			t.Fatalf("Marks() after reopen = %d, want 1", len(marks))
		}
		if marks[0].Outcome != MarkForced {
			t.Errorf("outcome = %q, want the later decision (%q)", marks[0].Outcome, MarkForced)
		}
	})
}

// TestObservationsAreNotPersisted pins the other half of the decision.
//
// Only the marks are written down. An observation is a fact about what
// arrived at *this* process -- persisting it would turn "a router
// fetched the CA at 14:02" into a standing claim about a router nobody
// is still watching, which is the kind of quiet lie this whole feature
// exists to avoid.
func TestObservationsAreNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first.NoteCAFetch("192.0.2.1", now)
	first.NoteSyslogConnection("192.0.2.1", now)
	first.NoteEvent("r1", true, now)
	// One mark, so the document exists at all.
	first.NoteMark(1, MarkSkipped, "tom", "", now)

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	sources, devices := second.Snapshot()
	if len(sources) != 0 {
		t.Errorf("sources after reopen = %+v, want none -- observations are re-made from arriving traffic", sources)
	}
	if len(devices) != 0 {
		t.Errorf("devices after reopen = %+v, want none", devices)
	}
	if len(second.Marks()) != 1 {
		t.Error("the mark should have survived alongside")
	}
}

// TestLoadRefusesWhatItCannotDescribe. A document written by an older
// build, or edited by hand, must not put a step the ledger does not have
// into a wizard that renders five.
func TestLoadRefusesWhatItCannotDescribe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	doc := `{"marks":[
		{"step":9,"outcome":"forced","actor":"tom","at":"2026-08-23T09:00:00Z"},
		{"step":0,"outcome":"skipped","actor":"tom","at":"2026-08-23T09:00:00Z"},
		{"step":2,"outcome":"finished","actor":"tom","at":"2026-08-23T09:00:00Z"},
		{"step":3,"outcome":"skipped","actor":"tom","at":"2026-08-23T09:00:00Z"}
	]}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("a readable document must load: %v", err)
	}
	marks := s.Marks()
	if len(marks) != 1 || marks[0].Step != 3 {
		t.Fatalf("Marks() = %+v, want only the one valid mark (step 3)", marks)
	}
}

// TestOpenRefusesAnUnparseableDocument is #378's fail-closed startup
// policy applied here: a store built around a backend whose load failed
// would overwrite the operator's document with its own near-empty state
// on the first write.
func TestOpenRefusesAnUnparseableDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "setup.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err == nil {
		t.Fatal("Open accepted an unparseable document")
	}
	if s != nil {
		t.Error("Open returned a store around a backend that failed to load")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != "{not json" {
		t.Error("the refused document was modified; it must be left exactly as found")
	}
}

// TestPersistFailureIsNotSilent mirrors internal/audit's test of the
// same name, for the same reason: every persisted store used to swallow
// all three failure paths in persistLocked, so a full disk or a
// read-only remount left mikroview running and reporting success while
// nothing reached disk.
//
// It asserts the observable contract that survives regardless of how
// logging is wired: the write genuinely failed, the in-memory state is
// still coherent, and the store did not corrupt the real document by
// leaving a half-written temp in its place.
func TestPersistFailureIsNotSilent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "setup.json")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.NoteMark(1, MarkSkipped, "admin", "no router has fetched /ca.crt", now)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the first decision to persist: %v", err)
	}

	// Make the directory read-only so the temp-file write fails. Skip
	// when running as root, which ignores the mode bits entirely.
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if _, ok := s.NoteMark(2, MarkForced, "admin", "no router has opened a syslog connection", now); !ok {
		t.Fatal("a failed persist must not refuse the decision -- the audit entry for it is written either way")
	}

	// The in-memory store must still be coherent: a failed persist is
	// not allowed to lose or corrupt what the process already knows.
	if got := len(s.Marks()); got != 2 {
		t.Errorf("in-memory marks = %d, want 2 (a failed persist must not drop in-memory state)", got)
	}

	// The on-disk document must be untouched rather than truncated: the
	// atomic write-temp-then-rename means a failure leaves the previous
	// good copy in place.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the previously-persisted document must survive a failed write: %v", err)
	}
	if string(after) != string(before) {
		t.Error("a failed persist modified the on-disk document; the previous good copy must be left intact")
	}
}

// TestInMemoryStoreStillWorks: New() is still a fully usable store, and
// an empty path is a supported, deliberate choice rather than an error.
func TestInMemoryStoreStillWorks(t *testing.T) {
	for name, s := range map[string]*Store{"New": New(), "Open(\"\")": mustOpenEmpty(t)} {
		t.Run(name, func(t *testing.T) {
			if _, ok := s.NoteMark(1, MarkSkipped, "tom", "", time.Now()); !ok {
				t.Fatal("NoteMark refused a valid mark")
			}
			if len(s.Marks()) != 1 {
				t.Error("an unpersisted store must still hold its marks in memory")
			}
		})
	}
}

func mustOpenEmpty(t *testing.T) *Store {
	t.Helper()
	s, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	return s
}
