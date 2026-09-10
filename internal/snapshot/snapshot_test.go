// SPDX-License-Identifier: AGPL-3.0-only

package snapshot

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSealer is a Sealer test double: real AES-256-GCM, but with a fixed
// key and no HKDF derivation, since these tests are about this package's
// own read/write/rotation/encrypt-at-rest logic, not about the master
// key's derivation -- internal/retention's own tests cover that, and this
// package cannot import internal/retention directly without an import
// cycle (see Sealer's doc comment in write.go).
type fakeSealer struct{ aead cipher.AEAD }

func newFakeSealer() *fakeSealer {
	block, err := aes.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &fakeSealer{aead: aead}
}

func (f *fakeSealer) Seal(info string, aad, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, f.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return f.aead.Seal(nonce, nonce, plaintext, append([]byte(info), aad...)), nil
}

func (f *fakeSealer) Open(info string, aad, envelope []byte) ([]byte, error) {
	if len(envelope) < f.aead.NonceSize() {
		return nil, errors.New("fakeSealer: envelope too short")
	}
	nonce, ciphertext := envelope[:f.aead.NonceSize()], envelope[f.aead.NonceSize():]
	return f.aead.Open(nil, nonce, ciphertext, append([]byte(info), aad...))
}

// testKey is the Sealer every test in this file seals and opens documents
// under.
var testKey = newFakeSealer()

// sealTestDocument seals raw JSON bytes the way Write does, for tests
// that plant a crafted document directly rather than going through a
// Writer -- so Load's own decrypt step does not reject it before the
// behaviour under test (a bad schema version, a missing taken time, ...)
// ever gets exercised. name is the file the bytes will be written under:
// AAD has to match what Load will pass, which is the file's own base
// name.
func sealTestDocument(t *testing.T, name string, plain []byte) []byte {
	t.Helper()
	sealed, err := testKey.Seal(warmRestartKeyInfo, []byte(filepath.Base(name)), plain)
	if err != nil {
		t.Fatalf("sealing a test document: %v", err)
	}
	return sealed
}

// captureLog swaps the package logger for one writing into a buffer, so
// the "skipped with one log line" requirement in #795 can be asserted
// rather than assumed. Restored by t.Cleanup, so tests must not run in
// parallel with each other.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := logger
	logger = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { logger = previous })
	return buf
}

