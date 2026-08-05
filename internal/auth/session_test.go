package auth

import (
	"testing"
	"time"
)

func TestSessionCreateAndValidate(t *testing.T) {
	s := NewSessionStore(time.Hour)
	now := time.Now()
	sess := s.Create("user-1", now)

	got, ok := s.Validate(sess.ID, now.Add(time.Minute))
	if !ok {
		t.Fatal("expected the freshly created session to validate")
	}
	if got.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user-1")
	}
}

func TestSessionValidateRejectsUnknownID(t *testing.T) {
	s := NewSessionStore(time.Hour)
	if _, ok := s.Validate("does-not-exist", time.Now()); ok {
		t.Error("expected an unknown session ID to fail validation")
	}
}

func TestSessionExpires(t *testing.T) {
	s := NewSessionStore(time.Minute)
	now := time.Now()
	sess := s.Create("user-1", now)

	if _, ok := s.Validate(sess.ID, now.Add(2*time.Minute)); ok {
		t.Error("expected the session to have expired")
	}
	// The expired lookup should have evicted it -- confirm it's really gone.
	if _, ok := s.Validate(sess.ID, now); ok {
		t.Error("expected the expired session to be evicted, not just reported expired")
	}
}

func TestSessionSlidingExpirationExtendsOnUse(t *testing.T) {
	s := NewSessionStore(time.Minute)
	now := time.Now()
	sess := s.Create("user-1", now)

	// Use it right before it would have expired -- should extend, not expire.
	if _, ok := s.Validate(sess.ID, now.Add(50*time.Second)); !ok {
		t.Fatal("expected the session to still be valid just before its original expiry")
	}
	// Now well past the *original* expiry, but within a fresh TTL from the last use.
	if _, ok := s.Validate(sess.ID, now.Add(90*time.Second)); !ok {
		t.Error("expected sliding expiration to have extended the session past its original TTL")
	}
}

func TestSessionRevoke(t *testing.T) {
	s := NewSessionStore(time.Hour)
	now := time.Now()
	sess := s.Create("user-1", now)

	s.Revoke(sess.ID)
	if _, ok := s.Validate(sess.ID, now); ok {
		t.Error("expected a revoked session to fail validation")
	}
}

func TestSessionRevokeAllForUser(t *testing.T) {
	s := NewSessionStore(time.Hour)
	now := time.Now()
	a1 := s.Create("user-1", now)
	a2 := s.Create("user-1", now)
	b1 := s.Create("user-2", now)

	s.RevokeAllForUser("user-1")

	if _, ok := s.Validate(a1.ID, now); ok {
		t.Error("expected user-1's first session to be revoked")
	}
	if _, ok := s.Validate(a2.ID, now); ok {
		t.Error("expected user-1's second session to be revoked")
	}
	if _, ok := s.Validate(b1.ID, now); !ok {
		t.Error("expected user-2's session to be unaffected")
	}
}
