// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// The two reads behind #1018's tools on the living topology: "where is
// this port used" and "where did this one line go".
//
// Both are whole-window scans of the ring rather than Query calls, for
// the reason HourTops already gives next door: a summary answered from
// Query's first `Limit` events would be a partial tally presented as a
// total, and the map draws it as though it were the window. The scan is
// bounded by what arrived in the window, walks backward from the newest
// event exactly as Query and HourTops do, and holds one RLock for the
// whole pass.
//
// Nothing here reaches past the log lines the router already sent. A
// port is only "seen" where a rule logged it, and the answer never
// implies anything about traffic no rule logs -- the doors that say
// where a rule *names* a port come from the pushed rule table instead,
// assembled in internal/api, and are deliberately a different fact.

// maxPortCandidates caps the port list the picker offers. The chips are
// a shortlist to click, not an inventory: a scanned network can present
// tens of thousands of distinct destination ports in a window, and a
// picker holding all of them is unusable as well as large on the wire.
// Busiest first, so the cut falls on the ports nobody asked about.
const maxPortCandidates = 24

// maxPortLines caps how many distinct lines the summary will count
// before it stops counting. `2 lines seen` is a headline, and the map
// draws nothing per line, so the exact figure past this bound buys
// nothing and the map is bounded by it instead of by traffic.
const maxPortLines = 10_000

// PortQuery selects the traffic a PortSummary is about.
//
// Ports empty means "no selection": the candidate list is still
// computed (it is what the picker offers), and nothing else is.
type PortQuery struct {
	Ports []int
	// Proto is "tcp", "udp", or "" for either. Matched
	// case-insensitively against the event's own protocol.
	Proto string
	// Device restricts the scan to one device's events; "" is every
	// device, the same "unset means no filter" convention Query uses.
	Device string
	// Since bounds the scan; zero means the whole held window.
	Since time.Time
	// Named is every port a pushed filter rule scopes to, so the
	// candidate list is built and *then* capped over both kinds at once.
	// Merging after the cap would re-add a busy port that had just
	// fallen off the list as though nothing had been logged on it.
	Named map[int]bool
	// NamedProto is the protocol a named-but-unseen port is offered
	// under: the one currently selected, or tcp when none is.
	NamedProto string
}

// PortCandidate is one entry in the picker: a destination port the
// window actually carried, with how many lines named it.
type PortCandidate struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"`
	Count uint64 `json:"count"`
	// Named is whether a pushed filter rule scopes to this port. Count 0
	// with Named true is a door nothing has knocked at, which is exactly
	// the thing worth asking about.
	Named bool `json:"named"`
}

// PortRib is one direction of one boundary pair, on the selected port.
//
// Out is empty for a line the router logged with no out-interface,
// which is the ordinary shape of a forward drop: the packet never
// reached one. That is a fact about the line, not a gap in it, so it
// travels as the empty string rather than being dropped from the
// answer -- the map draws that direction dying at the router.
type PortRib struct {
	In      string `json:"in"`
	Out     string `json:"out"`
	Events  uint64 `json:"events"`
	Accepts uint64 `json:"accepts"`
	Drops   uint64 `json:"drops"`
	// RefusedBy is the rule that refused this direction, from the most
	// recent refusal rather than the first -- same provenance and the
	// same reason as reality.ts's own refusedBy (#966): a rule table can
	// be edited under events already ingested, and naming a rule the
	// current table no longer has for a direction it no longer explains
	// would be a guess.
	RefusedBy string `json:"refusedBy,omitempty"`
}

// PortHost is one address seen on the selected port, either end of the
// line. The map lights the dots it already draws for these addresses;
// it never invents a dot from one.
type PortHost struct {
	IP      string `json:"ip"`
	Name    string `json:"name,omitempty"`
	Events  uint64 `json:"events"`
	Accepts uint64 `json:"accepts"`
	Drops   uint64 `json:"drops"`
}

