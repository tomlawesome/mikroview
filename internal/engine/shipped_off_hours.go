// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"fmt"
	"strconv"
	"time"

	"github.com/tomlawesome/mikroview/internal/store"
)

func init() {
	registerShippedProgrammatic("off_hours_activity", buildOffHoursDefinition)
}

// offHoursDefinition is off_hours ported onto the chassis (issue #405):
// a host active during a clock window it has no established history of
// being active in.
//
// # Programmatic, not declarative
//
// docs/decisions/evaluation-engine.md section 2 lists off_hours among
// the detectors expected to become declarative, and #405's plan repeats
// that. Porting it showed the expectation was wrong, and this is the
// recorded correction: off_hours is not a threshold over a window. It
// carries twenty-four independent EMA baselines per source, one per
// clock hour, each advancing exactly once per calendar day, and it fires
// on a z-score against the hour's own history gated by how many distinct
// prior days that hour has been observed on. None of that is expressible
// as conditions plus a count -- there is no window whose contents are
// being counted, and the thing being compared is a per-hour daily mean.
// Calling it declarative would have meant either inventing a
// per-hour-of-day counting mode nothing else uses, or dumbing the
// statistic down to something that would false-positive on exactly the
// traffic #104 designed it to ignore.
//
// # Why the baseline is zero-seeded
//
// internal/detect accumulated each hour's baseline from a standing start
// of zero: the first day observed at an hour moves it by
// emaAlpha * count, not to count. That is deliberate -- priming from the
// first observation would make one busy night the entire baseline, which
// is the false positive OffHoursMinSampleDays exists to rule out. See
// baselineSet.zeroSeeded.
//
// # What is fixed, and what deliberately is not
//
// Every hour's baseline advances regardless of the configured off-hours
// window; only *firing* is restricted to hours inside it. That is why
// widening the window later does not discard history already collected
// for the newly-included hours, and it is preserved here exactly.
type offHoursDefinition struct {
	programmaticBase

	startHour     int
	endHour       int
	minSampleDays int
	minCount      int

	// days tracks, per (source, hour), which calendar day that bucket is
	// currently counting and how many events it has seen today -- the
	// state that is not a baseline. Bounded by Keyed's own cap, the same
	// way internal/detect's perSource map was bounded by maxTrackedSources.
	days      *Keyed[*offHoursDay]
	baselines *baselineSet
}

// offHoursDay is one (source, hour) bucket's still-accumulating day.
type offHoursDay struct {
	// day is the server-local calendar day ("2006-01-02") count belongs
	// to, or "" if this bucket has never been observed.
	day   string
	count int
}

func buildOffHoursDefinition(def Definition, deps ShippedDeps) (Evaluated, error) {
	params, err := ValidateParams(def.ParamSchema, def.Params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	startHour, err := paramInt(params, "startHour")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	endHour, err := paramInt(params, "endHour")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	minSampleDays, err := paramInt(params, "minSampleDays")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	minCount, err := paramInt(params, "minCount")
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}
	cadence, err := cadenceFromParams(params)
	if err != nil {
		return nil, fmt.Errorf("engine: shipped programmatic definition %q: %w", def.ID, err)
	}

	// The floor is a sample count in *days*, which is what
	// OffHoursMinSampleDays means. primeWindow is zero because this
	// baseline is zero-seeded rather than primed at all.
	floor := BaselineFloor{MinSamples: minSampleDays}

	return &offHoursDefinition{
		programmaticBase: programmaticBase{def: def},
		startHour:        startHour,
		endHour:          endHour,
		minSampleDays:    minSampleDays,
		minCount:         minCount,
		days:             NewKeyed[*offHoursDay](),
		baselines:        newBaselineSet(def.ID, 0, floor, cadence, deps.State).zeroSeed(),
	}, nil
}

// offHoursKey is the (source, hour-of-day) key both this definition's
// maps use. The separator is a NUL rather than a colon or a pipe so it
// can never collide with anything a source address can contain.
func offHoursKey(srcIP string, hour int) string {
	return srcIP + "\x00" + strconv.Itoa(hour)
}

