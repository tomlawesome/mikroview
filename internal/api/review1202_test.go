// SPDX-License-Identifier: AGPL-3.0-only

package api

// #1202 review fixes (note_34034), written from the fix list before the
// fixes exist: R7 (preferences survive gauntlet's 500 partially-completed)
// and R8 (a role change, and ending one session, drop that session's
// vault unlock).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/gauntlet"
)

// #1202 review R7: gauntlet deletes the account, then fails to revoke the
// tokens it created, and answers 500 partially-completed. The account is
// gone, so its preferences must go with it: nothing can ever read them
// again. Paired with the ordinary deletion (200) and with the refused
// deletion keeping them (gauntlet_test.go).
func TestDeletingAUserRemovesTheirPreferencesWhenTokenRevocationFails(t *testing.T) {
	s := newAuthTestServer(t)
	// One save is allowed: minting the doomed account's token. Revoking
	// it during the deletion is the second, which fails.
	tokens, err := gauntlet.OpenTokenStore(&saveBudgetBackend{left: 1}, gauntlet.TokenOptions{Kinds: TokenKinds})
	if err != nil {
		t.Fatal(err)
	}
	s.Tokens = tokens
	mustGate(t, s)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, s, ts)

	other, err := s.Auth.CreateUser("operator", fixtureUserPassword, gauntlet.RoleUser, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Tokens.Create("operators-token", gauntlet.TokenKindAPI, "", other, time.Now()); err != nil {
		t.Fatalf("minting the token the deletion will fail to revoke: %v", err)
	}
	if err := s.Prefs.Merge(other.ID, []byte(`{"altitude":"street"}`)); err != nil {
		t.Fatal(err)
	}

	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+other.ID, map[string]string{"password": fixtureAdminPassword})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "partially-completed") {
		t.Fatalf("test premise: deletion = %d %s, want 500 partially-completed", resp.StatusCode, body)
	}
	if _, ok := s.Auth.ByUsername("operator"); ok {
		t.Fatal("test premise: the account should be gone after a partially-completed deletion")
	}
	if _, ok := s.Prefs.Get(other.ID); ok {
		t.Error("the account is deleted but its preferences were kept after the 500 partially-completed answer")
	}
}

func TestDeletingAUserRemovesTheirPreferencesOnSuccess(t *testing.T) {
	s := newAuthTestServer(t)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()
	admin := registerAdmin(t, s, ts)
	other, err := s.Auth.CreateUser("operator", fixtureUserPassword, gauntlet.RoleUser, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prefs.Merge(other.ID, []byte(`{"altitude":"street"}`)); err != nil {
		t.Fatal(err)
	}
	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+other.ID, map[string]string{"password": fixtureAdminPassword})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deletion = %d, want 200", resp.StatusCode)
	}
	if _, ok := s.Prefs.Get(other.ID); ok {
		t.Error("a successful deletion kept the account's preferences")
	}
}

// #1202 review R8 (role change): demoting an account that holds the
// vault unlock drops the unlock at once. Paired: a role change gauntlet
// refuses (the role is already held; a role that does not exist) drops
// nothing.
func TestARoleChangeDropsTheAccountsVaultUnlock(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()
	holder, err := s.Auth.CreateUser("keyholder", fixtureAdminPassword, gauntlet.RoleAdmin, time.Now())
	if err != nil {
		t.Fatalf("creating a second admin: %v", err)
	}
	s.vaultUnlock.claim("a-session-of-theirs", holder.ID, time.Now())

	for name, role := range map[string]string{"role already held": "admin", "no such role": "emperor"} {
		resp := putJSON(t, admin, ts.URL+"/api/auth/users/"+holder.ID+"/role", map[string]string{"role": role})
		resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatalf("%s: PUT role = %d, want a refusal", name, resp.StatusCode)
		}
		if s.Vault.Locked() {
			t.Fatalf("%s: a refused role change dropped the vault unlock", name)
		}
	}

	resp := putJSON(t, admin, ts.URL+"/api/auth/users/"+holder.ID+"/role", map[string]string{"role": "user"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("demoting the key holder = %d, want 200", resp.StatusCode)
	}
	if !s.Vault.Locked() {
		t.Error("demoting the account left its vault unlock in memory")
	}
}

// #1202 review R8 (ending one session): the admin's first session holds
// the unlock. Ending a different session of the same account changes
// nothing; ending the holding session drops the unlock at once.
func TestEndingOneSessionDropsThatSessionsVaultUnlock(t *testing.T) {
	s, ts, first, _ := vaultLockFixture(t)
	setPassphrase(t, first, ts, testVaultPassphrase).Body.Close()
	if s.Vault.Locked() {
		t.Fatal("test premise: the vault is locked right after its passphrase was set")
	}
	caller := loggedInClient(t, ts.URL, fixtureAdminUsername, fixtureAdminPassword)
	bystander := loggedInClient(t, ts.URL, fixtureAdminUsername, fixtureAdminPassword)

	holderID := sessionIDFromJar(t, s, first, ts.URL)
	bystanderID := sessionIDFromJar(t, s, bystander, ts.URL)
	refs := map[string]string{} // session ID -> ref
	for _, sess := range s.Sessions.ListForUser(mustUserID(t, s, fixtureAdminUsername), time.Now()) {
		refs[sess.ID] = sess.Ref()
	}
	if refs[holderID] == "" || refs[bystanderID] == "" {
		t.Fatalf("test premise: refs for the holder and the bystander are missing: %v", refs)
	}

	end := func(ref string) int {
		resp := deleteNoBody(t, caller, ts.URL+"/api/auth/sessions/"+ref)
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode
	}
	if got := end(refs[bystanderID]); got != http.StatusOK {
		t.Fatalf("ending the bystander session = %d, want 200", got)
	}
	if s.Vault.Locked() {
		t.Fatal("ending a session that held no unlock dropped the vault unlock")
	}
	if got := end(refs[holderID]); got != http.StatusOK {
		t.Fatalf("ending the holding session = %d, want 200", got)
	}
	if !s.Vault.Locked() {
		t.Error("ending the session that held the vault unlock left the key in memory")
	}
}

func mustUserID(t *testing.T, s *Server, username string) string {
	t.Helper()
	u, ok := s.Auth.ByUsername(username)
	if !ok {
		t.Fatalf("no account %q", username)
	}
	return u.ID
}
