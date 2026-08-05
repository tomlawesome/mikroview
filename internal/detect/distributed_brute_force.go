package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

// portSources tracks one critical port's recent distinct source IPs --
// a distinctRing rather than countRing (despite counting, the detector
// below is inherently about *distinct* sources; a countRing would fire
// on repeated attempts from a single source, defeating the point of
// "distributed").
type portSources struct {
	ips *distinctRing[string]
}

// observeDistributedBruteForce tracks, per critical port, the distinct
// external source IPs that have hit it recently -- flags when many
// different sources are hammering the same port, the signature of a
// coordinated/botnet campaign against one service, as distinct from the
// critical-port detector's "one source hitting it repeatedly." Keyed by
// port rather than source, so unlike the per-source maps this one is
// naturally bounded by len(Config.CriticalPorts) and needs no eviction.
func (d *Detector) observeDistributedBruteForce(e store.Event, now time.Time) {
	if !isTrackableConnState(e) {
		return
	}
	p, ok := d.criticalPortIPs[e.DstPort]
	if !ok {
		p = &portSources{ips: newDistinctRing[string](d.cfg.DistributedBruteForceWindow)}
		d.criticalPortIPs[e.DstPort] = p
	}
	p.ips.Add(now, e.SrcIP)
	count := p.ips.Count(now, d.cfg.DistributedBruteForceWindow, nil)

	if count >= d.cfg.DistributedBruteForceThreshold {
		target := fmt.Sprintf("port %d", e.DstPort)
		distinct := p.ips.Values(now, d.cfg.DistributedBruteForceWindow, nil)
		isNew := d.fs.AddWithDetail(flags.TypeDistributedBruteForce, target,
			fmt.Sprintf("%d distinct source IPs in %s", count, d.cfg.DistributedBruteForceWindow),
			overshootConfidence(count, d.cfg.DistributedBruteForceThreshold),
			flags.Evidence{Hosts: sortedHostsCapped(distinct)}, "", now)
		d.maybeCheckGroupReputation(flags.TypeDistributedBruteForce, target, distinct, isNew)
	}
}
