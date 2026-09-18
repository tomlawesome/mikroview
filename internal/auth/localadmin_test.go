// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
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

	if err := s.LinkOIDCIdentity(admin.ID, "https://idp.example", "subject-1", time.Now()); err != nil {
		t.Fatalf("LinkOIDCIdentity: %v", err)
	}
	if s.HasLocalAdmin() {
		t.Error("the admin linked to SSO still counts as a local way in, so nothing would ever warn")
	}
}

// An SSO-provisioned first account is an admin with no password, which
// is exactly the state #1252 exists to keep a deployment out of.
func TestHasLocalAdminIsFalseForAnSSOProvisionedAdmin(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}

	u, _, err := s.FindOrCreateOIDCUser("https://idp.example", "subject-1", "carol", time.Now())
	if err != nil {
		t.Fatalf("FindOrCreateOIDCUser: %v", err)
	}
	if u.Role != RoleAdmin {
		t.Fatalf("Role = %q, want admin -- this test is not set up as it thinks", u.Role)
	}
	if s.HasLocalAdmin() {
		t.Error("an SSO-provisioned admin counts as a local way in")
	}
}
