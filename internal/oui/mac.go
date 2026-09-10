// SPDX-License-Identifier: AGPL-3.0-only

package oui

import "strings"

// MAC is a parsed hardware address: the normalised text, the 24-bit
// prefix a vendor lookup keys on, and the two bits of the first octet
// that say what kind of address it is at all.
//
// The bits matter more than they look. IEEE 802 defines the two
// least-significant bits of the first octet as flags, not as part of any
// assignment: bit 0 (0x01) is the group bit, bit 1 (0x02) is the
// locally-administered bit. An address with the local bit set was made
// up by whatever software is using it -- a hypervisor, a container
// runtime, a phone randomising its Wi-Fi identity -- so there is no
// registry entry to find and no vendor to name. Reporting that as
// "vendor unknown" would be a lie of omission: the honest answer is that
// no vendor exists, which redirects an identification from "what gadget
// is this" to "which host made this".
type MAC struct {
	// Address is the normalised form, uppercase and colon-separated:
	// "DC:A6:32:11:22:33".
	Address string
	// OUI is the first three octets as uppercase hex with no
	// separators -- "DCA632" -- which is exactly how the IEEE MA-L
	// registry spells its Assignment column, so it is the lookup key
	// without further massaging.
	OUI string
	// LocallyAdministered reports the 0x02 bit of the first octet. True
	// means no IEEE assignment underlies this address; see the type
	// comment.
	LocallyAdministered bool
	// Group reports the 0x01 bit: a multicast/broadcast destination
	// rather than a station address. A *source* MAC should never have
	// it set, so seeing it on one is a sign of a forged or malformed
	// line rather than a device to identify. Kept separate from
	// LocallyAdministered because they are different claims: the pair
	// (group, local) is how the whole 802 address space is partitioned.
	Group bool
}

// Parse reads a hardware address in any of the spellings RouterOS and
// its neighbours use -- colons, dashes, dots or nothing at all between
// the octets -- and reports whether it was a well-formed 48-bit address.
//
// It is deliberately total: a malformed or empty string returns ok
// false rather than a zero MAC that would then be looked up as though
// it were an address, because "we could not read this" and "we read it
// and found nothing" are different facts about a host and the dossier
// prints them differently.
func Parse(s string) (MAC, bool) {
	var hex strings.Builder
	hex.Grow(12)
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'F':
			hex.WriteRune(r)
		case r >= 'a' && r <= 'f':
			hex.WriteRune(r - 'a' + 'A')
		case r == ':' || r == '-' || r == '.' || r == ' ':
			// separator: RouterOS writes colons, some exports write
			// dashes, Cisco writes dots. All the same address.
		default:
			return MAC{}, false
		}
	}
	h := hex.String()
	if len(h) != 12 {
		return MAC{}, false
	}
	first := unhex(h[0])<<4 | unhex(h[1])
	var b strings.Builder
	b.Grow(17)
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(h[i : i+2])
	}
	return MAC{
		Address:             b.String(),
		OUI:                 h[:6],
		LocallyAdministered: first&0x02 != 0,
		Group:               first&0x01 != 0,
	}, true
}

// unhex converts one already-validated uppercase hex digit.
func unhex(c byte) byte {
	if c <= '9' {
		return c - '0'
	}
	return c - 'A' + 10
}
