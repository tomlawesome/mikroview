// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
)

// "SSO is additive; keep a local admin" (#1252, from #1245 decision 2).
// mikroview holds exactly one admin, so the admin's link is the single
// request in the API that can leave a deployment with no way in that
// does not depend on the identity provider. The rule is enforced here
// and not only in SSOLinkOverlay.svelte -- a browser is not the only
// thing that can POST to this route.
func TestOIDCLinkRefusesTheLastLocalAdminWithoutAnAcknowledgement(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()

	resp := postJSON(t, client, ts.URL+"/api/auth/oidc/link", map[string]any{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for the last admin that can sign in without SSO", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// The refusal has to say the way back in, not just "no": this text
	// is what somebody reads when they got here past the overlay.
	if !strings.Contains(string(body), "-transfer-admin") {
		t.Errorf("refusal = %q, want it to name the command that recovers a locked-out deployment", strings.TrimSpace(string(body)))
	}
	for _, c := range resp.Cookies() {
		if c.Name == oidcFlowCookieName && c.Value != "" {
			t.Error("a link flow was started anyway -- the refusal only changed the status code")
		}
	}
	// Nothing about the account moved.
	alice, _ := s.Auth.ByUsername("alice")
	if !alice.LocalPassword() {
		t.Error("the refused request removed the password it refused to remove")
	}
}

// Refusing outright would be the wrong rule: an operator may genuinely
// want an SSO-only deployment, and #1252 asks for a deliberate act
// rather than a ban. The acknowledgement is that act.
func TestOIDCLinkAllowsTheLastLocalAdminOnceAcknowledged(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()

	resp := postJSON(t, client, ts.URL+"/api/auth/oidc/link", map[string]any{"acknowledgeLastLocalAdmin": true})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 once the admin has acknowledged what linking costs", resp.StatusCode)
	}
}

// The extra confirm belongs to the account whose link costs the
// deployment, not to everyone. An ordinary user linking their own
// account leaves the admin's password exactly where it was, so asking
// them to acknowledge a lock-out that cannot happen would teach people
// to click past the one that can.
func TestOIDCLinkAsksNothingExtraOfANonAdmin(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	admin := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, admin, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "bob", Password: "password456", Role: "user"}).Body.Close()

	bob := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, bob, ts.URL+"/api/auth/login", credentialsRequest{Username: "bob", Password: "password456"}).Body.Close()

	resp := postJSON(t, bob, ts.URL+"/api/auth/oidc/link", map[string]any{})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- a user's link cannot lock the deployment out", resp.StatusCode)
	}
}

// The audit log has to be able to answer "when did SSO become the only
// way in?" afterwards. Recorded on the completed link, not on the
// acknowledgement: a link that never came back from the provider
// changed nothing.
func TestCompletedLastLocalAdminLinkIsAudited(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()

	resp := doOIDCLinkFlow(t, ts, client)
	resp.Body.Close()

	if s.Auth.HasLocalAdmin() {
		t.Fatal("the admin still has a local password -- this test is not set up as it thinks")
	}
	var found bool
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "account.link_sso" && strings.Contains(e.Detail, "no admin can sign in without SSO now") {
			found = true
		}
	}
	if !found {
		t.Error("nothing in the audit log says the deployment lost its local way in")
	}
}
