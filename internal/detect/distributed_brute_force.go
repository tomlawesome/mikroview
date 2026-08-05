package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

type portSample struct {
	at time.Time
	ip string
}

type portSources struct {
	samples []portSample
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
		p = &portSources{}
		d.criticalPortIPs[e.DstPort] = p
	}
	p.samples = append(p.samples, portSample{at: now, ip: e.SrcIP})

	cutoff := now.Add(-d.cfg.DistributedBruteForceWindow)
	i := 0
	for i < len(p.samples) && p.samples[i].at.Before(cutoff) {
		i++
	}
	if i > 0 {
		p.samples = p.samples[i:]
	}

	distinct := make(map[string]struct{})
	for _, s := range p.samples {
		distinct[s.ip] = struct{}{}
	}

	if len(distinct) >= d.cfg.DistributedBruteForceThreshold {
		target := fmt.Sprintf("port %d", e.DstPort)
		isNew := d.fs.AddWithDetail(flags.TypeDistributedBruteForce, target,
			fmt.Sprintf("%d distinct source IPs in %s", len(distinct), d.cfg.DistributedBruteForceWindow),
			overshootConfidence(len(distinct), d.cfg.DistributedBruteForceThreshold),
			flags.Evidence{Hosts: sortedHostsCapped(distinct)}, "", now)
		d.maybeCheckGroupReputation(flags.TypeDistributedBruteForce, target, distinct, isNew)
	}
}
