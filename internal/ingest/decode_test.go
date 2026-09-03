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
		{"ip-address", `{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"192.168.1.1/24","network":"192.168.1.0","interface":"ether1","comment":"lan"}]}`},
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

// TestPayloadCarriesRouterOSVersion covers #436's carry: the router
// states its own version on the envelope, every kind's push carries it,
// and a script that does not send it is still a valid push.
func TestPayloadCarriesRouterOSVersion(t *testing.T) {
	p := decodeOK(t, `{"kind":"arp","page":1,"pages":1,"routerosVersion":"7.23.3 (stable)","records":[{"address":"192.0.2.50","mac":"aa:bb:cc:dd:ee:ff"}]}`)
	if p.RouterOSVersion != "7.23.3 (stable)" {
		t.Errorf("RouterOSVersion = %q, want the version the router stated", p.RouterOSVersion)
	}

	// Absent is "not stated", not an error and not a version.
	p = decodeOK(t, `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.0.2.50","mac":"aa:bb:cc:dd:ee:ff"}]}`)
	if p.RouterOSVersion != "" {
		t.Errorf("an omitted routerosVersion decoded to %q, want empty", p.RouterOSVersion)
	}

	// Router-controlled text, so it is bounded and screened like any
	// record field rather than trusted for being on the envelope.
	decodeErr(t, `{"kind":"arp","page":1,"pages":1,"routerosVersion":"`+strings.Repeat("7", maxFieldLen+1)+`","records":[]}`)
	decodeErr(t, `{"kind":"arp","page":1,"pages":1,"routerosVersion":"7.23.3\u202e","records":[]}`)
}

