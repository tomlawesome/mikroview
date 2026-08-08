// SPDX-License-Identifier: AGPL-3.0-only

package routerstate

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/ingest"
)

// decode builds a Payload through the real ingest decoder rather than a
// struct literal, so every test page here is one the endpoint would
// genuinely have accepted.
func decode(t *testing.T, body string) ingest.Payload {
	t.Helper()
	p, err := ingest.DecodePayload(strings.NewReader(body))
	if err != nil {
		t.Fatalf("DecodePayload(%s): %v", body, err)
	}
	return p
}

func apply(t *testing.T, s *Store, device, body string) {
	t.Helper()
	if err := s.Apply(device, decode(t, body), time.Now()); err != nil {
		t.Fatalf("Apply(%s): %v", body, err)
	}
}

func TestFilterRulesSortedByOrdinalAcrossPages(t *testing.T) {
	s := New()
	// Pages arrive out of order, each self-contained -- the table must
	// still come back in RouterOS's own display order.
	apply(t, s, "router-1", `{"kind":"filter-rule","page":2,"pages":2,"records":[{"ordinal":2,"comment":"third","chain":"forward","action":"drop","srcAddressList":"","logPrefix":""}]}`)
	apply(t, s, "router-1", `{"kind":"filter-rule","page":1,"pages":2,"records":[{"ordinal":0,"comment":"first","chain":"input","action":"accept","srcAddressList":"","logPrefix":"r0"},{"ordinal":1,"comment":"second","chain":"forward","action":"accept","srcAddressList":"lan","logPrefix":""}]}`)

	rules, updatedAt, ok := s.FilterRules("router-1")
	if !ok {
		t.Fatal("FilterRules reported no data after two applied pages")
	}
	if updatedAt.IsZero() {
		t.Error("updatedAt is zero after an apply")
	}
	if len(rules) != 3 {
		t.Fatalf("len(rules) = %d, want 3", len(rules))
	}
	for i, want := range []string{"first", "second", "third"} {
		if rules[i].Comment != want {
			t.Errorf("rules[%d].Comment = %q, want %q", i, rules[i].Comment, want)
		}
	}
}

func TestNoDataIsDistinctFromEmpty(t *testing.T) {
	s := New()
	if _, _, ok := s.FilterRules("router-1"); ok {
		t.Error("FilterRules reported ok for a device that never pushed")
	}
	if _, _, ok := s.NATRules("router-1"); ok {
		t.Error("NATRules reported ok for a device that never pushed")
	}
}

func TestPageReplacementWithinACycle(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"old","chain":"input","action":"accept","srcAddressList":"","logPrefix":""}]}`)
	apply(t, s, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"new","chain":"input","action":"accept","srcAddressList":"","logPrefix":""}]}`)

	rules, _, _ := s.FilterRules("router-1")
	if len(rules) != 1 || rules[0].Comment != "new" {
		t.Errorf("re-pushed page did not replace its predecessor: %+v", rules)
	}
}

func TestChangedPagesTotalDropsStalePages(t *testing.T) {
	s := New()
	// A 2-page cycle...
	apply(t, s, "router-1", `{"kind":"filter-rule","page":1,"pages":2,"records":[{"ordinal":0,"comment":"stale-a","chain":"","action":"","srcAddressList":"","logPrefix":""}]}`)
	apply(t, s, "router-1", `{"kind":"filter-rule","page":2,"pages":2,"records":[{"ordinal":1,"comment":"stale-b","chain":"","action":"","srcAddressList":"","logPrefix":""}]}`)
	// ...then the table shrank to 1 page. The old page 2 must not
	// survive to serve stale rules alongside the fresh page 1.
	apply(t, s, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"fresh","chain":"","action":"","srcAddressList":"","logPrefix":""}]}`)

	rules, _, _ := s.FilterRules("router-1")
	if len(rules) != 1 || rules[0].Comment != "fresh" {
		t.Errorf("stale pages from the previous cycle survived a Pages-total change: %+v", rules)
	}
}

func TestRulesForLogPrefixReturnsAllMatches(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[`+
		`{"ordinal":0,"comment":"ssh drop","chain":"input","action":"drop","srcAddressList":"","logPrefix":"DROP"},`+
		`{"ordinal":1,"comment":"telnet drop","chain":"input","action":"drop","srcAddressList":"","logPrefix":"DROP"},`+
		`{"ordinal":2,"comment":"allow lan","chain":"forward","action":"accept","srcAddressList":"lan","logPrefix":"r2"}]}`)

	// A shared prefix resolves to every rule carrying it -- #186 step 4c:
	// "Show every rule matching the prefix rather than picking one."
	got := s.RulesForLogPrefix("router-1", "DROP")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 rules sharing the DROP prefix", len(got))
	}
	if got[0].Comment != "ssh drop" || got[1].Comment != "telnet drop" {
		t.Errorf("wrong rules or order: %+v", got)
	}

	if got := s.RulesForLogPrefix("router-1", "r2"); len(got) != 1 || got[0].Comment != "allow lan" {
		t.Errorf("unique prefix lookup = %+v, want the one allow-lan rule", got)
	}
	if got := s.RulesForLogPrefix("router-1", ""); got != nil {
		t.Errorf("an empty prefix matched %d rules -- no prefix means no resolution, not 'match everything with no prefix'", len(got))
	}
	if got := s.RulesForLogPrefix("router-1", "NOSUCH"); got != nil {
		t.Errorf("an unknown prefix matched %+v", got)
	}
}

