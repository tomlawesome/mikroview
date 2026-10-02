// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// HasLocalAdmin is "SSO is additive; keep a local admin" (#1252) as a
// predicate. It has to answer from HasLocalPassword rather than from the
// stored hash, which is deliberately indistinguishable between a real
// password and the unmatchable filler an SSO account carries.
func TestHasLocalAdminFollowsTheAdminsPassword(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}

	if s.HasLocalAdmin() {
		t.Error("an empty store claims a local admin")
	}

	admin, err := s.Register("alice", "password123", time.Now())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if !s.HasLocalAdmin() {
		t.Fatal("a freshly registered admin is not counted as the local way in")
	}

	// A second account with a password is not the break-glass account:
	// mikroview holds one admin, and a user cannot reach the admin-gated
	// screens an operator needs to get back in.
	if _, err := s.CreateUser("bob", "password456", RoleUser, time.Now()); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Linking used to end it. Under #1252's ruling the admin keeps its
	// password, so connecting SSO adds a way in rather than swapping
	// one -- and the deployment still has a way in that the identity
	// provider cannot take away.
	if err := s.LinkOIDCIdentity(admin.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}
	if !s.HasLocalAdmin() {
		t.Error("linking the admin removed the local way in that #1252 exists to keep")
	}
}

// An SSO-provisioned first account is an admin with no password, which
// is exactly the state #1252 exists to keep a deployment out of. SSO no
// longer provisions the first account at all (#1415), but a deployment
// that let it before then still holds such an admin, so the document is
// written the way that older release left it. main.go says so at every
// start while it holds.
func TestHasLocalAdminIsFalseForAnSSOProvisionedAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	unmatchable, err := unmatchablePasswordHash()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(storeFile{Users: []*User{{
		ID: newID(), Username: "carol", PasswordHash: unmatchable, Role: RoleAdmin,
		CreatedAt: time.Now(), OIDCIssuer: "https://idp.example", OIDCSubject: "subject-1",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	if u := s.Admin(); u == nil || u.Username != "carol" {
		t.Fatalf("Admin() = %+v, want carol -- this test is not set up as it thinks", u)
	}
	if s.HasLocalAdmin() {
		t.Error("an SSO-provisioned admin counts as a local way in")
	}
}
