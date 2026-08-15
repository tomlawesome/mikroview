// SPDX-License-Identifier: AGPL-3.0-only

package ingest

import (
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
