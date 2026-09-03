// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("activity_spike", buildActivitySpikeDefinition)
}

// hostActivityMinSamples is a hard floor: a source with fewer prior
// observations than this can never raise a flag, however extreme its
// first few readings look. A baseline built from one or two samples is
// not a baseline -- there is nothing to have deviated from yet. Lifted
// unchanged from internal/detect.hostActivityMinSamples. Gates the
// fallback (global, single-EMA) baseline only -- see
// activityBucketMinDays for the hour bucket's own floor.
const hostActivityMinSamples = 5

// activityBucketMinDays is the hour bucket's own BaselineFloor.MinSamples
// (#420's redesign, see this file's top-level doc comment): a bucket
// counts one "sample" per distinct calendar day its hour has been folded
// on (see rollHourBucket), so this is a floor in *days*, not events --
// the same shape off_hours.OffHoursMinSampleDays declares (see
// baselineFloorFromParams's doc comment for the three shapes a
// BaselineFloor can take). Set to 1, deliberately lower than
// off_hours' own 14: off_hours asks "is this hour's activity unusual
// against a stable multi-week pattern," which needs real history to
// answer honestly, but activity_spike's bucket exists to answer a
// narrower question -- "does this host have ANY same-hour history to
// compare against, rather than borrowing the fallback EMA" -- and a
// single fully-observed prior day at this hour already answers that:
// it is a real reading of this specific host at this specific hour,
// strictly more informative than the fallback's cross-hour average, even
// before a second day exists to corroborate it. This is exactly what the
// backup-shape scenario (design item, #420) needs: night one is
// necessarily immature (zero prior days), so it fires against the
// fallback: night two already has one full prior day folded in, so it
// judges against the bucket -- see TestShippedActivitySpike_BucketLearnsAcrossNights.
const activityBucketMinDays = 1

// activitySpikeDefinition is activity_spike ported onto the chassis
// (issue #405) and then redesigned for #420 (owner decision recorded on
// that issue, 2026-08-22).
//
// # The #420 defect, and why a per-event EMA cannot fix it
//
// The original port (and internal/detect before it) folded every
// reading straight into one EMA baseline per source, on every event.
// That baseline can never lag the observed rate by more than
// 1/emaAlpha = 50 events (see baseline.go's emaAlpha), so by the time a
// ramp reaches the 200-event threshold the baseline has already caught
// up to within ~50 of it and the 3x multiplier condition is
// unreachable. UpdateCadence (baseline.go) was built to make "fold in
// per window instead of per event" expressible, but changing only the
// cadence does not fix this on its own: a coarser cadence still folds
// the spike's own elevated readings into the thing it is being measured
// against, just more slowly.
//
// # The redesign
//
// Two changes together close #420:
//
//  1. Freeze/thaw (design item 4): once a reading's z-score against
//     whichever baseline is currently being consulted clears emaMinZ,
//     this source stops folding readings into either baseline entirely
//     until the rate drops back under the absolute threshold (a
//     deliberately coarser, different bound than entry -- avoiding
//     boundary chatter right at the line) or a backstop forces one
//     fold-in after a long plateau (see activitySpikeFreezeBackstop).
//     This alone fixes "day one": a source with no relevant history yet
//     is judged against the fallback EMA (exactly today's baseline,
//     unchanged), but that EMA can no longer be dragged up by the very
//     spike it is meant to catch.
//  2. Per-source, per-hour buckets (design item 1): 24 EMA baselines per
//     source, one per clock hour, each folded once per calendar day (see
//     rollHourBucket) rather than per event -- off_hours' own shape
//     (shipped_off_hours.go), reused because the underlying problem is
//     the same one off_hours already solved: a statistic that needs to
//     accumulate across many days, not smooth itself out within one.
//     Once a bucket has at least one fully-observed prior day
//     (activityBucketMinDays), it replaces the fallback as "the
//     applicable baseline" for its hour, so a host with a genuine
//     recurring pattern at some hour is judged against that pattern
//     rather than its own cross-hour average.
//
// See checkBaseline for where these combine into one firing decision,
// and this package's baselineSet/Baseline for the machinery reused
// unchanged from both directions.
type activitySpikeDefinition struct {
	programmaticBase

	window        time.Duration
	threshold     int
	multiplier    float64
	warmupSamples int
	vpnInterfaces []string
	vpnMultiplier float64

	counts    *Keyed[*CountRing]
	baselines *baselineSet // fallback: one global EMA per source (today's baseline, unchanged shape)
	buckets   *baselineSet // NEW: 24 per-source hour-of-day EMAs, folded once per calendar day
	sources   *Keyed[*activitySpikeSourceState]
}

