// SPDX-License-Identifier: AGPL-3.0-only

// Package ipscope is the one inside/outside classifier for event
// addresses (#1481). Packages used to keep their own stateless copy of
// the predicate; networks.home gives it configured state, and copies of
// state drift, so they share this one.
//
// It is deliberately not an outbound-dial guard: geoip.IsPublicUnicast
// and oui's isPublicUnicast stay separate and stricter. A home set only
// ever makes addresses internal, so it can never widen what is dialled.
package ipscope

import (
	"fmt"
	"net/netip"
)

// Scope is the default address-only rule plus the configured home
// prefixes. A nil *Scope is the default rule alone.
type Scope struct {
	home []netip.Prefix
}

// New parses the networks.home entries. A /0 is refused: it would make
// every address internal and silently switch off every outside-facing
// detection, which is never what someone listing their inside means.
func New(prefixes []string) (*Scope, error) {
	s := &Scope{home: make([]netip.Prefix, 0, len(prefixes))}
	for _, p := range prefixes {
		pfx, err := Parse(p)
		if err != nil {
			return nil, err
		}
		s.home = append(s.home, pfx)
	}
	return s, nil
}

// Parse checks one networks.home entry, so config validation and New
// reject exactly the same inputs.
func Parse(s string) (netip.Prefix, error) {
	pfx, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q is not a CIDR prefix: %w", s, err)
	}
	if pfx.Bits() == 0 {
		return netip.Prefix{}, fmt.Errorf("%q covers every address; list the inside networks instead", s)
	}
	return pfx.Masked(), nil
}

// IsPublic reports whether addr is outside: not private, loopback,
// unspecified or link-local (today's rule, unchanged), and not in a
// configured home prefix.
func (s *Scope) IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() ||
		addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() {
		return false
	}
	if s == nil {
		return true
	}
	for _, p := range s.home {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}
