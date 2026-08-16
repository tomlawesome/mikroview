// SPDX-License-Identifier: AGPL-3.0-only

package ingest

import (
	"fmt"
	"strings"
	"testing"
)

func decodeOK(t *testing.T, body string) Payload {
	t.Helper()
	p, err := DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload(%s) = error %v, want success", body, err)
	}
	return p
}

func decodeErr(t *testing.T, body string) error {
	t.Helper()
	_, err := DecodePayload(strings.NewReader(body))
	if err == nil {
		t.Fatalf("DecodePayload(%s) = nil error, want a rejection", body)
	}
	return err
}

func TestDecodePayloadAcceptsEachKind(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"address-list", `{"kind":"address-list","page":1,"pages":1,"records":[{"list":"blocked","address":"198.51.100.1","comment":"port scan","dynamic":true}]}`},
		{"filter-rule", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"allow lan","chain":"forward","action":"accept","srcAddressList":"lan","logPrefix":"r0"}]}`},
		{"nat-rule", `{"kind":"nat-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"masquerade","chain":"srcnat","action":"masquerade"}]}`},
		{"dns-static", `{"kind":"dns-static","page":1,"pages":1,"records":[{"name":"nas.lan","address":"192.168.1.20"}]}`},
		{"dhcp-lease", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"laptop","mac":"aa:bb:cc:dd:ee:ff","address":"192.168.1.50"}]}`},
		{"arp", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.168.1.50","mac":"aa:bb:cc:dd:ee:ff"}]}`},
		{"wireguard-interface", `{"kind":"wireguard-interface","page":1,"pages":1,"records":[{"name":"wg0","comment":"site-to-site","publicKey":"abc123","listenPort":51820}]}`},
		{"wireguard-peer", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"abc123","allowedAddress":"10.10.0.0/24","endpointAddress":"203.0.113.5:51820","comment":"branch office"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := decodeOK(t, c.body)
			if p.Kind != Kind(c.name) {
				t.Errorf("Kind = %q, want %q", p.Kind, c.name)
			}
		})
	}
}

func TestDecodePayloadRoundTripsFields(t *testing.T) {
	p := decodeOK(t, `{"kind":"dhcp-lease","page":2,"pages":4,"records":[{"hostname":"laptop","mac":"aa:bb:cc:dd:ee:ff","address":"192.168.1.50"}]}`)
	if p.Page != 2 || p.Pages != 4 {
		t.Errorf("Page/Pages = %d/%d, want 2/4", p.Page, p.Pages)
	}
	if len(p.DHCPLeases) != 1 {
		t.Fatalf("len(DHCPLeases) = %d, want 1", len(p.DHCPLeases))
	}
	got := p.DHCPLeases[0]
	if got.Hostname != "laptop" || got.MAC != "aa:bb:cc:dd:ee:ff" || got.Address != "192.168.1.50" {
		t.Errorf("DHCPLeases[0] = %+v, unexpected", got)
	}
}

// TestFilterRuleRoundTripsDstPortAndProtocol is issue #243 slice 5's
// reproducer: a filter rule's dst-port and protocol are what make a
// "suggest watching this already-blocked port" candidate possible at
// all (the fields didn't exist before -- see FilterRule's doc comment).
// dst-port is free-form (RouterOS allows a list or a range, not just a
// single port), so this also checks that shape survives untouched.
func TestFilterRuleRoundTripsDstPortAndProtocol(t *testing.T) {
	p := decodeOK(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"drop rdp","chain":"input","action":"drop","srcAddressList":"","logPrefix":"D|rdp|","dstPort":"3389","protocol":"tcp"}]}`)
	if len(p.FilterRules) != 1 {
		t.Fatalf("len(FilterRules) = %d, want 1", len(p.FilterRules))
	}
	got := p.FilterRules[0]
	if string(got.DstPort) != "3389" || got.Protocol != "tcp" {
		t.Errorf("DstPort/Protocol = %q/%q, want 3389/tcp", got.DstPort, got.Protocol)
	}
}

func TestFilterRuleDstPortAcceptsRouterOSListAndRangeShapes(t *testing.T) {
	for _, dstPort := range []string{"22,23,3389", "1000-2000", ""} {
		p := decodeOK(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"input","action":"drop","srcAddressList":"","logPrefix":"","dstPort":"`+dstPort+`","protocol":"tcp"}]}`)
		if string(p.FilterRules[0].DstPort) != dstPort {
			t.Errorf("DstPort = %q, want %q", p.FilterRules[0].DstPort, dstPort)
		}
	}
}

