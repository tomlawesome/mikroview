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

// dropPairWindow tracks one (srcIP, dstPort) pair's recent drop/reject
// hits -- a countRing plus the lastActivity eviction needs, replacing a
// bare []time.Time hit list.
type dropPairWindow struct {
	hits         *countRing
	lastActivity time.Time
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
	w, ok := d.dropPairs[key]
	if !ok {
		if len(d.dropPairs) >= maxTrackedSources {
			evictOldestByActivity(d.dropPairs)
		}
		w = &dropPairWindow{hits: newCountRing(d.cfg.RepeatedDropsWindow)}
		d.dropPairs[key] = w
	}
	w.lastActivity = now
	w.hits.Add(now, true)
	count := w.hits.Count(now, d.cfg.RepeatedDropsWindow)

	if count >= d.cfg.RepeatedDropsThreshold {
		target := fmt.Sprintf("%s -> port %d", e.SrcIP, e.DstPort)
		detail := fmt.Sprintf("%d attempts against %s:%d dropped in %s -- check whether this port is meant to be open",
			count, e.DstIP, e.DstPort, d.cfg.RepeatedDropsWindow)
		var nat *flags.NATInfo
		if e.NatIP != "" || e.NatRaw != "" {
			nat = &flags.NATInfo{IP: e.NatIP, Port: e.NatPort, Raw: e.NatRaw}
		}
		isNew := d.fs.AddWithDetail(flags.TypeRepeatedDrops, target, detail,
			overshootConfidence(count, d.cfg.RepeatedDropsThreshold),
			flags.Evidence{NAT: nat}, e.SrcCountry, now)
		d.maybeCheckReputation(flags.TypeRepeatedDrops, target, e.SrcIP, isNew)
	}
}

func (w *dropPairWindow) lastActivityTime() time.Time { return w.lastActivity }