// activitySpikeSourceState is one source's freeze/day bookkeeping -- the
// state that is not itself a Baseline. Mirrors offHoursDay
// (shipped_off_hours.go) in kind and in how it is carried: never handed
// to the StateStore, which holds baselines and nothing else, but no
// longer lost on restart either.
//
// It used to be lost. Issue #795 (owner, 2026-09-02) is the decision
// that revisited that: this state now survives a restart through the
// periodic snapshot, separate from the StateStore -- see this
// definition's ExportState/ImportState in shipped_export.go, and that
// file's doc comment for what a restored day is and is not allowed to
// mean.
type activitySpikeSourceState struct {
	// hourDay/hourPeak track, per hour-of-day, which calendar day is
	// currently accumulating for that hour and the peak windowed rate
	// (activitySpikeDefinition.window-sized) observed so far that day --
	// what rollHourBucket folds into the bucket's EMA once the day
	// advances. off_hours' offHoursDay tracks a running *count* because
	// its own reading is a raw per-day event tally; this tracks a peak
	// because activity_spike's reading is already itself a windowed
	// rate, and the peak windowed rate is what a recurring-activity
	// bucket should learn as "how busy this host gets at this hour."
	hourDay  [24]string
	hourPeak [24]int

	// frozen/frozenSince implement the entry/thaw/backstop state machine
	// (design item 4). While frozen, no baseline -- neither the
	// currently-applicable hour bucket nor the fallback EMA -- receives
	// a fold-in; see checkBaseline and rollHourBucket.
	frozen      bool
	frozenSince time.Time
	// peaked marks that this freeze episode has, at some point, actually
	// reached the absolute threshold -- gating primary thaw (see
	// checkBaseline). Entry fires purely off z-score, which -- for a
	// source ramping up from a quiet baseline -- clears emaMinZ long
	// before the rate itself clears the absolute threshold (this is the
	// whole point: it is what lets the baseline lock in early, before a
	// ramp like #420's own pinned scenario can drag it upward). Without
	// this gate, "rate back under the absolute threshold" would already
	// be true on nearly every reading between entry and the threshold
	// being reached, thawing (and folding) on almost every one of them
	// and reproducing #420's exact bug inside the freeze window itself.
	// Requiring the threshold to have been reached at least once first
	// means thaw only ever fires on a genuine *descent* from having been
	// a real, threshold-clearing candidate -- see
	// TestShippedActivitySpike_ContinuousRampFiresAtDefaultConfig.
	peaked bool

	// maturityDay/maturityStreak/dirtyToday implement the per-source
	// consecutive-clean-days counter (design item 2): maturityStreak is
	// how many consecutive calendar days ended with no candidate-spike
	// entry anywhere on this source, dirtyToday marks the day currently
	// accumulating as already having had one (so rollMaturityDay knows
	// to reset rather than extend when that day closes).
	maturityDay    string
	maturityStreak int
	dirtyToday     bool
}

// activitySpikeFreezeBackstop expresses the backstop duration (design
// item 4) as a window multiple, following baselineFloorFromParams'
// pattern of deriving a bound from the definition's own window rather
// than a new duration constant: "how long is too long to hold a freeze"
// is inherently relative to how fast this definition's own count ring
// can possibly change (bounded by window), the same reasoning that
// pattern's neighboring comments give for tying a floor to window rather
// than to wall-clock time directly. Five window-lengths is long enough
// that ordinary rate jitter around the threshold cannot repeatedly
// retrigger it (the primary thaw path handles that boundary already),
// short enough that a genuinely new, permanently elevated normal is
// reflected in the baseline within a handful of minutes at shipped
// defaults (window=60s => 5 minutes), not left frozen indefinitely.
const activitySpikeFreezeBackstop = 5

func buildActivitySpikeDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	window, err := paramDuration(params, "window")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	threshold, err := paramInt(params, "threshold")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	multiplier, err := paramFloat(params, "baselineMultiplier")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	warmup, err := paramIntOptional(params, "warmupSamples")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	ifaces, err := paramStringList(params, "vpnInterfaces")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	vpnMult, err := paramFloatOptional(params, "vpnConfidenceMultiplier")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	cadence, err := cadenceFromParams(params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	// primeWindow zero: this baseline (the fallback) was primed on the
	// very first reading before #420's redesign too, and the firing
	// floor is a sample count (hostActivityMinSamples), not a duration.
	// Deferring priming to a full window would change when this can
	// fire outside of a spike -- unrelated to #420 -- so it stays as it
	// was. See baselineSet.primeWindow.
	floor, err := baselineFloorFromParams(params, 0)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	floor.MinSamples = hostActivityMinSamples

	// The hour buckets are a structural property of this redesign, not
	// an operator-tunable dimension: their floor is always
	// activityBucketMinDays (see that constant's own doc comment), and
	// their cadence is always "once per day" -- UpdatePerWindow is the
	// closest of the two declarable UpdateCadence values (folding is
	// certainly not per-event), used here for a coarser-than-per-event
	// cadence that is not literally per-window either; see
	// rollHourBucket for the actual fold-in trigger. A separate defID
	// namespace ("#hourly") keeps this baselineSet's StateStore keys
	// (srcIP+"\x00"+hour) from ever needing to be distinguished from the
	// fallback baselineSet's (plain srcIP) by construction, not by
	// coincidence of key shape.
	bucketFloor := BaselineFloor{MinSamples: activityBucketMinDays}

	return &activitySpikeDefinition{
		programmaticBase: programmaticBase{def: def},
		window:           window,
		threshold:        threshold,
		multiplier:       multiplier,
		warmupSamples:    warmup,
		vpnInterfaces:    ifaces,
		vpnMultiplier:    vpnMult,
		counts:           NewKeyed[*CountRing](),
		baselines:        newBaselineSet(def.ID, 0, floor, cadence, deps.State),
		buckets:          newBaselineSet(def.ID+"#hourly", 0, bucketFloor, UpdatePerWindow, deps.State),
		sources:          NewKeyed[*activitySpikeSourceState](),
	}, nil
}

// activityBucketKey is the (source, hour-of-day) key the buckets
// baselineSet uses -- offHoursKey's own separator reasoning applies
// unchanged: a NUL byte can never collide with anything a source address
// can contain.
func activityBucketKey(srcIP string, hour int) string {
	return srcIP + "\x00" + fmt.Sprintf("%02d", hour)
}

// activityDay is the server-local calendar day a reading at now belongs
// to, for both the hour-bucket rollover and the maturity-streak rollover
// -- the server receive clock (now, which is always e.ReceivedAt/the
// caller-supplied evaluation time -- see checkBaseline), never a
// wall-clock read, so replay and live evaluation agree deterministically
// (TestShippedActivitySpikeIsReplayable's determinism requirement, and
// see off_hours' identical use of this format for the same reason).
func activityDay(now time.Time) string {
	return now.Format("2006-01-02")
}

// Evaluate satisfies Evaluated: one per-source rolling event count,
// judged against that source's own applicable baseline (checkBaseline).
//
// The connection-state filter is internal/detect's isTrackableConnState,
// and it is not incidental: RouterOS commonly logs both directions of an
// established connection on one stateful accept rule, so without it a
// busy server's ordinary return traffic trivially crosses a threshold
// meant to catch new activity (mikroview issue #35). An empty
// ConnState -- a setup that does not log connection state at all -- is
// treated as trackable rather than discarded, so those deployments keep
// today's behaviour.
func (d *activitySpikeDefinition) Evaluate(e store.Event) {
	if e.SrcIP == "" || !d.active(e) {
		return
	}
	if e.ConnState != "" && e.ConnState != "new" {
		return
	}

	now := e.ReceivedAt
	ring := d.counts.GetOrCreate(e.SrcIP, now, func() *CountRing { return NewCountRing(d.window) })
	ring.Add(now, true)
	d.checkBaseline(e.SrcIP, e.SrcCountry, e.InInterface, ring.Count(now, d.window), now)
}