// TestFilterRuleDstPortAcceptsRouterOSNumericShape is the real bug this
// caught when actually tested against a real router (issue #243 slice
// 5): a rule scoping exactly one port serialises dst-port as a JSON
// *number* (3389.000000, not "3389") -- a plain string field, or one
// that only accepted the string shape, refuses this outright, which
// would mean every single-port rule -- the common case -- gets silently
// dropped from the pushed payload... except it isn't silent, it fails
// the whole page (see TestDecodePayloadRejectsWholePageOnOneBadRecord's
// same all-or-nothing contract), which is exactly how this was caught:
// a real push against a real single-port rule came back 400.
func TestFilterRuleDstPortAcceptsRouterOSNumericShape(t *testing.T) {
	p := decodeOK(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":null,"chain":"input","action":"drop","srcAddressList":null,"logPrefix":"D|rdp|","dstPort":3389.000000,"protocol":"tcp"}]}`)
	if string(p.FilterRules[0].DstPort) != "3389" {
		t.Errorf("DstPort = %q, want %q (from the numeric JSON shape)", p.FilterRules[0].DstPort, "3389")
	}
	// The null comment/srcAddressList in the fixture above are the other
	// real shape RouterOS emits for an empty field -- confirmed harmless:
	// encoding/json's documented behaviour is that a JSON null unmarshals
	// into a non-pointer string as a no-op, leaving the zero value, not
	// an error. This case exists so that stays true on purpose, not by
	// accident.
	if p.FilterRules[0].Comment != "" || p.FilterRules[0].SrcAddressList != "" {
		t.Errorf("Comment/SrcAddressList = %q/%q, want both empty", p.FilterRules[0].Comment, p.FilterRules[0].SrcAddressList)
	}
}

func TestFilterRuleDstPortRejectsFractionalNumericShape(t *testing.T) {
	decodeErr(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"","action":"","srcAddressList":"","logPrefix":"","dstPort":80.5,"protocol":"tcp"}]}`)
}

// TestRouterOSFloatIntLanding is the reproducer for the exact landmine
// #186 step 2 names: RouterOS's :serialize to=json emits integers as
// floats (443.000000, not 443). A plain Go int field rejects that shape
// outright -- this is the case that proves RouterOSInt fixes it.
func TestRouterOSFloatIntLanding(t *testing.T) {
	p := decodeOK(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":7.000000,"comment":"","chain":"forward","action":"accept","srcAddressList":"","logPrefix":""}]}`)
	if len(p.FilterRules) != 1 || p.FilterRules[0].Ordinal != 7 {
		t.Fatalf("FilterRules = %+v, want one rule with Ordinal 7", p.FilterRules)
	}
}

