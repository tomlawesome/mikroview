// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/tomlawesome/mikroview/internal/dossier"
	"github.com/tomlawesome/mikroview/internal/store"
)

// dossierEventLimit is how much of a host's retained traffic the
// fingerprint reads. It is a sample, not a census, and the response
// says so (dossier.Traffic.Note) when it is hit -- the shape of a
// host's traffic is legible from a couple of thousand events, and
// walking every event of a chatty host on every card open is a cost
// with no matching gain.
const dossierEventLimit = 2000

// handleHostDossier assembles the evidence card for one address
// (issue #410): its traffic fingerprint, hardware address and vendor,
// the names it goes by with their provenance, whether its address is a
// lease, when it was first and last seen, which firewall rules its
// traffic matched, and a suggested identity with the evidence behind
// it.
//
// This handler only gathers. Every judgement lives in internal/dossier,
// which is pure and testable without a server; that split is what keeps
// the heuristics reviewable as a table rather than as handler code.
//
// An address nobody has heard of is answered with 200 and a dossier
// that says, block by block, that nothing is known -- not a 404. "We
// have never seen this address" is an answer to the operator's
// question, and one they often need: it is the difference between a
// host mikroview is missing and a host that does not exist.
func (s *Server) handleHostDossier(w http.ResponseWriter, r *http.Request) {
	ip := hostDossierIP(r.PathValue("ip"))
	if _, err := netip.ParseAddr(ip); err != nil {
		http.Error(w, "not an IP address", http.StatusBadRequest)
		return
	}

	in := dossier.Input{IP: ip, Now: time.Now()}
	in.Events, in.WindowStart, in.EventsTruncated = s.dossierEvents(ip)
	in.EventMAC = mostRecentSrcMAC(ip, in.Events)
	in.Presence = s.dossierPresence(ip)
	in.MACHistory = s.dossierMACHistory(in.EventMAC)
	in.Routers = s.dossierRouters(ip)
	in.Name = s.dossierName(ip, in.Events)
	if s.OUI != nil {
		in.Vendors = s.OUI
	}
	in.Lookups = s.dossierLookups(in.Events)

	writeJSON(w, http.StatusOK, dossier.Assemble(in))
}

// hostDossierIP accepts either a bare address or a host-register key
// (`<iface>|<ip>`), because both are in the operator's hands: the
// register's own endpoints are keyed that way, and a UI passing the key
// it already holds should get the dossier rather than a 400.
func hostDossierIP(raw string) string {
	if i := strings.LastIndex(raw, "|"); i >= 0 {
		return raw[i+1:]
	}
	return raw
}

func (s *Server) dossierEvents(ip string) (events []store.Event, windowStart time.Time, truncated bool) {
	if s.Store == nil {
		return nil, time.Time{}, false
	}
	res := s.Store.Query(store.Query{IP: ip, Limit: dossierEventLimit})
	return res.Events, res.WindowStart, res.HasMore
}

// mostRecentSrcMAC takes the MAC off this host's own most recent event
// carrying one -- a direct observation of the address, unlike an ARP
// table's, which is the router's view of it.
func mostRecentSrcMAC(ip string, events []store.Event) string {
	var newest time.Time
	var mac string
	for _, e := range events {
		if e.SrcIP != ip || e.SrcMAC == "" {
			continue
		}
		t := e.Time
		if t.IsZero() {
			t = e.ReceivedAt
		}
		if mac == "" || t.After(newest) {
			mac, newest = e.SrcMAC, t
		}
	}
	return mac
}

func (s *Server) dossierPresence(ip string) []dossier.Presence {
	if s.Hosts == nil {
		return nil
	}
	var out []dossier.Presence
	for _, h := range s.Hosts.List() {
		if h.IP != ip {
			continue
		}
		out = append(out, dossier.Presence{
			Iface:     h.Iface,
			FirstSeen: h.FirstSeen,
			LastSeen:  h.LastSeen,
			Events:    h.Events,
		})
	}
	return out
}

