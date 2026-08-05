package detect

import (
	"fmt"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/store"
)

func dropPairKey(srcIP string, dstPort int) string {
	return fmt.Sprintf("%s->%d", srcIP, dstPort)
}

// observeRepeatedDrops tracks the same (source, destination port) pair
// repeatedly getting dropped/rejected against a locally-hosted service --
// unlike the critical-port detector, this isn't restricted to a curated
// port list or to external sources, since the point here isn't "someone
// probing a sensitive service" but "something keeps failing to reach one
// of your ports." For a self-hoster that's very often a misconfigured
// port-forward or firewall rule (the real client just keeps retrying),
// not necessarily an attack -- the flag detail is worded accordingly, and
// this fires on a much longer window/higher threshold than critical-port
// to avoid flagging ordinary internet background-scan noise.
func (d *Detector) observeRepeatedDrops(e store.Event, now time.Time) {
	key := dropPairKey(e.SrcIP, e.DstPort)
	hits, ok := d.dropPairs[key]
	if !ok && len(d.dropPairs) >= maxTrackedSources {
		d.evictOldestDropPair()
	}
	hits = append(hits, now)

	cutoff := now.Add(-d.cfg.RepeatedDropsWindow)
	i := 0
	for i < len(hits) && hits[i].Before(cutoff) {
		i++
	}
	hits = hits[i:]
	d.dropPairs[key] = hits

	if len(hits) >= d.cfg.RepeatedDropsThreshold {
		target := fmt.Sprintf("%s -> port %d", e.SrcIP, e.DstPort)
		detail := fmt.Sprintf("%d attempts against %s:%d dropped in %s -- check whether this port is meant to be open",
			len(hits), e.DstIP, e.DstPort, d.cfg.RepeatedDropsWindow)
		var nat *flags.NATInfo
		if e.NatIP != "" || e.NatRaw != "" {
			nat = &flags.NATInfo{IP: e.NatIP, Port: e.NatPort, Raw: e.NatRaw}
		}
		isNew := d.fs.AddWithDetail(flags.TypeRepeatedDrops, target, detail,
			overshootConfidence(len(hits), d.cfg.RepeatedDropsThreshold),
			flags.Evidence{NAT: nat}, e.SrcCountry, now)
		d.maybeCheckReputation(flags.TypeRepeatedDrops, target, e.SrcIP, isNew)
	}
}

func (d *Detector) evictOldestDropPair() {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, hits := range d.dropPairs {
		if len(hits) == 0 {
			continue
		}
		last := hits[len(hits)-1]
		if first || last.Before(oldest) {
			oldestKey, oldest, first = k, last, false
		}
	}
	if oldestKey != "" {
		delete(d.dropPairs, oldestKey)
	}
}