func logLines(buf *bytes.Buffer) []string {
	trimmed := strings.TrimSpace(buf.String())
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// testPart is a Part whose behaviour each test dictates: what it
// exports, whether either direction fails, and what it was handed.
type testPart struct {
	name      string
	payload   string
	exportErr error
	importErr error

	imported   json.RawMessage
	gotTaken   time.Time
	gotNow     time.Time
	importCall int
}

func (p *testPart) Name() string { return p.name }

func (p *testPart) Export() (json.RawMessage, error) {
	if p.exportErr != nil {
		return nil, p.exportErr
	}
	return json.RawMessage(p.payload), nil
}

func (p *testPart) Import(raw json.RawMessage, taken, now time.Time) error {
	p.importCall++
	if p.importErr != nil {
		return p.importErr
	}
	p.imported = raw
	p.gotTaken = taken
	p.gotNow = now
	return nil
}

func TestWriteThenLoadRestoresEveryPart(t *testing.T) {
	dir := t.TempDir()
	taken := time.Date(2026, 9, 3, 14, 2, 3, 0, time.UTC)

	store := &testPart{name: "store", payload: `{"total":7}`}
	devices := &testPart{name: "devices", payload: `[{"id":"core"}]`}

	path, err := New(dir, 3, testKey, store, devices).Write(taken)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := filepath.Base(path); got != "snapshot-20260903T140203Z.json" {
		t.Errorf("file name = %q, want the taken time in it so the directory sorts chronologically", got)
	}

	loadStore := &testPart{name: "store"}
	loadDevices := &testPart{name: "devices"}
	now := taken.Add(4 * time.Minute)
	report, err := Load(dir, testKey, now, loadStore, loadDevices)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if report.Path != path {
		t.Errorf("Report.Path = %q, want %q", report.Path, path)
	}
	if !report.Taken.Equal(taken) {
		t.Errorf("Report.Taken = %v, want %v", report.Taken, taken)
	}
	if got := strings.Join(report.Imported, ","); got != "store,devices" {
		t.Errorf("Imported = %v, want both parts in the order they were registered", report.Imported)
	}
	if len(report.Skipped) != 0 {
		t.Errorf("Skipped = %v, want none", report.Skipped)
	}
	if string(loadStore.imported) != `{"total":7}` {
		t.Errorf("store part got %q, want its own bytes back unchanged", loadStore.imported)
	}
	if string(loadDevices.imported) != `[{"id":"core"}]` {
		t.Errorf("devices part got %q, want its own bytes back unchanged", loadDevices.imported)
	}
	if !loadStore.gotTaken.Equal(taken) || !loadStore.gotNow.Equal(now) {
		t.Errorf("part was handed taken=%v now=%v, want %v and %v -- a part ages its contents against both",
			loadStore.gotTaken, loadStore.gotNow, taken, now)
	}
}

func TestWrittenFileIsPrivateAndCarriesTheDocumentShape(t *testing.T) {
	dir := t.TempDir()
	taken := time.Date(2026, 9, 3, 14, 2, 3, 0, time.UTC)

	path, err := New(dir, 3, testKey, &testPart{name: "store", payload: `{"total":1}`}).Write(taken)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %o, want 600 -- a snapshot describes a network", mode)
	}

	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	for _, marker := range []string{"total", "store", `"version"`} {
		if bytes.Contains(sealed, []byte(marker)) {
			t.Errorf("the written file contains plaintext marker %q -- it is not actually encrypted (#853)", marker)
		}
	}
	data, err := testKey.Open(warmRestartKeyInfo, []byte(filepath.Base(path)), sealed)
	if err != nil {
		t.Fatalf("decrypting the written file: %v", err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the decrypted file does not parse as a Document: %v", err)
	}
	if doc.Version != Version {
		t.Errorf("Version = %d, want %d", doc.Version, Version)
	}
	if !doc.Expires.Equal(taken.Add(MaxAge)) {
		t.Errorf("Expires = %v, want taken plus MaxAge (%v)", doc.Expires, taken.Add(MaxAge))
	}
	if _, ok := doc.Parts["store"]; !ok {
		t.Errorf("Parts = %v, want the store part keyed by its own name", doc.Parts)
	}

	// No temp files left behind: the rename is the only thing that should
	// have published anything.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want just the snapshot", len(entries))
	}
}

