// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/retention"
)

// #1470: -restore of a match_log in the v0.6 envelope shape (plain JSON
// lines, base64-wrapped by wrapForEnvelope) -- the shape the note says does
// not change.

const (
	v06MAC = "52:54:00:ab:cd:ef"
	v06Dst = "203.0.113.9"
)

// v06Envelope writes a backup file whose match_log is two plain JSON lines.
func v06Envelope(t *testing.T, dir string) string {
	t.Helper()
	lines := `{"kind":"record","id":"r1","entryId":"e1","tuple":{"source":{"mac":"` + v06MAC + `"},"destIp":"` + v06Dst + `","port":22},"event":{"raw":"v06-raw-line-1"},"firstSeen":"2026-08-09T12:00:00Z","lastSeen":"2026-08-09T12:00:00Z"}` + "\n" +
		`{"kind":"record","id":"r2","entryId":"e1","tuple":{"source":{"mac":"` + v06MAC + `"},"destIp":"` + v06Dst + `","port":23},"event":{"raw":"v06-raw-line-2"},"firstSeen":"2026-08-09T12:00:01Z","lastSeen":"2026-08-09T12:00:01Z"}` + "\n"
	wrapped, err := wrapForEnvelope("match_log", []byte(lines))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := backup.Write(&buf, "v0.6.0", map[string][]byte{"match_log": wrapped}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "v06.backup")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func restoreEnv(t *testing.T, dst, keyPath string) string {
	t.Helper()
	matchLogPath := filepath.Join(dst, "matchlog.jsonl")
	t.Setenv("MIKROVIEW_CONFIG", "")
	t.Setenv("MIKROVIEW_POSTGRES_DSN_FILE", "")
	t.Setenv("MIKROVIEW_HISTORY_KEY_FILE", keyPath)
	t.Setenv("MIKROVIEW_AUTH_STORE_PATH", filepath.Join(dst, "users.json"))
	t.Setenv("MIKROVIEW_WATCHLIST_MATCH_LOG_PATH", matchLogPath)
	return matchLogPath
}

func TestRestoreOfAMatchLogIntoAnInstallWithNoKeyIsRefusedAndWritesNothing(t *testing.T) {
	src := v06Envelope(t, t.TempDir())
	dst := t.TempDir()
	path := restoreEnv(t, dst, "")

	if code := runRestore([]string{src}); code != 1 {
		t.Errorf("runRestore with no key = %d, want 1", code)
	}
	if _, err := os.Stat(path); err == nil {
		raw, _ := os.ReadFile(path)
		t.Errorf("a match log file was written with no key (%d bytes, plaintext lines: %v)", len(raw), strings.Contains(string(raw), v06MAC))
	}
}

func TestV06EnvelopeRestoresIntoAKeyedInstallAsASealedLog(t *testing.T) {
	src := v06Envelope(t, t.TempDir())
	dst := t.TempDir()
	keyPath := filepath.Join(dst, "history.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("k", retention.MinKeyBytes)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := restoreEnv(t, dst, keyPath)

	if code := runRestore([]string{src}); code != 0 {
		t.Fatalf("runRestore = %d, want 0", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("restored match log: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("MVMLG")) {
		t.Errorf("restored match log does not start with MVMLG: %.12q", raw)
	}
	for _, n := range []string{v06MAC, v06Dst, "v06-raw-line", `"kind"`} {
		if bytes.Contains(raw, []byte(n)) {
			t.Errorf("restored match log holds %q in the clear", n)
		}
	}

	key, err := retention.LoadKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ml, err := matchlog.Open(path, 100, key)
	if err != nil {
		t.Fatalf("matchlog.Open on the restored log: %v", err)
	}
	defer ml.Close()
	var got []matchlog.Record
	if err := ml.Query(context.Background(), matchlog.Query{Source: matchlog.Identity{MAC: v06MAC}}, func(r matchlog.Record) bool {
		got = append(got, r)
		return true
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("restored log reads back %d records, want 2", len(got))
	}
}

// A v0.6 backup carries the raw match log, torn tail included; v0.6 skipped
// that line on replay, so -restore leaves it out and restores the rest
// (v0.7.0 audit, O1-F1).
func TestV06EnvelopeWithATornLineRestoresTheRest(t *testing.T) {
	good := func(id string, port int) string {
		return `{"kind":"record","id":"` + id + `","entryId":"e1","tuple":{"source":{"mac":"` + v06MAC + `"},"destIp":"` + v06Dst + `","port":` + strconv.Itoa(port) + `},"event":{"raw":"v06-raw-line"},"firstSeen":"2026-08-09T12:00:00Z","lastSeen":"2026-08-09T12:00:00Z"}`
	}
	lines := good("r1", 22) + "\n" + good("r2", 23) + "\n" + `{"kind":"record","id":"r3","entryId":"e1","tu`
	wrapped, err := wrapForEnvelope("match_log", []byte(lines))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := backup.Write(&buf, "v0.6.0", map[string][]byte{"match_log": wrapped, "suggestions": []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "v06-torn.backup")
	if err := os.WriteFile(src, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	keyPath := filepath.Join(dst, "history.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("k", retention.MinKeyBytes)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := restoreEnv(t, dst, keyPath)
	sugPath := filepath.Join(dst, "suggestions.json")
	t.Setenv("MIKROVIEW_WATCHLIST_SUGGESTIONS_STORE_PATH", sugPath)

	if code := runRestore([]string{src}); code != 0 {
		t.Fatalf("runRestore of a v0.6 backup with a torn match log line = %d, want 0", code)
	}
	key, err := retention.LoadKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ml, err := matchlog.Open(path, 100, key)
	if err != nil {
		t.Fatalf("matchlog.Open on the restored log: %v", err)
	}
	defer ml.Close()
	var got []string
	if err := ml.Query(context.Background(), matchlog.Query{Source: matchlog.Identity{MAC: v06MAC}}, func(r matchlog.Record) bool {
		got = append(got, r.ID)
		return true
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("restored log holds %v, want r1 and r2", got)
	}
	if _, err := os.Stat(sugPath); err != nil {
		t.Errorf("suggestions.json was not restored: %v", err)
	}
}
