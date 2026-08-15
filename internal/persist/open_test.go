// SPDX-License-Identifier: AGPL-3.0-only

package persist

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenAbsentDocumentIsFreshStart pins the "never written" half of
// #378's policy: no document at all is a normal first boot, not a
// failure, and decode must never be called against bytes that don't
// exist.
func TestOpenAbsentDocumentIsFreshStart(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		called := false
		version, existed, err := Open(context.Background(), b, "the test store", func(data []byte) error {
			called = true
			return nil
		})
		if err != nil {
			t.Fatalf("Open on a never-written document returned an error: %v", err)
		}
		if existed {
			t.Error("existed=true for a never-written document")
		}
		if version != 0 {
			t.Errorf("version = %d, want 0 for a never-written document", version)
		}
		if called {
			t.Error("decode was called for a document that doesn't exist")
		}
	})
}

// TestOpenDecodeFailureFailsClosed is the shared helper's core contract
// (#378): a document that exists but cannot be decoded produces a
// *StartupError, not a partially-applied decode plus a nil error. The
// payload is written directly through the backend so this exercises the
// exact bytes-in-hand case a corrupt JSON file or a hand-edited Postgres
// row would produce, independent of any one store's on-disk shape.
func TestOpenDecodeFailureFailsClosed(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		if _, err := b.Save(context.Background(), []byte("not valid json"), 0); err != nil {
			t.Fatalf("Save: %v", err)
		}

		decodeCalledWith := []byte(nil)
		_, existed, err := Open(context.Background(), b, "the test store", func(data []byte) error {
			decodeCalledWith = data
			return errors.New("boom: invalid syntax")
		})
		if err == nil {
			t.Fatal("Open returned a nil error for a decode failure, want fail-closed")
		}
		if existed {
			t.Error("existed=true on a decode failure, want false")
		}
		if string(decodeCalledWith) != "not valid json" {
			t.Errorf("decode was called with %q, want the stored bytes", decodeCalledWith)
		}

		var startupErr *StartupError
		if !errors.As(err, &startupErr) {
			t.Fatalf("err is not a *StartupError: %v (%T)", err, err)
		}
		if startupErr.Store != "the test store" {
			t.Errorf("StartupError.Store = %q, want %q", startupErr.Store, "the test store")
		}
		if startupErr.Location != b.Describe() {
			t.Errorf("StartupError.Location = %q, want %q", startupErr.Location, b.Describe())
		}
		if !errors.Is(err, startupErr.Err) {
			t.Error("StartupError does not unwrap to the decode error")
		}
	})
}

// TestOpenLoadFailureFailsClosed covers the other half of the shared
// helper's contract: the backend itself refusing to read (as opposed to
// handing back bytes that fail to parse) is wrapped the same way.
func TestOpenLoadFailureFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions are not enforced")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Deny read access to the file itself so os.ReadFile fails with
	// something other than "not exist" -- FileBackend.Load treats that
	// as a real error rather than the normal first-run case.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0o600) })

	b := NewFileBackend(path)
	_, existed, err := Open(context.Background(), b, "the test store", func(data []byte) error {
		t.Error("decode was called despite a Load failure")
		return nil
	})
	if err == nil {
		t.Fatal("Open returned a nil error for a Load failure, want fail-closed")
	}
	if existed {
		t.Error("existed=true on a Load failure, want false")
	}
	var startupErr *StartupError
	if !errors.As(err, &startupErr) {
		t.Fatalf("err is not a *StartupError: %v (%T)", err, err)
	}
}

// TestStartupErrorMessageNamesStoreLocationCauseAndRemedy is issue
// #378's Done-when made literal: the message an operator actually reads
// has to identify the store, where its document lives, what went wrong,
// and what to do about it -- a bare "failed to load" is not the fix.
func TestStartupErrorMessageNamesStoreLocationCauseAndRemedy(t *testing.T) {
	cause := errors.New("unexpected end of JSON input")
	e := &StartupError{Store: "the flags store", Location: "file /var/lib/mikroview/flags.json", Err: cause}
	msg := e.Error()

	for _, want := range []string{
		"the flags store",
		"/var/lib/mikroview/flags.json",
		"unexpected end of JSON input",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
	lower := strings.ToLower(msg)
	if !strings.Contains(lower, "restore") && !strings.Contains(lower, "backup") {
		t.Errorf("message %q does not mention restoring from a backup", msg)
	}
	if !strings.Contains(lower, "move") {
		t.Errorf("message %q does not mention moving the document aside to start fresh", msg)
	}
	if !strings.Contains(lower, "not a fresh install") && !strings.Contains(lower, "not a fresh") {
		t.Errorf("message %q does not say this is not a fresh install", msg)
	}
}

// TestOpenDecodeFailureLeavesTheDocumentUnchanged is the shared
// helper's half of #378's Done-when: Open only ever reads. A failed
// Open must never be the thing that leaves a store's on-disk document
// any different from how it found it -- the actual data-loss bug was
// always the *next write*, which this proves never gets triggered from
// inside Open itself, on either backend.
func TestOpenDecodeFailureLeavesTheDocumentUnchanged(t *testing.T) {
	eachBackend(t, func(t *testing.T, b Backend) {
		if _, err := b.Save(context.Background(), []byte("not valid json"), 0); err != nil {
			t.Fatalf("Save: %v", err)
		}
		before, err := b.Load(context.Background())
		if err != nil {
			t.Fatalf("Load before Open: %v", err)
		}

		_, _, err = Open(context.Background(), b, "the test store", func(data []byte) error {
			return errors.New("boom")
		})
		if err == nil {
			t.Fatal("expected Open to fail on a corrupt document")
		}

		after, err := b.Load(context.Background())
		if err != nil {
			t.Fatalf("Load after Open: %v", err)
		}
		if string(after.Payload) != string(before.Payload) {
			t.Errorf("the document changed across a failed Open: before %q, after %q", before.Payload, after.Payload)
		}
		if after.Version != before.Version {
			t.Errorf("the document's version changed across a failed Open: before %d, after %d", before.Version, after.Version)
		}
	})
}
