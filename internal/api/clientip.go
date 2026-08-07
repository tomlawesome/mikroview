package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// defaultClientIPHeader is what essentially every reverse proxy sets
// without being asked -- nginx, Caddy, Traefik, HAProxy, Cloudflare and
// the cloud load balancers all populate it. Server.ClientIPHeader can
// point somewhere else (X-Real-IP, CF-Connecting-IP, ...) for a proxy
// that doesn't, which is why nothing here hardcodes a vendor.
const defaultClientIPHeader = "X-Forwarded-For"

// clientIP returns the address that login rate limiting is keyed on (see
// handleAuthLogin's ipKey).
//
// Getting this wrong is a denial-of-service either way, in opposite
// directions, which is why the header is consulted conditionally rather
// than always or never:
//
//   - Always trusting a forwarding header means anyone can send a fresh
//     random X-Forwarded-For per request and get a fresh, empty rate-limit
//     bucket every time. That doesn't weaken the per-IP limiter, it
//     deletes it.
//   - Never consulting it means every request behind a reverse proxy
//     carries the proxy's own address, so all users share one bucket and
//     a single attacker's failures lock out everybody. (The per-username
//     limiter still protects individual accounts -- see handleAuthLogin --
//     but the shared IP bucket denies the whole deployment.)
//
// So the header is honoured only when the immediate peer is one of the
// operator-declared TrustedProxies. With none configured (the default),
// this is exactly the old behaviour: the direct peer address, headers
// ignored.
func (s *Server) clientIP(r *http.Request) string {
	peer := peerHost(r)
	if len(s.TrustedProxies) == 0 {
		return peer
	}

	addr, err := netip.ParseAddr(peer)
	if err != nil || !s.trusted(addr) {
		// The connection didn't come from a declared proxy, so whatever
		// forwarding header it carries is unverifiable -- it's just
		// client-supplied data. Key on what we actually observed.
		return peer
	}

	header := s.ClientIPHeader
	if header == "" {
		header = defaultClientIPHeader
	}
	if ip, ok := s.forwardedClient(r.Header.Values(header)); ok {
		return ip
	}
	return peer
}

// forwardedClient walks the forwarded chain right-to-left and returns the
// first address that isn't itself a trusted proxy.
//
// Direction matters and is the whole security argument: entries are
// appended as a request passes through each hop, so the rightmost entry
// was written by our own trusted proxy and everything further left could
// have been forged by the client before it ever arrived. Walking from the
// right and stopping at the first untrusted address yields the
// closest-to-us hop we have any reason to believe. Reading left-to-right
// -- the obvious implementation, and a well-worn spoofing bug -- would
// return whatever the attacker typed.
//
// Returns ok=false if every entry is a trusted proxy or none parse, so
// the caller falls back to the peer address rather than to something
// attacker-chosen.
func (s *Server) forwardedClient(values []string) (string, bool) {
	var chain []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				chain = append(chain, part)
			}
		}
	}

	for i := len(chain) - 1; i >= 0; i-- {
		addr, ok := parseForwardedAddr(chain[i])
		if !ok {
			// An unparseable entry means the chain is malformed from here
			// leftwards -- anything further left is no more trustworthy
			// than this was, so stop rather than skipping over it.
			return "", false
		}
		if s.trusted(addr) {
			continue
		}
		return addr.String(), true
	}
	return "", false
}

// parseForwardedAddr accepts the shapes that turn up in a forwarding
// header in practice: a bare address, or one with a port attached (some
// proxies include the client's source port, and IPv6 then arrives
// bracketed).
func parseForwardedAddr(s string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(s); err == nil {
		return addr.Unmap(), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	return netip.Addr{}, false
}

func (s *Server) trusted(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, p := range s.TrustedProxies {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// peerHost is the transport-level source address, with any port stripped.
// This is the only address in the whole request that can't be forged.
func peerHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	// A Go server reports IPv4 connections plainly, but an IPv4-mapped
	// IPv6 peer would otherwise key as a different string than the same
	// client arriving over v4 -- two buckets for one host.
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Unmap().String()
	}
	return host
}