// checkBaseline is the baseline half of Evaluate, split out at the seam
// internal/detect had (Observe computed the windowed count,
// checkHostActivityBaseline judged it) and every existing pinned test
// that drives it directly still does -- deliberately kept at the same
// signature through the #420 redesign so those pins (warm-up shape,
// VPN boost, scope, disabled-is-inert, ...) keep testing the same seam.
// The decision itself is activitySpikeCheck, shared verbatim with Replay
// (see that function's own doc comment for why, and Replay's for how) --
// this method is just that decision plus the live emit.
func (d *activitySpikeDefinition) checkBaseline(srcIP, country, iface string, count int, now time.Time) {
	fire, useBucket, hour, applicable, maturityStreak := activitySpikeCheck(
		d.baselines, d.buckets, d.sources, d.window, d.threshold, d.multiplier, srcIP, count, now)
	if !fire {
		return
	}
	if useBucket {
		d.emitBucketFiring(srcIP, country, iface, hour, count, maturityStreak, applicable, now)
		return
	}
	d.emitFallbackFiring(srcIP, country, iface, count, applicable, now)
}

// activitySpikeCheck is checkBaseline's entire #420 decision (design
// items 1-4), factored out to take baselines/buckets/sources and
// window/threshold/multiplier as explicit parameters rather than reading
// them off *activitySpikeDefinition -- so live evaluation (through d's
// own persisted baselines/buckets/sources) and Replay (through fresh,
// call-local, corpus-only equivalents -- see Replay's own doc comment)
// run exactly the same code, not merely equivalent code written twice.
// Reports whether to fire and everything a caller needs to report it;
// Detail/Confidence formatting stays each caller's own job, since live
// and Replay want different levels of detail (VPN boost, a real
// flags.Flag vs a ReplaySample).
//
// The decision, in order:
//
//  1. Roll the maturity streak and this hour's bucket forward if the
//     calendar day has advanced since either was last touched (rollMaturityDay,
//     rollHourBucket) -- both use activityDay(now), so a reading that is
//     itself the first of a new day is judged against state as it stood
//     at the end of the *previous* day.
//  2. Track this reading against the current hour's peak (for whatever
//     day rollHourBucket eventually folds in) and pick the applicable
//     baseline: the hour bucket if it has cleared activityBucketMinDays,
//     otherwise the fallback EMA -- design item 3's "mature bucket, else
//     warm-up fallback."
//  3. Run the freeze/thaw/backstop state machine (design item 4) against
//     that applicable baseline's z-score.
//  4. Fold this reading into the fallback EMA if not frozen and the
//     bucket isn't the applicable one (the bucket only ever folds via
//     rollHourBucket's once-a-day rollover, never live).
//  5. Fire using the *applicable* baseline exactly as it stood before
//     this call (Baseline.Reading's own "before" contract, preserved
//     here by construction: the applicable snapshot is always taken by
//     Snapshot/peek before any fold this call performs).
func activitySpikeCheck(
	baselines, buckets *baselineSet, sources *Keyed[*activitySpikeSourceState],
	window time.Duration, threshold int, multiplier float64,
	srcIP string, count int, now time.Time,
) (fire, useBucket bool, hour int, applicable Snapshot, maturityStreak int) {
	st := sources.GetOrCreate(srcIP, now, func() *activitySpikeSourceState { return &activitySpikeSourceState{} })
	hour = now.Hour()
	day := activityDay(now)

	rollMaturityDay(st, day)
	bucketKey := activityBucketKey(srcIP, hour)
	rollHourBucket(buckets, st, bucketKey, hour, day, now)

	rate := float64(count)
	if count > st.hourPeak[hour] {
		st.hourPeak[hour] = count
	}

	bucketSnap, bucketOK := buckets.snapshot(bucketKey, now)
	useBucket = bucketOK && bucketSnap.Ready

	if useBucket {
		applicable = bucketSnap
	} else {
		applicable, _ = baselines.snapshot(srcIP, now)
	}
	applicable.ZScore = emaZScore(rate, applicable.Value, applicable.Variance)

	// Freeze/thaw/backstop, driven off the applicable baseline computed
	// above -- see activitySpikeSourceState's own doc comment and
	// activitySpikeFreezeBackstop.
	justThawed := false
	if st.frozen {
		if rate >= float64(threshold) {
			st.peaked = true
		}
		switch {
		case st.peaked && rate < float64(threshold):
			// Primary thaw: a coarser, different bound than entry
			// (emaMinZ against the baseline) deliberately, so a rate
			// oscillating right at the entry boundary does not
			// repeatedly enter/exit freeze -- design item 4. Gated on
			// st.peaked (see that field's own doc comment): without it,
			// this condition is already true on nearly every reading
			// between entry and the rate actually reaching the
			// threshold for a source ramping up from a quiet baseline
			// (entry itself fires purely off z-score, well before the
			// absolute threshold), thawing -- and folding -- on almost
			// every one of them and reproducing #420's exact bug inside
			// the freeze window itself.
			//
			// justThawed suppresses the entry check below for this same
			// call: a spike that is decaying rather than gone can still
			// read as statistically elevated against the still-stale,
			// not-yet-refolded baseline, and re-entering on that same
			// reading would mean the baseline never actually resumes
			// moving (every future reading would keep reading as
			// elevated against itself, forever). Thaw's own contract
			// ("resumes normal fold-in with no jump") means this reading
			// folds in this call; entry is reconsidered fresh on the
			// next one, against the now-updated baseline.
			st.frozen = false
			justThawed = true
		case now.Sub(st.frozenSince) > activitySpikeFreezeBackstop*window:
			// Backstop: unlike primary thaw, the design explicitly wants
			// an immediate re-evaluate-and-possibly-re-freeze on this
			// same reading, so entry is not deferred here.
			applicable = forceFoldApplicable(baselines, buckets, useBucket, bucketKey, srcIP, now, rate)
			applicable.ZScore = emaZScore(rate, applicable.Value, applicable.Variance)
			if applicable.Ready && applicable.ZScore >= emaMinZ {
				st.frozenSince = now // still elevated -- re-freeze, restart the backstop clock
			} else {
				st.frozen = false
			}
		}
	}
	if !st.frozen && !justThawed && applicable.Ready && applicable.ZScore >= emaMinZ {
		// Entry: freeze starts immediately, excluding the very reading
		// that revealed the candidate spike from ever being folded in --
		// this call's firing decision is unaffected either way, since
		// `applicable` above was already captured pre-fold (Baseline's
		// own "before" contract), but every reading from here until
		// thaw must not move the baseline being measured against.
		st.frozen = true
		st.frozenSince = now
		st.peaked = rate >= float64(threshold)
		st.dirtyToday = true
	}
	if !st.frozen && !useBucket {
		// The bucket never folds live -- only rollHourBucket's once-a-day
		// rollover does. The fallback folds on every non-frozen call,
		// exactly as it always has.
		baselines.reading(srcIP, now, rate)
		baselines.persist(srcIP, now)
	}

	maturityStreak = st.maturityStreak
	if !applicable.Ready || applicable.ZScore < emaMinZ || rate < float64(threshold) ||
		applicable.Value <= 0 || rate < applicable.Value*multiplier {
		return false, useBucket, hour, applicable, maturityStreak
	}
	return true, useBucket, hour, applicable, maturityStreak
}

