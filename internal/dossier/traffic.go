// SPDX-License-Identifier: AGPL-3.0-only

package dossier

import (
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/tomlawesome/mikroview/internal/oui"
)

// fingerprint is the reduced form of a host's traffic that the identity
// heuristics read. It exists so the heuristic table matches against
// named signals rather than re-walking events, which is what keeps that
// table a table.
type fingerprint struct {
	events int
	span   time.Duration
	// signals maps each observed signal to the evidence line that
	// observed it.
	signals map[Signal]string
	// servedPorts are ports something else reached on this host, and
	// webPorts the subset that would answer a browser. Both feed the
	// suggested probe.
	servedPorts []int
	webPorts    []int
	// distinctPeers and publicPeers back the thin-evidence rule and the
	// "few endpoints" signal.
	distinctPeers int
	publicPeers   int
}

// note records a signal with the evidence line that observed it. The
// first line wins: signals come from aggregated port use, so a second
// line for the same signal would be another port saying the same thing.
func (fp *fingerprint) note(s Signal, detail string) {
	if _, seen := fp.signals[s]; !seen {
		fp.signals[s] = detail
	}
}

// macFacts is what the identity heuristics may read from the hardware
// address: whether a vendor exists at all, and who it is when one does.
type macFacts struct {
	parsed bool
	mac    oui.MAC
	vendor oui.Vendor
}

// peerAgg accumulates one peer's traffic while the event walk runs.
type peerAgg struct {
	Peer
	ports map[int]struct{}
}

// portKey identifies one port in one direction on one protocol.
type portKey struct {
	port      int
	protocol  string
	direction string
}

// portAgg accumulates one port's use.
type portAgg struct {
	PortUse
	peers map[string]struct{}
}

func buildTraffic(in Input, absent []string) (Traffic, fingerprint, []string) {
	fp := fingerprint{signals: map[Signal]string{}}

	if len(in.Events) == 0 {
		t := Traffic{Note: "no retained event involves this address, so there is no traffic fingerprint to read"}
		t.Cadence.Note = t.Note
		absent = append(absent, "traffic: no retained event involves this address")
		return t, fp, absent
	}

	dests := map[string]*peerAgg{}
	talkers := map[string]*peerAgg{}
	ports := map[portKey]*portAgg{}
	var times []time.Time

	for _, e := range in.Events {
		t := eventTime(e)
		if !t.IsZero() {
			times = append(times, t)
		}

		var peers map[string]*peerAgg
		var peerIP, direction, country string
		var port int
		switch {
		case e.SrcIP == in.IP:
			// The host reached something: the peer is the destination
			// and the interesting port is the one it reached.
			peers, peerIP, direction, country, port = dests, e.DstIP, "out", e.DstCountry, e.DstPort
		case e.DstIP == in.IP:
			// Something reached the host: the peer is the source, and
			// the interesting port is still the destination one --
			// that is the port *on this host*, which is what says what
			// it serves.
			peers, peerIP, direction, country, port = talkers, e.SrcIP, "in", e.SrcCountry, e.DstPort
		default:
			// The event matched this address on some other field (a NAT
			// address, say). It is still about this host, but it names
			// no peer this card can attribute, so it counts towards
			// cadence and nothing else.
			continue
		}

		if peerIP != "" {
			p := peers[peerIP]
			if p == nil {
				p = &peerAgg{
					Peer:  Peer{IP: peerIP, Scope: scopeOf(peerIP), Country: country},
					ports: map[int]struct{}{},
				}
				if in.Lookups.PeerName != nil {
					p.Name = in.Lookups.PeerName(peerIP)
				}
				peers[peerIP] = p
			}
			p.Events++
			if port > 0 {
				p.ports[port] = struct{}{}
			}
			if t.After(p.LastSeen) {
				p.LastSeen = t
			}
		}

		if port > 0 {
			k := portKey{port, e.Protocol, direction}
			pu := ports[k]
			if pu == nil {
				name := e.DstPortName
				if name == "" && in.Lookups.PortName != nil {
					name = in.Lookups.PortName(port)
				}
				pu = &portAgg{
					PortUse: PortUse{Port: port, Protocol: e.Protocol, Name: name, Direction: direction},
					peers:   map[string]struct{}{},
				}
				ports[k] = pu
			}
			pu.Events++
			if peerIP != "" {
				pu.peers[peerIP] = struct{}{}
			}
			if t.After(pu.LastSeen) {
				pu.LastSeen = t
			}
		}
	}

	tr := Traffic{Known: true}
	if in.EventsTruncated {
		tr.Note = "read from the most recent retained events for this host, not from every event in the window -- a busy host's older traffic is not counted here"
	}
	tr.Destinations, tr.MoreDestinations = topPeers(dests)
	tr.Talkers, tr.MoreTalkers = topPeers(talkers)

	portList := make([]PortUse, 0, len(ports))
	for k, pu := range ports {
		pu.Peers = len(pu.peers)
		portList = append(portList, pu.PortUse)
		if k.direction == "in" {
			fp.servedPorts = appendUnique(fp.servedPorts, k.port)
			if isWebPort(k.port) {
				fp.webPorts = appendUnique(fp.webPorts, k.port)
			}
		}
	}
	sort.Slice(portList, func(i, j int) bool {
		if portList[i].Events != portList[j].Events {
			return portList[i].Events > portList[j].Events
		}
		return portList[i].Port < portList[j].Port
	})
	if len(portList) > maxPorts {
		tr.MorePorts = len(portList) - maxPorts
		portList = portList[:maxPorts]
	}
	tr.Ports = portList
	sort.Ints(fp.servedPorts)
	sort.Ints(fp.webPorts)

	tr.Cadence = cadenceOf(times)
	fp.events = len(in.Events)
	if tr.Cadence.Known {
		fp.span = time.Duration(tr.Cadence.SpanSeconds * float64(time.Second))
	}
	fp.distinctPeers = len(dests) + len(talkers)
	for _, p := range dests {
		if p.Scope == "internet" {
			fp.publicPeers++
		}
	}

	// Signals are read off aggregated port use rather than raw events,
	// so one evidence line covers every event that produced it.
	for k, pu := range ports {
		sig, ok := signalForPort(k.port, k.protocol, k.direction)
		if !ok {
			continue
		}
		fp.note(sig, fmt.Sprintf("%s %s on port %d (%s), %d events",
			directionVerb(k.direction), plural(len(pu.peers), "host"), k.port, portLabel(pu.Name, sig), pu.Events))
	}
	if fp.publicPeers > 0 && fp.publicPeers <= 3 {
		fp.note(SigFewInternetPeers, fmt.Sprintf("reaches only %s outside the LAN", plural(fp.publicPeers, "host")))
	}
	if fp.publicPeers == 0 && len(dests) > 0 {
		fp.note(SigLANOnly, "every destination it reached is on the local network")
	}

	return tr, fp, absent
}

