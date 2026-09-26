// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// #1331: regenerating recovery codes without removing a second factor.
// Reuses totp_test.go's shared fixtures (totpTestServer, bilbo,
// totpEnrolAndConfirm) rather than duplicating them -- this route sits
// right next to handleTOTPDelete and is exercised the same way.

func regenerateCodes(t *testing.T, client *http.Client, ts *httptest.Server, password string) *http.Response {
	t.Helper()
	return postJSON(t, client, ts.URL+"/api/auth/recovery-codes", recoveryCodesRegenerateRequest{Password: password})
}

// TestRecoveryCodesRegenerateWrongPasswordRefusesAndKeepsOldCodes proves
// the route needs the caller's password, same guard as
// handleTOTPDelete, and that a failed attempt leaves the existing set
// standing.
func TestRecoveryCodesRegenerateWrongPasswordRefusesAndKeepsOldCodes(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	_, codes, _ := totpEnrolAndConfirm(t, bilbo, ts)

	resp := regenerateCodes(t, bilbo, ts, "not-the-password")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("a wrong password got %d, want 401: %s", resp.StatusCode, body)
	}

	// The old set must still work: a wrong-password attempt must not
	// have touched it.
	pending := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	login := submitLoginFactor(t, pending, ts, codes[0])
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Errorf("an old recovery code after a refused regenerate got %d, want 200", login.StatusCode)
	}
}

// TestRecoveryCodesRegenerateRefusedWithoutASecondFactor covers the
// issue's "only when the account has at least one second factor" rule.
// A local account with no factor at all never reaches this route to
// begin with -- requireAuth's forced-enrolment door (secondfactordoor_
// test.go) already refuses every route but the four enrolment ones for
// exactly that account, and this isn't one of them.
func TestRecoveryCodesRegenerateRefusedWithoutASecondFactor(t *testing.T) {
	_, ts, c := unenrolledUser(t)

	resp := regenerateCodes(t, c, ts, "password12345")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("regenerating with no second factor got %d, want 403: %s", resp.StatusCode, body)
	}
}

// TestRecoveryCodesRegenerateOldSetStopsWorkingImmediately is the
// atomic-replacement contract: the moment the new ten are committed,
// every one of the old ten is dead, and one of the new ten works.
func TestRecoveryCodesRegenerateOldSetStopsWorkingImmediately(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	_, oldCodes, _ := totpEnrolAndConfirm(t, bilbo, ts)

	resp := regenerateCodes(t, bilbo, ts, totpBilboPassword)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("regenerate returned %d: %s", resp.StatusCode, body)
	}
	var out recoveryCodesRegenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.RecoveryCodes) != 10 {
		t.Fatalf("got %d fresh recovery codes, want 10", len(out.RecoveryCodes))
	}
	for _, nc := range out.RecoveryCodes {
		for _, oc := range oldCodes {
			if nc == oc {
				t.Fatalf("a freshly minted code (%s) matched an old one -- not actually a new set", nc)
			}
		}
	}

	oldLogin := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	oldResp := submitLoginFactor(t, oldLogin, ts, oldCodes[0])
	defer oldResp.Body.Close()
	if oldResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an old recovery code after regenerating got %d, want 401", oldResp.StatusCode)
	}

	newLogin := startTOTPLogin(t, ts, totpBilboUsername, totpBilboPassword)
	newResp := submitLoginFactor(t, newLogin, ts, out.RecoveryCodes[0])
	defer newResp.Body.Close()
	if newResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(newResp.Body)
		t.Errorf("a fresh recovery code after regenerating got %d, want 200: %s", newResp.StatusCode, body)
	}
}

// TestRecoveryCodesRegenerateDoesNotEndOtherSessions is the design
// lead's 2026-09-26 ruling: unlike confirming a factor (which ends
// every other session) or removing the account's last factor (same),
// regenerating recovery codes changes nothing about which factors
// protect the account, so nothing about it should sign any session out
// -- the codes are a spare key, not the lock.
func TestRecoveryCodesRegenerateDoesNotEndOtherSessions(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	deviceA := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	enrolAndRememberFactor(t, deviceA, ts, totpBilboUsername)
	deviceB := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)

	resp := regenerateCodes(t, deviceA, ts, totpBilboPassword)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("regenerate returned %d: %s", resp.StatusCode, body)
	}

	for name, client := range map[string]*http.Client{"deviceA (the caller)": deviceA, "deviceB": deviceB} {
		r, err := client.Get(ts.URL + "/api/flags")
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("%s's session got %d after regenerating recovery codes, want 200 (no session should have ended)", name, r.StatusCode)
		}
	}
}

