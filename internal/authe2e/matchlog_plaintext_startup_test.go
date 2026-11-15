// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/retention"
)

// #1470: a server started with a key that finds a plaintext JSON-lines
// match log (the format an earlier release wrote) refuses to start. The
// error names the file and says to move it, and the file is left byte for
// byte. Paired: without that file the same install starts.
func TestPlaintextMatchLogRefusesStartUpAndIsLeftAlone(t *testing.T) {
	t.Parallel()
	first := mustStart(t, Opts{})
	first.stop()

	path := first.file("matchlog.jsonl")
	plain := []byte(`{"kind":"open","id":"m-1","entryId":"entry-1470","tuple":{"source":{"mac":"52:55:0a:00:02:02"},"destIp":"203.0.113.7","port":443},"event":{"id":1,"sourceIp":"10.0.2.2","chain":"forward"},"firstSeen":"2026-01-02T03:04:05Z","lastSeen":"2026-01-02T03:04:05Z"}` + "\n")
	must(t, os.WriteFile(path, plain, 0o600))

	code, out, _ := refused(t, Opts{Root: first.root, KeyFile: first.key})
	if code == 0 {
		t.Errorf("exit status 0 with a plaintext match log\n%s", out)
	}
	if !strings.Contains(out, "matchlog.jsonl") {
		t.Errorf("the refusal does not name the file:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "move") {
		t.Errorf("the refusal does not say to move the file:\n%s", out)
	}
	if got := readBytes(t, path); !bytes.Equal(got, plain) {
		t.Errorf("the plaintext match log was changed:\n got %q\nwant %q", got, plain)
	}

	must(t, os.Remove(path))
	if _, err := start(t, Opts{Root: first.root, KeyFile: first.key}); err != nil {
		t.Errorf("the same install without the plaintext file did not start: %v", err)
	}
}

// PROP-R1: a start refused part way (here by the plaintext match log, which
// opens after a dozen sealed stores) leaves every file still sealed under its
// pre-#1450 path binding exactly as the earlier release wrote it, so that
// release can still open it. Re-sealing happens only once the start commits.
// Paired: with the match log gone the same install starts and re-binds them.
func TestAStartRefusedByAPlaintextMatchLogLeavesOldFormatFilesAsTheyWere(t *testing.T) {
	ri := newRichInstall(t)
	key, err := retention.LoadKey(ri.w.a.key)
	must(t, err)

	old := map[string][]byte{}
	for _, name := range ri.files {
		path := ri.w.a.file(name)
		sealed, err := key.Seal(retention.StateStoreKeyInfo, []byte(path), ri.plain[name])
		must(t, err)
		must(t, os.WriteFile(path, sealed, 0o600))
		old[name] = sealed
	}

	mlPath := ri.w.a.file("matchlog.jsonl")
	plain := []byte(`{"kind":"open","id":"m-1","entryId":"entry-1470","tuple":{"source":{"mac":"52:55:0a:00:02:02"},"destIp":"203.0.113.7","port":443},"event":{"id":1,"sourceIp":"10.0.2.2","chain":"forward"},"firstSeen":"2026-01-02T03:04:05Z","lastSeen":"2026-01-02T03:04:05Z"}` + "\n")
	must(t, os.WriteFile(mlPath, plain, 0o600))

	code, out, _ := refused(t, Opts{Root: ri.w.a.root})
	if code == 0 {
		t.Errorf("exit status 0 with a plaintext match log\n%s", out)
	}
	if !strings.Contains(out, "matchlog.jsonl") {
		t.Errorf("the refusal does not name the file:\n%s", out)
	}
	for _, name := range ri.files {
		if got := readBytes(t, ri.w.a.file(name)); !bytes.Equal(got, old[name]) {
			t.Errorf("the refused start re-sealed %s; it must be left as the earlier release wrote it", name)
		}
	}

	must(t, os.Remove(mlPath))
	a, err := start(t, Opts{Root: ri.w.a.root})
	if err != nil {
		t.Fatalf("start without the plaintext match log: %v", err)
	}
	ri.checkWorks(t, a)
	a.stop()
	for _, name := range ri.files {
		if bytes.Equal(readBytes(t, ri.w.a.file(name)), old[name]) {
			t.Errorf("%s was not re-bound to its store name by the committed start", name)
		}
	}
	b, err := start(t, Opts{Root: ri.w.a.root})
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	ri.checkWorks(t, b)
}