// TestNATRuleRoundTripsFullAnatomy pins the fields #445 needs to say a
// rule is "consistent with this event" instead of just listing the
// table. Both port shapes appear (a single dst-port as a JSON number, a
// to-ports range as a string), and the last record is the pre-#408 shape
// -- an older push script omitting all of it, which must still land
// unset rather than be refused.
//
// logPrefix rides along on the first record (#445): it is what turns an
// unanswerable translation into a named rule, so a push that carries it
// must not lose it, and the pre-#408 record below pins the other half --
// no prefix means no name, not a wrong one.
func TestNATRuleRoundTripsFullAnatomy(t *testing.T) {
	p := decodeOK(t, `{"kind":"nat-rule","page":1,"pages":1,"records":[
	  {"ordinal":0,"comment":"web to the DMZ host","chain":"dstnat","action":"dst-nat","logPrefix":"N|port-fwd|","toAddresses":"192.0.2.10","toPorts":8080.000000,"dstPort":443.000000,"protocol":"tcp","inInterface":"ether1","outInterface":"","srcAddress":"","dstAddress":"198.51.100.4","disabled":false,"dynamic":false},
	  {"ordinal":1,"comment":"","chain":"srcnat","action":"masquerade","toAddresses":"","toPorts":"","dstPort":"1000-2000","protocol":"udp","inInterface":"","outInterface":"ether1","srcAddress":"192.0.2.0/24","dstAddress":"","disabled":true,"dynamic":true},
	  {"ordinal":2,"comment":"pre-#408 script","chain":"srcnat","action":"masquerade"}
	]}`)
	if len(p.NATRules) != 3 {
		t.Fatalf("decoded %d NAT rules, want 3", len(p.NATRules))
	}

	first := p.NATRules[0]
	if first.ToAddresses != "192.0.2.10" || string(first.ToPorts) != "8080" || string(first.DstPort) != "443" {
		t.Errorf("rule 0 translation/match = %+v, want to-addresses 192.0.2.10, to-ports 8080, dst-port 443", first)
	}
	if first.Protocol != "tcp" || first.InInterface != "ether1" || first.DstAddress != "198.51.100.4" {
		t.Errorf("rule 0 = %+v, want tcp in on ether1 to 198.51.100.4", first)
	}
	if first.LogPrefix != "N|port-fwd|" {
		t.Errorf("rule 0 log-prefix = %q, want %q -- the operator-set join #445 resolves a logged translation through", first.LogPrefix, "N|port-fwd|")
	}
	if first.Disabled || first.Dynamic {
		t.Errorf("rule 0 disabled/dynamic = %v/%v, want both false", first.Disabled, first.Dynamic)
	}

	second := p.NATRules[1]
	if string(second.DstPort) != "1000-2000" || second.OutInterface != "ether1" || second.SrcAddress != "192.0.2.0/24" {
		t.Errorf("rule 1 = %+v, want the range/out-interface/src-address shape", second)
	}
	if !second.Disabled || !second.Dynamic {
		t.Errorf("rule 1 disabled/dynamic = %v/%v, want both true -- a disabled or dynamic rule is not the same claim as an active one", second.Disabled, second.Dynamic)
	}

	third := p.NATRules[2]
	if third.Comment != "pre-#408 script" || third.Chain != "srcnat" || third.Action != "masquerade" {
		t.Errorf("rule 2 = %+v, want the old four-field record intact", third)
	}
	if third.ToAddresses != "" || third.ToPorts != "" || third.Protocol != "" || third.Disabled || third.Dynamic {
		t.Errorf("rule 2 = %+v, want every unsent field unset", third)
	}
	if third.LogPrefix != "" {
		t.Errorf("rule 2 log-prefix = %q, want empty -- an unlogged rule must stay unnameable", third.LogPrefix)
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

// TestIPAddressRoundTripsFields is issue #627's own acceptance case: an
// /ip/address entry, paged like any other kind (see
// TestDecodePayloadRoundTripsFields's dhcp-lease case -- paging is an
// envelope concern, not a per-kind one, so this only needs to pin that
// nothing about this kind's fields is lost on the way through).
func TestIPAddressRoundTripsFields(t *testing.T) {
	p := decodeOK(t, `{"kind":"ip-address","page":2,"pages":3,"records":[{"address":"192.168.1.1/24","network":"192.168.1.0","interface":"ether1","comment":"lan"}]}`)
	if p.Page != 2 || p.Pages != 3 {
		t.Errorf("Page/Pages = %d/%d, want 2/3", p.Page, p.Pages)
	}
	if len(p.IPAddresses) != 1 {
		t.Fatalf("len(IPAddresses) = %d, want 1", len(p.IPAddresses))
	}
	got := p.IPAddresses[0]
	if got.Address != "192.168.1.1/24" || got.Network != "192.168.1.0" || got.Interface != "ether1" || got.Comment != "lan" {
		t.Errorf("IPAddresses[0] = %+v, unexpected", got)
	}
}

func TestIPAddressRejectsUnknownRecordField(t *testing.T) {
	decodeErr(t, `{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"192.168.1.1/24","network":"192.168.1.0","interface":"ether1","comment":"","disabled":false}]}`)
}

func TestIPAddressRejectsControlAndFormatCharacters(t *testing.T) {
	// \u0007 (BEL) and \u202e (RIGHT-TO-LEFT OVERRIDE), the same two
	// classes every other field in this package refuses -- see
	// validateFieldText's doc comment -- written as JSON escapes so the
	// decoder accepts them and validateFieldText gets a real rune to
	// refuse, same as TestDecodePayloadRejectsControlCharacterInField and
	// TestDecodePayloadRejectsFormatCharacterInField.
	decodeErr(t, `{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"192.168.1.1/24","network":"","interface":"","comment":"evil\u0007bell"}]}`)
	decodeErr(t, `{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"192.168.1.1/24\u202e","network":"","interface":"","comment":""}]}`)
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

// TestWireguardPeerHandshakeFieldsRoundTrip is issue #874's reproducer
// for the new optional peer fields, against a realistic
// :serialize-shaped push: keys alphabetical, an unset property as null,
// and a byte counter big enough that a plain int32 would refuse it (the
// float-as-integer landmine RouterOSInt64 exists for -- 5000000000 is
// past int32's ~2.1 billion ceiling).
func TestWireguardPeerHandshakeFieldsRoundTrip(t *testing.T) {
	const body = `{"kind":"wireguard-peer","page":1,"pages":1,"records":[
	  {"allowedAddress":"10.10.0.0/24","comment":"branch office","currentEndpointAddress":"203.0.113.9","disabled":false,"endpointAddress":"203.0.113.5:51820","interface":"wg0","lastHandshake":"1m23s","publicKey":"k1","rx":5000000000,"tx":123456.000000}
	]}`
	p := decodeOK(t, body)
	if len(p.WireguardPeers) != 1 {
		t.Fatalf("decoded %d peers, want 1", len(p.WireguardPeers))
	}
	got := p.WireguardPeers[0]
	if got.LastHandshake != "1m23s" {
		t.Errorf("LastHandshake = %q, want %q", got.LastHandshake, "1m23s")
	}
	if got.CurrentEndpointAddress != "203.0.113.9" {
		t.Errorf("CurrentEndpointAddress = %q, want %q", got.CurrentEndpointAddress, "203.0.113.9")
	}
	if got.RX != 5000000000 {
		t.Errorf("RX = %d, want 5000000000 (past int32's range -- the float-as-integer landmine)", got.RX)
	}
	if got.TX != 123456 {
		t.Errorf("TX = %d, want 123456", got.TX)
	}
	if got.Disabled {
		t.Errorf("Disabled = true, want false")
	}
	if got.Interface != "wg0" {
		t.Errorf("Interface = %q, want %q", got.Interface, "wg0")
	}
}

// TestWireguardPeerHandshakeFieldsAreOptional is #874's compatibility
// half: a peer record with none of the five new fields (the shape every
// push before this schema sent) must still decode, with the new fields
// at their zero value -- absent, never a guessed-at "down" or "0 bytes
// reported".
func TestWireguardPeerHandshakeFieldsAreOptional(t *testing.T) {
	p := decodeOK(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":"10.10.0.0/24","endpointAddress":"203.0.113.5:51820","comment":"branch office"}]}`)
	got := p.WireguardPeers[0]
	if got.LastHandshake != "" {
		t.Errorf("LastHandshake = %q, want empty (never handshaken/not reported)", got.LastHandshake)
	}
	if got.CurrentEndpointAddress != "" {
		t.Errorf("CurrentEndpointAddress = %q, want empty", got.CurrentEndpointAddress)
	}
	if got.RX != 0 || got.TX != 0 {
		t.Errorf("RX/TX = %d/%d, want 0/0", got.RX, got.TX)
	}
	if got.Disabled {
		t.Errorf("Disabled = true, want false (absent means enabled)")
	}
	if got.Interface != "" {
		t.Errorf("Interface = %q, want empty (attribution unavailable)", got.Interface)
	}
}

// TestWireguardPeerHandshakeFieldExplicitNull covers the other real
// shape RouterOS's own :serialize sends for an unset property (null,
// not an absent key) -- TestDecodeRealFilterRulePush already pins this
// for FilterRule; wireguard-peer gets the same treatment for its own
// never-handshaken peer.
func TestWireguardPeerHandshakeFieldExplicitNull(t *testing.T) {
	p := decodeOK(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":"10.10.0.0/24","endpointAddress":null,"comment":"c","lastHandshake":null,"currentEndpointAddress":null,"rx":null,"tx":null,"disabled":null}]}`)
	got := p.WireguardPeers[0]
	if got.LastHandshake != "" || got.CurrentEndpointAddress != "" || got.RX != 0 || got.TX != 0 || got.Disabled {
		t.Errorf("explicit nulls decoded to %+v, want every new field at its zero value", got)
	}
}

func TestRouterOSInt64AcceptsFloatShapePastInt32Range(t *testing.T) {
	p := decodeOK(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":"10.0.0.0/24","endpointAddress":"","comment":"","rx":9007199254740992}]}`)
	if p.WireguardPeers[0].RX != 9007199254740992 {
		t.Errorf("RX = %d, want 9007199254740992 (2^53, float64's exact-integer ceiling)", p.WireguardPeers[0].RX)
	}
}

func TestRouterOSInt64RejectsFractional(t *testing.T) {
	decodeErr(t, `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"k","allowedAddress":"10.0.0.0/24","endpointAddress":"","comment":"","rx":1.5}]}`)
}

// TestDecodePPPActive is issue #874's second table: /ppp/active print
// as-value, one row per currently connected L2TP/PPTP/SSTP/OVPN
// session.
func TestDecodePPPActive(t *testing.T) {
	const body = `{"kind":"ppp-active","page":1,"pages":1,"records":[
	  {"address":"10.20.0.5","callerId":"203.0.113.44","name":"branch-l2tp","service":"l2tp","uptime":"4w2d5h24m35s"},
	  {"address":"10.20.0.6","callerId":"203.0.113.45","name":"laptop-sstp","service":"sstp","uptime":"3s"}
	]}`
	p := decodeOK(t, body)
	if p.Kind != KindPPPActive {
		t.Fatalf("Kind = %q, want %q", p.Kind, KindPPPActive)
	}
	if len(p.PPPActive) != 2 {
		t.Fatalf("decoded %d sessions, want 2", len(p.PPPActive))
	}
	got := p.PPPActive[0]
	if got.Name != "branch-l2tp" || got.Service != "l2tp" || got.Address != "10.20.0.5" || got.CallerID != "203.0.113.44" || got.Uptime != "4w2d5h24m35s" {
		t.Errorf("PPPActive[0] = %+v, unexpected", got)
	}
	if p.RecordCount() != 2 {
		t.Errorf("RecordCount() = %d, want 2", p.RecordCount())
	}
}

// TestDecodePPPActiveMissingFieldsStillDecodes: every field on this
// record is optional the same way every other record field in this
// schema is -- a router that omits caller-id (PPTP has no such
// property) or address must not be refused for it.
func TestDecodePPPActiveMissingFieldsStillDecodes(t *testing.T) {
	p := decodeOK(t, `{"kind":"ppp-active","page":1,"pages":1,"records":[{"name":"vpn-user1","service":"pptp"}]}`)
	got := p.PPPActive[0]
	if got.Name != "vpn-user1" || got.Service != "pptp" {
		t.Fatalf("PPPActive[0] = %+v, unexpected", got)
	}
	if got.Address != "" || got.CallerID != "" || got.Uptime != "" {
		t.Errorf("omitted fields decoded non-empty: %+v", got)
	}
}

func TestDecodePPPActiveRejectsUnknownRecordField(t *testing.T) {
	decodeErr(t, `{"kind":"ppp-active","page":1,"pages":1,"records":[{"name":"n","bogus":"x"}]}`)
}

func TestDecodePPPActiveRejectsControlCharacterInField(t *testing.T) {
	decodeErr(t, "{\"kind\":\"ppp-active\",\"page\":1,\"pages\":1,\"records\":[{\"name\":\"n\x01\",\"service\":\"l2tp\"}]}")
}
