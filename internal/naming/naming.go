// SPDX-License-Identifier: AGPL-3.0-only

// Package naming resolves friendly display names for firewall rule
// labels, host IPs, and (issue #109) ports from two layers: internal/
// config's static RuleNames/HostNames maps (config-driven only, no
// auto-discovery or liveness tracking the way internal/device's
// Registry has, since a rule label or a host IP doesn't have a
// "connection" to observe the way a syslog source does), and, taking
// precedence when set, internal/entities' live, admin-manageable Store
// (issue #107) -- so an alias edited in the UI overrides the
// YAML-configured one for the same key without a restart, while
// config.yaml stays a fully supported fallback for anything not (yet)
// given an entity. Ports have no config.yaml-level equivalent of
// RuleNames/HostNames (that predates port aliasing entirely, issue
// #109), so Port has no fallback map to consult -- an entity is the
// only source, same as every other lookup once no entity exists.
package naming

import (
	"strconv"

	"github.com/tomlawesome/mikroview/internal/entities"
)

// Resolver holds the configured name maps and resolves against them. The
// zero value is usable -- nil maps/Entities just miss every lookup -- so
// this works identically whether or not naming is configured at all.
type Resolver struct {
	Rules map[string]string
	Hosts map[string]string
	// Entities, if set, is consulted before Rules/Hosts -- see this
	// package's doc comment. nil (the zero value) means "no entity store
	// wired up," falling straight back to Rules/Hosts, exactly today's
	// pre-issue-#107 behavior.
	Entities *entities.Store
	// RouterHosts, if set, is consulted before everything else for host
	// names -- issue #186 step 4c's owner decision, verbatim: "RouterOS
	// always wins... names in mikroview always match RouterOS. No drift,
	// no reconciling, no second source of truth." A name pushed by the
	// router (a DNS static entry, a DHCP lease, a WireGuard peer
	// comment) shadows an entity or config alias for the same address
	// while the router keeps pushing it; anything RouterOS does not
	// cover falls straight through to Entities/Hosts untouched, so
	// hand-made labels for hosts outside the router's knowledge simply
	// persist. Shadowing rather than overwriting is deliberate: the
	// mikroview-side label is never destroyed, it is out-ranked -- and
	// if the router stops naming that host, it resurfaces, which is
	// exactly the "manage router-known hosts in RouterOS" contract the
	// decision describes.
	RouterHosts RouterHostLookup
}

// RouterHostLookup is the one method of internal/routerstate.Store this
// package needs -- an interface rather than the concrete type so naming
// (imported by main's per-event hot path) doesn't pull in the whole
// ingest schema, and so tests can fake it without pushing real payloads.
type RouterHostLookup interface {
	// HostName takes the device the traffic was observed on, not just
	// the address, so one router's pushed names can never be applied to
	// another router's traffic -- see routerstate.Store.HostName.
	HostName(device, ip string) string
	// HostNameSource is HostName plus which pushed table supplied the
	// name (routerstate's HostSource* constants), or ("", "") for a
	// miss. Only HostProvenance uses it -- the per-event hot path stays
	// on HostName -- but it is on the same interface rather than a
	// second optional one because a router-host lookup that cannot say
	// where a name came from cannot answer the question #413 asks of it.
	HostNameSource(device, ip string) (name, source string)
}

// Source values reported by Provenance.Source: which layer supplied the
// name that is actually displayed.
//
// The router-* values all mean "RouterOS won" (see Resolver.Host), and
// they are the ones #413's inline editor refuses to write under: a
// mikroview-side label saved for such a host is stored faithfully and
// then never shown, which is precisely the edit-with-no-effect that
// issue exists to prevent. They name the specific pushed table rather
// than just saying "the router", because an operator sent to go and fix
// the name needs to know whether to look at dns-static, a DHCP lease or
// a WireGuard peer comment.
const (
	// SourceNone: nothing names this key; the raw value is what shows.
	SourceNone = "none"
	// SourceEntity: an admin's own label, set here or in the Entities
	// panel -- the only source #413's editor writes.
	SourceEntity = "entity"
	// SourceConfig: a config.yaml ruleNames/hostNames alias. Editable in
	// the sense that an entity out-ranks it (see Rule/Host), so saving a
	// label here does take effect.
	SourceConfig = "config"

	SourceRouterDNSStatic     = "router-dns-static"
	SourceRouterDHCPLease     = "router-dhcp-lease"
	SourceRouterWireguardPeer = "router-wireguard-peer"
	// SourceRouterUnknown covers a router-supplied name whose table the
	// lookup did not name. Nothing produces it today; it exists so a
	// future identity-carrying kind cannot silently arrive looking like
	// an editable name.
	SourceRouterUnknown = "router"
)

// Provenance is where a displayed name came from, for one key.
//
// Name is what is displayed (empty when nothing names the key) and
// Source says which layer produced it. Label is separate and is always
// the operator's own entity label for the key, whether or not it is the
// one that won -- so a caller can show "you already labelled this
// 'nas', but the router's 'android-dhcp' is what everyone sees" rather
// than having to choose between the two facts.
type Provenance struct {
	Name   string
	Source string
	Label  string
}

