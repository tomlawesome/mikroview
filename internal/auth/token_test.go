package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOpenTokenStoreEmptyPathIsUsableButNotPersisted(t *testing.T) {
	s, err := OpenTokenStore("")
	if err != nil {
		t.Fatalf("OpenTokenStore(\"\"): %v", err)
	}
	if s.Persisted() {
		t.Error("expected an empty path to leave the store unpersisted")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(s.List()))
	}
}

func TestTokenCreateRefusesWhenNotPersisted(t *testing.T) {
	s, _ := OpenTokenStore("")
	if _, _, err := s.Create("birdcage", time.Now()); err != ErrTokenNotPersisted {
		t.Errorf("expected ErrTokenNotPersisted, got %v", err)
	}
}

func TestTokenCreateAndAuthenticate(t *testing.T) {
	s, err := OpenTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	raw, tok, err := s.Create("birdcage", now)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tok.Name != "birdcage" {
		t.Errorf("Name = %q, want %q", tok.Name, "birdcage")
	}
	if tok.HashedValue == "" {
		t.Error("expected a non-empty HashedValue")
	}
	if raw == "" || raw == tok.HashedValue {
		t.Error("expected a distinct, non-empty raw value")
	}

	got, ok := s.Authenticate(raw, now.Add(time.Minute))
	if !ok {
		t.Fatal("expected the freshly created token's raw value to authenticate")
	}
	if got.ID != tok.ID {
		t.Errorf("ID = %q, want %q", got.ID, tok.ID)
	}
	if got.LastUsedAt.IsZero() {
		t.Error("expected LastUsedAt to be recorded on successful authentication")
	}
}

// TestTokenRawValueNeverStored is the load-bearing property behind
// storing a SHA-256 hash rather than the raw bearer value: the raw
// value itself must never validate as a lookup key, and must never
// appear verbatim as HashedValue.
func TestTokenRawValueNeverStored(t *testing.T) {
	s, err := OpenTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, tok, err := s.Create("birdcage", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if tok.HashedValue == raw {
		t.Fatal("expected HashedValue to differ from the raw token value")
	}
	for _, listed := range s.List() {
		if listed.HashedValue != "" {
			t.Errorf("expected List() to never expose HashedValue, got %q", listed.HashedValue)
		}
	}
}

func TestTokenAuthenticateRejectsUnknownValue(t *testing.T) {
	s, err := OpenTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Authenticate("not-a-real-token", time.Now()); ok {
		t.Error("expected an unknown token value to fail authentication")
	}
	if _, ok := s.Authenticate("", time.Now()); ok {
		t.Error("expected an empty token value to fail authentication")
	}
}

func TestTokenRevoke(t *testing.T) {
	s, err := OpenTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw, tok, err := s.Create("birdcage", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Revoke(tok.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, ok := s.Authenticate(raw, time.Now()); ok {
		t.Error("expected a revoked token to fail authentication")
	}
	if len(s.List()) != 0 {
		t.Errorf("expected the token list to be empty after revoking the only token, got %d", len(s.List()))
	}
}

func TestTokenRevokeUnknownIDReturnsErrTokenNotFound(t *testing.T) {
	s, err := OpenTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke("does-not-exist"); err != ErrTokenNotFound {
		t.Errorf("expected ErrTokenNotFound, got %v", err)
	}
}

// TestTokenSurvivesReopen is the property that actually distinguishes
// Token from Session: it must still authenticate after the store is
// closed and reopened from the same path, simulating a process restart.
func TestTokenSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	s1, err := OpenTokenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, tok, err := s1.Create("birdcage", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	s2, err := OpenTokenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s2.Authenticate(raw, time.Now())
	if !ok {
		t.Fatal("expected the token to still authenticate after reopening the store")
	}
	if got.ID != tok.ID {
		t.Errorf("ID = %q, want %q", got.ID, tok.ID)
	}
}

func TestTokenListNeverIncludesRevokedTokens(t *testing.T) {
	s, err := OpenTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, keep, err := s.Create("keep-me", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_, drop, err := s.Create("revoke-me", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(drop.ID); err != nil {
		t.Fatal(err)
	}

	list := s.List()
	if len(list) != 1 || list[0].ID != keep.ID {
		t.Errorf("expected only %q to remain listed, got %+v", keep.Name, list)
	}
}
