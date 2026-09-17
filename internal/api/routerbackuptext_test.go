// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	v, err := backupvault.Open(t.TempDir(), testRetentionKey(t))
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
