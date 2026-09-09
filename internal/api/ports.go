// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/store"
)

// The two reads behind #1018's tools on the living topology (round 53):
// the port filter -- "show me where this port is used" -- and the event
// trace -- "show me the path this one line took".
//
// Both are answers about what the router already told mikroview.
// Neither reaches the network, and neither invents a hop: an event's
// path, as the router knows it, is exactly one -- in-interface, the
// rule that decided, NAT if any, out-interface -- and a refused packet
// ends at the router. Nothing beyond it is knowable, so nothing beyond
// it is returned.
//
// The port answer is deliberately two facts kept apart (owner,
// 2026-09-08): *seen*, which is traffic a rule logged, and *policy*,
// which is a rule naming the port whether or not anything hit it. The
// map draws them differently, so they travel differently -- `ribs` and
// `hosts` versus `doors` -- and a caller can never mistake a door for
// traffic.

// maxSelectedPorts caps how many ports one selection may name.
//
// The picker offers chips and a typed list; a range typed into that
// field expands here, and an operator who types 1-65535 is asking for
// "every port", which is the unfiltered map they already have. The cap
// refuses the whole selection rather than silently truncating it: a map
// filtered to a different set from the one the pill says would be worse
// than a refusal.
const maxSelectedPorts = 64

// portDoor is one pushed filter rule that names the selected port.
//
// It is not traffic and never counts as any. In/Out are the rule's own
// interface conditions, which is what decides where the door is drawn:
// a rule that drops stops the packet on the way in, a rule that accepts
// opens the way out, and the map places the two accordingly.
type portDoor struct {
	Device string `json:"device"`
	// Label is the rule as an operator would name it in RouterOS --
	// `#12`, from its ordinal in the pushed table. The ordinal is the
	// display order RouterOS itself reports, which is what "go look at
	// rule 12" means.
	Label   string `json:"label"`
	Ordinal int    `json:"ordinal"`
	// Action is the rule's own, verbatim: "accept", "drop", "reject",
	// and whatever else a table carries. The map draws a leaf for
	// accept and a bar for anything that refuses.
	Action  string `json:"action"`
	Chain   string `json:"chain"`
	In      string `json:"in,omitempty"`
	Out     string `json:"out,omitempty"`
	DstPort string `json:"dstPort"`
	// Who is the short "wan → any drop" phrase the door's title carries,
	// built from the rule's own interfaces and action -- never from an
	// inference about what the rule is for.
	Who string `json:"who"`
	// Comment is the operator's own on the rule, when there is one.
	Comment string `json:"comment,omitempty"`
}

type portSelection struct {
	Ports []int  `json:"ports"`
	Proto string `json:"proto"`
	// Label is what the pill reads: `445/tcp`, `22,23/tcp`, or `445` on
	// a selection that named no protocol.
	Label string `json:"label"`
}

type portsResponse struct {
	GeneratedAt int64 `json:"generatedAt"`
	// WindowSeconds is the event buffer's own window -- the "in the
	// window" every wording on this feature refers to.
	WindowSeconds int `json:"windowSeconds"`
	// Candidates is what the picker offers: every destination port the
	// window carried on tcp or udp, busiest first, plus every port a
	// pushed rule names. Ports a rule names but nothing hit carry
	// count 0 -- which is the whole reason policy is drawn at all.
	Candidates []store.PortCandidate `json:"candidates"`
	// Selection, Ribs, Hosts, Doors and the counts are absent (null /
	// zero) until ports are actually selected. An empty selection is not
	// an empty answer: it is the picker's own state.
	Selection *portSelection   `json:"selection,omitempty"`
	Events    uint64           `json:"events"`
	Accepts   uint64           `json:"accepts"`
	Drops     uint64           `json:"drops"`
	Lines     int              `json:"lines"`
	Ribs      []store.PortRib  `json:"ribs"`
	Hosts     []store.PortHost `json:"hosts"`
	Doors     []portDoor       `json:"doors"`
}

