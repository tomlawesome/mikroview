// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"net"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/reputation"
)

var reputationSinkLogger = logging.New("engine-reputation")

// ReputationLookup is the subset of *reputation.Client's API this sink
// depends on -- mirrors internal/detect.reputationLookup (reputation.go)
// exactly, for the same reason that package keeps its own small
// interface rather than depending on the concrete type: tests can inject
// a fake without making real network calls.
type ReputationLookup interface {
	Lookup(ctx context.Context, ip string) (reputation.Result, error)
}

// reputationLookupTimeout mirrors internal/detect.reputationLookupTimeout
// -- generous headroom above reputation.Client's own internal 5s HTTP
// timeout, belt-and-braces against a leaked/hung context rather than the
// primary bound.
const reputationLookupTimeout = 10 * time.Second

// ReputationSink is FlagsSink plus a best-effort, async, pool-bounded
// reputation lookup against a newly-raised (never a re-fire) episode's
// Target -- the engine-side counterpart to internal/detect's
// maybeCheckReputation (reputation.go), for a declarative definition
// whose Target is a single source IP (port_scan, critical_port, ...).
// Ported alongside each detector's own #405 move so this doesn't
// silently regress detector by detector -- the old detect.Detector had
// this wired for every single-IP-keyed detector via WithReputation.
//
// client == nil is a valid, explicit "not configured" no-op -- returns
// plain FlagsSink(fs) rather than a lookup path that would always skip
// anyway, the same "nil is inert" convention every optional dependency in
// this codebase follows (internal/detect.Detector's own
// reputation/entities/knownBad/netclass fields).
//
// concurrency bounds in-flight lookups -- a *separate* pool from
// internal/detect's own reputationLookupConcurrency-sized one, not
// shared, since the two subsystems still run side by side (feeding the
// same *reputation.Client, but budgeting their own concurrency
// independently) until every detector has moved off internal/detect.
// main.go is expected to pass the same concurrency figure
// internal/detect uses today, so the two pools' combined worst case
// stays within what this codebase has already tuned AbuseIPDB's free-tier
// quota against, until #405 finishes and internal/detect's own pool is
// deleted.
func ReputationSink(fs *flags.Store, client ReputationLookup, concurrency int) func(RoutedEmission) {
	if client == nil {
		return FlagsSink(fs)
	}
	slots := make(chan struct{}, concurrency)
	return func(r RoutedEmission) {
		isNew := raiseDetectionFlag(fs, r)
		if !isNew || r.Detection == nil {
			return
		}
		f := r.Detection
		// r.SourceIP where the definition supplied one, falling back to
		// the Target: the two coincide for a source-keyed definition
		// (port_scan, critical_port), and differ for one whose Target is a
		// composite (repeated_drops' "<source> -> port <N>"). This is the
		// same (target, ip) split internal/detect.maybeCheckReputation
		// took as two parameters -- the flag is keyed on the target, the
		// lookup is about the address.
		ip := r.SourceIP
		if ip == "" {
			ip = f.Target
		}
		if !isPublicIPAddress(ip) {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			return // pool saturated -- skip this episode's lookup, same as detect.maybeCheckReputation
		}
		go func() {
			defer func() { <-slots }()
			defer logging.Recover(reputationSinkLogger)
			ctx, cancel := context.WithTimeout(context.Background(), reputationLookupTimeout)
			defer cancel()
			result, err := client.Lookup(ctx, ip)
			if err != nil {
				return
			}
			// Stored even without an AbuseScore -- see
			// internal/detect.maybeCheckReputation's identical comment:
			// a Shodan-only result is still worth capturing as a
			// snapshot, and flags.Store.ApplyReputationSnapshot only
			// raises the confidence floor when a score is actually
			// present.
			fs.ApplyReputationSnapshot(f.Type, f.Target, result)
		}()
	}
}

// isPublicIPAddress mirrors internal/detect.isPublic (and this package's
// own isPublicIP, which takes a net.IP rather than a string) -- a
// definition's Target is not always a parseable IP (KeyGlobal's "global",
// a rule label for a future rule-keyed definition), so an unparseable
// value is treated as "not a lookup candidate," not an error.
func isPublicIPAddress(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	return isPublicIP(ip)
}
