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
	if _, _, ok := s.DHCPLeases("router-1"); ok {
		t.Error("DHCPLeases reported ok for a device that never pushed")
	}
	if _, _, ok := s.ARPEntries("router-1"); ok {
		t.Error("ARPEntries reported ok for a device that never pushed")
	}
	if _, _, ok := s.AddressLists("router-1"); ok {
		t.Error("AddressLists reported ok for a device that never pushed")
	}
	if _, _, ok := s.IPAddresses("router-1"); ok {
		t.Error("IPAddresses reported ok for a device that never pushed")
	}
}

// TestDHCPLeasesARPAddressListsSortedAndAccessible is issue #243 slice
// 5's reproducer: these three kinds were already accepted and stored by
// Apply (routerstate stores every kind generically), but had no exported
// getter at all before slice 5 needed one -- this pins the getters
// themselves, sorted output included, the same contract FilterRules
// already has.
func TestDHCPLeasesARPAddressListsSortedAndAccessible(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[{"hostname":"zeta","mac":"aa:bb:cc:dd:ee:02","address":"192.168.1.2"},{"hostname":"alpha","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.1"}]}`)
	apply(t, s, "router-1", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.168.1.9","mac":"aa:bb:cc:dd:ee:09"},{"address":"192.168.1.5","mac":"aa:bb:cc:dd:ee:05"}]}`)
	apply(t, s, "router-1", `{"kind":"address-list","page":1,"pages":1,"records":[{"list":"blocked","address":"198.51.100.9","comment":"","dynamic":false},{"list":"blocked","address":"198.51.100.1","comment":"","dynamic":false}]}`)
	apply(t, s, "router-1", `{"kind":"ip-address","page":1,"pages":1,"records":[{"address":"192.168.1.9/24","network":"192.168.1.0","interface":"ether1","comment":""},{"address":"192.168.1.1/24","network":"192.168.1.0","interface":"ether1","comment":""}]}`)

	leases, updatedAt, ok := s.DHCPLeases("router-1")
	if !ok || updatedAt.IsZero() {
		t.Fatal("DHCPLeases reported no data after an applied page")
	}
	if len(leases) != 2 || leases[0].Address != "192.168.1.1" || leases[1].Address != "192.168.1.2" {
		t.Errorf("DHCPLeases = %+v, want sorted by address", leases)
	}

	arp, _, ok := s.ARPEntries("router-1")
	if !ok {
		t.Fatal("ARPEntries reported no data after an applied page")
	}
	if len(arp) != 2 || arp[0].Address != "192.168.1.5" || arp[1].Address != "192.168.1.9" {
		t.Errorf("ARPEntries = %+v, want sorted by address", arp)
	}

	lists, _, ok := s.AddressLists("router-1")
	if !ok {
		t.Fatal("AddressLists reported no data after an applied page")
	}
	if len(lists) != 2 || lists[0].Address != "198.51.100.1" || lists[1].Address != "198.51.100.9" {
		t.Errorf("AddressLists = %+v, want sorted by (list, address)", lists)
	}

	addrs, _, ok := s.IPAddresses("router-1")
	if !ok {
		t.Fatal("IPAddresses reported no data after an applied page")
	}
	if len(addrs) != 2 || addrs[0].Address != "192.168.1.1/24" || addrs[1].Address != "192.168.1.9/24" {
		t.Errorf("IPAddresses = %+v, want sorted by address", addrs)
	}
}

