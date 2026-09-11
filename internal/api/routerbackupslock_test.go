// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testVaultPassphrase = "correct horse battery staple"

// vaultLockFixture is a server with one stored generation and an admin
// signed in, which is the starting point for every test below.
func vaultLockFixture(t *testing.T) (*Server, *httptest.Server, *http.Client, string) {
	t.Helper()
	s := newAuthTestServer(t)
	s.Vault = vaultWithOnePush(t)
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)
	client := setUpAdmin(t, ts)

	gens := s.Vault.Generations("rb5009")
	if len(gens) != 1 {
		t.Fatalf("fixture has %d generations, want 1", len(gens))
	}
	return s, ts, client, gens[0].ID
}

func downloadStatus(t *testing.T, client *http.Client, ts *httptest.Server, generation string) int {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/router-backups/rb5009/" + generation + "/backup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func setPassphrase(t *testing.T, client *http.Client, ts *httptest.Server, passphrase string) *http.Response {
	t.Helper()
	return postJSON(t, client, ts.URL+"/api/router-backups/passphrase", vaultPassphraseRequest{Passphrase: passphrase})
}

func lockStatus(t *testing.T, client *http.Client, ts *httptest.Server) vaultLockStatusResponse {
	t.Helper()
	resp, err := client.Get(ts.URL + "/api/router-backups")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out routerBackupsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Lock
}

func TestVaultLockControlsAreAdminOnly(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	_ = s
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "operator", Password: "password456", Role: "user"}).Body.Close()

	user := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, user, ts.URL+"/api/auth/login", credentialsRequest{Username: "operator", Password: "password456"}).Body.Close()

	for _, path := range []string{"/api/router-backups/unlock", "/api/router-backups/lock", "/api/router-backups/passphrase"} {
		resp := postJSON(t, user, ts.URL+path, vaultPassphraseRequest{Passphrase: testVaultPassphrase})
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST %s as a non-admin = %d, want 403", path, resp.StatusCode)
		}
	}
}

func TestSetPassphraseThenLockRefusesTheDownloadUntilUnlocked(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)

	if got := downloadStatus(t, admin, ts, gen); got != http.StatusOK {
		t.Fatalf("download before any passphrase = %d, want 200", got)
	}

	resp := setPassphrase(t, admin, ts, testVaultPassphrase)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setting the passphrase = %d, want 200", resp.StatusCode)
	}
	// Setting it leaves this session holding the unlock.
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusOK {
		t.Fatalf("download right after setting the passphrase = %d, want 200", got)
	}

	postJSON(t, admin, ts.URL+"/api/router-backups/lock", nil).Body.Close()
	if !s.Vault.Locked() {
		t.Fatal("the vault is not locked after POST /api/router-backups/lock")
	}
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusForbidden {
		t.Fatalf("download while locked = %d, want 403", got)
	}

	wrong := postJSON(t, admin, ts.URL+"/api/router-backups/unlock", vaultPassphraseRequest{Passphrase: "not the passphrase"})
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusForbidden {
		t.Fatalf("unlock with the wrong passphrase = %d, want 403", wrong.StatusCode)
	}
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusForbidden {
		t.Fatalf("download after a refused unlock = %d, want 403", got)
	}

	right := postJSON(t, admin, ts.URL+"/api/router-backups/unlock", vaultPassphraseRequest{Passphrase: testVaultPassphrase})
	right.Body.Close()
	if right.StatusCode != http.StatusOK {
		t.Fatalf("unlock with the right passphrase = %d, want 200", right.StatusCode)
	}
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusOK {
		t.Fatalf("download after unlocking = %d, want 200", got)
	}
}

