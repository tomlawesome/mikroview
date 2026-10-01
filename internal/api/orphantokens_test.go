// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/device"
	"github.com/tomlawesome/mikroview/internal/persist"
)

// newOrphanSweepFixtures builds the three stores RevokeOrphanedIngestTokens
// reads and writes: a memory-only device registry (Create/Delete need no
// persistence to behave correctly, same as the rest of internal/device's
// tests), a token store backed by a scratch-dir file (TokenStore.Create
// refuses to mint anything unpersisted -- see Persisted -- so this needs a
// real, if throwaway, backend the same way internal/auth's own
// eachAuthBackend "file" case does), and a memory-only audit log.
func newOrphanSweepFixtures(t *testing.T) (*device.Registry, *auth.TokenStore, *audit.Store) {
	t.Helper()
	devices := device.NewRegistry(nil)
	tokens, err := auth.OpenTokenStoreWithBackend(persist.NewFileBackend(filepath.Join(t.TempDir(), "tokens.json")))
	if err != nil {
		t.Fatalf("OpenTokenStoreWithBackend: %v", err)
	}
	auditStore, err := audit.OpenWithBackend(nil)
	if err != nil {
		t.Fatalf("audit.OpenWithBackend: %v", err)
	}
	return devices, tokens, auditStore
}

// mustCreateIngestToken mints an ingest token for device, backdated (or
// postdated) to createdAt -- Create takes the timestamp as a parameter,
// so tests can place a token's CreatedAt on either side of an audit
// entry stamped with the real clock (audit.Store.Record has no such
// parameter -- see TestRevokeOrphanedIngestTokensSkipsATokenCreatedAfterRemoval
// for why that is what drives the ordering in these tests instead).
func mustCreateIngestToken(t *testing.T, tokens *auth.TokenStore, deviceID string, createdAt time.Time) *auth.Token {
	t.Helper()
	_, tok, err := tokens.Create("push token", auth.TokenKindIngest, deviceID, nil, createdAt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return tok
}

// tokenStillExists reports whether id is still a live token in tokens.
func tokenStillExists(tokens *auth.TokenStore, id string) bool {
	for _, tok := range tokens.List() {
		if tok.ID == id {
			return true
		}
	}
	return false
}

// TestRevokeOrphanedIngestTokensRevokesAnOrphan is the positive case: a
// router removed (device.removed, #1385's own action string) after its
// ingest token was minted, and never re-added -- exactly what #1385
// would have revoked on the spot had it existed at deletion time.
func TestRevokeOrphanedIngestTokensRevokesAnOrphan(t *testing.T) {
	devices, tokens, auditStore := newOrphanSweepFixtures(t)
	tok := mustCreateIngestToken(t, tokens, "router-1", time.Now().Add(-time.Hour))
	auditStore.Record("admin", "device.removed", "router-1", "its token, its enrolment and its record")

	RevokeOrphanedIngestTokens(devices, tokens, auditStore)

	if tokenStillExists(tokens, tok.ID) {
		t.Fatalf("token %s should have been revoked", tok.ID)
	}
	res := auditStore.Query(audit.Query{})
	found := false
	for _, e := range res.Entries {
		if e.Action == "token.revoke" && e.Target == tok.ID && e.Actor == "system" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a system token.revoke audit entry for %s, got %+v", tok.ID, res.Entries)
	}
}

// TestRevokeOrphanedIngestTokensSkipsWithNoRemovalEntry covers (a): the
// device is gone from the registry, but nothing in the audit log ever
// recorded removing it -- e.g. it was never created there in the first
// place (a Settings-minted token for a router that has not pushed yet
// has no registry entry either, and must not be swept).
func TestRevokeOrphanedIngestTokensSkipsWithNoRemovalEntry(t *testing.T) {
	devices, tokens, auditStore := newOrphanSweepFixtures(t)
	tok := mustCreateIngestToken(t, tokens, "router-1", time.Now().Add(-time.Hour))

	RevokeOrphanedIngestTokens(devices, tokens, auditStore)

	if !tokenStillExists(tokens, tok.ID) {
		t.Fatalf("token %s should not have been revoked: no device.removed entry exists", tok.ID)
	}
}

// TestRevokeOrphanedIngestTokensSkipsATokenCreatedAfterRemoval covers
// (b): the removal predates the token, so this token was minted for a
// router that did not exist yet at removal time -- not one the removal
// orphaned.
func TestRevokeOrphanedIngestTokensSkipsATokenCreatedAfterRemoval(t *testing.T) {
	devices, tokens, auditStore := newOrphanSweepFixtures(t)
	auditStore.Record("admin", "device.removed", "router-1", "")
	tok := mustCreateIngestToken(t, tokens, "router-1", time.Now().Add(time.Hour))

	RevokeOrphanedIngestTokens(devices, tokens, auditStore)

	if !tokenStillExists(tokens, tok.ID) {
		t.Fatalf("token %s should not have been revoked: it was created after the removal", tok.ID)
	}
}

// TestRevokeOrphanedIngestTokensSkipsADeviceBackInTheRegistry covers
// (c): the router was removed and then came back (redeclared, or pushed
// again and re-Ensure'd) -- its current token is live again, not an
// orphan.
func TestRevokeOrphanedIngestTokensSkipsADeviceBackInTheRegistry(t *testing.T) {
	devices, tokens, auditStore := newOrphanSweepFixtures(t)
	tok := mustCreateIngestToken(t, tokens, "router-1", time.Now().Add(-time.Hour))
	auditStore.Record("admin", "device.removed", "router-1", "")
	if _, err := devices.Create("router-1", "", time.Now()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	RevokeOrphanedIngestTokens(devices, tokens, auditStore)

	if !tokenStillExists(tokens, tok.ID) {
		t.Fatalf("token %s should not have been revoked: router-1 is back in the registry", tok.ID)
	}
}

// TestIsRevocationCandidateSkipsANonIngestToken covers (d): only ingest
// tokens are ever scoped to a device, so no other kind is ever a
// candidate. auth.TokenStore.Create refuses to mint a non-ingest token
// that carries a Device at all (ErrTokenDeviceNotAllowed), so this
// exercises isRevocationCandidate directly against a hand-built Token --
// the only way to ask the question, and also the defensive case: an old
// store file predating that rule must not be trusted to have obeyed it.
func TestIsRevocationCandidateSkipsANonIngestToken(t *testing.T) {
	devices, _, _ := newOrphanSweepFixtures(t)
	tok := auth.Token{ID: "t1", Kind: auth.TokenKindAPI, Device: "router-1", CreatedAt: time.Now()}

	if isRevocationCandidate(tok, devices) {
		t.Fatalf("a non-ingest token must never be a revocation candidate: %+v", tok)
	}
}

// TestIsRevocationCandidateSkipsATokenWithNoDevice covers (e): an ingest
// token with no Device could not exist through Create (which requires
// one), but the sweep must not assume that and must never treat an empty
// Device as "matches every removal." Same hand-built-Token approach as
// the non-ingest case above, for the same reason.
func TestIsRevocationCandidateSkipsATokenWithNoDevice(t *testing.T) {
	devices, _, _ := newOrphanSweepFixtures(t)
	tok := auth.Token{ID: "t1", Kind: auth.TokenKindIngest, Device: "", CreatedAt: time.Now()}

	if isRevocationCandidate(tok, devices) {
		t.Fatalf("an ingest token with no device must never be a revocation candidate: %+v", tok)
	}
}