// TestWireguardAndPPPActiveSortedAndAccessible is issue #874's
// reproducer for the two new accessors: WireguardInterfaces,
// WireguardPeers and PPPActive were already accepted and stored
// generically by Apply, but had no exported getter before this issue
// needed one -- the same gap #243 slice 5 found for DHCP/ARP/address
// lists.
func TestWireguardAndPPPActiveSortedAndAccessible(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"wireguard-interface","page":1,"pages":1,"records":[{"name":"wg1","comment":"","publicKey":"","listenPort":51821},{"name":"wg0","comment":"","publicKey":"","listenPort":51820}]}`)
	apply(t, s, "router-1", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[{"publicKey":"zzz","allowedAddress":"10.0.1.0/24","endpointAddress":"","comment":"z"},{"publicKey":"aaa","allowedAddress":"10.0.0.0/24","endpointAddress":"","comment":"a"}]}`)
	apply(t, s, "router-1", `{"kind":"ppp-active","page":1,"pages":1,"records":[{"name":"zeta","service":"l2tp"},{"name":"alpha","service":"sstp"}]}`)

	ifaces, updatedAt, ok := s.WireguardInterfaces("router-1")
	if !ok || updatedAt.IsZero() {
		t.Fatal("WireguardInterfaces reported no data after an applied page")
	}
	if len(ifaces) != 2 || ifaces[0].Name != "wg0" || ifaces[1].Name != "wg1" {
		t.Errorf("WireguardInterfaces = %+v, want sorted by name", ifaces)
	}

	peers, _, ok := s.WireguardPeers("router-1")
	if !ok {
		t.Fatal("WireguardPeers reported no data after an applied page")
	}
	if len(peers) != 2 || peers[0].PublicKey != "aaa" || peers[1].PublicKey != "zzz" {
		t.Errorf("WireguardPeers = %+v, want sorted by public key", peers)
	}

	sessions, _, ok := s.PPPActive("router-1")
	if !ok {
		t.Fatal("PPPActive reported no data after an applied page")
	}
	if len(sessions) != 2 || sessions[0].Name != "alpha" || sessions[1].Name != "zeta" {
		t.Errorf("PPPActive = %+v, want sorted by name", sessions)
	}
}

// TestWireguardAndPPPActiveAreDistinctNoData extends
// TestNoDataIsDistinctFromEmpty to the three new accessors.
func TestWireguardAndPPPActiveAreDistinctNoData(t *testing.T) {
	s := New()
	if _, _, ok := s.WireguardInterfaces("router-1"); ok {
		t.Error("WireguardInterfaces reported ok for a device that never pushed")
	}
	if _, _, ok := s.WireguardPeers("router-1"); ok {
		t.Error("WireguardPeers reported ok for a device that never pushed")
	}
	if _, _, ok := s.PPPActive("router-1"); ok {
		t.Error("PPPActive reported ok for a device that never pushed")
	}
}

func TestDevicesListsEveryPushingDeviceSorted(t *testing.T) {
	s := New()
	if devs := s.Devices(); len(devs) != 0 {
		t.Fatalf("Devices() = %v before any push, want empty", devs)
	}
	apply(t, s, "router-b", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"10.0.0.1","mac":"aa:bb:cc:dd:ee:01"}]}`)
	apply(t, s, "router-a", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"10.0.0.2","mac":"aa:bb:cc:dd:ee:02"}]}`)

	devs := s.Devices()
	if len(devs) != 2 || devs[0] != "router-a" || devs[1] != "router-b" {
		t.Errorf("Devices() = %v, want [router-a router-b]", devs)
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
		if got := s.HostName("router-1", ip); got != want {
			t.Errorf("HostName(%q) = %q, want %q", ip, got, want)
		}
	}
}

