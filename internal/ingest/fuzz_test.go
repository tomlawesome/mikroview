// SPDX-License-Identifier: AGPL-3.0-only

package ingest

import (
	"strings"
	"testing"
)

// FuzzDecodePayload is this package's contribution to the "unauthenticated
// -- or in this case, authenticated but still attacker-shaped -- input"
// fuzz targets AGENTS.md asks for. POST /api/ingest/routeros requires a
// valid ingest token, but that token only proves which device is
// speaking, not that the bytes it sends are well-formed: a compromised
// router, a buggy router-side script, or a leaked token (any read-capable
// RouterOS user can read one out of a script -- see issue #186 step 5)
// all reach this decoder with content nothing has validated yet.
//
// The contract, same shape as internal/routeros.FuzzParse and
// internal/syslog.FuzzParseEnvelope: DecodePayload must never panic on
// any input, and it must never return a Payload alongside a non-nil
// error -- this package's whole design (see DecodePayload's doc comment)
// rests on "refused whole, never partially applied," and a caller that
// checked err != nil but then used a half-populated Payload anyway would
// be exactly the bug that invariant exists to prevent.
func FuzzDecodePayload(f *testing.F) {
	f.Add(`{"kind":"address-list","page":1,"pages":1,"records":[{"list":"blocked","address":"198.51.100.1","comment":"port scan","dynamic":true}]}`)
	f.Add(`{"kind":"filter-rule","page":1,"pages":4,"records":[{"ordinal":7.000000,"comment":"allow lan","chain":"forward","action":"accept","srcAddressList":"lan","logPrefix":"r7"}]}`)
	// The #408 fields in both shapes a connection-state set arrives as,
	// alongside the record above that omits them entirely (the older
	// script the documented upgrade order has to keep accepting).
	f.Add(`{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"input","action":"accept","srcAddressList":"","logPrefix":"A|est|","connectionState":["established","related"],"inInterface":"ether1","outInterface":"!ether2"}]}`)
	f.Add(`{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"input","action":"drop","srcAddressList":"","logPrefix":"","connectionState":"invalid","inInterface":null,"outInterface":null}]}`)
	f.Add(`{"kind":"nat-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"masquerade","chain":"srcnat","action":"masquerade"}]}`)
	// The full #408/#445 NAT anatomy, with both port shapes -- a single
	// port as a JSON number, a range as a string.
	f.Add(`{"kind":"nat-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"dstnat","action":"dst-nat","toAddresses":"192.0.2.10","toPorts":8080.000000,"dstPort":"1000-2000","protocol":"tcp","inInterface":"ether1","outInterface":null,"srcAddress":null,"dstAddress":"198.51.100.4","disabled":false,"dynamic":true}]}`)
	f.Add(`{"kind":"dns-static","page":1,"pages":1,"records":[{"name":"nas.lan","address":"192.168.1.20"}]}`)
	f.Add(`{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"laptop","mac":"aa:bb:cc:dd:ee:ff","address":"192.168.1.50"}]}`)
	f.Add(`{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.168.1.50","mac":"aa:bb:cc:dd:ee:ff"}]}`)
	f.Add(`{"kind":"wireguard-interface","page":1,"pages":1,"records":[{"name":"wg0","comment":"","publicKey":"abc123","listenPort":51820.000000}]}`)
	f.Add(`{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"abc123","allowedAddress":"10.10.0.0/24","endpointAddress":"203.0.113.5:51820","comment":"branch office"}]}`)
	// The array shape RouterOS actually sends for a multi-CIDR peer
	// (issue #443), alongside the joined-string shape above -- both are
	// accepted, so both are seeds.
	f.Add(`{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"abc123","allowedAddress":["192.0.2.0/24","198.51.100.0/24"],"endpointAddress":null,"comment":"branch office"}]}`)
	f.Add(`{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"abc123","allowedAddress":[],"endpointAddress":"","comment":""}]}`)
	f.Add(`{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"abc123","allowedAddress":[1,2],"endpointAddress":"","comment":""}]}`) // a list of the wrong element type

	// Shapes chosen to probe this package's own specific bounds and
	// footguns, not generic JSON malformation (the stdlib decoder is
	// already fuzzed upstream):
	f.Add(``)                                                                                                                                               // empty body
	f.Add(`null`)                                                                                                                                           // valid JSON, not an object
	f.Add(`{}`)                                                                                                                                             // missing everything
	f.Add(`{"kind":"arp"`)                                                                                                                                  // truncated mid-object
	f.Add(`{"kind":"arp","records":[`)                                                                                                                      // truncated mid-array
	f.Add(`{"kind":"routing-table","page":1,"pages":1,"records":[]}`)                                                                                       // unrecognised kind
	f.Add(`{"kind":"arp","page":1,"pages":1,"records":[],"extra":1}`)                                                                                       // unknown top-level field
	f.Add(`{"kind":"arp","page":1,"pages":1,"records":[{"address":"1.1.1.1","mac":"","owner":"admin"}]}`)                                                   // unknown record field
	f.Add(`{"kind":"arp","page":0,"pages":0,"records":[]}`)                                                                                                 // page/pages both zero
	f.Add(`{"kind":"arp","page":999999999,"pages":1,"records":[]}`)                                                                                         // page far past pages
	f.Add(`{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":1e400,"comment":"","chain":"","action":"","srcAddressList":"","logPrefix":""}]}`) // float overflow
	f.Add(`{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":"7","comment":"","chain":"","action":"","srcAddressList":"","logPrefix":""}]}`)   // ordinal as a JSON string, not a number
	f.Add(`{"kind":"arp","page":1,"pages":1,"records":[{"address":"` + strings.Repeat("a", 100000) + `","mac":""}]}`)                                       // grossly oversized field
	f.Add(strings.Repeat(`{`, 5000))                                                                                                                        // deep nesting bait
	f.Add(`{"kind":"arp","page":1,"pages":1,"records":[]}{"kind":"arp","page":1,"pages":1,"records":[]}`)                                                   // trailing JSON value
	f.Add("\x00\x00\x00")
	f.Add(`{"kind":"arp","page":1,"pages":1,"records":"not an array"}`)

	f.Fuzz(func(t *testing.T, body string) {
		p, err := DecodePayload(strings.NewReader(body))
		if err != nil {
			if p.Kind != "" || p.AddressList != nil || p.FilterRules != nil || p.NATRules != nil ||
				p.DNSStatic != nil || p.DHCPLeases != nil || p.ARP != nil ||
				p.WireguardInterfaces != nil || p.WireguardPeers != nil {
				t.Errorf("DecodePayload(%q) returned an error alongside a non-zero Payload %+v -- must return the zero value on any error", body, p)
			}
		}
	})
}