// rollMaturityDay advances the per-source consecutive-clean-days streak
// (design item 2) exactly when the calendar day changes: the day just
// closed extends the streak if it had no candidate-spike entry anywhere
// on this source (dirtyToday never got set), or resets it to zero if it
// did.
func rollMaturityDay(st *activitySpikeSourceState, day string) {
	if st.maturityDay == day {
		return
	}
	prevDay := st.maturityDay
	dirty := st.dirtyToday
	st.maturityDay = day
	st.dirtyToday = false
	if prevDay == "" {
		return // first day ever seen for this source -- nothing to roll yet
	}
	if dirty {
		st.maturityStreak = 0
	} else {
		st.maturityStreak++
	}
}

// rollHourBucket closes out the previous day's peak for this hour bucket
// once the calendar day has advanced, folding that peak into the
// bucket's EMA -- off_hours' own day-rollover shape
// (shipped_off_hours.go's Evaluate), generalized from "day's cumulative
// count" to "day's peak windowed rate" because activity_spike's own
// reading is already a windowed rate, not a raw per-event count.
//
// Skipped (the fold withheld, not merely deferred) when the source is
// currently frozen: design item 4's "stop folding ... into current
// bucket" is, at this once-a-day cadence, "a day whose peak was reached
// during an active candidate spike must not become this hour's new
// normal" -- the same protection an active freeze gives the fallback's
// live per-event folds, applied at the cadence the bucket actually
// folds on.
func rollHourBucket(buckets *baselineSet, st *activitySpikeSourceState, key string, hour int, day string, now time.Time) {
	if st.hourDay[hour] == day {
		return
	}
	prevDay := st.hourDay[hour]
	prevPeak := st.hourPeak[hour]
	st.hourDay[hour] = day
	st.hourPeak[hour] = 0
	if prevDay == "" {
		return // first time this hour has ever been seen for this source
	}
	if st.frozen {
		return
	}
	buckets.reading(key, now, float64(prevPeak))
	buckets.persist(key, now)
}

