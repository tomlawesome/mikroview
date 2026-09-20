// SPDX-License-Identifier: AGPL-3.0-only

package export

import (
	"strings"
	"testing"
)

// Every secret in this file is an obvious placeholder. The point of the
// test is that the placeholder does not survive -- if one of these
// strings ever shows up in a stored export, the pass is broken.
const placeholder = "not-a-real-secret"

// TestRedactLeavesAHideSensitiveExportUntouched is the ordinary case:
// the router ran /export hide-sensitive, there is nothing left to take,
// and the text comes back byte-identical with no marker comment
// claiming otherwise.
func TestRedactLeavesAHideSensitiveExportUntouched(t *testing.T) {
	in := loadFixture(t)
	out, red := Redact(in)
	if red.Count != 0 {
		t.Errorf("Count = %d, want 0", red.Count)
	}
	if len(red.Lines) != 0 {
		t.Errorf("Lines = %v, want none", red.Lines)
	}
	if red.Marker() != "" {
		t.Errorf("Marker = %q, want empty", red.Marker())
	}
	if out != in {
		t.Error("Redact rewrote an export that carried no secret; want byte-identical")
	}
}

// TestRedactReplacesSecretsAndSaysSo is the case the pass exists for: a
// plain /export, hide-sensitive forgotten, secrets sitting in the text.
// Each one is replaced in place, and the file says how many went and
// where.
func TestRedactReplacesSecretsAndSaysSo(t *testing.T) {
	in := strings.Join([]string{
		`# 2026/09/01 10:00:00 by RouterOS 7.24.1`,
		`/interface wireguard`,
		`add listen-port=13231 name=wg0 private-key="` + placeholder + `"`,
		``,
		`/ip firewall filter`,
		`add action=accept chain=input comment="allow established"`,
		``,
		`/interface wireless security-profiles`,
		`set [ find default=yes ] wpa2-pre-shared-key="` + placeholder + `" mode=dynamic-keys`,
	}, "\n")

	out, red := Redact(in)

	if red.Count != 2 {
		t.Fatalf("Count = %d, want 2", red.Count)
	}
	if strings.Contains(out, placeholder) {
		t.Fatal("the redacted text still carries the secret")
	}
	lines := strings.Split(out, "\n")
	if got, want := lines[0], "# mikroview: 2 secret values removed at ingest (lines 4, 10)"; got != want {
		t.Errorf("marker = %q, want %q", got, want)
	}
	if got := red.Marker(); got != lines[0] {
		t.Errorf("Marker() = %q, want the line it prepended, %q", got, lines[0])
	}
	// The recorded line numbers are counted in the redacted copy, which
	// is the only copy anyone reads.
	for _, n := range red.Lines {
		if !strings.Contains(lines[n-1], RemovedValue) {
			t.Errorf("line %d = %q, want the %s marker on it", n, lines[n-1], RemovedValue)
		}
	}
	if want := `add listen-port=13231 name=wg0 private-key=` + RemovedValue; lines[3] != want {
		t.Errorf("line 4 = %q, want %q", lines[3], want)
	}
	// Everything that was not a secret is exactly where it was.
	if lines[6] != `add action=accept chain=input comment="allow established"` {
		t.Errorf("an unrelated line was rewritten: %q", lines[6])
	}
}

// TestRedactCatchesASecretWrappedOverAContinuation covers RouterOS's
// own line wrapping: a value split across a `\` continuation must not
// leave its second half behind.
func TestRedactCatchesASecretWrappedOverAContinuation(t *testing.T) {
	in := strings.Join([]string{
		`/ppp secret`,
		`add name=vpn-user service=l2tp \`,
		`    password="` + placeholder + `" profile=default`,
	}, "\n")

	out, red := Redact(in)
	if red.Count != 1 {
		t.Fatalf("Count = %d, want 1", red.Count)
	}
	if strings.Contains(out, placeholder) {
		t.Fatalf("the wrapped secret survived: %q", out)
	}
	if !strings.Contains(out, `password=`+RemovedValue) {
		t.Errorf("no marker where the value was: %q", out)
	}
	if !strings.Contains(out, "profile=default") {
		t.Errorf("the rest of the line was lost: %q", out)
	}
}

// TestRedactLeavesAnAlreadyEmptySecretAlone: `password=""` is
// hide-sensitive's own redaction (or simply an unset property). Nothing
// was there, so nothing is claimed to have been removed.
func TestRedactLeavesAnAlreadyEmptySecretAlone(t *testing.T) {
	in := "/ppp secret\nadd name=vpn-user password=\"\" profile=default\n"
	out, red := Redact(in)
	if red.Count != 0 {
		t.Errorf("Count = %d, want 0", red.Count)
	}
	if out != in {
		t.Errorf("Redact = %q, want it untouched", out)
	}
}

// TestParseNeverSeesTheSecret is the invariant the whole ingest order
// rests on: Parse refuses an unredacted export (SecretFieldError), and
// accepts the redacted copy -- so a backup that arrives with secrets in
// it still parses, and what parses has no secret in it.
func TestParseNeverSeesTheSecret(t *testing.T) {
	in := "/interface wireguard\nadd name=wg0 private-key=\"" + placeholder + "\"\n"

	if _, err := Parse(in); err == nil {
		t.Fatal("Parse accepted an unredacted export; want a SecretFieldError")
	}

	redacted, red := Redact(in)
	if red.Count != 1 {
		t.Fatalf("Count = %d, want 1", red.Count)
	}
	ex, err := Parse(redacted)
	if err != nil {
		t.Fatalf("Parse of the redacted copy = error %v, want success", err)
	}
	for i, l := range ex.Lines {
		if strings.Contains(l, placeholder) {
			t.Fatalf("Export.Lines[%d] carries the secret: %q", i, l)
		}
	}
	if strings.Contains(ex.Text(), placeholder) {
		t.Fatal("Export.Text() carries the secret")
	}
}
