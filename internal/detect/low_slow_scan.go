package detect

import (
	"fmt"
	"math"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// lowSlowScanMinZ/lowSlowScanFullConfidenceZ mirror host_baseline.go's
// hostActivityMinZ/hostActivityFullConfidenceZ -- same z-score shape,
// applied to this detector's own breadth metric instead of raw event
// rate.
const lowSlowScanMinZ = 2.0
const lowSlowScanFullConfidenceZ = 6.0

// lowSlowScanWarmupSamples is smaller than activity-spike's
// HostActivityWarmupSamples (20) -- each sample here already represents
// a whole LowSlowScanWindow-sized snapshot of a source's behavior
// (hours), not one event, so far fewer are needed before a baseline is
// trustworthy.
const lowSlowScanWarmupSamples = 10

type lowSlowSample struct {
	at     time.Time
	port   int
	dstIP  string
	action store.Action
}

type lowSlowWindow struct {
	samples      []lowSlowSample
	firstSeen    time.Time
	lastActivity time.Time

	// EMA baseline of this source's own destination-breadth rate
	// (distinct ports + distinct hosts per window) -- same
	// primed/baseline/variance/sampleCount shape as sourceWindow's
	// activity baseline in host_baseline.go, tracking breadth instead of
	// raw event count.
	baseline    float64
	variance    float64
	primed      bool
	sampleCount int
}

// observeLowSlowScan is the low-and-slow counterpart to
// observeScanAndSpike's port_scan branch (issue #20): a scan deliberately
// paced to stay under PortScanWindow's short-burst threshold. Judged over
// a much longer window and gated by several independent signals rather
// than one count -- see Config.LowSlowScanWindow's doc comment for why a
// single "distinct ports per hour" threshold was explicitly rejected as
// too prone to false positives (container orchestration, health checks,
// browsers).
func (d *Detector) observeLowSlowScan(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}

	ls := d.settings.Get(DetectorLowSlowScan)
	if !ls.Enabled || !scopeMatchesHost(ls.Scope, e.SrcIP) {
		return
	}

	w, ok := d.lowSlowWindows[e.SrcIP]
	if !ok {
		if len(d.lowSlowWindows) >= maxTrackedSources {
			d.evictOldestLowSlowWindow()
		}
		w = &lowSlowWindow{firstSeen: now}
		d.lowSlowWindows[e.SrcIP] = w
	}
	w.lastActivity = now
	w.samples = append(w.samples, lowSlowSample{at: now, port: e.DstPort, dstIP: e.DstIP, action: e.Action})

	cutoff := now.Add(-d.cfg.LowSlowScanWindow)
	i := 0
	for i < len(w.samples) && w.samples[i].at.Before(cutoff) {
		i++
	}
	if i > 0 {
		w.samples = w.samples[i:]
	}

	distinctPorts := make(map[int]struct{})
	distinctHosts := make(map[string]struct{})
	var dropCount, total int
	for _, s := range w.samples {
		total++
		if s.action == store.ActionDrop || s.action == store.ActionReject {
			dropCount++
		}
		if s.port != 0 && scopeMatchesPort(ls.Scope, s.port) {
			distinctPorts[s.port] = struct{}{}
		}
		if s.dstIP != "" {
			distinctHosts[s.dstIP] = struct{}{}
		}
	}
	breadth := float64(len(distinctPorts) + len(distinctHosts))

	if w.primed {
		prevBaseline := w.baseline
		stddev := math.Sqrt(w.variance)

		var z float64
		switch {
		case stddev > 0:
			z = (breadth - prevBaseline) / stddev
		case breadth > prevBaseline:
			z = lowSlowScanFullConfidenceZ
		default:
			z = 0
		}

		observedLongEnough := now.Sub(w.firstSeen) >= d.cfg.LowSlowScanMinObservation
		breadthCleared := len(distinctPorts) >= d.cfg.LowSlowScanPortThreshold &&
			len(distinctHosts) >= d.cfg.LowSlowScanHostThreshold
		dropRatio := 0.0
		if total > 0 {
			dropRatio = float64(dropCount) / float64(total)
		}
		dropCleared := total > 0 && dropRatio >= d.cfg.LowSlowScanDropRatio
		baselineCleared := z >= lowSlowScanMinZ && prevBaseline > 0 &&
			breadth >= prevBaseline*d.cfg.LowSlowScanBaselineMultiplier

		if observedLongEnough && breadthCleared && dropCleared && baselineCleared {
			historyConfidence := math.Min(1, float64(w.sampleCount)/float64(lowSlowScanWarmupSamples))
			deviationConfidence := math.Min(1, math.Max(0, (z-lowSlowScanMinZ)/(lowSlowScanFullConfidenceZ-lowSlowScanMinZ)))
			baselineConfidence := int(math.Round(historyConfidence * deviationConfidence * 100))
			dropConfidence := int(math.Round(math.Min(1, math.Max(0, (dropRatio-d.cfg.LowSlowScanDropRatio)/(1-d.cfg.LowSlowScanDropRatio))) * 100))
			portConfidence := overshootConfidence(len(distinctPorts), d.cfg.LowSlowScanPortThreshold)
			hostConfidence := overshootConfidence(len(distinctHosts), d.cfg.LowSlowScanHostThreshold)

			// The weakest-clearing axis bounds overall confidence --
			// several independent signals must each be convincing, not
			// just the strongest one.
			confidence := portConfidence
			for _, c := range []int{hostConfidence, dropConfidence, baselineConfidence} {
				if c < confidence {
					confidence = c
				}
			}

			detail := fmt.Sprintf(
				"%d distinct ports, %d distinct hosts over %s (%.0f%% drop/reject, %.1fσ above this source's normal breadth)",
				len(distinctPorts), len(distinctHosts), d.cfg.LowSlowScanWindow, dropRatio*100, z,
			)
			isNew := d.fs.AddWithDetail(flags.TypeLowSlowScan, e.SrcIP, detail, confidence,
				flags.Evidence{Ports: sortedPortsCapped(distinctPorts), Hosts: sortedHostsCapped(distinctHosts)},
				e.SrcCountry, now)
			d.maybeCheckReputation(flags.TypeLowSlowScan, e.SrcIP, e.SrcIP, isNew)
		}
	}

	// EMA update, after the check above so a flag (if any) compares
	// against the baseline as it stood *before* this reading -- same
	// ordering checkHostActivityBaseline uses.
	if !w.primed {
		w.baseline = breadth
		w.variance = 0
		w.primed = true
		w.sampleCount = 1
		return
	}
	diff := breadth - w.baseline
	incr := emaAlpha * diff
	w.baseline += incr
	w.variance = (1 - emaAlpha) * (w.variance + diff*incr)
	if w.sampleCount < lowSlowScanWarmupSamples {
		w.sampleCount++
	}
}

func (d *Detector) evictOldestLowSlowWindow() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, w := range d.lowSlowWindows {
		if first || w.lastActivity.Before(oldest) {
			oldestKey, oldest, first = k, w.lastActivity, false
		}
	}
	if oldestKey != "" {
		delete(d.lowSlowWindows, oldestKey)
	}
}