// forceFoldApplicable is the backstop's one forced fold-in (design item
// 4): folds rate into whichever baseline is currently applicable --
// the hour bucket if useBucket, otherwise the fallback -- and returns
// the resulting snapshot so the caller can re-evaluate immediately
// against the post-fold state.
func forceFoldApplicable(baselines, buckets *baselineSet, useBucket bool, bucketKey, srcIP string, now time.Time, rate float64) Snapshot {
	if useBucket {
		buckets.reading(bucketKey, now, rate)
		buckets.persist(bucketKey, now)
		snap, _ := buckets.snapshot(bucketKey, now)
		return snap
	}
	baselines.reading(srcIP, now, rate)
	baselines.persist(srcIP, now)
	snap, _ := baselines.snapshot(srcIP, now)
	return snap
}

// emitFallbackFiring emits a flag judged against the fallback EMA --
// today's Detail/confidence shape, byte-for-byte unchanged from before
// the #420 redesign, so every existing pin against it (warm-up,
// boundary, VPN boost, refire/clear/revive, ...) keeps meaning what it
// always meant. samples is capped by warmupSamples exactly as before.
func (d *activitySpikeDefinition) emitFallbackFiring(srcIP, country, iface string, count int, applicable Snapshot, now time.Time) {
	samples := applicable.Samples
	if d.warmupSamples > 0 && samples > d.warmupSamples {
		samples = d.warmupSamples
	}
	confidence := vpnBoostConfidence(
		emaConfidence(applicable.ZScore, samples, d.warmupSamples),
		d.vpnInterfaces, d.vpnMultiplier, iface)

	detail := fmt.Sprintf(
		"%d events in %s vs a baseline of %.1f for this host (based on %d samples, %.1fσ above normal)",
		count, d.window, applicable.Value, samples, applicable.ZScore,
	) + vpnDetailSuffix(d.vpnInterfaces, iface)

	// Size is the event count in the window -- activity_spike's declared
	// size, the measure its own threshold param is compared against.
	// See ShippedSizeMeasure and #640.
	size := count
	d.emit(Emission{
		Target:     srcIP,
		Detail:     detail,
		Confidence: &confidence,
		Country:    country,
		SourceIP:   srcIP,
		EventTime:  now,
		Size:       &size,
	})
}

// emitBucketFiring emits a flag judged against a mature hour bucket.
// Confidence incorporates the source's held-days maturity streak through
// the same emaConfidence shape off_hours' own sampleDays scoring uses
// (design item 5), capped by warmupSamples for the same reason the
// fallback path caps its own sample count -- a display/scoring
// normalization, not a different statistic. Detail names the actual
// (capped) held-days count, never a different or hardcoded number --
// design item 5's "may only claim what is true."
func (d *activitySpikeDefinition) emitBucketFiring(srcIP, country, iface string, hour, count, maturityStreak int, applicable Snapshot, now time.Time) {
	days := maturityStreak
	if d.warmupSamples > 0 && days > d.warmupSamples {
		days = d.warmupSamples
	}
	confidence := vpnBoostConfidence(
		emaConfidence(applicable.ZScore, days, d.warmupSamples),
		d.vpnInterfaces, d.vpnMultiplier, iface)

	detail := fmt.Sprintf(
		"%d events in %s vs a baseline of %.1f for this host at %02d:00 (%d day(s) of consistent history at this hour, %.1fσ above normal)",
		count, d.window, applicable.Value, hour, days, applicable.ZScore,
	) + vpnDetailSuffix(d.vpnInterfaces, iface)

	// Size is the event count in the window, exactly as on the fallback
	// path above -- which baseline judged the firing changes the
	// confidence, not what the size means.
	size := count
	d.emit(Emission{
		Target:     srcIP,
		Detail:     detail,
		Confidence: &confidence,
		Country:    country,
		SourceIP:   srcIP,
		EventTime:  now,
		Size:       &size,
	})
}