func TestRouterOSIntRejectsFractional(t *testing.T) {
	decodeErr(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":7.5,"comment":"","chain":"","action":"","srcAddressList":"","logPrefix":""}]}`)
}

func TestRouterOSIntRejectsOutOfRange(t *testing.T) {
	decodeErr(t, `{"kind":"wireguard-interface","page":1,"pages":1,"records":[{"name":"wg0","comment":"","publicKey":"","listenPort":99999999999}]}`)
}

func TestDecodePayloadRejectsUnknownTopLevelField(t *testing.T) {
	err := decodeErr(t, `{"kind":"arp","page":1,"pages":1,"records":[],"extra":"field"}`)
	// Also confirms the "records is required" check doesn't fire first
	// and mask the real rejection reason on an empty array.
	_ = err
}

func TestDecodePayloadRejectsUnknownRecordField(t *testing.T) {
	decodeErr(t, `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.168.1.1","mac":"aa:bb:cc:dd:ee:ff","owner":"admin"}]}`)
}

func TestDecodePayloadRejectsUnknownKind(t *testing.T) {
	decodeErr(t, `{"kind":"routing-table","page":1,"pages":1,"records":[]}`)
}

func TestDecodePayloadRejectsMissingRecords(t *testing.T) {
	decodeErr(t, `{"kind":"arp","page":1,"pages":1}`)
}

func TestDecodePayloadRejectsTrailingData(t *testing.T) {
	decodeErr(t, `{"kind":"arp","page":1,"pages":1,"records":[]}{"kind":"arp","page":1,"pages":1,"records":[]}`)
}

func TestDecodePayloadRejectsBadPageBounds(t *testing.T) {
	cases := []string{
		`{"kind":"arp","page":0,"pages":1,"records":[]}`,
		`{"kind":"arp","page":2,"pages":1,"records":[]}`,
		`{"kind":"arp","page":1,"pages":0,"records":[]}`,
		`{"kind":"arp","page":1,"pages":1001,"records":[]}`,
	}
	for _, c := range cases {
		decodeErr(t, c)
	}
}

func TestDecodePayloadRejectsTooManyRecords(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"kind":"arp","page":1,"pages":1,"records":[`)
	for i := 0; i < maxRecordsPerPage+1; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"address":"10.0.0.1","mac":"aa:bb:cc:dd:ee:ff"}`)
	}
	b.WriteString(`]}`)
	decodeErr(t, b.String())
}

func TestDecodePayloadRejectsOversizedField(t *testing.T) {
	long := strings.Repeat("a", maxFieldLen+1)
	decodeErr(t, `{"kind":"dns-static","page":1,"pages":1,"records":[{"name":"`+long+`","address":"192.168.1.1"}]}`)
}

func TestDecodePayloadAcceptsFieldAtMaxLen(t *testing.T) {
	// Boundary check the other direction: exactly maxFieldLen must not
	// be rejected -- only over it.
	atMax := strings.Repeat("a", maxFieldLen)
	p := decodeOK(t, `{"kind":"dns-static","page":1,"pages":1,"records":[{"name":"`+atMax+`","address":"192.168.1.1"}]}`)
	if len(p.DNSStatic) != 1 || p.DNSStatic[0].Name != atMax {
		t.Fatalf("record at the length boundary was not accepted intact")
	}
}

