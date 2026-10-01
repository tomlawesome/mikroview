// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/audit"
	"github.com/tomlawesome/mikroview/internal/auth"
	"github.com/tomlawesome/mikroview/internal/device"
)

// RevokeOrphanedIngestTokens is a one-time startup sweep (#1399) for
// ingest tokens minted for a router that was removed the old way --
// before #1385 taught handleDeviceDelete (internal/api/devices.go) to
// revoke a device's tokens as part of the same delete. Any token that
// delete would have revoked on the spot, had #1385 existed yet, is
// revoked here instead, on the next restart.
//
// Deliberately narrower than "revoke every ingest token whose device
// isn't in the registry": a Settings-minted token for a router that has
// not pushed yet also has no registry entry -- device.Registry.Ensure
// only creates one on the first accepted line -- and that is not
// orphaned, just early. Requiring a matching "device.removed" audit
// entry, timestamped after the token's own creation, is what tells the
// two apart.
//
// Called once from main.go, after devices, tokens and auditStore have
// all opened. A revoke that fails to save is logged and left for the
// next restart to retry, not fatal -- see the per-token loop below.
func RevokeOrphanedIngestTokens(devices *device.Registry, tokens *auth.TokenStore, auditStore *audit.Store) {
	if devices == nil || tokens == nil || auditStore == nil {
		return
	}

	var candidates []auth.Token
	for _, tok := range tokens.List() {
		if isRevocationCandidate(tok, devices) {
			candidates = append(candidates, tok)
		}
	}
	if len(candidates) == 0 {
		return
	}

	removedAt := deviceRemovalTimestamps(auditStore)
	revoked, failed := 0, 0
	for _, tok := range candidates {
		if !removedAfter(removedAt[tok.Device], tok.CreatedAt) {
			continue
		}
		if err := tokens.Revoke(tok.ID); err != nil {
			failed++
			apiLog.Error(fmt.Sprintf("revoking orphaned ingest token %s for removed router %s failed: %v -- will retry on the next restart", tok.ID, tok.Device, err))
			continue
		}
		auditStore.Record("system", "token.revoke", tok.ID, fmt.Sprintf("its router %s was removed before removal revoked tokens (#1385)", tok.Device))
		revoked++
	}
	if revoked > 0 || failed > 0 {
		msg := fmt.Sprintf("orphaned ingest token sweep (#1399): revoked %d token(s) left behind by a router removed before #1385", revoked)
		if failed > 0 {
			msg += fmt.Sprintf("; %d failed to save and will be retried on the next restart", failed)
		}
		apiLog.Info(msg)
	}
}

// isRevocationCandidate is condition (1) of the sweep -- an ingest token
// scoped to a device the registry does not currently have -- as a plain
// function of a Token value rather than a TokenStore method, so it is
// testable against a hand-built Token directly, including combinations
// TokenStore.Create itself refuses to mint (a non-ingest token carrying
// a Device, or an ingest token carrying none). An old store file written
// before that rule existed is not assumed to have obeyed it.
func isRevocationCandidate(tok auth.Token, devices *device.Registry) bool {
	return tok.Kind == auth.TokenKindIngest && tok.Device != "" && !devices.Has(tok.Device)
}

// removedAfter reports whether any timestamp in removals falls after
// since -- condition (3) of the sweep, "revoke only if the removal
// happened after this token was created."
func removedAfter(removals []time.Time, since time.Time) bool {
	for _, t := range removals {
		if t.After(since) {
			return true
		}
	}
	return false
}

// deviceRemovalTimestamps walks the entire audit log and returns every
// "device.removed" entry's timestamp, keyed by the device id it targeted
// (handleDeviceDelete's own action string -- internal/api/devices.go).
//
// audit.Query caps a single call at its own maxLimit, so this pages
// backward by Until, using the oldest entry already seen (minus a
// nanosecond) as the next page's upper bound, until a page reports no
// more. Two entries landing at the exact same nanosecond, straddling
// that boundary, could in principle be missed -- the same timestamp-tie
// risk audit.Entry's own ID field doc comment already accepts for this
// store, and not one worth inventing an ID-based cursor audit.Query does
// not expose.
func deviceRemovalTimestamps(auditStore *audit.Store) map[string][]time.Time {
	out := make(map[string][]time.Time)
	var until time.Time
	for {
		// Limit is clamped internally to audit.Query's own per-call
		// ceiling; asking for far more than that just means "give me a
		// full page."
		res := auditStore.Query(audit.Query{Until: until, Limit: 1 << 30})
		if len(res.Entries) == 0 {
			return out
		}
		for _, e := range res.Entries {
			if e.Action == "device.removed" {
				out[e.Target] = append(out[e.Target], e.Timestamp)
			}
		}
		if !res.HasMore {
			return out
		}
		until = res.Entries[0].Timestamp.Add(-time.Nanosecond)
	}
}