func (s *Server) dossierMACHistory(mac string) *dossier.MACHistory {
	if s.MACRegistry == nil || mac == "" {
		return nil
	}
	want := strings.ToLower(mac)
	for _, e := range s.MACRegistry.List() {
		if strings.ToLower(e.MAC) != want {
			continue
		}
		return &dossier.MACHistory{FirstSeen: e.FirstSeen, LastSeen: e.LastSeen}
	}
	return nil
}

// dossierRouters reads each router's pushed DHCP and ARP tables for
// this address. The Pushed flags carry the distinction the card rests
// on: a table that was pushed and does not mention the address is
// evidence, and a table that was never pushed is silence.
func (s *Server) dossierRouters(ip string) []dossier.RouterFacts {
	if s.RouterState == nil {
		return nil
	}
	var out []dossier.RouterFacts
	for _, device := range s.RouterState.Devices() {
		f := dossier.RouterFacts{Device: device}

		leases, at, ok := s.RouterState.DHCPLeases(device)
		f.DHCPPushed, f.DHCPPushedAt = ok, at
		if ok {
			for _, l := range leases {
				if l.Address == ip {
					f.Lease = &dossier.Lease{MAC: l.MAC, Hostname: l.Hostname}
					break
				}
			}
		}

		entries, arpAt, arpOK := s.RouterState.ARPEntries(device)
		f.ARPPushed, f.ARPPushedAt = arpOK, arpAt
		if arpOK {
			for _, e := range entries {
				if e.Address == ip {
					f.ARPMAC = e.MAC
					break
				}
			}
		}

		if f.DHCPPushed || f.ARPPushed {
			out = append(out, f)
		}
	}
	return out
}

// dossierName resolves the host's name through the naming layer. Names
// pushed by a router are per-device, so the device carrying this host's
// most recent traffic is asked first; the rest are tried only if it has
// nothing, and the answer carries which layer supplied it either way.
func (s *Server) dossierName(ip string, events []store.Event) dossier.NameFacts {
	var facts dossier.NameFacts
	for _, device := range dossierDevices(events, s) {
		p := s.Naming.HostProvenance(device, ip)
		if p.Name != "" {
			return dossier.NameFacts{Name: p.Name, Source: p.Source, Label: p.Label}
		}
		if p.Label != "" && facts.Label == "" {
			facts.Label = p.Label
		}
	}
	if facts.Source == "" {
		facts.Source = "none"
	}
	return facts
}

// dossierDevices orders the devices worth asking: those carrying this
// host's traffic first (most recent first), then every other known
// device, then the empty device id, which is what a lookup with no
// router context uses.
func dossierDevices(events []store.Event, s *Server) []string {
	var order []string
	seen := map[string]bool{}
	add := func(d string) {
		if seen[d] {
			return
		}
		seen[d] = true
		order = append(order, d)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].DeviceID != "" {
			add(events[i].DeviceID)
		}
	}
	if s.Devices != nil {
		for _, d := range s.Devices.List() {
			add(d.ID)
		}
	}
	add("")
	return order
}

func (s *Server) dossierLookups(events []store.Event) dossier.Lookups {
	return dossier.Lookups{
		PortName: s.Naming.Port,
		PeerName: func(peer string) string {
			// Peer names are resolved against the device that carried
			// the traffic, same as the host's own.
			for _, device := range dossierDevices(events, s) {
				if n := s.Naming.Host(device, peer); n != "" {
					return n
				}
			}
			return ""
		},
		RuleName:    s.Naming.Rule,
		RuleComment: s.dossierRuleComment,
	}
}

// dossierRuleComment returns the comment on the pushed firewall rule
// carrying this log prefix. The bool is the load-bearing half: false
// means this router has never pushed its rule table, which is a
// different statement from a rule that carries no comment.
func (s *Server) dossierRuleComment(device, label string) (string, bool) {
	if s.RouterState == nil || device == "" || label == "" {
		return "", false
	}
	if _, _, ok := s.RouterState.FilterRules(device); !ok {
		return "", false
	}
	for _, r := range s.RouterState.RulesForLogPrefix(device, label) {
		if r.Comment != "" {
			return r.Comment, true
		}
	}
	return "", true
}
