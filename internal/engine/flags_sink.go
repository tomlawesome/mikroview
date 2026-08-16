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
// Confidence: every declarative definition shipped so far
// (shipped_declarative.go) sets Emission.Confidence via
// overshootConfidence, so f.Confidence is never nil in practice today.
// If a future definition kind ever left it nil ("not scored"),
// flags.Store has no public method that accepts detail+evidence+country+
// provisional without a confidence value -- this sink defaults that case
// to 0 rather than failing to raise the flag at all, which is a real gap
// worth widening flags.Store's API to close if a real caller ever hits
// it, not something to silently paper over indefinitely.
func FlagsSink(fs *flags.Store) func(RoutedEmission) {
	return func(r RoutedEmission) {
		if fs == nil || r.Detection == nil {
			return
		}
		f := r.Detection
		confidence := 0
		if f.Confidence != nil {
			confidence = *f.Confidence
		}
		fs.AddProvisional(f.Type, f.Target, f.Detail, confidence, f.Evidence, f.Country, f.Provisional, r.EventTime)
	}
}