// handlePorts serves the port filter's whole answer (#1018).
//
// Viewer tier, the same asymmetry GET /api/baseline/off and GET
// /api/hosts already carry: a non-admin looking at the map is exactly
// who needs to ask where a port is used. Deliberately not on
// readOnlyRoutes for the same reason as those two -- the hosts and
// addresses in it are a partial inventory of the operator's private
// address space, which no bearer token has ever been able to read.
func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()

	ports, ok := parsePortSelection(w, qs.Get("port"))
	if !ok {
		return
	}
	proto := strings.ToLower(strings.TrimSpace(qs.Get("proto")))
	if proto != "" && proto != "tcp" && proto != "udp" {
		badQueryParam(w, "proto", "tcp or udp")
		return
	}
	var since time.Time
	if v := qs.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badQueryParam(w, "since", "RFC 3339")
			return
		}
		since = t
	}

	// The named ports go *into* the scan rather than being merged onto
	// its answer: the store caps the candidate list, and merging after
	// the cap re-adds a busy port that had just fallen off it as though
	// nothing had been logged on it.
	summary := s.Store.Ports(store.PortQuery{
		Ports:      ports,
		Proto:      proto,
		Device:     qs.Get("device"),
		Since:      since,
		Named:      s.namedPorts(qs.Get("device")),
		NamedProto: proto,
	})

	out := portsResponse{
		GeneratedAt:   time.Now().Unix(),
		WindowSeconds: int(s.Store.Stats().Window.Seconds()),
		Events:        summary.Events,
		Accepts:       summary.Accepts,
		Drops:         summary.Drops,
		Lines:         summary.Lines,
		Ribs:          summary.Ribs,
		Hosts:         summary.Hosts,
		Candidates:    summary.Candidates,
		Doors:         []portDoor{},
	}

	if len(ports) > 0 {
		out.Selection = &portSelection{Ports: ports, Proto: proto, Label: portLabel(ports, proto)}
		out.Doors = s.doorsFor(qs.Get("device"), ports, proto)
	}
	writeJSON(w, http.StatusOK, out)
}

// parsePortSelection reads the `port` parameter: a comma-separated list
// of ports and ranges, exactly the shape RouterOS itself accepts in a
// dst-port and the shape the picker's own text field takes.
//
// Present and unparseable is a 400, not an ignored filter -- the
// standing rule badQueryParam documents: an operator who believes they
// filtered and is shown everything has been lied to, and on a map whose
// whole claim is "this is where that port is used" the lie is the
// finding.
func parsePortSelection(w http.ResponseWriter, spec string) ([]int, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, true
	}
	seen := map[int]struct{}{}
	var out []int
	add := func(p int) bool {
		if _, dup := seen[p]; dup {
			return true
		}
		seen[p] = struct{}{}
		out = append(out, p)
		return len(out) <= maxSelectedPorts
	}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, isRange := strings.Cut(part, "-"); isRange {
			loN, err1 := strconv.Atoi(strings.TrimSpace(lo))
			hiN, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 != nil || err2 != nil || loN < 1 || hiN < loN || hiN > 65535 {
				badQueryParam(w, "port", "ports and ranges between 1 and 65535, comma separated")
				return nil, false
			}
			for p := loN; p <= hiN; p++ {
				if !add(p) {
					badQueryParam(w, "port", "at most "+strconv.Itoa(maxSelectedPorts)+" ports")
					return nil, false
				}
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 65535 {
			badQueryParam(w, "port", "ports and ranges between 1 and 65535, comma separated")
			return nil, false
		}
		if !add(n) {
			badQueryParam(w, "port", "at most "+strconv.Itoa(maxSelectedPorts)+" ports")
			return nil, false
		}
	}
	sort.Ints(out)
	return out, true
}

// portLabel is what the pill reads. One port reads `445/tcp`; several
// read the list as typed back, so the pill says what was asked for
// rather than a count of it. A range collapses back to `lo-hi` only
// where the selection is exactly contiguous, which is the case an
// operator typed as a range.
func portLabel(ports []int, proto string) string {
	if len(ports) == 0 {
		return ""
	}
	var body string
	switch {
	case len(ports) == 1:
		body = strconv.Itoa(ports[0])
	case ports[len(ports)-1]-ports[0] == len(ports)-1:
		body = strconv.Itoa(ports[0]) + "-" + strconv.Itoa(ports[len(ports)-1])
	default:
		parts := make([]string, len(ports))
		for i, p := range ports {
			parts[i] = strconv.Itoa(p)
		}
		body = strings.Join(parts, ",")
	}
	if proto == "" {
		return body
	}
	return body + "/" + proto
}

