package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// Off-hours activity (issue #104): flag a host active during a clock
// window it has no established history of being active in. Roadmap item
// from #94's audit, with an explicit caution from the project owner
// driving this design: a couple of stray queries at 3am is not useful
// signal, and a naive "any activity inside a configured off-hours
// window" check would false-positive constantly -- a phone syncing, a
// scheduled job, a clock-skewed IoT device. This extends
// host_baseline.go's per-host EMA baseline machinery with a time-of-day
// dimension (one independent baseline per hour, see
// sourceWindow.hourly) rather than inventing new statistics: the same
// emaUpdate/emaZScore/emaConfidence primitives, applied per hour-bucket
// instead of once globally per host.
//
// Design decision (left explicitly open in the issue): "off-hours" is
// defined by a fixed, operator-configured clock window
// (Config.OffHoursStartHour/EndHour), not a per-host-learned quiet
// period. A per-host-learned window (e.g. "whichever hours this host's
// own baseline reads as unusually quiet relative to its other hours")
// is more tailored to an individual household's schedule, but was
// rejected here for three reasons: (1) it has its own bootstrapping
// problem -- deciding which hours are "quiet" needs the same 24-hour
// baseline history this detector is already gated on, so it wouldn't
// meaningfully shorten the wait before the detector can protect at all;
// (2) it's harder for a human reviewing a flag to sanity-check --
// "flagged because active outside your configured 23:00-06:00 window"
// is a fact anyone can verify at a glance, "flagged because this hour
// scored as statistically quiet relative to your other 23 hours" is
// not; (3) it adds a second layer of derived state on top of the
// per-hour baselines with its own tuning knobs, for a network this tool
// is scoped for (see package doc comment -- "an interrogation helper",
// not a research platform). A fixed window is simpler, predictable, and
// matches this project's existing convention of not committing to exact
// detector sensitivity without live false-positive data (see
// RuleSpikeMinRate/GlobalSpikeMinEPS's own doc comments) -- it can be
// revisited if real-world use shows a fixed window doesn't fit typical
// deployments well.
//
// Every hour's baseline is tracked continuously regardless of the
// configured window (see checkOffHoursActivity) -- only whether a flag
// can *fire* is restricted to hours inside it. That means widening or
// narrowing OffHoursStartHour/EndHour later doesn't discard history
// that was already being collected for the newly-included hours.

// observeOffHours is off-hours activity's entry point from Observe,
// alongside observeScanAndSpike/observeLowSlowScan. Shares
// d.perSource with those two (this is a per-hour extension of the same
// per-host baseline concept, not a separate tracked population) --
// unlike them, it does its own get-or-create here rather than assuming
// a window already exists, since a deployment can have this detector on
// while port-scan and activity-spike are both off.
func (d *Detector) observeOffHours(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}

	oh := d.settings.Get(DetectorOffHoursActivity)
	if !oh.Enabled || !scopeMatchesHost(oh.Scope, e.SrcIP) {
		return
	}

	w, ok := d.perSource[e.SrcIP]
	if !ok {
		if len(d.perSource) >= maxTrackedSources {
			evictOldestByActivity(d.perSource)
		}
		w = &sourceWindow{
			spikes: newCountRing(d.cfg.ActivitySpikeWindow),
			ports:  newDistinctRing[int](d.cfg.PortScanWindow),
		}
		d.perSource[e.SrcIP] = w
	}
	w.lastActivity = now

	d.checkOffHoursActivity(w, e.SrcIP, e.SrcCountry, now)
}

// checkOffHoursActivity is host_baseline.go's checkHostActivityBaseline,
// applied per hour-of-day instead of once globally per host. hour's
// bucket only advances (EMA-updates, sampleDays++) once per calendar
// day -- unlike checkHostActivityBaseline's rolling-window rate, what
// this needs to track isn't "events per some short window" but "how
// many events does this host typically produce during this specific
// clock hour, and on how many distinct prior days has that been
// observed" -- sampleDays is the entire point of the false-positive
// guard (see the package doc comment above), so it has to count
// distinct days, not just accumulated event count.
func (d *Detector) checkOffHoursActivity(w *sourceWindow, srcIP, srcCountry string, now time.Time) {
	hour := now.Hour()
	day := now.Format("2006-01-02")
	b := &w.hourly[hour]

	if w.hourlyDay[hour] != day {
		// Crossed into a new day at this hour bucket (or this is the
		// first time this source has ever been seen at this hour).
		// Fold the *previous* day's final count into the EMA (and count
		// it toward sampleDays) before resetting for today -- so
		// today's still-accumulating count, checked below, is always
		// compared against a baseline built only from prior days, never
		// one that already includes itself.
		if w.hourlyDay[hour] != "" {
			b.baseline, b.variance = emaUpdate(float64(w.hourlyCount[hour]), b.baseline, b.variance)
			if b.sampleDays < d.cfg.OffHoursMinSampleDays {
				b.sampleDays++
			}
		}
		w.hourlyDay[hour] = day
		w.hourlyCount[hour] = 0
	}
	w.hourlyCount[hour]++

	// Baseline bookkeeping above runs for every hour, always -- but a
	// flag can only ever fire for an hour inside the configured
	// off-hours window (see the package doc comment's design-decision
	// section for why this is a fixed window rather than a learned
	// one).
	if !inOffHoursWindow(hour, d.cfg.OffHoursStartHour, d.cfg.OffHoursEndHour) {
		return
	}

	count := w.hourlyCount[hour]
	z := emaZScore(float64(count), b.baseline, b.variance)

	// Both conditions required, not either/or -- see the package doc
	// comment and Config.OffHoursMinSampleDays/MinCount's own doc
	// comments for why each one exists independently:
	//   (1) sampleDays >= OffHoursMinSampleDays: this specific hour has
	//       real history behind it, not just one busy night.
	//   (2) count >= OffHoursMinCount *and* z >= emaMinZ: an absolute
	//       floor alongside the statistical one, so a handful of events
	//       against a near-zero baseline doesn't read as a huge
	//       deviation just because the baseline itself is tiny.
	if b.sampleDays >= d.cfg.OffHoursMinSampleDays &&
		count >= d.cfg.OffHoursMinCount &&
		z >= emaMinZ {

		// sampleDays doubles as emaConfidence's warmupSamples here (see
		// Config.OffHoursMinSampleDays' doc comment): once the gate
		// above is satisfied at all, history is already trusted, so
		// confidence past that point is driven by the deviation alone.
		confidence := emaConfidence(z, b.sampleDays, d.cfg.OffHoursMinSampleDays)

		detail := fmt.Sprintf(
			"%d events at %02d:00 vs a baseline of %.1f for this host at this hour (%d days of history, %.1fσ above normal)",
			count, hour, b.baseline, b.sampleDays, z,
		)
		isNew := d.fs.AddWithDetail(flags.TypeOffHoursActivity, srcIP, detail, confidence, flags.Evidence{}, srcCountry, now)
		d.maybeCheckReputation(flags.TypeOffHoursActivity, srcIP, srcIP, isNew)
	}
}

// inOffHoursWindow reports whether hour (0-23) falls inside the
// [start, end) clock window, wrapping past midnight when start > end
// (e.g. start=23, end=6 means 23:00-24:00 union 00:00-06:00). start ==
// end is treated as "never" rather than "always" -- a degenerate/
// unconfigured window disabling the detector is the safer failure mode
// than one that fires on everything, consistent with how an empty scope
// list elsewhere in this package means "no restriction" rather than
// "matches nothing."
func inOffHoursWindow(hour, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return hour >= start && hour < end
	}
	return hour >= start || hour < end
}