// TestRotationKeepsExactlyKeepFilesUnderAHostileClock is #795's own
// rotation requirement: a snapshot dated in the future must not break
// the count, and must never be the one that gets loaded.
func TestRotationKeepsExactlyKeepFilesUnderAHostileClock(t *testing.T) {
	captureLog(t)
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	base := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	// A file a year ahead of the clock, planted before any real write, so
	// it sorts newest for the whole test.
	future := base.Add(365 * 24 * time.Hour)
	futureName := filepath.Join(dir, fileName(future))
	futureDoc, err := json.Marshal(Document{Version: Version, Taken: future, Expires: future.Add(MaxAge), Parts: map[string]json.RawMessage{"store": json.RawMessage(`{"from":"the future"}`)}})
	if err != nil {
		t.Fatalf("marshalling the hostile document: %v", err)
	}
	if err := os.WriteFile(futureName, sealTestDocument(t, futureName, futureDoc), 0o600); err != nil {
		t.Fatalf("planting the hostile snapshot: %v", err)
	}

	const keep = 3
	w := New(dir, keep, testKey, &testPart{name: "store", payload: `{"generation":"real"}`})
	var lastReal string
	for i := 0; i < 6; i++ {
		lastReal, err = w.Write(base.Add(time.Duration(i) * time.Minute))
		if err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != keep {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory holds %d files (%v), want exactly keep=%d", len(entries), names, keep)
	}

	part := &testPart{name: "store"}
	report, err := Load(dir, testKey, base.Add(10*time.Minute), part)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if report.Path != lastReal {
		t.Errorf("loaded %q, want the newest real snapshot %q -- a future-dated file must never win", report.Path, lastReal)
	}
	if string(part.imported) != `{"generation":"real"}` {
		t.Errorf("imported %q, want the real generation's bytes", part.imported)
	}
}

func TestLoadSkipsATruncatedFileAndFallsThroughToTheNextNewest(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	base := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	w := New(dir, 5, testKey, &testPart{name: "store", payload: `{"generation":"older"}`})
	older, err := w.Write(base)
	if err != nil {
		t.Fatalf("Write older: %v", err)
	}
	newer, err := New(dir, 5, testKey, &testPart{name: "store", payload: `{"generation":"newer"}`}).Write(base.Add(time.Minute))
	if err != nil {
		t.Fatalf("Write newer: %v", err)
	}

	// Truncate the newest mid-document, the shape a crash between rename
	// and flush would leave if the syncs were not there.
	data, err := os.ReadFile(newer)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(newer, data[:len(data)/2], 0o600); err != nil {
		t.Fatalf("truncating: %v", err)
	}

	part := &testPart{name: "store"}
	report, err := Load(dir, testKey, base.Add(2*time.Minute), part)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if report.Path != older {
		t.Errorf("loaded %q, want the intact older snapshot %q", report.Path, older)
	}
	if lines := logLines(buf); len(lines) != 1 {
		t.Errorf("log lines = %d (%v), want exactly one for the skipped file", len(lines), lines)
	}
}

func TestLoadSkipsAForeignSchemaAndFallsThroughToTheNextNewest(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	base := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	older, err := New(dir, 5, testKey, &testPart{name: "store", payload: `{"generation":"older"}`}).Write(base)
	if err != nil {
		t.Fatalf("Write older: %v", err)
	}

	// Valid JSON, plausible file name, a schema this build does not read.
	foreign := base.Add(time.Minute)
	foreignPath := filepath.Join(dir, fileName(foreign))
	body := fmt.Sprintf(`{"version":%d,"taken":%q,"expires":%q,"parts":{}}`,
		Version+41, foreign.Format(time.RFC3339), foreign.Add(MaxAge).Format(time.RFC3339))
	if err := os.WriteFile(foreignPath, sealTestDocument(t, foreignPath, []byte(body)), 0o600); err != nil {
		t.Fatalf("planting the foreign snapshot: %v", err)
	}

	report, err := Load(dir, testKey, base.Add(2*time.Minute), &testPart{name: "store"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if report.Path != older {
		t.Errorf("loaded %q, want the readable older snapshot %q", report.Path, older)
	}
	if lines := logLines(buf); len(lines) != 1 {
		t.Errorf("log lines = %d (%v), want exactly one for the skipped file", len(lines), lines)
	}
}

func TestLoadRefusesAnExpiredSnapshot(t *testing.T) {
	captureLog(t)
	dir := t.TempDir()
	taken := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	if _, err := New(dir, 5, testKey, &testPart{name: "store", payload: `{}`}).Write(taken); err != nil {
		t.Fatalf("Write: %v", err)
	}

	part := &testPart{name: "store"}
	_, err := Load(dir, testKey, taken.Add(MaxAge+time.Minute), part)
	if !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("Load past the document's own expiry = %v, want ErrNoSnapshot", err)
	}
	if part.importCall != 0 {
		t.Errorf("part was imported from an expired snapshot")
	}
}

func TestLoadOnAMissingDirectoryIsAColdStartNotAFailure(t *testing.T) {
	report, err := Load(filepath.Join(t.TempDir(), "never-written"), testKey, time.Now(), &testPart{name: "store"})
	if !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("Load on a missing directory = %v, want ErrNoSnapshot", err)
	}
	if report.Path != "" {
		t.Errorf("Report = %+v, want the zero value", report)
	}
}

func TestForeignFilesAreNeitherLoadedNorRotatedAway(t *testing.T) {
	captureLog(t)
	dir := t.TempDir()
	base := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	notes := filepath.Join(dir, "operator-notes.txt")
	if err := os.WriteFile(notes, []byte("not ours"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w := New(dir, 1, testKey, &testPart{name: "store", payload: `{}`})
	for i := 0; i < 3; i++ {
		if _, err := w.Write(base.Add(time.Duration(i) * time.Minute)); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	if _, err := os.Stat(notes); err != nil {
		t.Errorf("a file that is not ours was rotated away: %v", err)
	}
	if _, err := Load(dir, testKey, base.Add(3*time.Minute), &testPart{name: "store"}); err != nil {
		t.Errorf("Load: %v, want the foreign file simply ignored", err)
	}
}

func TestAPartThatCannotExportIsOmittedRatherThanFailingTheWrite(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	taken := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	broken := &testPart{name: "engine", exportErr: errors.New("half-built state")}
	good := &testPart{name: "store", payload: `{"total":3}`}
	if _, err := New(dir, 2, testKey, broken, good).Write(taken); err != nil {
		t.Fatalf("Write: %v, want one broken part not to cost the whole document", err)
	}
	if lines := logLines(buf); len(lines) != 1 {
		t.Errorf("log lines = %d (%v), want exactly one for the omitted part", len(lines), lines)
	}

	loadEngine := &testPart{name: "engine"}
	loadStore := &testPart{name: "store"}
	report, err := Load(dir, testKey, taken.Add(time.Minute), loadEngine, loadStore)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Join(report.Imported, ",") != "store" {
		t.Errorf("Imported = %v, want only the part that exported", report.Imported)
	}
	if strings.Join(report.Skipped, ",") != "engine" {
		t.Errorf("Skipped = %v, want the part with nothing in the document", report.Skipped)
	}
	if loadEngine.importCall != 0 {
		t.Errorf("a part with no bytes in the document was still imported")
	}
}

func TestAPartThatCannotImportDoesNotStopTheOthers(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	taken := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	if _, err := New(dir, 2, testKey,
		&testPart{name: "engine", payload: `{"baselines":1}`},
		&testPart{name: "store", payload: `{"total":3}`},
	).Write(taken); err != nil {
		t.Fatalf("Write: %v", err)
	}

	loadEngine := &testPart{name: "engine", importErr: errors.New("unreadable baselines")}
	loadStore := &testPart{name: "store"}
	report, err := Load(dir, testKey, taken.Add(time.Minute), loadEngine, loadStore)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if strings.Join(report.Imported, ",") != "store" {
		t.Errorf("Imported = %v, want the part that could import", report.Imported)
	}
	if strings.Join(report.Skipped, ",") != "engine" {
		t.Errorf("Skipped = %v, want the part that refused its bytes", report.Skipped)
	}
	if string(loadStore.imported) != `{"total":3}` {
		t.Errorf("store part got %q, want a partly warm start rather than a cold one", loadStore.imported)
	}
	if lines := logLines(buf); len(lines) != 1 {
		t.Errorf("log lines = %d (%v), want exactly one for the part that failed", len(lines), lines)
	}
}

func TestKeepIsFlooredAtOneSoRotationNeverDeletesWhatItJustWrote(t *testing.T) {
	dir := t.TempDir()
	taken := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	path, err := New(dir, 0, testKey, &testPart{name: "store", payload: `{}`}).Write(taken)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("keep=0 deleted the snapshot it had just written: %v", err)
	}
}

func TestLoadRefusesADocumentWithNoTakenTime(t *testing.T) {
	buf := captureLog(t)
	dir := t.TempDir()
	base := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	path := filepath.Join(dir, fileName(base))
	body := fmt.Sprintf(`{"version":%d,"parts":{"store":{}}}`, Version)
	if err := os.WriteFile(path, sealTestDocument(t, path, []byte(body)), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Load(dir, testKey, base.Add(time.Minute), &testPart{name: "store"}); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("Load = %v, want ErrNoSnapshot: a document with no taken time has an unknowable age", err)
	}
	if lines := logLines(buf); len(lines) != 1 {
		t.Errorf("log lines = %d (%v), want exactly one", len(lines), lines)
	}
}

func TestASnapshotWithinTheClockSkewToleranceIsStillLoaded(t *testing.T) {
	captureLog(t)
	dir := t.TempDir()
	taken := time.Date(2026, 9, 3, 14, 0, 0, 0, time.UTC)

	if _, err := New(dir, 2, testKey, &testPart{name: "store", payload: `{}`}).Write(taken); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The loader's clock a few seconds behind the writer's: an NTP nudge,
	// not a hostile file.
	if _, err := Load(dir, testKey, taken.Add(-5*time.Second), &testPart{name: "store"}); err != nil {
		t.Errorf("Load = %v, want a few seconds of clock skew tolerated", err)
	}
}
