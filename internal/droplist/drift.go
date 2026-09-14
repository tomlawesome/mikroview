// SPDX-License-Identifier: AGPL-3.0-only

package droplist

import (
	"net/netip"
	"strings"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// Held counts how many of entries a device's pushed address-list
// snapshot (internal/routerstate.Store.AddressLists) actually holds
// under ListName -- issue #1225's drift number: the admin API shows
// this next to len(entries) so an operator can see a router that has
// fallen behind (never fetched, or fetching a stale generation) rather
// than assuming a stored entry is already enforced.
//
// Both sides are normalised through netip before comparing, since
// RouterOS itself renders a pushed /32 as a bare address
// ("203.0.113.5") and any broader prefix with its mask
// ("203.0.113.0/24") -- entries' own CIDR is always canonical already
// (see Entry's doc comment), so only the snapshot side ever needs it. A
// snapshot address that fails to parse, or belongs to a list other than
// ListName, never counts -- the same tolerant-of-one-bad-record
// convention routerstate's own decoders use, since this is a read path,
// not something that should fail the whole request over one malformed
// row.
func Held(entries []Entry, snapshot []ingest.AddressListEntry) int {
	held := make(map[string]bool, len(snapshot))
	for _, e := range snapshot {
		if e.List != ListName {
			continue
		}
		p, err := normalizeAddressListEntry(e.Address)
		if err != nil {
			continue
		}
		held[p.String()] = true
	}

	count := 0
	for _, e := range entries {
		if held[e.CIDR.String()] {
			count++
		}
	}
	return count
}

// normalizeAddressListEntry parses one pushed address-list entry's
// Address field into the same canonical form Entry.CIDR always carries:
// a bare address (RouterOS's rendering of a /32, e.g. "203.0.113.5")
// becomes an explicit /32 first, then both forms go through Masked() so
// a prefix pushed with stray host bits set still compares equal to the
// same range's canonical CIDR.
func normalizeAddressListEntry(address string) (netip.Prefix, error) {
	if !strings.Contains(address, "/") {
		ip, err := netip.ParseAddr(address)
		if err != nil {
			return netip.Prefix{}, err
		}
		return netip.PrefixFrom(ip, ip.BitLen()).Masked(), nil
	}
	p, err := netip.ParsePrefix(address)
	if err != nil {
		return netip.Prefix{}, err
	}
	return p.Masked(), nil
}