// Learning satisfies LearningReporter, merging this definition's two
// baselineSets (see this file's own doc comment for why there are two)
// into one per-*source* answer, as every other optional interface here
// merges rather than exposing bucketKey/buckets separately.
//
// keys/ready are counted per source, not per (source, hour) bucket: the
// bucket set's key space is an internal implementation detail (up to 24
// keys per source) that would make "ready for 12 of 50 sources" actually
// mean "ready for 12 of 1,200," which is not the question an operator is
// asking. A source counts as ready the moment *either* representation
// does, matching activitySpikeCheck's own useBucket rule (a mature hour
// bucket is the applicable baseline the instant it clears its floor,
// fallback otherwise) -- so this answers the same "could this source
// actually fire today" question Fire itself would.
//
// The floor reported, and the one nearest's progress is measured
// against, is always the fallback's: it is the one an operator's own
// params actually tune (bucketFloor is a fixed structural constant, see
// activityBucketMinDays), and the two floors' dimensions are not
// comparable numbers to blend. A source with bucket-only progress (no
// fallback entry yet) falls back to that bucket key's own progress as
// the least-wrong stand-in; in practice every source acquires a fallback
// entry before any bucket ever can (checkBaseline always folds the
// fallback until useBucket first turns true), so this path is a
// defensive fallback, not the common case.
func (d *activitySpikeDefinition) Learning(now time.Time) (LearningState, bool) {
	fallback := d.baselines.learning(now)
	buckets := d.buckets.learning(now)

	merged := make(map[string]baselineLearning, len(fallback))
	for srcIP, bl := range fallback {
		merged[srcIP] = bl
	}
	for key, bl := range buckets {
		srcIP, _, ok := strings.Cut(key, "\x00")
		if !ok {
			continue
		}
		existing, has := merged[srcIP]
		switch {
		case !has:
			merged[srcIP] = bl
		case bl.ready && !existing.ready:
			existing.ready = true
			merged[srcIP] = existing
		}
	}
	return learningStateFrom(d.baselines.floor, merged), true
}

