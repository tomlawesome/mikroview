// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPreUpgradeAccountsAreNotLockedOut is the highest-risk case in
// issue #133.
//
// Every accounts.json in the wild predates HasLocalPassword. If a
// missing field read as false, every existing local admin would be
// marked SSO-only -- and the admin-reset and -recover-admin-account
// paths both refuse for such an account, by design. The operator would
// be locked out of their own recovery by an upgrade, with no error and
// nothing obviously wrong.
//
// The fixture below is a real pre-upgrade file shape: no
// hasLocalPassword key anywhere.
func TestPreUpgradeAccountsAreNotLockedOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")
	const preUpgrade = `{
	  "users": [
	    {
	      "id": "u1",
	      "username": "admin",
	      "passwordHash": "argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0c2E$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhcw",
	      "role": "admin",
	      "createdAt": "2026-01-01T00:00:00Z"
	    },
	    {
	      "id": "u2",
	      "username": "sso-user",
	      "passwordHash": "argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0c2E$aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhcw",
	      "role": "user",
	      "createdAt": "2026-01-01T00:00:00Z",
	      "oidcIssuer": "https://idp.example.com",
	      "oidcSubject": "abc123"
	    }
	  ]
	}`
	if err := os.WriteFile(path, []byte(preUpgrade), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	admin, ok := s.ByUsername("admin")
	if !ok {
		t.Fatal("admin not found after load")
	}
	if !admin.LocalPassword() {
		t.Error("a pre-upgrade local admin was marked SSO-only -- this is the lockout")
	}

	sso, ok := s.ByUsername("sso-user")
	if !ok {
		t.Fatal("sso-user not found after load")
	}
	if sso.LocalPassword() {
		t.Error("a pre-upgrade SSO account was marked as having a local password, " +
			"which would let an admin set a real password on it")
	}
}

// The reload path re-parses the file independently of Open, so it needs
// the same migration. Missing one of two load paths is a bug shape this
// codebase has already been bitten by once.
func TestMigrationAppliesOnReloadToo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register("admin", "correct-horse-battery-staple", time.Now()); err != nil {
		t.Fatal(err)
	}

	// Rewrite the file without the field, as an older build would have,
	// and bump mtime so reloadIfStale picks it up.
	const rewritten = `{"users":[{"id":"u1","username":"admin","passwordHash":"x","role":"admin","createdAt":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	u, ok := s.ByUsername("admin") // a read path, which triggers reloadIfStale
	if !ok {
		t.Fatal("admin not found after reload")
	}
	if !u.LocalPassword() {
		t.Error("the reload path did not migrate -- an account loaded after startup " +
			"is treated as SSO-only and cannot be recovered")
	}
}

func TestNewAccountsRecordTheFieldExplicitly(t *testing.T) {
	// A real path: Register refuses on an unpersisted store, since an
	// account that can't survive a restart is a trap rather than a
	// convenience.
	s, err := Open(filepath.Join(t.TempDir(), "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := s.Register("admin", "correct-horse-battery-staple", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if local.HasLocalPassword == nil {
		t.Error("Register left the field unset, so it relies on inference rather than recording the fact")
	} else if !*local.HasLocalPassword {
		t.Error("Register recorded no local password for a password-created account")
	}

	sso, _, err := s.FindOrCreateOIDCUser("https://idp.example.com", "abc123", "ssouser", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if sso.HasLocalPassword == nil {
		t.Error("FindOrCreateOIDCUser left the field unset")
	} else if *sso.HasLocalPassword {
		t.Error("an SSO-provisioned account claims a local password -- its hash is random and unmatchable")
	}
}
