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
	HostName(ip string) string
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
func (r Resolver) Host(ip string) string {
	if r.RouterHosts != nil {
		if v := r.RouterHosts.HostName(ip); v != "" {
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