// Replay satisfies Replayable: the same per-source count-and-baseline
// walk against fresh, call-local state -- and, since the owner decision
// on #420 recorded that live-fires-but-replay-cannot is the product
// contradicting itself (the exact truthfulness class #420 exists to
// remove), the *same* #420 arithmetic checkBaseline uses, not the
// pre-redesign single-EMA-per-event walk this method used to run.
// activitySpikeCheck -- freeze/thaw/backstop, hour buckets, maturity --
// runs unchanged here, driven entirely by each corpus event's own
// ReceivedAt (never a wall-clock read, so replay stays deterministic --
// TestShippedActivitySpikeIsReplayable), against baselines/buckets/
// sources built fresh for this call alone: replay never reads or writes
// the live definition's own persisted state (that would let one Replay
// call see a different answer depending on what the live process
// happened to have already learned, and let a Replay call corrupt what
// live evaluation judges against) -- constructed the same way the
// pre-existing rings map already was, just three more of them. The
// `nil` StateStore on both baselineSets means neither ever touches
// persistence either, on top of being thrown away when Replay returns.
//
// One honest edge, not a gap: a corpus shorter than a full day can never
// give any hour bucket a fully-observed prior day, so activitySpikeCheck
// always resolves useBucket=false and every emission here is judged
// against the fallback EMA -- exactly the same "day one" behaviour live
// evaluation shows a brand-new source, for the same reason (see
// activityBucketMinDays). That is what the corpus actually supports;
// Replay is not licensed to claim a bucket-mature verdict it cannot
// back with a second day.
//
// Candidate params override window/threshold/baselineMultiplier -- the
// three that decide firing -- exactly as before.
func (d *activitySpikeDefinition) Replay(corpus Corpus, candidate Params) (Result, error) {
	window, threshold, multiplier := d.window, d.threshold, d.multiplier
	if len(candidate) > 0 {
		validated, err := ValidateParams(activitySpikeReplaySchema, candidate)
		if err != nil {
			return Result{}, fmt.Errorf("engine: definition %q: replay candidate params: %w", d.def.ID, err)
		}
		if raw, ok := validated["window"]; ok {
			parsed, err := time.ParseDuration(raw.(string))
			if err != nil {
				return Result{}, fmt.Errorf("engine: definition %q: replay candidate window: %w", d.def.ID, err)
			}
			window = parsed
		}
		if raw, ok := validated["threshold"]; ok {
			threshold = raw.(int)
		}
		if raw, ok := validated["baselineMultiplier"]; ok {
			multiplier = toFloat(raw)
		}
	}
	if window <= 0 {
		return Result{}, fmt.Errorf("engine: definition %q: replay window must be positive, got %s", d.def.ID, window)
	}

	rings := map[string]*CountRing{}
	// Fresh, call-local, corpus-only state -- never the live d.baselines/
	// d.buckets/d.sources -- see this method's own doc comment. An empty
	// defID and a nil StateStore are safe here specifically because
	// baselineSet only ever consults defID to key a StateStore call, and
	// every such call is a guarded no-op against a nil store (see
	// baselineSet.newBaseline/maybePersist) -- so this reads as "never
	// persisted," not as "persisted under a blank key." The floor/cadence
	// mirror exactly what buildActivitySpikeDefinition gives the live
	// fallback baseline before any operator-set baselineFloorDuration/
	// updateCadence extension -- Replay never honored those two knobs
	// (pre-dating this port), and porting the #420 arithmetic is not
	// license to newly wire them in as a side effect.
	baselines := newBaselineSet("", 0, BaselineFloor{MinSamples: hostActivityMinSamples}, UpdatePerEvent, nil)
	buckets := newBaselineSet("", 0, BaselineFloor{MinSamples: activityBucketMinDays}, UpdatePerWindow, nil)
	sources := NewKeyed[*activitySpikeSourceState]()

	var (
		emissionCount int
		sample        []ReplaySample
	)

	corpusWindow := corpus.Replay(func(e store.Event) {
		if e.SrcIP == "" || !d.active(e) {
			return
		}
		if e.ConnState != "" && e.ConnState != "new" {
			return
		}
		now := e.ReceivedAt
		ring, ok := rings[e.SrcIP]
		if !ok {
			ring = NewCountRing(window)
			rings[e.SrcIP] = ring
		}
		ring.Add(now, true)
		count := ring.Count(now, window)

		fire, useBucket, hour, applicable, maturityStreak := activitySpikeCheck(
			baselines, buckets, sources, window, threshold, multiplier, e.SrcIP, count, now)
		if !fire {
			return
		}
		emissionCount++
		if len(sample) >= replaySampleBound {
			return
		}
		var detail string
		if useBucket {
			detail = fmt.Sprintf("%d events in %s vs a baseline of %.1f for this host at %02d:00 (%d day(s) of consistent history at this hour, %.1fσ above normal)",
				count, window, applicable.Value, hour, maturityStreak, applicable.ZScore)
		} else {
			detail = fmt.Sprintf("%d events in %s vs a baseline of %.1f for this host (%.1fσ above normal)",
				count, window, applicable.Value, applicable.ZScore)
		}
		sample = append(sample, ReplaySample{At: now, Target: e.SrcIP, Detail: detail})
	})

	span := corpusWindow.End.Sub(corpusWindow.Start)
	if corpusWindow.Count == 0 || span < window {
		return Result{Decline: &Decline{
			Reason: fmt.Sprintf(
				"corpus covers %s (%d event(s)), shorter than this definition's %s window -- declining rather than reporting a potentially misleading count",
				span, corpusWindow.Count, window),
			CorpusSpan:       span,
			DefinitionWindow: window,
		}}, nil
	}

	w, err := NewWindow(corpusWindow.Start, corpusWindow.End, corpusWindow.Count)
	if err != nil {
		return Result{}, fmt.Errorf("engine: definition %q: replay: %w", d.def.ID, err)
	}
	receipt, err := NewReceipt(w, emissionCount, sample, corpusWindow.Truncated)
	if err != nil {
		return Result{}, fmt.Errorf("engine: definition %q: replay: %w", d.def.ID, err)
	}
	return Result{Receipt: &receipt}, nil
}

var activitySpikeReplaySchema = []ParamSchema{
	{Name: "window", Type: ParamTypeDuration, Min: durationBound(time.Second)},
	{Name: "threshold", Type: ParamTypeInt, Min: floatBound(1)},
	{Name: "baselineMultiplier", Type: ParamTypeFloat, Min: floatBound(0)},
}
