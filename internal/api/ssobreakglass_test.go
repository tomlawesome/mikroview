// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tomlawesome/mikroview/internal/audit"
)

// "SSO is additive; keep a local admin" (#1252, from #1245 decision 2),
// as the owner ruled it on 2026-09-18: the admin must always be able to
// sign in, even with the identity provider down. mikroview holds
// exactly one admin and never authenticates to the provider on its own
// behalf, so a provider that cannot answer means nobody gets in --
// unless that one account kept a password.
//
// The rule is an invariant in auth.Store.LinkOIDCIdentity rather than
// something the handler arranges; these drive the whole HTTP flow, so
// what is pinned is what a browser actually gets.
//
// (This file replaced an earlier set testing an acknowledgement the
// admin had to send before linking, on the theory that linking cost the
// deployment its last local way in. The ruling removed the cost, so it
// removed the acknowledgement with it.)
func TestCompletedAdminLinkKeepsTheLocalPassword(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()
	totpEnrolAndConfirm(t, client, ts) // #1253: needed before POST /api/auth/oidc/link inside doOIDCLinkFlow below

	resp := doOIDCLinkFlow(t, ts, client)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("link callback = %d, want 302", resp.StatusCode)
	}

	alice, _ := s.Auth.ByUsername("alice")
	if !alice.LocalPassword() {
		t.Error("the linked admin reports no local password")
	}
	if alice.OIDCSubject == "" {
		t.Error("the identity was not attached, so this proves nothing")
	}
	// The end the rule exists for: the provider is now irrelevant to
	// getting in. A fresh client, because the link rotated the session.
	fresh := &http.Client{Jar: mustCookieJar(t)}
	login := postJSON(t, fresh, ts.URL+"/api/auth/login", credentialsRequest{Username: "alice", Password: "password123"})
	defer login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Errorf("signing in with the admin's password after linking = %d, want 200", login.StatusCode)
	}
	if !s.Auth.HasLocalAdmin() {
		t.Error("the deployment lost the way in that does not need the identity provider")
	}
}

// Every other role is unchanged: a successful link converts the account
// to SSO-only, because keeping the weaker local way in alive on an
// account that has moved past it is what linking exists to end.
func TestCompletedNonAdminLinkStillRemovesTheLocalPassword(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	admin := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, admin, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()
	totpEnrolAndConfirm(t, admin, ts) // #1253: needed before POST /api/auth/users below
	postJSON(t, admin, ts.URL+"/api/auth/users", createUserRequest{Username: "bob", Password: "password456", Role: "user"}).Body.Close()

	bob := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, bob, ts.URL+"/api/auth/login", credentialsRequest{Username: "bob", Password: "password456"}).Body.Close()
	totpEnrolAndConfirm(t, bob, ts) // #1253: needed before POST /api/auth/oidc/link inside doOIDCLinkFlow below

	resp := doOIDCLinkFlow(t, ts, bob)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("link callback = %d, want 302", resp.StatusCode)
	}

	linked, _ := s.Auth.ByUsername("bob")
	if linked.LocalPassword() {
		t.Error("a user's link left the local password in place")
	}
	fresh := &http.Client{Jar: mustCookieJar(t)}
	login := postJSON(t, fresh, ts.URL+"/api/auth/login", credentialsRequest{Username: "bob", Password: "password456"})
	defer login.Body.Close()
	if login.StatusCode == http.StatusOK {
		t.Error("the old password still signs the account in after linking")
	}
}

// Nothing extra is asked of the admin at the start of the flow. The
// request carries no parameters at all -- the account is the session's
// -- so an empty body is a complete request whoever sends it.
func TestOIDCLinkStartAsksTheAdminForNothingExtra(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()
	totpEnrolAndConfirm(t, client, ts) // #1253: needed before POST /api/auth/oidc/link below

	resp := postJSON(t, client, ts.URL+"/api/auth/oidc/link", map[string]any{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- the admin's link needs no acknowledgement any more", resp.StatusCode)
	}
}

// The audit log has to be able to answer "what did that link cost the
// account?" afterwards, now that the answer depends on the role.
// Recorded on the completed link, not on the request that started it: a
// link that never came back from the provider changed nothing.
func TestCompletedLinkRecordsWhatItCostTheAccount(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()
	totpEnrolAndConfirm(t, client, ts) // #1253: needed before POST /api/auth/oidc/link inside doOIDCLinkFlow below

	resp := doOIDCLinkFlow(t, ts, client)
	resp.Body.Close()

	var found bool
	for _, e := range s.Audit.Query(audit.Query{}).Entries {
		if e.Action == "account.link_sso" && strings.Contains(e.Detail, "local password kept (admin)") {
			found = true
		}
	}
	if !found {
		t.Error("nothing in the audit log says the admin's link left its password in place")
	}
}

// Once the admin is connected it has both a password and an identity,
// so the only thing "connect SSO" could still mean is a second
// identity. Refused at the start of the flow as well as in the store,
// and the session says the account is connected so the app stops
// offering it at all.
func TestOIDCLinkRefusesASecondConnect(t *testing.T) {
	fp := newFakeOIDCProvider(t)
	s := newOIDCTestServer(t, fp)
	ts := httptest.NewServer(s.Routes())
	defer ts.Close()

	client := &http.Client{Jar: mustCookieJar(t)}
	postJSON(t, client, ts.URL+"/api/auth/register", credentialsRequest{Username: "alice", Password: "password123"}).Body.Close()
	totpEnrolAndConfirm(t, client, ts) // #1253: needed before POST /api/auth/oidc/link inside doOIDCLinkFlow below
	doOIDCLinkFlow(t, ts, client).Body.Close()

	sessResp, err := client.Get(ts.URL + "/api/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer sessResp.Body.Close()
	var body sessionResponse
	if err := json.NewDecoder(sessResp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Authenticated || !body.HasLocalPassword || !body.SSOConnected {
		t.Fatalf("session = %+v, want an authenticated admin with both a password and an identity", body)
	}

	again := postJSON(t, client, ts.URL+"/api/auth/oidc/link", map[string]any{})
	defer again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		t.Errorf("second link start = %d, want 409", again.StatusCode)
	}
}
