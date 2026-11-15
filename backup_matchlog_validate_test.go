// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// #1470 review: a match_log in the envelope with a line that is not a
// match log line is refused in -restore's validate-first pass, before the
// restore marker or any store is written.
func TestRestoreOfABadMatchLogLineChangesNothing(t *testing.T) {
	good := `{"kind":"record","id":"r1","entryId":"e1","tuple":{"source":{"mac":"52:54:00:ab:cd:ef"},"destIp":"203.0.113.9","port":22},"firstSeen":"2026-08-09T12:00:00Z","lastSeen":"2026-08-09T12:00:00Z"}`
	wrapped, err := wrapForEnvelope("match_log", []byte(good+"\n"+`{"kind":"wat","id":"x"}`+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	suggestions := []byte(`{}`)
	var buf bytes.Buffer
	if err := backup.Write(&buf, "v0.7.0", map[string][]byte{"match_log": wrapped, "suggestions": suggestions}); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "bad.backup")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	keyPath := filepath.Join(dst, "history.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("k", retention.MinKeyBytes)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	restoreEnv(t, dst, keyPath)
	// A live suggestions file, to show it is not touched at all -- not
	// written and then rolled back -- when the match log is refused.
	sugPath := filepath.Join(dst, "suggestions.json")
	t.Setenv("MIKROVIEW_WATCHLIST_SUGGESTIONS_STORE_PATH", sugPath)
	live := []byte("live suggestions, left as they are")
	if err := os.WriteFile(sugPath, live, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(sugPath, old, old); err != nil {
		t.Fatal(err)
	}

	if code := runRestore([]string{src, "--force"}); code != 1 {
		t.Errorf("runRestore of a bad match log line = %d, want 1", code)
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "history.key" && e.Name() != "suggestions.json" {
			t.Errorf("restore wrote %s before refusing a bad match log line", e.Name())
		}
	}
	if got, _ := os.ReadFile(sugPath); !bytes.Equal(got, live) {
		t.Errorf("the live suggestions file was changed: %q", got)
	}
	if fi, err := os.Stat(sugPath); err != nil || !fi.ModTime().Equal(old) {
		t.Errorf("the live suggestions file was rewritten (mtime %v, %v)", fi.ModTime(), err)
	}
	if parent, err := os.ReadDir(filepath.Dir(dst)); err == nil {
		for _, e := range parent {
			if strings.Contains(e.Name(), "restore") {
				t.Errorf("a restore marker %s was left beside the data directory", e.Name())
			}
		}
	}
}
