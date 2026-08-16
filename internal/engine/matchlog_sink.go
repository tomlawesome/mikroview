// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"errors"
	"fmt"

	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/matchlog"
)

// The two gates below throttle the per-match failure lines, moved here
// with the code that emits them when internal/watchlist.Evaluator was
// deleted (#406) -- same reasoning that package gave (#322 item 3):
// once the match log hits capacity, *every* subsequent match fails --
// its own doc comment says so -- which at event rate for a busy
// watchlisted host is a WARN flood, not information. Two gates rather
// than one, so a capacity-reached steady state cannot drown out a
// genuinely unexpected failure.
var (
	matchLogFullGate = logging.NewLimiter(dropLogInterval)
	matchFailGate    = logging.NewLimiter(dropLogInterval)
)

// MatchlogSink returns an OnRoutedEmission callback (see
// DeclarativeDefinition.OnRoutedEmission, declarative.go) that records an
// expectation-intent RoutedEmission to ml -- the matchlog counterpart of
// FlagsSink (flags_sink.go), and the wiring every expectation-intent
// definition registered on the engine gets in main.go.
//
// A nil ml is a safe no-op, the same convention FlagsSink follows: the
// match log genuinely can fail to open at startup (see main.go), and a
// deployment in that state should evaluate expectations harmlessly
// rather than panic on every match. A detection-intent RoutedEmission
// (r.Expectation == nil) is silently ignored -- this sink only ever
// handles the expectation half.
//
// An emission whose Tuple has neither a source MAC nor a source IP is
// dropped before it reaches Append. matchlog refuses such a record
// outright (ErrEmptyIdentity) because there would be nothing to attribute
// it to, and internal/watchlist's own matchNonInverted returned NoMatch
// for exactly this case rather than producing a record nothing could be
// keyed under -- dropping it here preserves that, and keeps the failure
// out of the rate-limited warning path where it would look like a real
// problem.
func MatchlogSink(ml matchlog.Store) func(RoutedEmission) {
	return func(r RoutedEmission) { recordExpectationMatch(ml, r) }
}

// recordExpectationMatch is MatchlogSink's callback body. Reports
// whether a record was actually written, for a caller (or a test) that
// needs to know an emission reached the log rather than being dropped.
func recordExpectationMatch(ml matchlog.Store, r RoutedEmission) bool {
	if ml == nil || r.Expectation == nil {
		return false
	}
	w := r.Expectation
	if w.Tuple.Source.Empty() {
		return false
	}
	err := ml.AppendProvisional(w.EntryID, w.Tuple, w.Event, r.EventTime, w.Provisional)
	if err == nil {
		return true
	}
	// ErrCapacityReached is the expected, already-documented steady state
	// once a deployment's match log fills (#243 section 3: refused, not
	// silently overwritten) -- surfaced rather than swallowed, since from
	// this point on every further genuinely-new match for this
	// expectation is silently lost until the operator acts. Which also
	// means it repeats at event rate, so it is gated, with the running
	// count carrying what the gate suppressed.
	if errors.Is(err, matchlog.ErrCapacityReached) {
		if total, ok := matchLogFullGate.Allow(); ok {
			logger.Warn(fmt.Sprintf("the match log is full -- new matches are no longer being recorded (%d lost so far, latest for entry %q); raise watchlist.matchLogCapacity or clear old matches", total, w.EntryID))
		}
	} else if total, ok := matchFailGate.Allow(); ok {
		logger.Warn(fmt.Sprintf("recording a match for entry %q failed: %v (%d match-recording failures so far)", w.EntryID, err, total))
	}
	return false
}
