// SPDX-License-Identifier: AGPL-3.0-only

package authe2e

import (
	"bytes"
	"os"
	"strings"
	"testing"
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
