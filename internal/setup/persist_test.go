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
		if _, ok, err := first.NoteMark(2, MarkForced, "tom", "no router has opened a syslog connection", at); err != nil || !ok {
			t.Fatalf("NoteMark refused a valid force: ok=%v err=%v", ok, err)
		}
		if _, ok, err := first.NoteMark(4, MarkSkipped, "tom", "no pushed table has arrived", at); err != nil || !ok {
			t.Fatalf("NoteMark refused a valid skip: ok=%v err=%v", ok, err)
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

// TestAddressSurvivesARestart pins #1213's whole point: the operator's
// answer to "what address can your router reach mikroview on?" is
// persisted beside the marks, the same eachSetupBackend suite proves
// they survive with, so a restart mid-wizard does not force it to be
// asked again.
func TestAddressSurvivesARestart(t *testing.T) {
	eachSetupBackend(t, func(t *testing.T, open func() *Store) {
		first := open()
		if first.Address() != "" {
			t.Fatalf("Address() on a fresh store = %q, want empty", first.Address())
		}
		if ok, err := first.SetAddress("10.0.40.5:8443"); err != nil || !ok {
			t.Fatalf("SetAddress refused a valid address: ok=%v err=%v", ok, err)
		}

		second := open()
		if got := second.Address(); got != "10.0.40.5:8443" {
			t.Errorf("Address() after reopen = %q, want the value just stored", got)
		}

		// Editable afterwards (re-running setup on a moved instance must
		// not require a reinstall): a later answer replaces the first,
		// and that replacement survives too.
		if ok, err := second.SetAddress("192.168.1.9"); err != nil || !ok {
			t.Fatalf("SetAddress refused a valid replacement: ok=%v err=%v", ok, err)
		}
		third := open()
		if got := third.Address(); got != "192.168.1.9" {
			t.Errorf("Address() after the replacement reopen = %q, want the newer value", got)
		}
	})
}

// TestBackupTransportSurvivesARestart pins #955's "the choice is a
// property of the deployment, not the browser": it is stored beside the
// address and read back after a restart, so an operator on an HTTPS-only
// install is offered the step their install uses rather than the SFTP
// one every time the process comes back.
func TestBackupTransportSurvivesARestart(t *testing.T) {
	eachSetupBackend(t, func(t *testing.T, open func() *Store) {
		first := open()
		if got := first.BackupTransport(); got != BackupTransportSFTP {
			t.Fatalf("BackupTransport() on a fresh store = %q, want the %q default", got, BackupTransportSFTP)
		}
		if ok, err := first.SetBackupTransport(BackupTransportHTTPS); err != nil || !ok {
			t.Fatalf("SetBackupTransport refused https: ok=%v err=%v", ok, err)
		}

		second := open()
		if got := second.BackupTransport(); got != BackupTransportHTTPS {
			t.Errorf("BackupTransport() after reopen = %q, want https", got)
		}

		// Switching back is one answer replacing another, not a second
		// claim -- and the replacement survives the same way.
		if ok, err := second.SetBackupTransport(BackupTransportSFTP); err != nil || !ok {
			t.Fatalf("SetBackupTransport refused sftp: ok=%v err=%v", ok, err)
		}
		third := open()
		if got := third.BackupTransport(); got != BackupTransportSFTP {
			t.Errorf("BackupTransport() after switching back = %q, want sftp", got)
		}
	})
}

// TestChangedMindSurvivesAsOneMark. A step has exactly one outcome at a
// time in memory; a restart must not resurrect the one it replaced.
func TestChangedMindSurvivesAsOneMark(t *testing.T) {
	eachSetupBackend(t, func(t *testing.T, open func() *Store) {
		now := time.Now()
		first := open()
		if _, _, err := first.NoteMark(3, MarkSkipped, "tom", "nothing yet", now); err != nil {
			t.Fatal(err)
		}
		if _, _, err := first.NoteMark(3, MarkForced, "tom", "still nothing", now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}

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
	if _, _, err := first.NoteMark(1, MarkSkipped, "tom", "", now); err != nil {
		t.Fatal(err)
	}

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

// TestPersistFailureIsNotSilent is the v0.6.0 audit's R6 finding fixed:
// NoteMark used to swallow a failed write and report the decision as
// made anyway, so a restart before the next good write silently
// resurrected whatever mark the operator thought they had just changed.
// It now rolls the in-memory mark back and reports the failure, so the
// caller (and the audit entry it writes) can't claim a decision that
// isn't durable.
func TestPersistFailureIsNotSilent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "setup.json")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, _, err := s.NoteMark(1, MarkSkipped, "admin", "no router has fetched /ca.crt", now); err != nil {
		t.Fatal(err)
	}

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

	if _, ok, err := s.NoteMark(2, MarkForced, "admin", "no router has opened a syslog connection", now); ok || err == nil {
		t.Fatalf("NoteMark against an unwritable backend was reported as succeeding: ok=%v err=%v", ok, err)
	}

	// The in-memory store must roll back to what was durably recorded --
	// the whole point of R6 is that this must not silently gain a mark
	// the disk does not have.
	if got := len(s.Marks()); got != 1 {
		t.Errorf("in-memory marks = %d, want 1 (the failed mark must be rolled back)", got)
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
			if _, ok, err := s.NoteMark(1, MarkSkipped, "tom", "", time.Now()); err != nil || !ok {
				t.Fatalf("NoteMark refused a valid mark: ok=%v err=%v", ok, err)
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