// topPeers sorts by event count, caps the list, and reports how many
// were left out so a truncated list is never read as a whole one.
func topPeers(m map[string]*peerAgg) ([]Peer, int) {
	if len(m) == 0 {
		return nil, 0
	}
	out := make([]Peer, 0, len(m))
	for _, p := range m {
		peer := p.Peer
		for port := range p.ports {
			peer.Ports = append(peer.Ports, port)
		}
		sort.Ints(peer.Ports)
		out = append(out, peer)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Events != out[j].Events {
			return out[i].Events > out[j].Events
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > maxPeers {
		return out[:maxPeers], len(out) - maxPeers
	}
	return out, 0
}

func appendUnique(list []int, v int) []int {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// scopeOf is this package's own copy of the public/private predicate,
// following the codebase's precedent that each package keeps its own
// rather than exporting one across a boundary -- here it decides
// display scope, nothing security-relevant.
func scopeOf(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "unknown"
	}
	addr = addr.Unmap()
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsMulticast() || addr.IsUnspecified() {
		return "local"
	}
	return "internet"
}

func isWebPort(port int) bool {
	switch port {
	case 80, 443, 8000, 8080, 8081, 8443:
		return true
	}
	return false
}

// cadenceOf turns event timestamps into the coarse shape the Cadence
// type describes. Fewer than three events cannot describe a rhythm, and
// saying so is better than dividing by one.
func cadenceOf(times []time.Time) Cadence {
	if len(times) < 3 {
		return Cadence{Note: "too few retained events to say anything about cadence"}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	span := times[len(times)-1].Sub(times[0])
	if span <= 0 {
		return Cadence{Note: "every retained event carries the same timestamp, so cadence cannot be read"}
	}

	gaps := make([]float64, 0, len(times)-1)
	var total float64
	for i := 1; i < len(times); i++ {
		g := times[i].Sub(times[i-1]).Seconds()
		gaps = append(gaps, g)
		total += g
	}
	sort.Float64s(gaps)
	median := gaps[len(gaps)/2]

	c := Cadence{
		Known:            true,
		SpanSeconds:      span.Seconds(),
		MeanGapSeconds:   total / float64(len(gaps)),
		MedianGapSeconds: median,
	}
	// Regular means most gaps sit near the median -- a device on a
	// timer. Bursty means they do not, which is what human-driven or
	// event-driven traffic looks like. The thresholds are round numbers
	// chosen to separate those two cases legibly, not fitted to data,
	// and the shape is only ever shown as shape, never as a claim about
	// what the device is doing.
	var near int
	for _, g := range gaps {
		if median > 0 && g >= median*0.5 && g <= median*1.5 {
			near++
		}
	}
	switch {
	case median > 0 && float64(near)/float64(len(gaps)) >= 0.6:
		c.Shape = "regular"
		c.Note = "talks about every " + roundDuration(median)
	case median > 15*60:
		c.Shape = "occasional"
		c.Note = "long, uneven gaps between events"
	default:
		c.Shape = "bursty"
		c.Note = "traffic arrives in bursts rather than on a timer"
	}
	return c
}

func roundDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < time.Hour:
		return d.Round(time.Minute).String()
	}
	return d.Round(time.Hour).String()
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func directionVerb(direction string) string {
	if direction == "in" {
		return "reached by"
	}
	return "reached"
}

func portLabel(name string, s Signal) string {
	if name != "" {
		return name
	}
	return string(s)
}