func TestAnotherSessionOfTheSameAdminStillSeesALockedVault(t *testing.T) {
	_, ts, admin, gen := vaultLockFixture(t)

	// mikroview holds one admin account (auth.ErrSingleAdmin), so "another
	// admin" is another sign-in by the same person -- a second browser, or
	// the phone in their pocket. That is the case worth pinning: the
	// unlock belongs to the session that made it, not to the account.
	second := &http.Client{Jar: mustCookieJar(t)}
	login := postJSON(t, second, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"})
	login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("second sign-in = %d, want 200", login.StatusCode)
	}

	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	if got := downloadStatus(t, second, ts, gen); got != http.StatusForbidden {
		t.Fatalf("download from the admin's other session = %d, want 403", got)
	}
	status := lockStatus(t, second, ts)
	if !status.PassphraseSet {
		t.Error("the other session cannot see that a passphrase is set")
	}
	if status.UnlockedForYou {
		t.Error("UnlockedForYou = true for a session that never unlocked")
	}

	// Locking is allowed from any admin session, and takes the unlock
	// with it wherever it was made.
	postJSON(t, second, ts.URL+"/api/router-backups/lock", nil).Body.Close()
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusForbidden {
		t.Fatalf("download by the unlocking session after the other one locked = %d, want 403", got)
	}
}

func TestSigningOutDropsTheVaultKey(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()
	if s.Vault.Locked() {
		t.Fatal("the vault is locked immediately after its passphrase was set")
	}

	postJSON(t, admin, ts.URL+"/api/auth/logout", nil).Body.Close()
	if !s.Vault.Locked() {
		t.Fatal("signing out left the vault's private key in memory")
	}
}

func TestAnIdleUnlockExpires(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	// Age the unlock past its idle window. Reaching into the state
	// directly rather than waiting fifteen minutes; the clock the
	// handler reads is time.Now, and this is the one thing in the
	// feature that cannot be driven from the request side.
	s.vaultUnlock.mu.Lock()
	s.vaultUnlock.lastUsed = time.Now().Add(-vaultUnlockIdle - time.Minute)
	s.vaultUnlock.mu.Unlock()

	if got := downloadStatus(t, admin, ts, gen); got != http.StatusForbidden {
		t.Fatalf("download on an idle unlock = %d, want 403", got)
	}
	if !s.Vault.Locked() {
		t.Fatal("an expired unlock left the vault's private key in memory")
	}
}

func TestPassphraseTooShortIsRefusedBeforeAnythingChanges(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)
	resp := setPassphrase(t, admin, ts, "short")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("setting a too-short passphrase = %d, want 400", resp.StatusCode)
	}
	if s.Vault.PassphraseSet() {
		t.Fatal("a refused passphrase was set anyway")
	}
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusOK {
		t.Fatalf("download after a refused passphrase = %d, want 200", got)
	}
}

func TestSecondSetPassphraseIsRefused(t *testing.T) {
	_, ts, admin, _ := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()
	resp := setPassphrase(t, admin, ts, "another passphrase entirely")
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("setting a second passphrase = %d, want 409", resp.StatusCode)
	}
}

func TestRemovePassphraseNeedsTheCurrentOne(t *testing.T) {
	s, ts, admin, gen := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	resp := deleteJSON(t, admin, ts.URL+"/api/router-backups/passphrase", vaultPassphraseRequest{Passphrase: "not the passphrase"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("removing the passphrase with the wrong one = %d, want 403", resp.StatusCode)
	}
	if !s.Vault.PassphraseSet() {
		t.Fatal("a refused removal took the passphrase off anyway")
	}

	resp = deleteJSON(t, admin, ts.URL+"/api/router-backups/passphrase", vaultPassphraseRequest{Passphrase: testVaultPassphrase})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("removing the passphrase with the right one = %d, want 200", resp.StatusCode)
	}
	if s.Vault.PassphraseSet() || s.Vault.Locked() {
		t.Fatal("the passphrase survived its own removal")
	}
	if got := downloadStatus(t, admin, ts, gen); got != http.StatusOK {
		t.Fatalf("download after removing the passphrase = %d, want 200", got)
	}
}