// Evaluate satisfies Evaluated.
func (d *offHoursDefinition) Evaluate(e store.Event) {
	if e.SrcIP == "" || !d.active(e) {
		return
	}
	if e.ConnState != "" && e.ConnState != "new" {
		return
	}

	now := e.ReceivedAt
	hour := now.Hour()
	day := now.Format("2006-01-02")
	key := offHoursKey(e.SrcIP, hour)

	bucket := d.days.GetOrCreate(key, now, func() *offHoursDay { return &offHoursDay{} })
	baseline := d.baselines.get(key, now)

	if bucket.day != day {
		// Crossed into a new day at this hour bucket (or first sight).
		// Fold the *previous* day's final count into the EMA before
		// resetting, so today's still-accumulating count is always
		// compared against a baseline built only from prior days, never
		// one that already includes itself.
		if bucket.day != "" {
			baseline.Reading(now, float64(bucket.count))
			d.baselines.persist(key, now)
		}
		bucket.day = day
		bucket.count = 0
	}
	bucket.count++

	// Baseline bookkeeping above runs for every hour, always -- only
	// firing is restricted to the configured window, which is what makes
	// widening that window later a change that keeps its history.
	if !inOffHoursWindow(hour, d.startHour, d.endHour) {
		return
	}

	snap := baseline.Snapshot(now)
	count := bucket.count
	z := emaZScore(float64(count), snap.Value, snap.Variance)

	// Both conditions required, not either/or: (1) this specific hour has
	// real history behind it, not one busy night; (2) an absolute floor
	// alongside the statistical one, so a handful of events against a
	// near-zero baseline does not read as a huge deviation just because
	// the baseline itself is tiny.
	if !snap.Ready || count < d.minCount || z < emaMinZ {
		return
	}

	// sampleDays doubles as emaConfidence's warmupSamples here: once the
	// gate above is satisfied at all, history is already trusted, so
	// confidence past that point is driven by the deviation alone.
	// Capped for display exactly as internal/detect's own counter was.
	sampleDays := snap.Samples
	if d.minSampleDays > 0 && sampleDays > d.minSampleDays {
		sampleDays = d.minSampleDays
	}
	confidence := emaConfidence(z, sampleDays, d.minSampleDays)

	d.emit(Emission{
		Target: e.SrcIP,
		Detail: fmt.Sprintf(
			"%d events at %02d:00 vs a baseline of %.1f for this host at this hour (%d days of history, %.1fσ above normal)",
			count, hour, snap.Value, sampleDays, z),
		Confidence: &confidence,
		Country:    e.SrcCountry,
		SourceIP:   e.SrcIP,
		EventTime:  now,
	})
}

// inOffHoursWindow reports whether hour (0-23) falls inside the
// [start, end) clock window, wrapping past midnight when start > end
// (23, 6 means 23:00-24:00 union 00:00-06:00). start == end is treated
// as "never" rather than "always": a degenerate window disabling the
// definition is the safer failure mode than one that fires on
// everything. Unchanged from internal/detect.
func inOffHoursWindow(hour, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}

// Learning satisfies LearningReporter: one baseline per (source, hour)
// key, so Ready answers how many of those keys have the
// minSampleDays of same-hour history this definition's floor requires --
// see baselineSet.learning and learningStateFrom for the shared
// read/reduce this and every other baseline-backed shipped definition
// rely on.
func (d *offHoursDefinition) Learning(now time.Time) (LearningState, bool) {
	return learningStateFrom(d.baselines.floor, d.baselines.learning(now)), true
}

// NonReplayableReason satisfies NonReplayable.
//
// off_hours judges a clock hour against how that same host behaved at
// that same hour on at least fourteen distinct prior days. #403's own
// "floor-exceeds-corpus" case, exactly: the corpus is the in-memory
// event ring, which on a real deployment holds minutes, and no amount of
// waiting makes it hold a fortnight. A replay could only ever report a
// confident zero -- "would have fired 0 times" -- which is the specific
// dishonesty the replay contract exists to rule out, because the true
// answer is not zero but unknowable from this corpus.
//
// This is a permanent property of the definition against this corpus,
// not the dynamic "this particular corpus happens to be short" case
// Decline covers, so it is declared once rather than declined per call.
// If replay ever gains a retention-backed corpus (Corpus is an interface
// precisely so it can), this declaration is what should be revisited
// first.
func (d *offHoursDefinition) NonReplayableReason() string {
	return fmt.Sprintf(
		"off_hours compares an hour against the same host's history at that hour across at least %d distinct prior days; the replay corpus is the in-memory event window, which holds minutes, so no replay over it could distinguish 'would not have fired' from 'could not tell'",
		d.minSampleDays)
}
