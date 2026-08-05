package auth

import (
	"sync"
	"time"
)

// Session is deliberately an opaque random ID (see newID), not a JWT --
// easy to revoke (delete it server-side) and needs no signing-key
// management. Sessions are in-memory only, unlike accounts themselves:
// losing them on restart just means re-login, not the lockout/silent-
// reopen risk losing an account would carry.
type Session struct {
	ID     string
	UserID string
	// IssuedAt is set once at Create and never changed by Validate's
	// sliding-expiration renewal -- it answers "when did this specific
	// login happen," used to invalidate a session issued before a
	// password reset (see User.PasswordChangedAt), which ExpiresAt alone
	// can't express.
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// SessionStore holds active sessions, mutex-protected like every other
// piece of mikroview's runtime state.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session
	ttl      time.Duration
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{sessions: make(map[string]Session), ttl: ttl}
}

// Create starts a new session for userID.
func (s *SessionStore) Create(userID string, now time.Time) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := Session{ID: newID(), UserID: userID, IssuedAt: now, ExpiresAt: now.Add(s.ttl)}
	s.sessions[sess.ID] = sess
	return sess
}

// Validate reports whether id is a live session, extending its expiry
// on success (sliding expiration -- stays alive while actively used,
// rather than forcing a re-login mid-session at a fixed wall-clock
// time). An expired session is evicted on the read that finds it,
// rather than needing a separate sweep.
func (s *SessionStore) Validate(id string, now time.Time) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	if now.After(sess.ExpiresAt) {
		delete(s.sessions, id)
		return Session{}, false
	}
	sess.ExpiresAt = now.Add(s.ttl)
	s.sessions[id] = sess
	return sess, true
}

// Revoke ends one session (logout).
func (s *SessionStore) Revoke(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// RevokeAllForUser ends every session belonging to userID -- used when
// a password is reset (via the CLI recovery tool), so a stolen session
// doesn't survive a deliberate credential reset.
func (s *SessionStore) RevokeAllForUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if sess.UserID == userID {
			delete(s.sessions, id)
		}
	}
}
