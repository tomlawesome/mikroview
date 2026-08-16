// SPDX-License-Identifier: AGPL-3.0-only

package engine

import "github.com/tomlawesome/mikroview/internal/flags"

// FlagsSink returns an OnRoutedEmission callback (see
// DeclarativeDefinition.OnRoutedEmission, declarative.go) that raises or
// re-fires a detection-intent RoutedEmission against fs, via
// flags.Store.AddProvisional -- the wiring issue #405 needs for every
// detection-intent definition registered on the engine (main.go), and
// what this package's own shipped-declarative tests (see
// shipped_declarative_test.go) use to drive a real flags.Store the same
// way production does, rather than re-implementing the translation by
// hand at every call site.
//
// A nil fs is a safe no-op, matching every other "nil sink" convention
// in this codebase (e.g. internal/detect.Detector's reputation/entities/
// knownBad/netclass fields). An expectation-intent RoutedEmission
// (r.Detection == nil) is silently ignored -- this sink only ever
// handles the detection half; a matchlog counterpart is #406's job.
//
// Confidence is passed through as the optional value it is, nil
// included. Every declarative definition sets it via
// overshootConfidence, but a deterministic programmatic one
// (unexpected_mail_sender) genuinely does not score its emissions, and
// "not scored" must not arrive at the store as "scored zero" -- an
// analyst reads a 0 as a judgement, not as its absence. This sink used
// to default nil to 0 because flags.Store had no method accepting an
// optional confidence alongside evidence/country/provisional; #405's
// final block widened that API (flags.Store.AddEmission) rather than
// keep papering over it, which is exactly what that note said should
// happen once a real caller hit it.
func FlagsSink(fs *flags.Store) func(RoutedEmission) {
	return func(r RoutedEmission) { raiseDetectionFlag(fs, r) }
}

// raiseDetectionFlag is FlagsSink's callback body, factored out so
// ReputationSink (reputation_sink.go) can reuse it while also observing
// AddProvisional's isNew return -- a plain FlagsSink caller has no reason
// to see that value, but a reputation lookup must only ever start on a
// genuinely new episode (a re-fire must never re-trigger it), the same
// gate internal/detect.maybeCheckReputation's own isNewEpisode parameter
// enforces.
func raiseDetectionFlag(fs *flags.Store, r RoutedEmission) (isNew bool) {
	if fs == nil || r.Detection == nil {
		return false
	}
	f := r.Detection
	return fs.AddEmission(f.Type, f.Target, f.Detail, f.Confidence, f.Evidence, f.Country, f.Provisional, r.EventTime)
}

// FlagsConfidenceFloorRaiser adapts a *flags.Store to
// ConfidenceFloorRaiser (programmatic.go) -- the one place flags.Type's
// string identity is relied on, kept here rather than spread across the
// two reinforcement definitions that need it. A nil store is a safe
// no-op, same convention as FlagsSink.
func FlagsConfidenceFloorRaiser(fs *flags.Store) ConfidenceFloorRaiser {
	return flagsFloorRaiser{fs: fs}
}

type flagsFloorRaiser struct{ fs *flags.Store }

func (r flagsFloorRaiser) RaiseConfidenceFloor(t FlagType, target string, floor int) {
	if r.fs == nil {
		return
	}
	r.fs.RaiseConfidenceFloor(flags.Type(t), target, floor)
}