// RouterWins reports whether the displayed name came from RouterOS, and
// therefore whether saving a mikroview-side label for this key would
// have no visible effect. This is the gate #413's editor is built on:
// true means show the operator where the name really comes from instead
// of a writable field.
func (p Provenance) RouterWins() bool {
	switch p.Source {
	case SourceRouterDNSStatic, SourceRouterDHCPLease, SourceRouterWireguardPeer, SourceRouterUnknown:
		return true
	}
	return false
}

// routerSource maps routerstate's own source constant onto the
// Source* value for it. Kept as a translation rather than sharing one
// set of constants so internal/naming does not import
// internal/routerstate -- the RouterHostLookup interface exists
// precisely to keep that dependency out of the hot path.
func routerSource(s string) string {
	switch s {
	case "dns-static":
		return SourceRouterDNSStatic
	case "dhcp-lease":
		return SourceRouterDHCPLease
	case "wireguard-peer":
		return SourceRouterWireguardPeer
	}
	return SourceRouterUnknown
}

// Rule returns the friendly name for a raw rule label -- an
// internal/entities record of type "rule" for label, if one has a
// non-empty Label set, otherwise the Rules map, otherwise "" if neither
// has one.
func (r Resolver) Rule(label string) string {
	if r.Entities != nil {
		if v := r.Entities.Label(entities.TypeRule, label); v != "" {
			return v
		}
	}
	return r.Rules[label]
}

// Host returns the friendly name for a host IP: the router-pushed name
// first (see RouterHosts -- RouterOS always wins), then an
// internal/entities record of type "host", then the config map.
//
// device is which router the traffic was observed on, and only the
// router-pushed lookup uses it: a name pushed by one router must not be
// applied to another router's traffic (see RouterHostLookup). Entity
// labels and config aliases are mikroview's own, set by an admin rather
// than by a router, so they stay deployment-wide as before. An empty
// device therefore still resolves those two, and only skips the
// router-pushed layer.
func (r Resolver) Host(device, ip string) string {
	if r.RouterHosts != nil {
		if v := r.RouterHosts.HostName(device, ip); v != "" {
			return v
		}
	}
	if r.Entities != nil {
		if v := r.Entities.Label(entities.TypeHost, ip); v != "" {
			return v
		}
	}
	return r.Hosts[ip]
}

// Port returns the friendly name for a port number, against
// internal/entities records of type "port" (issue #109), keyed by the
// port formatted as a decimal string -- there is no config.yaml-level
// fallback (see this package's doc comment), so a miss here always
// means "" rather than falling through to a second map. port <= 0 --
// SrcPort/DstPort's own "0 means no port" convention (internal/store/
// event.go, e.g. non-TCP/UDP protocols) -- always misses without
// consulting the store at all, the same way Rule/Host never look up an
// empty label/IP.
func (r Resolver) Port(port int) string {
	if port <= 0 || r.Entities == nil {
		return ""
	}
	return r.Entities.Label(entities.TypePort, strconv.Itoa(port))
}

// HostProvenance is Host with the reason attached: the same precedence,
// the same answer, plus which layer produced it and what the operator's
// own label for ip is.
//
// Deliberately a second method rather than a wider Host: Host is called
// once per address per event on the ingest path, and this one is called
// when a human opens an editor.
func (r Resolver) HostProvenance(device, ip string) Provenance {
	var p Provenance
	if r.Entities != nil {
		p.Label = r.Entities.Label(entities.TypeHost, ip)
	}

	if r.RouterHosts != nil {
		if name, src := r.RouterHosts.HostNameSource(device, ip); name != "" {
			p.Name, p.Source = name, routerSource(src)
			return p
		}
	}
	if p.Label != "" {
		p.Name, p.Source = p.Label, SourceEntity
		return p
	}
	if v := r.Hosts[ip]; v != "" {
		p.Name, p.Source = v, SourceConfig
		return p
	}
	p.Source = SourceNone
	return p
}

// RuleProvenance is Rule with the reason attached. No router layer
// exists for rule labels -- a pushed filter table carries comments and
// log-prefixes, never a display alias for one -- so an entity label
// always wins here and RouterWins is never true.
func (r Resolver) RuleProvenance(label string) Provenance {
	var p Provenance
	if r.Entities != nil {
		p.Label = r.Entities.Label(entities.TypeRule, label)
	}
	if p.Label != "" {
		p.Name, p.Source = p.Label, SourceEntity
		return p
	}
	if v := r.Rules[label]; v != "" {
		p.Name, p.Source = v, SourceConfig
		return p
	}
	p.Source = SourceNone
	return p
}

// PortProvenance is Port with the reason attached. Ports have no
// config.yaml fallback and no router layer (see Port), so the only two
// answers are an entity label or nothing -- the well-known service name
// a UI shows for an unlabelled port is the frontend's own table
// (frontend/src/lib/commonPorts.ts), not something this resolver has.
func (r Resolver) PortProvenance(port int) Provenance {
	var p Provenance
	if port > 0 && r.Entities != nil {
		p.Label = r.Entities.Label(entities.TypePort, strconv.Itoa(port))
	}
	if p.Label != "" {
		p.Name, p.Source = p.Label, SourceEntity
		return p
	}
	p.Source = SourceNone
	return p
}