// PortSummary is what one port selection did in the window.
type PortSummary struct {
	Candidates []PortCandidate `json:"candidates"`
	Events     uint64          `json:"events"`
	Accepts    uint64          `json:"accepts"`
	Drops      uint64          `json:"drops"`
	// Lines is distinct source → destination · port · proto lines, the
	// same grammar the baseline register uses, capped at maxPortLines.
	Lines int        `json:"lines"`
	Ribs  []PortRib  `json:"ribs"`
	Hosts []PortHost `json:"hosts"`
}

// Ports answers "where is this port used", over the whole held window.
func (s *Store) Ports(q PortQuery) PortSummary {
	want := make(map[int]struct{}, len(q.Ports))
	for _, p := range q.Ports {
		if p > 0 && p <= 65535 {
			want[p] = struct{}{}
		}
	}
	proto := strings.ToLower(strings.TrimSpace(q.Proto))
	device := q.Device

	out := PortSummary{Candidates: []PortCandidate{}, Ribs: []PortRib{}, Hosts: []PortHost{}}

	candidates := map[PortCandidate]uint64{}
	ribs := map[string]*PortRib{}
	hosts := map[string]*PortHost{}
	lines := map[string]struct{}{}
	linesFull := false

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.count == 0 {
		return out
	}
	idx := s.head - 1
	if idx < 0 {
		idx = s.capacity - 1
	}
	for i := 0; i < s.count; i++ {
		e := s.buf[idx]
		idx--
		if idx < 0 {
			idx = s.capacity - 1
		}
		if !q.Since.IsZero() && e.ReceivedAt.Before(q.Since) {
			break
		}
		if device != "" && e.DeviceID != device {
			continue
		}
		if e.DstPort <= 0 {
			continue
		}
		p := strings.ToLower(e.Protocol)
		// The picker offers tcp and udp and nothing else, so a port on
		// any other protocol is not a candidate: a chip that selected it
		// could never be reached through the protocol toggle beside it.
		if p == "tcp" || p == "udp" {
			candidates[PortCandidate{Port: e.DstPort, Proto: p}]++
		}
		if len(want) == 0 {
			continue
		}
		if _, ok := want[e.DstPort]; !ok {
			continue
		}
		if proto != "" && p != proto {
			continue
		}

		out.Events++
		accepted := e.Action == ActionAccept
		refused := e.Action == ActionDrop || e.Action == ActionReject
		if accepted {
			out.Accepts++
		}
		if refused {
			out.Drops++
		}

		if !linesFull {
			lines[e.SrcIP+">"+e.DstIP+"/"+strconv.Itoa(e.DstPort)+"/"+p] = struct{}{}
			if len(lines) >= maxPortLines {
				linesFull = true
			}
		}

		if e.InInterface != "" || e.OutInterface != "" {
			key := e.InInterface + "|" + e.OutInterface
			r := ribs[key]
			if r == nil {
				r = &PortRib{In: e.InInterface, Out: e.OutInterface}
				ribs[key] = r
			}
			r.Events++
			if accepted {
				r.Accepts++
			}
			if refused {
				r.Drops++
				// Newest first, so the first refusal met on a direction
				// is the most recent one.
				if r.RefusedBy == "" {
					r.RefusedBy = e.RuleLabel
				}
			}
		}

		for _, end := range [2]struct {
			ip, name string
		}{{e.SrcIP, e.SrcHostName}, {e.DstIP, e.DstHostName}} {
			if end.ip == "" {
				continue
			}
			h := hosts[end.ip]
			if h == nil {
				h = &PortHost{IP: end.ip}
				hosts[end.ip] = h
			}
			if h.Name == "" {
				h.Name = end.name
			}
			h.Events++
			if accepted {
				h.Accepts++
			}
			if refused {
				h.Drops++
			}
		}
	}

	// Every port the window carried, whether or not it survives the cap
	// below. A named port that was carried and then cut for being quiet
	// must not come back on the end as though nothing had been logged on
	// it -- that is a false statement about the network, made by the
	// truncation rather than by the data.
	carried := map[int]bool{}
	for c, n := range candidates {
		c.Count = n
		c.Named = q.Named[c.Port]
		carried[c.Port] = true
		out.Candidates = append(out.Candidates, c)
	}
	sort.Slice(out.Candidates, func(i, j int) bool {
		a, b := out.Candidates[i], out.Candidates[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.Proto < b.Proto
	})
	if len(out.Candidates) > maxPortCandidates {
		out.Candidates = out.Candidates[:maxPortCandidates]
	}

	// The ports a rule names but nothing carried go on the end, and the
	// cut above never reaches them: they are few (a rule table is
	// bounded, traffic is not), and they are the only reason policy is
	// offered beside traffic at all -- "knowing where a door is open,
	// even if unused, is useful information". Cutting them by volume
	// would drop exactly the ports nobody has knocked at, which is the
	// set the operator opened the picker to ask about.
	//
	// A named port the window carried is not re-added here: it is
	// already above with its real count, or it was cut for being quiet
	// and saying "nothing logged" about it would be untrue.
	fill := strings.ToLower(strings.TrimSpace(q.NamedProto))
	if fill != "tcp" && fill != "udp" {
		fill = "tcp"
	}
	unused := make([]int, 0, len(q.Named))
	for p := range q.Named {
		if !carried[p] {
			unused = append(unused, p)
		}
	}
	sort.Ints(unused)
	if len(unused) > maxPortCandidates {
		unused = unused[:maxPortCandidates]
	}
	for _, p := range unused {
		out.Candidates = append(out.Candidates, PortCandidate{Port: p, Proto: fill, Named: true})
	}

	out.Lines = len(lines)
	for _, r := range ribs {
		out.Ribs = append(out.Ribs, *r)
	}
	sort.Slice(out.Ribs, func(i, j int) bool {
		a, b := out.Ribs[i], out.Ribs[j]
		if a.Events != b.Events {
			return a.Events > b.Events
		}
		if a.In != b.In {
			return a.In < b.In
		}
		return a.Out < b.Out
	})
	for _, h := range hosts {
		out.Hosts = append(out.Hosts, *h)
	}
	sort.Slice(out.Hosts, func(i, j int) bool {
		a, b := out.Hosts[i], out.Hosts[j]
		if a.Events != b.Events {
			return a.Events > b.Events
		}
		return a.IP < b.IP
	})
	return out
}

// TraceQuery names the one line to trace: an event by id, or -- for a
// caller holding a pair and a port rather than an id, which is what the
// map's own unplanned callout has -- the most recent line matching the
// pair.
type TraceQuery struct {
	ID      uint64
	In, Out string
	Port    int
	Proto   string
	Since   time.Time
	Device  string
	SrcIP   string
	DstIP   string
	// NoOut asks for a line that never reached an out-interface at all --
	// the ordinary shape of a forward drop. Distinct from leaving Out
	// unset, which means "any": a caller tracing the pair a callout names
	// would otherwise land on a newer line that did leave the router, and
	// the drawing and the callout would disagree about what happened.
	NoOut bool
}

// maxTraceRelated caps how many rows each column of the trace's own list
// (round 56, A1) carries. Eight is the drawing's own number: a shortlist
// to scan, not the full tally -- Like and SameMinuteTotal already carry
// the exact counts, and each column's own "more in the stream ▸" footer
// is where the rest live.
const maxTraceRelated = 8

// TraceResult is one hop through the router, as the router knows it.
//
// Event is nil when nothing in the window matches -- an honest miss,
// distinct from a trace of an event carrying no path.
type TraceResult struct {
	Event *Event
	// Like is how many events in the window are on the same line
	// (source → destination · port · proto), this one included. The
	// crumb says "and N more like it" from Like-1.
	Like uint64
	// DstReached is how many of those the router accepted -- with Like,
	// the two tallies the lit host cards carry ("cam-porch · 14x today",
	// "tom-desktop · never reached").
	DstReached uint64
	// SameLine is the line's own events -- the same match Like counts --
	// newest first, this one included so the list can mark it, capped at
	// maxTraceRelated. The list's SAME LINE column.
	SameLine []Event
	// SameMinute is the traced event's source's other events that fall in
	// its own clock minute, excluding anything already counted in
	// SameLine, newest first, capped at maxTraceRelated. The list's SAME
	// MINUTE column.
	SameMinute []Event
	// SameMinuteTotal is the exact count SameMinute is capped from, for
	// the column's own header.
	SameMinuteTotal uint64
}

// Trace answers "where did this one line go".
//
// Two passes under one lock: the subject first, then the line's tallies.
// One pass cannot do it -- the scan runs newest-first, so an event named
// by id has newer siblings on the same line already behind the cursor by
// the time its own line is known.
func (s *Store) Trace(q TraceQuery) TraceResult {
	proto := strings.ToLower(strings.TrimSpace(q.Proto))

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.count == 0 {
		return TraceResult{}
	}

	matches := func(e *Event) bool {
		if q.ID != 0 {
			return e.ID == q.ID
		}
		if q.Device != "" && e.DeviceID != q.Device {
			return false
		}
		if q.In != "" && e.InInterface != q.In {
			return false
		}
		if q.NoOut {
			if e.OutInterface != "" {
				return false
			}
		} else if q.Out != "" && e.OutInterface != q.Out {
			return false
		}
		if q.Port != 0 && e.DstPort != q.Port {
			return false
		}
		if proto != "" && strings.ToLower(e.Protocol) != proto {
			return false
		}
		if q.SrcIP != "" && e.SrcIP != q.SrcIP {
			return false
		}
		if q.DstIP != "" && e.DstIP != q.DstIP {
			return false
		}
		return true
	}

	start := s.head - 1
	if start < 0 {
		start = s.capacity - 1
	}

	var subject *Event
	idx := start
	for i := 0; i < s.count; i++ {
		e := &s.buf[idx]
		idx--
		if idx < 0 {
			idx = s.capacity - 1
		}
		// An id lookup deliberately ignores Since: the operator named
		// this event, and refusing it because it fell out of a default
		// window would be the tool declining to answer the question it
		// was asked.
		if q.ID == 0 && !q.Since.IsZero() && e.ReceivedAt.Before(q.Since) {
			break
		}
		if matches(e) {
			copied := *e
			subject = &copied
			break
		}
	}
	if subject == nil {
		return TraceResult{}
	}

	out := TraceResult{Event: subject, SameLine: []Event{}, SameMinute: []Event{}}
	minute := subject.Time.Truncate(time.Minute)
	idx = start
	for i := 0; i < s.count; i++ {
		e := &s.buf[idx]
		idx--
		if idx < 0 {
			idx = s.capacity - 1
		}
		// Bounded by the same window the subject was found in, and for
		// the same reason: an id lookup answers about the event the
		// operator named, so its own line is tallied over everything
		// held rather than over a window that had already excluded it --
		// which would report "and 0 more like it" about a line the
		// buffer plainly holds fourteen of.
		if q.ID == 0 && !q.Since.IsZero() && e.ReceivedAt.Before(q.Since) {
			break
		}
		// The device filter applies here as it does to the subject: two
		// routers can log the same five-tuple, and "and N more like it"
		// must not quietly count the other one's.
		if q.Device != "" && e.DeviceID != q.Device {
			continue
		}
		sameLine := e.SrcIP == subject.SrcIP && e.DstIP == subject.DstIP && e.DstPort == subject.DstPort &&
			strings.EqualFold(e.Protocol, subject.Protocol)
		if sameLine {
			out.Like++
			if e.Action == ActionAccept {
				out.DstReached++
			}
			if len(out.SameLine) < maxTraceRelated {
				out.SameLine = append(out.SameLine, *e)
			}
			continue
		}
		// SAME MINUTE is the sender's other lines, whatever they went to --
		// same line is excluded above so the two columns never repeat a
		// row between them.
		if e.SrcIP == subject.SrcIP && e.Time.Truncate(time.Minute).Equal(minute) {
			out.SameMinuteTotal++
			if len(out.SameMinute) < maxTraceRelated {
				out.SameMinute = append(out.SameMinute, *e)
			}
		}
	}
	return out
}
