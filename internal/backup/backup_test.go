// SPDX-License-Identifier: AGPL-3.0-only

package backup

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func writeSample(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	err := Write(&buf, "v0.1.0", map[string][]byte{
		"auth":  []byte(`{"users":[{"id":"u1","username":"alice"}]}`),
		"flags": []byte(`{"flags":[]}`),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	env, err := Read(bytes.NewReader(writeSample(t)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if env.Format != FormatVersion {
		t.Errorf("format = %d, want %d", env.Format, FormatVersion)
	}
	if len(env.Stores) != 2 {
		t.Fatalf("got %d stores, want 2", len(env.Stores))
	}
	var auth struct {
		Users []struct{ Username string }
	}
	if err := json.Unmarshal(env.Stores["auth"], &auth); err != nil {
		t.Fatal(err)
	}
	if len(auth.Users) != 1 || auth.Users[0].Username != "alice" {
		t.Errorf("auth store did not survive the round trip: %+v", auth)
	}
}

// The format carries no filenames at all, which is the property that
// makes path traversal impossible rather than merely defended against.
// If a path ever appears in the document, the whole argument for choosing
// this over tar stops holding.
func TestTheDocumentContainsNoFilenames(t *testing.T) {
	zr, err := gzip.NewReader(bytes.NewReader(writeSample(t)))
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if _, err := plain.ReadFrom(zr); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(plain.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"path", "paths", "file", "files", "name", "filename"} {
		if _, ok := env[key]; ok {
			t.Errorf("envelope carries a %q field; the format is supposed to have no filenames in it", key)
		}
	}
	if strings.Contains(plain.String(), "/var/lib") || strings.Contains(plain.String(), "..") {
		t.Error("envelope contains something path-shaped")
	}
}

// A few hundred bytes of gzip can expand to gigabytes. The cap is what
// stops that becoming the process's memory.
//
// This exercises readCapped directly, at a small cap, rather than Read
// against the real MaxDecompressed (~6.5 GiB as of #394): gzip-bombing and
// then decompressing that much on every CI run is what timed out pipeline
// 456's 10-minute budget, with this test still running when the job was
// killed. The cap's own value is pinned separately by
// TestMaxDecompressedCoversTenVaultGenerationsPerRouter; nothing here
// depends on gzip at all, since readCapped's check has nothing to do with
// how the bytes were produced.
func TestRefusesADecompressionBomb(t *testing.T) {
	const smallCap = 4 << 20                          // a few MiB: enough to be a real cap, small enough to run in milliseconds
	data := bytes.Repeat([]byte("A"), smallCap+1<<20) // comfortably past the cap

	_, err := readCapped(bytes.NewReader(data), smallCap)
	if err == nil {
		t.Fatal("a bomb expanding past the cap was accepted")
	}
	if !strings.Contains(err.Error(), "decompression bomb") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// Exhausting the limit must be distinguishable from a clean end of
// stream. Without the +1 and the post-read check, a bomb truncated
// exactly at the cap reads as a valid short backup.
//
// Same small cap as TestRefusesADecompressionBomb and the same reason: the
// real MaxDecompressed made this test the next one to time out pipeline
// 456 after TestRefusesADecompressionBomb did.
func TestABombTruncatedExactlyAtTheCapIsStillRefused(t *testing.T) {
	const smallCap = 4 << 20
	data := bytes.Repeat([]byte("A"), smallCap+1)

	_, err := readCapped(bytes.NewReader(data), smallCap)
	if err == nil {
		t.Fatal("a payload sitting exactly on the cap was accepted")
	}
	// It has to be refused *by the cap*. Asserting only "some error"
	// passes even without the +1, because the payload then fails to
	// parse as JSON instead -- which would not save a bomb whose bytes
	// happen to be valid JSON.
	if !strings.Contains(err.Error(), "decompression bomb") {
		t.Errorf("refused by something other than the size cap, so the cap is not doing the work: %v", err)
	}
}

// A second gzip member appended to a legitimate backup must not be
// silently folded in.
func TestRefusesConcatenatedGzipMembers(t *testing.T) {
	first := writeSample(t)
	var second bytes.Buffer
	zw := gzip.NewWriter(&second)
	zw.Write([]byte(`{"format":1,"stores":{"planted":{}}}`))
	zw.Close()

	joined := append(append([]byte{}, first...), second.Bytes()...)
	env, err := Read(bytes.NewReader(joined))
	// The first member must parse cleanly and the second must be
	// invisible. Accepting "either it works or it errors" is not a test:
	// it passes with Multistream(false) removed, which is the whole
	// defence.
	if err != nil {
		t.Fatalf("the legitimate first member should still parse: %v", err)
	}
	if len(env.Stores) != 2 {
		t.Errorf("got %d stores, want the first member's 2", len(env.Stores))
	}
	if _, planted := env.Stores["planted"]; planted {
		t.Error("the appended member's store was folded into the result")
	}
}

func TestRefusesUnknownFields(t *testing.T) {
	var plain bytes.Buffer
	plain.WriteString(`{"format":1,"created":"2026-01-01T00:00:00Z","appVersion":"v0","stores":{"auth":{}},"exec":"rm -rf /"}`)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(plain.Bytes())
	zw.Close()

	if _, err := Read(bytes.NewReader(buf.Bytes())); err == nil {
		t.Error("a document carrying an unknown field was accepted")
	}
}

func TestRefusesATruncatedStream(t *testing.T) {
	full := writeSample(t)
	if _, err := Read(bytes.NewReader(full[:len(full)/2])); err == nil {
		t.Error("a truncated stream was accepted")
	}
}

func TestRefusesAFutureFormat(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(`{"format":99,"created":"2026-01-01T00:00:00Z","appVersion":"v9","stores":{"auth":{}}}`))
	zw.Close()

	_, err := Read(bytes.NewReader(buf.Bytes()))
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("a future format was not refused as such: %v", err)
	}
}

func TestRefusesPlainJSONThatIsNotGzipped(t *testing.T) {
	if _, err := Read(strings.NewReader(`{"format":1,"stores":{}}`)); err == nil {
		t.Error("an ungzipped document was accepted")
	}
}

// TestMaxDecompressedCoversTenVaultGenerationsPerRouter pins
// maxVaultBytesPerRouter against backupvault's own retention constants
// (#394) -- 10 generations, two 16 MiB files each -- so a change to
// either side's numbers is caught here rather than silently letting
// -backup refuse a vault that is actually within the documented limits.
// Not an import of internal/backupvault (see MaxDecompressed's own doc
// comment for why this package stays a dependency-free leaf): the
// figures are asserted directly instead.
func TestMaxDecompressedCoversTenVaultGenerationsPerRouter(t *testing.T) {
	const (
		generationsPerRouter = 10
		filesPerGeneration   = 2
		maxFileBytes         = 16 << 20
	)
	want := generationsPerRouter * filesPerGeneration * maxFileBytes
	if maxVaultBytesPerRouter != want {
		t.Errorf("maxVaultBytesPerRouter = %d, want %d (10 generations x 2 files x 16 MiB)", maxVaultBytesPerRouter, want)
	}
	if MaxDecompressed < baseMaxDecompressed+maxVaultBytesPerRouter {
		t.Error("MaxDecompressed does not even cover one router's full vault on top of every other store")
	}
}