func TestHostNamePrecedence(t *testing.T) {
	s := New()
	// DHCP self-reported name and a DNS static entry for the same
	// address: the operator's written DNS entry must win.
	apply(t, s, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[`+
		`{"hostname":"self-reported","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.20"},`+
		`{"hostname":"laptop","mac":"aa:bb:cc:dd:ee:02","address":"192.168.1.50"}]}`)
	apply(t, s, "router-1", `{"kind":"dns-static","page":1,"pages":1,"records":[{"name":"nas.lan","address":"192.168.1.20"}]}`)
	apply(t, s, "router-1", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[`+
		`{"publicKey":"abc","allowedAddress":"10.10.0.0/24","endpointAddress":"","comment":"branch office"},`+
		`{"publicKey":"def","allowedAddress":"10.10.0.7/32","endpointAddress":"","comment":"branch NAS"}]}`)

	cases := map[string]string{
		"192.168.1.20": "nas.lan",       // DNS static beats the DHCP self-report
		"192.168.1.50": "laptop",        // DHCP alone
		"10.10.0.9":    "branch office", // CIDR containment
		"10.10.0.7":    "branch NAS",    // most-specific CIDR wins over the /24
		"192.168.1.99": "",              // unnamed
		"":             "",
	}
	for ip, want := range cases {
		if got := s.HostName(ip); got != want {
			t.Errorf("HostName(%q) = %q, want %q", ip, got, want)
		}
	}
}

func TestRecordCapRefusesPageWhole(t *testing.T) {
	s := New()
	// Build pages of 1000 records (the ingest per-page max) until the
	// 5000-per-kind cap would be crossed; the crossing page must be
	// refused whole and the store left at its prior state.
	page := func(n, pages int) string {
		var b strings.Builder
		b.WriteString(`{"kind":"arp","page":` + itoa(n) + `,"pages":` + itoa(pages) + `,"records":[`)
		for i := 0; i < 1000; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"address":"10.` + itoa(n) + `.` + itoa(i/250) + `.` + itoa(i%250) + `","mac":"aa:bb:cc:dd:ee:ff"}`)
		}
		b.WriteString(`]}`)
		return b.String()
	}
	for n := 1; n <= 5; n++ {
		apply(t, s, "router-1", page(n, 6))
	}
	if err := s.Apply("router-1", decode(t, page(6, 6)), time.Now()); err == nil {
		t.Fatal("the 6th 1000-record page was accepted past the 5000 cap")
	}
}

func TestDeviceCap(t *testing.T) {
	prev := maxDevices
	maxDevices = 2
	t.Cleanup(func() { maxDevices = prev })

	s := New()
	body := `{"kind":"arp","page":1,"pages":1,"records":[{"address":"10.0.0.1","mac":"aa:bb:cc:dd:ee:ff"}]}`
	apply(t, s, "router-1", body)
	apply(t, s, "router-2", body)
	if err := s.Apply("router-3", decode(t, body), time.Now()); err == nil {
		t.Error("a third device was accepted past a cap of 2")
	}
	// An already-tracked device keeps working at the cap.
	apply(t, s, "router-1", body)
}

func TestDevicesAreIsolated(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"r1 rule","chain":"","action":"","srcAddressList":"","logPrefix":"P"}]}`)
	apply(t, s, "router-2", `{"kind":"filter-rule","page":1,"pages":1,"records":[{"ordinal":0,"comment":"r2 rule","chain":"","action":"","srcAddressList":"","logPrefix":"P"}]}`)

	if got := s.RulesForLogPrefix("router-1", "P"); len(got) != 1 || got[0].Comment != "r1 rule" {
		t.Errorf("router-1's prefix lookup leaked across devices: %+v", got)
	}
	rules, _, _ := s.FilterRules("router-2")
	if len(rules) != 1 || rules[0].Comment != "r2 rule" {
		t.Errorf("router-2's table = %+v, want only its own rule", rules)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
