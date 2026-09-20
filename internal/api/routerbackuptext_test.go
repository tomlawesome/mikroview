// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backupvault"
)

// vaultWithTwoExports stores two generations whose exports differ by one
// rule, and whose first one arrived carrying a secret hide-sensitive
// should have taken out -- the pair the read and the comparison below
// are both about.
func vaultWithTwoExports(t *testing.T) *backupvault.Vault {
	t.Helper()
	v, err := backupvault.Open(t.TempDir(), testRetentionKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	v.SetSpaceProbeForTest(func(string) (int64, int64, error) { return 50 << 30, 100 << 30, nil })

	backup := append([]byte{0x88, 0xac, 0xa1, 0xb1}, []byte("a backup")...)
	first := strings.Join([]string{
		"# 2026/09/01 03:00:00 by RouterOS 7.24.1",
		"/ppp secret",
		`add name=vpn-user password="not-a-real-secret"`,
		"/ip firewall filter",
		"add action=accept chain=input",
		"",
	}, "\n")
	second := strings.Join([]string{
		"# 2026/09/02 03:00:00 by RouterOS 7.24.1",
		"/ppp secret",
		`add name=vpn-user password=""`,
		"/ip firewall filter",
		"add action=accept chain=input",
		"add action=drop chain=forward",
		"",
	}, "\n")

	now := time.Now()
	for i, text := range []string{first, second} {
		at := now.Add(time.Duration(i) * time.Hour)
		if err := v.Store("rb5009", backupvault.KindBackup, backup, at); err != nil {
			t.Fatal(err)
		}
		if err := v.Store("rb5009", backupvault.KindRsc, []byte(text), at); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func generationIDs(t *testing.T, v *backupvault.Vault) []string {
	t.Helper()
	gens := v.Generations("rb5009")
	ids := make([]string, len(gens))
	for i, g := range gens {
		ids[i] = g.ID
	}
	return ids
}

// TestRouterBackupTextReadsTheRedactedCopy is the whole ingest promise
// seen from the far end: the router pushed a secret, and what an admin
// can read back has the marker where the value was and says so.
func TestRouterBackupTextReadsTheRedactedCopy(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithTwoExports(t)
	ids := generationIDs(t, s.Vault)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/" + ids[0] + "/text")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out routerBackupTextResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Text, "not-a-real-secret") {
		t.Fatal("the stored export still carries the secret the router pushed")
	}
	if !out.Redacted {
		t.Error("Redacted = false, want the read to say something was taken out")
	}
	if !strings.HasPrefix(out.Text, "# mikroview: 1 secret values removed at ingest") {
		t.Errorf("text begins %q, want the redaction marker", firstLine(out.Text))
	}
	if out.Lines < 5 {
		t.Errorf("Lines = %d, want the whole export counted", out.Lines)
	}
}

// TestRouterBackupDiffReportsOnlyWhatChanged: the date header differs on
// every export and is ignored; the rule that was added is not.
func TestRouterBackupDiffReportsOnlyWhatChanged(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithTwoExports(t)
	ids := generationIDs(t, s.Vault)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/diff?from=" + ids[0] + "&to=" + ids[1])
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out routerBackupDiffResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Same {
		t.Fatal("Same = true, want the added rule reported")
	}
	var added, removed []string
	for _, l := range out.Lines {
		if strings.Contains(l.Text, "by RouterOS") {
			t.Errorf("the date header reached the diff: %q", l.Text)
		}
		if l.Op == "+" {
			added = append(added, l.Text)
		} else {
			removed = append(removed, l.Text)
		}
	}
	if !contains(added, "add action=drop chain=forward") {
		t.Errorf("added = %q, want the new rule", added)
	}
	// The older side's redaction marker went, and its redacted password
	// line changed -- both real facts about the pair, neither a secret.
	if !contains(removed, `add name=vpn-user password="<removed>"`) {
		t.Errorf("removed = %q, want the redacted password line", removed)
	}
}

// TestRouterBackupDiffNeedsBothGenerations: a comparison with one half
// missing is a bad request, not an empty answer that reads as "nothing
// changed".
func TestRouterBackupDiffNeedsBothGenerations(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithTwoExports(t)
	ids := generationIDs(t, s.Vault)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	client := setUpAdmin(t, ts)

	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/diff?from=" + ids[0])
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func textStatus(t *testing.T, client *http.Client, ts *httptest.Server, generation string) int {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/" + generation + "/text")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func diffStatus(t *testing.T, client *http.Client, ts *httptest.Server, from, to string) int {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/diff?from=" + from + "&to=" + to)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestTextAndDiffRefuseALockedVault covers #1262: handleRouterBackupText
// and handleRouterBackupDiff carry the same vault passphrase gate
// handleRouterBackupDownload has always had (#956), but nothing
// exercised it against either of these two newer routes -- so a gate
// that silently stopped applying here (a refactor that missed one call
// site, say) would have shipped unnoticed.
func TestTextAndDiffRefuseALockedVault(t *testing.T) {
	s := newAuthTestServer(t)
	s.Vault = vaultWithTwoExports(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := setUpAdmin(t, ts)
	ids := generationIDs(t, s.Vault)

	if got := textStatus(t, admin, ts, ids[0]); got != http.StatusOK {
		t.Fatalf("text before any passphrase = %d, want 200", got)
	}
	if got := diffStatus(t, admin, ts, ids[0], ids[1]); got != http.StatusOK {
		t.Fatalf("diff before any passphrase = %d, want 200", got)
	}

	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()
	postJSON(t, admin, ts.URL+"/api/router-backups/lock", nil).Body.Close()
	if !s.Vault.Locked() {
		t.Fatal("the vault is not locked after POST /api/router-backups/lock")
	}

	if got := textStatus(t, admin, ts, ids[0]); got != http.StatusForbidden {
		t.Errorf("text while locked = %d, want 403", got)
	}
	if got := diffStatus(t, admin, ts, ids[0], ids[1]); got != http.StatusForbidden {
		t.Errorf("diff while locked = %d, want 403", got)
	}

	unlock := postJSON(t, admin, ts.URL+"/api/router-backups/unlock", vaultPassphraseRequest{Passphrase: testVaultPassphrase})
	unlock.Body.Close()
	if unlock.StatusCode != http.StatusOK {
		t.Fatalf("unlock = %d, want 200", unlock.StatusCode)
	}
	if got := textStatus(t, admin, ts, ids[0]); got != http.StatusOK {
		t.Errorf("text after unlocking = %d, want 200", got)
	}
	if got := diffStatus(t, admin, ts, ids[0], ids[1]); got != http.StatusOK {
		t.Errorf("diff after unlocking = %d, want 200", got)
	}
}

// TestVaultUnlockRefusalIsOneGateNotTwoCopies covers the other half of
// #1262: the "another session holds the vault unlock" refusal --
// word-for-word identical between handleRouterBackupDownload
// (routerbackups.go) and readBackupText (this file) except for the verb
// at the end -- was hand-copied rather than shared, so a future wording
// fix applied to one had nothing to stop it silently missing the other.
// Both routes must produce that refusal through one function, which
// means the literal text appears in the package's source exactly once.
func TestVaultUnlockRefusalIsOneGateNotTwoCopies(t *testing.T) {
	const refusal = "another session holds the vault unlock"
	var total int
	for _, file := range []string{"routerbackups.go", "routerbackuptext.go", "routerbackupslock.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		total += bytes.Count(source, []byte(refusal))
	}
	if total != 1 {
		t.Errorf("%q appears %d times across routerbackups.go, routerbackuptext.go and routerbackupslock.go, want exactly 1 (one shared gate, not a hand-copied second one)", refusal, total)
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