func TestDecodePayloadRejectsControlCharacterInField(t *testing.T) {
	// \u0007 (BEL) is a valid JSON escape sequence, so json.Decoder
	// accepts it and hands validateFieldText a real control rune to
	// catch -- unlike a literal unescaped control byte in the JSON
	// source, which the decoder itself would already refuse per the
	// JSON spec before validateFieldText ever runs.
	decodeErr(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"evil\u0007bell","chain":"","action":"","srcAddressList":"","logPrefix":""}]}`)
}

func TestDecodePayloadRejectsFormatCharacterInField(t *testing.T) {
	// \u202e is RIGHT-TO-LEFT OVERRIDE, the Unicode Cf class
	// validateFieldText also refuses -- see that function's doc comment
	// and internal/auth.ValidateUsername's bidi-spoofing reasoning.
	decodeErr(t, `{"kind":"dns-static","page":1,"pages":1,"records":[{"name":"safe\u202ename","address":"192.168.1.1"}]}`)
}

func TestDecodePayloadRejectsWholePageOnOneBadRecord(t *testing.T) {
	// The first record is entirely valid; the second is oversized. Both
	// must be refused -- this package never partially applies a page.
	long := strings.Repeat("a", maxFieldLen+1)
	body := `{"kind":"dns-static","page":1,"pages":1,"records":[` +
		`{"name":"good.lan","address":"192.168.1.1"},` +
		`{"name":"` + long + `","address":"192.168.1.2"}` +
		`]}`
	p, err := DecodePayload(strings.NewReader(body))
	if err == nil {
		t.Fatalf("expected the whole page to be rejected, got %+v", p)
	}
	if len(p.DNSStatic) != 0 {
		t.Errorf("a failed decode must not return a partially populated Payload, got %d records", len(p.DNSStatic))
	}
}

func TestDecodePayloadRejectsEmptyBody(t *testing.T) {
	decodeErr(t, ``)
}

func TestDecodePayloadRejectsNonObjectBody(t *testing.T) {
	decodeErr(t, `["not", "an", "object"]`)
}

// TestDecodePayloadNeverPanics is a small non-fuzz sanity net on top of
// FuzzDecodePayload in fuzz_test.go -- a handful of shapes past ones that
// broke naive JSON-to-struct decoders before, run under `go test` on
// every CI run rather than only during the fuzz job's fuzztime window.
func TestDecodePayloadNeverPanics(t *testing.T) {
	inputs := []string{
		`null`,
		`{}`,
		`{"kind":null}`,
		`{"kind":123}`,
		`{"records":"not an array"}`,
		`{"kind":"arp","page":-1,"pages":-1,"records":null}`,
		strings.Repeat("{", 10000),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("DecodePayload(%q) panicked: %v", in, r)
				}
			}()
			DecodePayload(strings.NewReader(in))
		}()
	}
}

// TestFilterRuleAddressFieldsAreBoundedLikeEveryOtherField closes a gap
// found while adding the #408 fields: dstAddress and srcAddress were the
// only record fields in this package with no validate() line, so a
// router-controlled value could arrive oversized or carrying a
// bidi-override character and reach the UI, the exports and the audit
// trail unscreened. Every sibling field already refuses both.
func TestFilterRuleAddressFieldsAreBoundedLikeEveryOtherField(t *testing.T) {
	long := strings.Repeat("a", maxFieldLen+1)
	const prefix = `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"","chain":"","action":"","srcAddressList":"","logPrefix":"",`
	decodeErr(t, prefix+`"dstAddress":"`+long+`"}]}`)
	decodeErr(t, prefix+`"srcAddress":"`+long+`"}]}`)
	// \u202e is RIGHT-TO-LEFT OVERRIDE and \u0007 is BEL, both written as
	// JSON escapes so the decoder accepts them and validateFieldText gets
	// a real rune to refuse -- the bidi-spoofing class
	// internal/auth.ValidateUsername cites, and the control class beside
	// it.
	decodeErr(t, prefix+`"dstAddress":"203.0.113.9\u202e"}]}`)
	decodeErr(t, prefix+`"srcAddress":"198.51.100.1\u0007"}]}`)

	// The ordinary shapes still pass -- this bounds the field, it does not
	// narrow what a rule may legitimately match on.
	p := decodeOK(t, prefix+`"dstAddress":"!192.0.2.0/24","srcAddress":"198.51.100.1-198.51.100.5"}]}`)
	if p.FilterRules[0].DstAddress != "!192.0.2.0/24" || p.FilterRules[0].SrcAddress != "198.51.100.1-198.51.100.5" {
		t.Errorf("a negated CIDR and a range must still round-trip, got %+v", p.FilterRules[0])
	}
}

// TestFilterRuleRoundTripsConnectionStateAndInterfaces is issue #408's
// own scope: the three fields arrive, in both shapes RouterOS can send a
// set in, and absent means unset rather than "matches nothing".
func TestFilterRuleRoundTripsConnectionStateAndInterfaces(t *testing.T) {
	p := decodeOK(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[
	  {"ordinal":0,"comment":"","chain":"input","action":"accept","srcAddressList":"","logPrefix":"A|est-rel|","connectionState":["established","related"],"inInterface":"ether1","outInterface":""},
	  {"ordinal":1,"comment":"","chain":"input","action":"drop","srcAddressList":"","logPrefix":"D|invalid|","connectionState":"invalid","inInterface":"","outInterface":"!ether2"},
	  {"ordinal":2,"comment":"","chain":"forward","action":"drop","srcAddressList":"","logPrefix":"D|all|"}
	]}`)
	if len(p.FilterRules) != 3 {
		t.Fatalf("decoded %d rules, want 3", len(p.FilterRules))
	}

	if got := p.FilterRules[0].ConnectionState.String(); got != "established,related" {
		t.Errorf("rule 0 ConnectionState = %q, want established,related", got)
	}
	if p.FilterRules[0].InInterface != "ether1" || p.FilterRules[0].OutInterface != "" {
		t.Errorf("rule 0 interfaces = %q/%q, want ether1/empty", p.FilterRules[0].InInterface, p.FilterRules[0].OutInterface)
	}
	// The joined-string shape, and a negated interface: both are one
	// value, not a set of characters to be clever about.
	if got := p.FilterRules[1].ConnectionState; len(got) != 1 || got[0] != "invalid" {
		t.Errorf("rule 1 ConnectionState = %v, want [invalid]", got)
	}
	if p.FilterRules[1].OutInterface != "!ether2" {
		t.Errorf("rule 1 OutInterface = %q, want !ether2", p.FilterRules[1].OutInterface)
	}

	// A rule that matches on no connection state omits the key. That must
	// decode to unset -- an empty list -- and not to a rule claiming it
	// matches some state, since the two say opposite things about what
	// the rule can be answerable for.
	third := p.FilterRules[2]
	if len(third.ConnectionState) != 0 {
		t.Errorf("an absent connectionState decoded to %v, want unset", third.ConnectionState)
	}
	if third.ConnectionState.String() != "" {
		t.Errorf("an absent connectionState renders as %q, want empty", third.ConnectionState.String())
	}
	if third.InInterface != "" || third.OutInterface != "" {
		t.Errorf("absent interfaces decoded to %q/%q, want empty", third.InInterface, third.OutInterface)
	}
}