// TestRouterOSVersionIsPerDeviceAndSticky covers #436's derived-version
// source as #408 carries it: the router states its version on a push
// envelope, the store keeps the last one it stated, a device that never
// stated one reports not-stated rather than a guess, and one device's
// claim never answers for another's.
func TestRouterOSVersionIsPerDeviceAndSticky(t *testing.T) {
	s := New()

	if _, _, ok := s.RouterOSVersion("router-1"); ok {
		t.Error("a device that never pushed reported a version")
	}

	apply(t, s, "router-1", `{"kind":"arp","page":1,"pages":1,"routerosVersion":"7.23.3 (stable)","records":[{"address":"192.0.2.50","mac":"aa:bb:cc:dd:ee:01"}]}`)
	got, at, ok := s.RouterOSVersion("router-1")
	if !ok || got != "7.23.3 (stable)" {
		t.Fatalf("RouterOSVersion = %q/%v, want the stated version", got, ok)
	}
	if at.IsZero() {
		t.Error("updatedAt is zero for a version that arrived")
	}

	// A push with no version does not clear the last answer: reverting to
	// an older push script is not a RouterOS downgrade, and forgetting on
	// a silent page would invent a state change nothing observed.
	apply(t, s, "router-1", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.0.2.51","mac":"aa:bb:cc:dd:ee:02"}]}`)
	if got, _, ok := s.RouterOSVersion("router-1"); !ok || got != "7.23.3 (stable)" {
		t.Errorf("a version-less push cleared the stored version (%q/%v)", got, ok)
	}

	// A later push that states one replaces it.
	apply(t, s, "router-1", `{"kind":"arp","page":1,"pages":1,"routerosVersion":"7.24.1 (stable)","records":[{"address":"192.0.2.52","mac":"aa:bb:cc:dd:ee:03"}]}`)
	if got, _, _ := s.RouterOSVersion("router-1"); got != "7.24.1 (stable)" {
		t.Errorf("RouterOSVersion = %q, want the newly stated version", got)
	}

	// Scoped like every other read here: router-2 has pushed, and says
	// nothing about its own version, so nothing is what it reports.
	apply(t, s, "router-2", `{"kind":"arp","page":1,"pages":1,"records":[{"address":"192.0.2.60","mac":"aa:bb:cc:dd:ee:04"}]}`)
	if got, _, ok := s.RouterOSVersion("router-2"); ok || got != "" {
		t.Errorf("router-2 reported %q from router-1's claim", got)
	}
}

// TestHostNameUsesEveryAllowedAddressOfAPeer is the routerstate half of
// issue #443: a WireGuard peer holds a *set* of allowed addresses, and
// each one names the peer. Before the schema took the array shape only
// one string could arrive at all, so "the second subnet is unnamed" was
// not even reachable as a bug -- it is now, and this is what stops it.
func TestHostNameUsesEveryAllowedAddressOfAPeer(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[`+
		`{"publicKey":"k1","allowedAddress":["192.0.2.0/24","198.51.100.0/24","203.0.113.7"],"endpointAddress":"","comment":"branch office"},`+
		`{"publicKey":"k2","allowedAddress":["198.51.100.9/32"],"endpointAddress":"","comment":"branch NAS"}]}`)

	cases := map[string]string{
		"192.0.2.15":    "branch office", // first CIDR of the set
		"198.51.100.20": "branch office", // second CIDR -- the one a single-string schema lost
		"203.0.113.7":   "branch office", // bare address in the set, promoted to /32
		"198.51.100.9":  "branch NAS",    // most-specific still wins across peers
		"203.0.113.8":   "",              // outside every allowed address
	}
	for ip, want := range cases {
		if got := s.HostName("router-1", ip); got != want {
			t.Errorf("HostName(%q) = %q, want %q", ip, got, want)
		}
	}
}

// Every other read in routerstate.go goes through kindLocked(device,
// kind) and TestDevicesAreIsolated already asserted that for the rule
// tables. HostName was the one accessor that never got the same
// treatment: it iterated every device that had ever pushed and returned
// the first match, so one router's ingest token could name any address
// in the world and have that name displayed on traffic seen through
// every other router.
//
// That contradicts what internal/auth/token.go says an ingest token is
// for -- "one compromised router could report state for every other
// device in the deployment" is exactly what scoping it prevents -- and
// what internal/api/ingest.go claims outright: "a router cannot report
// state for any device but the one its credential is scoped to."
//
// TestHostNamePrecedence above only ever used one device, which is why
// nothing caught this. Found independently by two of #272's phase 2
// reviewers and reproduced by a third. See #285, #283, #284.
func TestHostNamesAreScopedToTheDeviceThatPushedThem(t *testing.T) {
	s := New()

	// router-evil names an address it has no relationship with, and
	// claims a catch-all WireGuard range over the entire IPv4 space --
	// the two shapes the reviewers reproduced.
	apply(t, s, "router-evil", `{"kind":"dns-static","page":1,"pages":1,"records":[`+
		`{"name":"trusted-nas","address":"192.168.1.50"},`+
		`{"name":"trusted-internal-server","address":"203.0.113.99"}]}`)
	apply(t, s, "router-evil", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[`+
		`{"publicKey":"abc","allowedAddress":"0.0.0.0/0","endpointAddress":"","comment":"Verified Trusted Network"}]}`)

	// router-victim has pushed a table of its own, so it exists as a
	// device -- this is not "the device is unknown", it is "the device
	// never said anything about these addresses".
	apply(t, s, "router-victim", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[`+
		`{"hostname":"printer","mac":"aa:bb:cc:dd:ee:03","address":"192.168.1.60"}]}`)

	for _, ip := range []string{"192.168.1.50", "203.0.113.99", "8.8.8.8", "1.2.3.4", "198.51.100.7"} {
		if got := s.HostName("router-victim", ip); got != "" {
			t.Errorf("HostName(router-victim, %q) = %q -- router-evil's pushed name reached another device's traffic", ip, got)
		}
	}

	// The scoping must not break the feature for the device that
	// legitimately pushed the data.
	if got := s.HostName("router-evil", "192.168.1.50"); got != "trusted-nas" {
		t.Errorf("HostName(router-evil, 192.168.1.50) = %q, want trusted-nas", got)
	}
	if got := s.HostName("router-evil", "8.8.8.8"); got != "Verified Trusted Network" {
		t.Errorf("HostName(router-evil, 8.8.8.8) = %q -- a device's own catch-all peer still applies to its own traffic", got)
	}
	if got := s.HostName("router-victim", "192.168.1.60"); got != "printer" {
		t.Errorf("HostName(router-victim, 192.168.1.60) = %q, want printer", got)
	}

	// A device that has never pushed anything, and the no-device case,
	// must not be answered from some other router's claims.
	if got := s.HostName("router-unknown", "192.168.1.50"); got != "" {
		t.Errorf("HostName(router-unknown, ...) = %q, want empty", got)
	}
	if got := s.HostName("", "192.168.1.50"); got != "" {
		t.Errorf("HostName(\"\", ...) = %q, want empty", got)
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

// TestHostNameSourceNamesThePushedTable is #413's requirement at the
// store: an editor that must send an operator to the router to change a
// name has to say which table holds it. "The router named this" leaves
// them hunting through dns-static, the lease list and the peer comments
// in turn.
//
// The same fixture as TestHostNamePrecedence above, deliberately: the
// source must follow the name that actually won, not the last table
// that mentioned the address.
func TestHostNameSourceNamesThePushedTable(t *testing.T) {
	s := New()
	apply(t, s, "router-1", `{"kind":"dhcp-lease","page":1,"pages":1,"records":[`+
		`{"hostname":"self-reported","mac":"aa:bb:cc:dd:ee:01","address":"192.168.1.20"},`+
		`{"hostname":"laptop","mac":"aa:bb:cc:dd:ee:02","address":"192.168.1.50"}]}`)
	apply(t, s, "router-1", `{"kind":"dns-static","page":1,"pages":1,"records":[{"name":"nas.lan","address":"192.168.1.20"}]}`)
	apply(t, s, "router-1", `{"kind":"wireguard-peer","page":1,"pages":1,"records":[`+
		`{"publicKey":"abc","allowedAddress":"10.10.0.0/24","endpointAddress":"","comment":"branch office"}]}`)

	cases := []struct {
		ip     string
		name   string
		source string
	}{
		{"192.168.1.20", "nas.lan", HostSourceDNSStatic},
		{"192.168.1.50", "laptop", HostSourceDHCPLease},
		{"10.10.0.9", "branch office", HostSourceWireguardPeer},
		{"192.168.1.99", "", ""},
	}
	for _, tc := range cases {
		name, source := s.HostNameSource("router-1", tc.ip)
		if name != tc.name || source != tc.source {
			t.Errorf("HostNameSource(%q) = (%q, %q), want (%q, %q)", tc.ip, name, source, tc.name, tc.source)
		}
	}

	// Scoping is the property the whole accessor was rewritten for
	// (#285/#283/#284) -- a new entry point onto the same index must
	// not reopen it.
	if name, source := s.HostNameSource("router-victim", "192.168.1.20"); name != "" || source != "" {
		t.Errorf("HostNameSource(router-victim, ...) = (%q, %q) -- another device's pushed name leaked", name, source)
	}
}