// TestRecoveryCodesRegenerateAuditEntry checks the audit line #1331
// asks for, naming who and when -- the same shape account.totp_enabled
// and account.totp_disabled already carry.
func TestRecoveryCodesRegenerateAuditEntry(t *testing.T) {
	_, ts, admin := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	totpEnrolAndConfirm(t, bilbo, ts)

	resp := regenerateCodes(t, bilbo, ts, totpBilboPassword)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("regenerate returned %d", resp.StatusCode)
	}

	entry := findAuditEntry(t, admin, ts, "account.recovery_codes_regenerated")
	if entry.Actor != totpBilboUsername || entry.Target != totpBilboUsername {
		t.Errorf("account.recovery_codes_regenerated entry = %+v, want actor/target %q", entry, totpBilboUsername)
	}
}

// recoveryCodesSaveBudgetBackend is totp_test.go's package's own copy of
// internal/auth's unexported saveBudgetBackend -- that type is
// package-private to internal/auth, so this level needs its own to
// drive a persist failure through the real HTTP handler rather than
// only through auth.Store's own unit tests (which already cover
// GenerateRecoveryCodes' restore-on-failure contract directly).
type recoveryCodesSaveBudgetBackend struct{ left int }

func (b *recoveryCodesSaveBudgetBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	return persist.Snapshot{}, nil
}

func (b *recoveryCodesSaveBudgetBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	if b.left <= 0 {
		return 0, errors.New("backend unavailable")
	}
	b.left--
	return expect + 1, nil
}

func (b *recoveryCodesSaveBudgetBackend) Close() error { return nil }
func (b *recoveryCodesSaveBudgetBackend) Describe() string {
	return "recovery-codes save-budget test backend"
}

// TestRecoveryCodesRegenerateFailedWriteLeavesOldSetIntact is the
// handler-level half of GenerateRecoveryCodes' restore-on-failure
// contract: when the store can't durably commit the fresh ten, the
// route answers 500 and the account's existing codes still work
// afterward, exactly as if regenerating had never been attempted.
func TestRecoveryCodesRegenerateFailedWriteLeavesOldSetIntact(t *testing.T) {
	budget := &recoveryCodesSaveBudgetBackend{left: 1000} // generous through setup
	authStore, err := auth.OpenWithBackend(budget)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newTestServer(t)
	s.Auth = authStore
	ts := httptest.NewServer(s.Routes())
	t.Cleanup(ts.Close)

	const username = "bilbo"
	const password = "recovery-codes-budget-password"
	client := &http.Client{Jar: mustCookieJar(t)}
	reg := postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: username, Password: password})
	reg.Body.Close()
	if reg.StatusCode != http.StatusCreated {
		t.Fatalf("register returned %d", reg.StatusCode)
	}
	_, oldCodes, _ := totpEnrolAndConfirm(t, client, ts)

	budget.left = 0
	resp := regenerateCodes(t, client, ts, password)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("regenerate against a backend that cannot save got %d, want 500: %s", resp.StatusCode, body)
	}

	// The old set must still verify: nothing about the failed write may
	// have replaced it in memory.
	budget.left = 1000
	pending := startTOTPLogin(t, ts, username, password)
	login := submitLoginFactor(t, pending, ts, oldCodes[0])
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(login.Body)
		t.Errorf("an old recovery code after a failed regenerate got %d, want 200: %s", login.StatusCode, body)
	}
}

// TestRecoveryCodesRegenerateRequiresCSRFHeader is a light seam check:
// this route mutates state via POST, so it must sit behind the same
// CSRF header requirement as every other mutating auth route -- not a
// contract specific to #1331, but worth pinning once for a brand new
// route rather than assuming the mux wiring carried it over.
func TestRecoveryCodesRegenerateRequiresCSRFHeader(t *testing.T) {
	_, ts, _ := totpTestServer(t)
	bilbo := loggedInClient(t, ts.URL, totpBilboUsername, totpBilboPassword)
	totpEnrolAndConfirm(t, bilbo, ts)

	b, err := json.Marshal(recoveryCodesRegenerateRequest{Password: totpBilboPassword})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/recovery-codes", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := bilbo.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Error("a request with no CSRF header succeeded, want it refused")
	}
}