// TestFilterRuleWithoutNewFieldsStillDecodes is the documented safe
// upgrade order, pinned: an older push script against this build omits
// every field added since it was written and is still accepted, unset
// rather than refused.
func TestFilterRuleWithoutNewFieldsStillDecodes(t *testing.T) {
	p := decodeOK(t, `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"allow lan","chain":"forward","action":"accept","srcAddressList":"lan","logPrefix":"r0"}]}`)
	if len(p.FilterRules) != 1 || p.FilterRules[0].Comment != "allow lan" {
		t.Fatalf("a pre-#408 record was not accepted intact: %+v", p.FilterRules)
	}
}

// TestWireguardPeerAcceptsRouterOSArrayShape is issue #443's acceptance
// case, in the exact shape that failed on a live deployment: a peers
// table pushed by the docs' own reference pattern -- :serialize to=json
// over /interface/wireguard/peers print as-value -- where allowed-address
// is an array because a peer holds several allowed CIDRs.
//
// The refusal it reproduces was "json: cannot unmarshal array into Go
// struct field WireguardPeer.allowedAddress of type string", a 400 on
// every push of this kind, with the other seven kinds landing fine
// because each block is independently guarded on the router side. The
// deployment deliberately kept its wg-peer block *unpatched* (owner
// decision on #443) so the next scheduled push after this lands is the
// live acceptance test -- which means this decoding cleanly with zero
// script changes is the whole contract, not just a convenience.
//
// Field order, null endpoint and float ordinal below are RouterOS's own
// serialisation, not tidied: :serialize emits keys alphabetically, an
// unset property as null, and integers as floats.
func TestWireguardPeerAcceptsRouterOSArrayShape(t *testing.T) {
	const body = `{"kind":"wireguard-peer","page":1,"pages":1,"records":[
	  {"allowedAddress":["192.0.2.0/24","198.51.100.0/24","203.0.113.7/32"],"comment":"branch office","endpointAddress":"203.0.113.5:51820","publicKey":"c3ludGhldGljLXB1YmxpYy1rZXktb25l"},
	  {"allowedAddress":["203.0.113.42/32"],"comment":"laptop","endpointAddress":null,"publicKey":"c3ludGhldGljLXB1YmxpYy1rZXktdHdv"},
	  {"allowedAddress":[],"comment":"no ranges yet","endpointAddress":null,"publicKey":"c3ludGhldGljLXB1YmxpYy1rZXktdGhy"}
	]}`

	p := decodeOK(t, body)
	if len(p.WireguardPeers) != 3 {
		t.Fatalf("decoded %d peers, want 3", len(p.WireguardPeers))
	}
	want := [][]string{
		{"192.0.2.0/24", "198.51.100.0/24", "203.0.113.7/32"},
		{"203.0.113.42/32"},
		{},
	}
	for i, w := range want {
		got := p.WireguardPeers[i].AllowedAddress
		if len(got) != len(w) {
			t.Fatalf("peer %d: AllowedAddress = %v, want %v", i, got, w)
		}
		for j := range w {
			if got[j] != w[j] {
				t.Errorf("peer %d: AllowedAddress[%d] = %q, want %q", i, j, got[j], w[j])
			}
		}
	}
	// Every CIDR survives, not just the first -- a peer's second subnet
	// is what the multi-CIDR case exists for.
	if p.WireguardPeers[0].AllowedAddress.String() != "192.0.2.0/24,198.51.100.0/24,203.0.113.7/32" {
		t.Errorf("String() = %q, want the whole set joined", p.WireguardPeers[0].AllowedAddress.String())
	}
}