// namedPorts is every port a pushed filter rule scopes to, across every
// device that has pushed a table (or one, when the caller named it).
//
// Ranges are deliberately not expanded: a rule scoping 1000-2000 names
// a thousand ports and none of them is a chip anyone wants to click.
// The rule still draws its door the moment such a port is selected --
// doorsFor asks the spec itself, not this list.
func (s *Server) namedPorts(device string) map[int]bool {
	out := map[int]bool{}
	if s.RouterState == nil {
		return out
	}
	for _, d := range s.routerDevices(device) {
		rules, _, ok := s.RouterState.FilterRules(d)
		if !ok {
			continue
		}
		for _, rule := range rules {
			if rule.Disabled {
				continue
			}
			for _, part := range strings.Split(string(rule.DstPort), ",") {
				part = strings.TrimSpace(part)
				if part == "" || strings.Contains(part, "-") {
					continue
				}
				if n, err := strconv.Atoi(part); err == nil && n >= 1 && n <= 65535 {
					out[n] = true
				}
			}
		}
	}
	return out
}

// doorsFor is every pushed filter rule that *names* one of the selected
// ports -- lists and ranges included, which is what ingest.NamesPorts
// answers. A rule with no dst-port at all is not a door: it covers
// every port and so says nothing about this one.
//
// A disabled rule is left out. It is a row in a table, not a door: an
// open door drawn where nothing can pass would be exactly the "policy
// reading as traffic" the design forbids.
func (s *Server) doorsFor(device string, ports []int, proto string) []portDoor {
	out := []portDoor{}
	if s.RouterState == nil {
		return out
	}
	for _, d := range s.routerDevices(device) {
		rules, _, ok := s.RouterState.FilterRules(d)
		if !ok {
			continue
		}
		for _, rule := range rules {
			if rule.Disabled {
				continue
			}
			// A rule scoped to another protocol cannot be a door for
			// this one; a rule scoped to none is a door for both, the
			// same "unset means any" reading RouterOS itself has.
			rp := strings.ToLower(strings.TrimSpace(rule.Protocol))
			if proto != "" && rp != "" && rp != proto {
				continue
			}
			if !ingest.NamesPorts(string(rule.DstPort), ports) {
				continue
			}
			out = append(out, portDoor{
				Device:  d,
				Label:   "#" + strconv.Itoa(int(rule.Ordinal)),
				Ordinal: int(rule.Ordinal),
				Action:  rule.Action,
				Chain:   rule.Chain,
				In:      rule.InInterface,
				Out:     rule.OutInterface,
				DstPort: string(rule.DstPort),
				Who:     doorWho(rule.InInterface, rule.OutInterface, rule.Action),
				Comment: rule.Comment,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Device != out[j].Device {
			return out[i].Device < out[j].Device
		}
		return out[i].Ordinal < out[j].Ordinal
	})
	return out
}

// doorWho is the door's own one-line title: `wan → any drop`. "any"
// where the rule names no interface, because that is what an unset
// interface condition means -- never a guess at which one was intended.
func doorWho(in, out, action string) string {
	if in == "" {
		in = "any"
	}
	if out == "" {
		out = "any"
	}
	return in + " → " + out + " " + action
}

// routerDevices is the devices a pushed-table read covers: the one
// named, or every device that has pushed anything.
func (s *Server) routerDevices(device string) []string {
	if device != "" {
		return []string{device}
	}
	if s.RouterState == nil {
		return nil
	}
	return s.RouterState.Devices()
}

// traceResponse is one line's hop through the router.
//
// Found is false when nothing in the window matches what was asked for
// -- an honest miss the map says in words, rather than an empty trace
// drawn as though the line had gone nowhere.
type traceResponse struct {
	Found bool `json:"found"`
	// Verdict is "accepted" for a line the router let through and
	// "refused" for one it stopped. Anything else the parser could not
	// classify as either travels as "" and the map draws no verdict
	// colour rather than picking one.
	Verdict string       `json:"verdict,omitempty"`
	Event   *store.Event `json:"event,omitempty"`
	// Like is how many more events in the window are on the same line --
	// the crumb's "and 13 more like it", already the count *minus this
	// one*, so nothing has to subtract on the far side.
	Like uint64 `json:"like"`
	// SrcSeen is how many times this line was seen from its source in
	// the window, and DstReached how many of those the router accepted.
	// DstReached 0 is what "never reached" is drawn from.
	SrcSeen    uint64 `json:"srcSeen"`
	DstReached uint64 `json:"dstReached"`
	// SameLine is the list's SAME LINE column (round 56, A1): the line's
	// own events, newest first, the traced one included so the client can
	// mark it, capped at eight.
	SameLine []store.Event `json:"sameLine"`
	// SameMinute is the list's SAME MINUTE column: the source's other
	// events in the traced event's own clock minute, excluding anything
	// already in SameLine, newest first, capped at eight. SameMinuteTotal
	// is the exact count that is capped from.
	SameMinute      []store.Event `json:"sameMinute"`
	SameMinuteTotal uint64        `json:"sameMinuteTotal"`
}

// handleTrace serves one event's path (#1018). Same tier and same
// reasoning as handlePorts above.
//
// Two ways to name the line, because the two places the trace opens
// from hold different things: a stream row has the event's own id, and
// the map's unplanned callout has a boundary pair and a port. Either
// resolves to one event, and the answer is that event's own hop.
func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := store.TraceQuery{
		In:     qs.Get("in"),
		Out:    qs.Get("out"),
		Proto:  strings.ToLower(strings.TrimSpace(qs.Get("proto"))),
		Device: qs.Get("device"),
		SrcIP:  qs.Get("src"),
		DstIP:  qs.Get("dst"),
	}
	// An out-interface the caller states is absent is a different ask
	// from one it did not state: a forward drop never reached one, and
	// without this the trace can land on a newer line that did leave the
	// router -- disagreeing with the callout that opened it.
	if qs.Get("noOut") == "1" {
		q.NoOut = true
	}
	if v := qs.Get("event"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 {
			badQueryParam(w, "event", "a positive integer")
			return
		}
		q.ID = n
	}
	if v := qs.Get("port"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 {
			badQueryParam(w, "port", "a port between 1 and 65535")
			return
		}
		q.Port = n
	}
	if v := qs.Get("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			badQueryParam(w, "since", "RFC 3339")
			return
		}
		q.Since = t
	}
	if q.ID == 0 && q.In == "" && q.Out == "" && q.Port == 0 && q.SrcIP == "" && q.DstIP == "" && !q.NoOut {
		http.Error(w, "trace needs an event id, or a pair to look one up by", http.StatusBadRequest)
		return
	}

	res := s.Store.Trace(q)
	if res.Event == nil {
		writeJSON(w, http.StatusOK, traceResponse{Found: false, SameLine: []store.Event{}, SameMinute: []store.Event{}})
		return
	}
	out := traceResponse{
		Found:           true,
		Verdict:         traceVerdict(res.Event.Action),
		Event:           res.Event,
		SrcSeen:         res.Like,
		DstReached:      res.DstReached,
		SameLine:        res.SameLine,
		SameMinute:      res.SameMinute,
		SameMinuteTotal: res.SameMinuteTotal,
	}
	if res.Like > 0 {
		out.Like = res.Like - 1
	}
	writeJSON(w, http.StatusOK, out)
}

// traceVerdict maps the router's own action onto the two the drawing
// has ink for. A log, mark or NAT line is neither: it says what kind of
// rule logged the packet, not whether the packet passed, so it travels
// as no verdict at all rather than being coloured as one.
func traceVerdict(a store.Action) string {
	switch a {
	case store.ActionAccept:
		return "accepted"
	case store.ActionDrop, store.ActionReject:
		return "refused"
	default:
		return ""
	}
}
