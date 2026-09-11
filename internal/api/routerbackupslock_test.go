// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
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
	// The list tells the second session `locked: false` (the first one
	// holds the unlock), so the refusal must not say "the vault is
	// locked" (#1124): it names the real reason.
	resp, err := second.Get(ts.URL + "/api/router-backups/rb5009/" + gen + "/backup")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "another session holds the vault unlock") {
		t.Errorf("403 body = %q, want it to say another session holds the unlock", body)
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

// #1120: the four paths where the private key outlived the promise that
// it exists only while an admin holds a live unlock.

func TestSigningOutEverywhereDropsTheVaultKey(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	resp := postJSON(t, admin, ts.URL+"/api/auth/logout-all", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign out everywhere = %d, want 200", resp.StatusCode)
	}
	if !s.Vault.Locked() {
		t.Fatal("signing out everywhere left the vault's private key in memory")
	}
}

func TestDeletingAUserDropsTheVaultKey(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "viewer", Password: "password456", Role: "user"}).Body.Close()
	list, err := admin.Get(ts.URL + "/api/auth/users")
	if err != nil {
		t.Fatal(err)
	}
	var users []userSummary
	if err := json.NewDecoder(list.Body).Decode(&users); err != nil {
		t.Fatal(err)
	}
	list.Body.Close()
	var id string
	for _, u := range users {
		if u.Username == "viewer" {
			id = u.ID
		}
	}
	if id == "" {
		t.Fatal("the user just created is not in the account list")
	}
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	resp := deleteJSON(t, admin, ts.URL+"/api/auth/users/"+id, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deleting the user = %d, want 200", resp.StatusCode)
	}
	if !s.Vault.Locked() {
		t.Fatal("deleting a user left the vault's private key in memory")
	}
}

func TestChangingAPasswordDropsAnotherSessionsVaultKey(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	// The unlock is held by the first sign-in; the password is changed
	// from the second. That is the shape an operator acting on a
	// suspected theft produces, and the old code locked nothing because
	// the calling session was not the holder.
	second := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, second, ts.URL+"/api/auth/login", credentialsRequest{Username: "admin", Password: "password123"}).Body.Close()
	resp := postJSON(t, second, ts.URL+"/api/auth/password", changePasswordRequest{CurrentPassword: "password123", NewPassword: "password789"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("changing the password = %d, want 200", resp.StatusCode)
	}
	if !s.Vault.Locked() {
		t.Fatal("changing the password left another session's vault key in memory")
	}
}

func TestAStatusPollDoesNotRenewTheUnlock(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	// Put the unlock most of the way through its idle window, then poll
	// the status the way an open settings tab does. The poll must read
	// the clock, not reset it.
	aged := time.Now().Add(-vaultUnlockIdle + time.Minute)
	s.vaultUnlock.mu.Lock()
	s.vaultUnlock.lastUsed = aged
	s.vaultUnlock.mu.Unlock()

	if status := lockStatus(t, admin, ts); !status.UnlockedForYou {
		t.Fatal("a still-live unlock is not reported to the session holding it")
	}

	s.vaultUnlock.mu.Lock()
	after := s.vaultUnlock.lastUsed
	s.vaultUnlock.mu.Unlock()
	if !after.Equal(aged) {
		t.Fatal("polling the lock status renewed the unlock it was reporting on")
	}
}

func TestAnIdleUnlockExpiresWithNoTraffic(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	// The sweeper reads Server.Now, so the fifteen minutes pass without
	// the test waiting for them.
	s.Now = func() time.Time { return time.Now().Add(2 * vaultUnlockIdle) }
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()
	if s.Vault.Locked() {
		t.Fatal("the vault is locked immediately after its passphrase was set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.RunVaultUnlockExpiry(ctx, time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
	for !s.Vault.Locked() {
		if time.Now().After(deadline) {
			t.Fatal("an unlock left idle with no requests never expired")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestAnUnlockNobodyHoldsIsDropped(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()

	// What a passphrase change that failed half way leaves behind: the
	// key in memory, and no session holding it.
	s.vaultUnlock.release()
	s.expireVaultUnlock(time.Now())
	if !s.Vault.Locked() {
		t.Fatal("a key with no holder was left in memory")
	}
}

func TestUnlockingAVaultWithNoPassphraseIsNotAuditedAsAGuess(t *testing.T) {
	s, ts, admin, _ := vaultLockFixture(t)
	resp := postJSON(t, admin, ts.URL+"/api/router-backups/unlock", vaultPassphraseRequest{Passphrase: testVaultPassphrase})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("unlocking a vault with no passphrase = %d, want 409", resp.StatusCode)
	}
	for _, e := range s.Audit.Query(audit.Query{Limit: 100}).Entries {
		if e.Action == "router_backup.unlock_failed" {
			t.Fatal("a vault with no passphrase recorded a failed unlock attempt, which is the signal that means someone is guessing")
		}
	}

	// The signal itself still fires, on the one case that means it.
	setPassphrase(t, admin, ts, testVaultPassphrase).Body.Close()
	postJSON(t, admin, ts.URL+"/api/router-backups/unlock", vaultPassphraseRequest{Passphrase: "not the passphrase"}).Body.Close()
	var guesses int
	for _, e := range s.Audit.Query(audit.Query{Limit: 100}).Entries {
		if e.Action == "router_backup.unlock_failed" {
			guesses++
		}
	}
	if guesses != 1 {
		t.Fatalf("wrong-passphrase attempts audited = %d, want 1", guesses)
	}
}