// TestWireguardPeerAcceptsJoinedStringShape keeps the compatibility half
// of #443's fix honest: the join workaround the issue documented (and
// any script still running it) sends a comma-joined string, and a bare
// single CIDR is the shape every push before this schema sent.
func TestWireguardPeerAcceptsJoinedStringShape(t *testing.T) {
	cases := map[string][]string{
		`"192.0.2.0/24,198.51.100.0/24"`: {"192.0.2.0/24", "198.51.100.0/24"},
		`"192.0.2.0/24"`:                 {"192.0.2.0/24"},
		`""`:                             nil,
		`null`:                           nil,
	}
	for shape, want := range cases {
		p := decodeOK(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":`+shape+`,"endpointAddress":"","comment":"c"}]}`)
		got := p.WireguardPeers[0].AllowedAddress
		if len(got) != len(want) {
			t.Fatalf("allowedAddress %s decoded to %v, want %v", shape, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("allowedAddress %s -> [%d] = %q, want %q", shape, i, got[i], want[i])
			}
		}
	}
}

func TestRouterOSListRejectsBadShapesAndOversizedSets(t *testing.T) {
	// A list of numbers is not a set of RouterOS values, and neither is
	// an object -- refused rather than coerced.
	decodeErr(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":[1,2],"endpointAddress":"","comment":"c"}]}`)
	decodeErr(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":{"a":"b"},"endpointAddress":"","comment":"c"}]}`)

	// Each element is held to the same text bound a scalar field is.
	long := strings.Repeat("a", maxFieldLen+1)
	decodeErr(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":["192.0.2.0/24","`+long+`"],"endpointAddress":"","comment":"c"}]}`)

	// And the set itself is bounded, refused whole rather than trimmed.
	var b strings.Builder
	b.WriteString(`{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":[`)
	for i := 0; i <= maxListItems; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"192.0.2.%d/32"`, i%256)
	}
	b.WriteString(`],"endpointAddress":"","comment":"c"}]}`)
	decodeErr(t, b.String())
}

// TestDecodeRealFilterRulePush decodes a payload captured verbatim from a
// real RouterOS 7.23.3, produced by the exact script in
// docs/routeros-setup.md section 4c -- not by a hand-written body that
// happens to have the right field names.
//
// The distinction earned itself here. Every shape below came off the
// router, and two are not what a reasonable person would write by hand:
//
//   - A rule with log=no serialises "log": null -- not false, not
//     absent -- because the script reads a property RouterOS does not
//     set. encoding/json unmarshals null into a bool as a no-op, so Log
//     lands false, which is the right answer but by Go's semantics
//     rather than by anyone's design. Worth pinning for that reason.
//   - dst-address arrives in four shapes: bare address, CIDR, range and
//     negated. All plain strings, unlike dst-port, which serialises a
//     single port as a JSON number (see FilterRule's doc comment for why
//     that one needed its own type).
//
// Captured 2026-08-12 under scripts/live-routeros.sh: six rules covering
// every address shape plus one non-logging rule.
func TestDecodeRealFilterRulePush(t *testing.T) {
	const body = `{"kind":"filter-rule","page":1,"pages":1,"records":[
	  {"action":"drop","chain":"forward","comment":null,"dstAddress":"203.0.113.9","dstPort":null,"log":true,"logPrefix":"D|one-ip|","ordinal":0,"protocol":null,"srcAddress":null,"srcAddressList":null},
	  {"action":"drop","chain":"forward","comment":null,"dstAddress":"10.0.0.0/8","dstPort":null,"log":true,"logPrefix":"D|cidr|","ordinal":1,"protocol":null,"srcAddress":"192.168.88.0/24","srcAddressList":null},
	  {"action":"drop","chain":"forward","comment":null,"dstAddress":"10.0.0.1-10.0.0.5","dstPort":null,"log":true,"logPrefix":"D|range|","ordinal":2,"protocol":null,"srcAddress":null,"srcAddressList":null},
	  {"action":"drop","chain":"forward","comment":null,"dstAddress":"!10.0.0.0/8","dstPort":null,"log":true,"logPrefix":"D|negated|","ordinal":3,"protocol":null,"srcAddress":null,"srcAddressList":null},
	  {"action":"accept","chain":"input","comment":null,"dstAddress":null,"dstPort":null,"log":true,"logPrefix":"A|noaddr|","ordinal":4,"protocol":null,"srcAddress":null,"srcAddressList":null},
	  {"action":"drop","chain":"forward","comment":"silent rule","dstAddress":"198.51.100.0/24","dstPort":null,"log":null,"logPrefix":null,"ordinal":5,"protocol":null,"srcAddress":null,"srcAddressList":null}
	]}`

	p := decodeOK(t, body)
	if len(p.FilterRules) != 6 {
		t.Fatalf("decoded %d rules, want 6", len(p.FilterRules))
	}

	want := []struct {
		log        bool
		dstAddress string
		srcAddress string
	}{
		{true, "203.0.113.9", ""},
		{true, "10.0.0.0/8", "192.168.88.0/24"},
		{true, "10.0.0.1-10.0.0.5", ""},
		{true, "!10.0.0.0/8", ""},
		{true, "", ""},
		{false, "198.51.100.0/24", ""}, // "log": null -> false
	}
	for i, w := range want {
		got := p.FilterRules[i]
		if got.Log != w.log {
			t.Errorf("rule %d: Log = %v, want %v", i, got.Log, w.log)
		}
		if got.DstAddress != w.dstAddress {
			t.Errorf("rule %d: DstAddress = %q, want %q", i, got.DstAddress, w.dstAddress)
		}
		if got.SrcAddress != w.srcAddress {
			t.Errorf("rule %d: SrcAddress = %q, want %q", i, got.SrcAddress, w.srcAddress)
		}
	}

	// The field the whole coverage answer rests on: a rule that does not
	// log feeds mikroview nothing, whatever else it matches.
	if p.FilterRules[5].Log {
		t.Error("a rule with log=no decoded as logging -- every coverage answer built on this would be wrong")
	}
}
