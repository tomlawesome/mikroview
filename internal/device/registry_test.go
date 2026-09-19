// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"sort"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/ingest"
)

// pushedAddresses is an AddressTables standing in for
// internal/routerstate.Store: device must not import it (see
// AddressTables' comment), and the registry reads the same two methods
// whatever produced the rows. Keyed by device, valued by the addresses
// that device pushed, written the way RouterOS writes them ("a.b.c.d/nn").
type pushedAddresses map[string][]string

func (p pushedAddresses) Devices() []string {
	out := make([]string, 0, len(p))
	for dev := range p {
		out = append(out, dev)
	}
	sort.Strings(out)
	return out
}

func (p pushedAddresses) IPAddresses(device string) ([]ingest.IPAddressEntry, time.Time, bool) {
	rows, ok := p[device]
	if !ok {
		return nil, time.Time{}, false
	}
	entries := make([]ingest.IPAddressEntry, 0, len(rows))
	for _, addr := range rows {
		entries = append(entries, ingest.IPAddressEntry{Address: addr})
	}
	return entries, time.Time{}, true
}

func TestResolveConfiguredDevice(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
	})

	id := r.Resolve("192.168.1.1", time.Now())
	if id != "core" {
		t.Errorf("Resolve() = %q, want %q", id, "core")
	}

	devices := r.List()
	if len(devices) != 1 || !devices[0].Configured || devices[0].EventCount != 1 {
		t.Errorf("unexpected device state: %+v", devices)
	}
	if devices[0].FirstSeen.IsZero() {
		t.Errorf("expected FirstSeen to be set for a configured device, got zero value")
	}
}

// #1281's attribution order, step (a): config.yaml is the strongest
// attribution there is -- the operator said which address is which
// router -- so a token enrolment cannot take an address it names. The
// enrolment is refused outright rather than recorded and then outranked
// at read time: a second row claiming the address would say "Enrolled
// at 192.168.1.1" on a card that receives nothing.
func TestResolveAttributesByConfiguredSourceIPFirst(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	now := time.Now()
	if _, err := r.Create("other", "Other", now); err != nil {
		t.Fatal(err)
	}
	token, _, err := r.MintEnrolment("other", now)
	if err != nil {
		t.Fatalf("MintEnrolment: %v", err)
	}
	if r.TryEnrol("192.168.1.1", []byte(`<30>Jan  1 00:00:00 router mikroview-enrol `+token)) {
		t.Error("TryEnrol() = true at a declared device's own address, want false")
	}

	if id := r.Resolve("192.168.1.1", now); id != "core" {
		t.Errorf("Resolve() = %q, want the declared device %q -- config.yaml outranks a token enrolment", id, "core")
	}
	if got := r.Unattributed(); len(got) != 0 {
		t.Errorf("Unattributed() = %+v, want none", got)
	}
}

// #1281's attribution order, step (b): a device this registry enrolled
// by a redeemed token is attributed by its AcceptedIP, no config.yaml
// entry needed -- the replacement for the pushed-address-table claim
// #1281's audit removed (TestResolveIgnoresThePushedAddressTable below
// covers that removal directly).
func TestResolveAttributesByAcceptedIP(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	enrolAt(t, r, "hap-ax3", "10.10.0.1")

	if id := r.Resolve("10.10.0.1", now); id != "hap-ax3" {
		t.Errorf("Resolve() = %q, want %q -- the device enrolled at that address", id, "hap-ax3")
	}
	if got := r.Unattributed(); len(got) != 0 {
		t.Errorf("Unattributed() = %+v, want none: the address was enrolled", got)
	}

	var seen Info
	for _, info := range r.List() {
		if info.ID == "hap-ax3" {
			seen = info
		}
	}
	if seen.EventCount != 1 || seen.SourceIP != "10.10.0.1" {
		t.Errorf("attributed device = %+v, want one event and the attributed address as its sourceIp", seen)
	}
}

// TestResolveIgnoresThePushedAddressTable is issue #1281's audit,
// pinned directly: before this issue, an address exactly one device's
// pushed /ip/address table named was attributed to that device with no
// token at all (AddressTables, computeClaimLocked). That evidence is no
// longer trusted for attribution -- a router asserting its own address
// in a payload an ingest token merely let it push is not proof of
// identity -- so the same setup that used to attribute now leaves the
// address unattributed.
func TestResolveIgnoresThePushedAddressTable(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	r.Ensure("hap-ax3", now)

	// A pushed table naming this exact address, deliberately never
	// handed to EnrolFromPushedAddresses (the one place this evidence
	// still matters -- the once-at-startup upgrade nudge). Resolve
	// itself must not consult it at all.
	_ = pushedAddresses{"hap-ax3": {"10.10.0.1/24"}}

	if id := r.Resolve("10.10.0.1", now); id != "10.10.0.1" {
		t.Errorf("Resolve() = %q, want the address itself: a pushed table alone is no longer attribution evidence", id)
	}
	sources := r.Unattributed()
	if len(sources) != 1 || sources[0].Address != "10.10.0.1" {
		t.Fatalf("Unattributed() = %+v, want the one unattributed address", sources)
	}
}

// Nothing has claimed the address and nothing has pushed anything: the
// source is remembered as a source, and no device is invented from it.
// The lines still have somewhere to go -- Resolve returns the address,
// so they are stored under it exactly as before.
func TestResolveLeavesAnUnclaimedSourceUnattributed(t *testing.T) {
	r := NewRegistry(nil)

	id := r.Resolve("10.0.0.5", time.Now())
	if id != "10.0.0.5" {
		t.Errorf("Resolve() = %q, want %q", id, "10.0.0.5")
	}

	if devices := r.List(); len(devices) != 0 {
		t.Errorf("List() = %+v, want no devices: a syslog packet carries no identity", devices)
	}
	sources := r.Unattributed()
	if len(sources) != 1 || sources[0].Address != "10.0.0.5" || sources[0].Lines != 1 {
		t.Errorf("Unattributed() = %+v, want one source with one line", sources)
	}
	if sources[0].FirstSeen.IsZero() || len(sources[0].Claimants) != 0 {
		t.Errorf("unattributed source = %+v, want a first-seen and no claimants", sources[0])
	}
}

// A source that arrives before its device is enrolled is unattributed;
// once a token is redeemed at that address, the next line is attributed
// and the address stops being listed as unclaimed. The evidence moved,
// so the answer moves with it.
func TestATokenAttributesASourceThatArrivedBeforeIt(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()

	r.Resolve("10.10.0.1", now)
	if got := r.Unattributed(); len(got) != 1 {
		t.Fatalf("Unattributed() = %+v, want the source before any enrolment", got)
	}

	if _, err := r.Create("hap-ax3", "hap-ax3", now); err != nil {
		t.Fatal(err)
	}
	enrolAt(t, r, "hap-ax3", "10.10.0.1")

	if id := r.Resolve("10.10.0.1", now.Add(2*time.Minute)); id != "hap-ax3" {
		t.Errorf("Resolve() = %q, want %q once the device was enrolled at the address", id, "hap-ax3")
	}
	if got := r.Unattributed(); len(got) != 0 {
		t.Errorf("Unattributed() = %+v, want none: the address is enrolled now", got)
	}
}

// Ensure is #1170's "a push from token device X ensures X exists": the
// device is in the one list every count reads from its first push, and
// is honestly reported as never having logged anything -- a push is not
// a log line, and the fleet's never-seen status and the silence
// detector both depend on not confusing the two.
func TestEnsureAddsAPushingRouterWithoutFakingSyslog(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	r.Ensure("hap-ax3", now)
	r.Ensure("hap-ax3", now.Add(time.Hour))

	devices := r.List()
	if len(devices) != 1 {
		t.Fatalf("List() = %+v, want the one pushing device", devices)
	}
	d := devices[0]
	if d.ID != "hap-ax3" || d.Name != "hap-ax3" || d.Configured {
		t.Errorf("device = %+v, want the token's device name, undeclared", d)
	}
	if !d.FirstSeen.Equal(now) {
		t.Errorf("FirstSeen = %v, want the first push at %v", d.FirstSeen, now)
	}
	if !d.LastSeen.IsZero() || d.EventCount != 0 {
		t.Errorf("device = %+v, want no syslog liveness: nothing has been logged", d)
	}
}

// A router declared in config.yaml that pushes under the same id stays
// one device, not two: Ensure finds the declared entry rather than
// minting a second one beside it.
func TestEnsureKeepsADeclaredDeviceSingular(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"}})
	r.Ensure("core", time.Now())

	devices := r.List()
	if len(devices) != 1 || devices[0].ID != "core" || !devices[0].Configured || devices[0].Name != "Core Router" {
		t.Errorf("List() = %+v, want the one declared device unchanged", devices)
	}
}

func TestResolveIncrementsEventCount(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", SourceIP: "10.0.0.5"}})
	now := time.Now()
	r.Resolve("10.0.0.5", now)
	r.Resolve("10.0.0.5", now.Add(time.Second))
	r.Resolve("10.0.0.5", now.Add(2*time.Second))

	devices := r.List()
	if len(devices) != 1 || devices[0].EventCount != 3 {
		t.Errorf("expected EventCount=3, got %+v", devices)
	}
}

// Different textual forms of the same address (here, IPv6 shorthand vs.
// its fully-expanded form) must land on the same entry rather than
// silently splitting one source's lines across two -- normalizeIP
// re-serializes through net.IP.String() specifically to collapse this.
func TestResolveNormalizesEquivalentIPForms(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()

	r.Resolve("::1", now)
	r.Resolve("0:0:0:0:0:0:0:1", now.Add(time.Second))

	sources := r.Unattributed()
	if len(sources) != 1 {
		t.Fatalf("expected both forms to resolve to one source, got %d: %+v", len(sources), sources)
	}
	if sources[0].Lines != 2 {
		t.Errorf("Lines = %d, want 2", sources[0].Lines)
	}
	if sources[0].Address != "::1" {
		t.Errorf("Address = %q, want the normalized form %q", sources[0].Address, "::1")
	}
}

// A source that isn't a parseable IP at all (shouldn't happen from a real
// syslog listener, which always supplies conn.RemoteAddr()'s host, but
// normalizeIP has no other caller to guarantee that) falls back to the
// raw string unchanged rather than losing the value.
func TestNormalizeIPFallsBackForUnparseableInput(t *testing.T) {
	r := NewRegistry(nil)
	id := r.Resolve("not-an-ip", time.Now())
	if id != "not-an-ip" {
		t.Errorf("Resolve() = %q, want the unparsed input returned unchanged", id)
	}
}

// List() must return an independent copy: mutating a returned Info (or
// the slice itself) must not corrupt the registry's own state, since
// callers on the /api/devices read path have no other isolation from
// concurrent ingest-side writes.
func TestListReturnsIndependentSnapshot(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", Name: "Core Router", SourceIP: "10.0.0.5"}})
	r.Resolve("10.0.0.5", time.Now())

	devices := r.List()
	devices[0].EventCount = 999
	devices[0].Name = "tampered"

	fresh := r.List()
	if fresh[0].EventCount == 999 || fresh[0].Name == "tampered" {
		t.Errorf("mutating a List() result affected subsequent List() output: %+v", fresh)
	}
}

// Unattributed() is a copy for the same reason List() is: the API
// serialises it outside the registry's lock.
func TestUnattributedReturnsIndependentSnapshot(t *testing.T) {
	r := NewRegistry(nil)
	now := time.Now()
	r.Resolve("10.0.0.5", now)

	sources := r.Unattributed()
	sources[0].Lines = 999

	fresh := r.Unattributed()
	if fresh[0].Lines == 999 {
		t.Errorf("mutating an Unattributed() result affected subsequent output: %+v", fresh)
	}
}

func TestListIncludesConfiguredAndPushed(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	now := time.Now()
	r.Resolve("192.168.1.1", now)
	r.Ensure("hap-ax3", now)

	devices := r.List()
	if len(devices) != 2 {
		t.Fatalf("expected 2 devices (1 declared + 1 that pushed), got %d: %+v", len(devices), devices)
	}

	var sawConfigured, sawPushed bool
	for _, d := range devices {
		if d.ID == "core" && d.Configured {
			sawConfigured = true
		}
		if d.ID == "hap-ax3" && !d.Configured {
			sawPushed = true
		}
	}
	if !sawConfigured || !sawPushed {
		t.Errorf("expected one declared + one pushing device, got %+v", devices)
	}
}

// TestMultihomedCandidatesFlagsSilentDeclaredDevice is issue #442's core
// scenario: a router is declared under one address, its syslog actually
// arrives from another (a different, VLAN-facing interface), and that
// other address is claimed by nothing -- so the declared device never
// receives an event while lines arrive unattributed.
func TestMultihomedCandidatesFlagsSilentDeclaredDevice(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	r.Resolve("10.10.0.1", time.Now())

	got := r.MultihomedCandidates()
	if len(got) != 1 {
		t.Fatalf("MultihomedCandidates() returned %d candidates, want 1: %+v", len(got), got)
	}
	c := got[0]
	if c.DeclaredID != "core-router" || c.DeclaredSourceIP != "192.168.1.1" {
		t.Errorf("unexpected declared side: %+v", c)
	}
	if len(c.Unattributed) != 1 || c.Unattributed[0].Address != "10.10.0.1" {
		t.Errorf("unexpected arriving side: %+v", c.Unattributed)
	}
}

// TestMultihomedCandidatesEmptyWhenDeclaredDeviceHasTraffic guards
// against false positives: a declared device that has actually received
// its own events is not "silent" just because some other, unrelated
// address is also arriving.
func TestMultihomedCandidatesEmptyWhenDeclaredDeviceHasTraffic(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	r.Resolve("192.168.1.1", time.Now())
	r.Resolve("10.10.0.1", time.Now())

	if got := r.MultihomedCandidates(); got != nil {
		t.Errorf("MultihomedCandidates() = %+v, want nil: declared device has traffic under its own id", got)
	}
}

// TestMultihomedCandidatesEmptyWithNoUnattributedSources guards the other
// false-positive direction: a declared device with no traffic yet is
// unremarkable on its own when nothing is arriving unclaimed -- the
// router may simply not have started logging yet.
func TestMultihomedCandidatesEmptyWithNoUnattributedSources(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})

	if got := r.MultihomedCandidates(); got != nil {
		t.Errorf("MultihomedCandidates() = %+v, want nil: nothing is arriving unclaimed", got)
	}
}

// TestMultihomedCandidatesReadTheSameAttribution: a source the routers'
// own pushed tables settle is not a multi-homing puzzle at all. #1170
// gave the registry that evidence, so the candidate list is now only
// what the tables could not answer.
func TestMultihomedCandidatesReadTheSameAttribution(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	now := time.Now()
	enrolAt(t, r, "core-router", "10.10.0.1")
	r.Resolve("10.10.0.1", now)

	if got := r.MultihomedCandidates(); got != nil {
		t.Errorf("MultihomedCandidates() = %+v, want nil: the address was already enrolled", got)
	}
}

// TestMultihomedCandidatesListsEveryUnattributedSource: Registry cannot
// itself tell which unclaimed address (if any) is the same physical
// router as a silent declared one, so with more than one it must report
// all of them rather than guessing at one -- the same "several rules
// share a prefix, the honest answer is all of them" rule
// RulesForLogPrefix follows.
func TestMultihomedCandidatesListsEveryUnattributedSource(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "core-router", Name: "Core Router", SourceIP: "192.168.1.1"},
	})
	r.Resolve("10.10.0.1", time.Now())
	r.Resolve("10.10.0.2", time.Now())

	got := r.MultihomedCandidates()
	if len(got) != 1 || len(got[0].Unattributed) != 2 {
		t.Fatalf("expected 1 candidate with 2 unattributed sources, got %+v", got)
	}
}

// fixedNames is a NameLookup standing in for internal/naming.Resolver:
// device must not import it (see NameLookup's comment), and what the
// registry does with an answer is the same whatever produced it.
type fixedNames map[string]string

func (f fixedNames) Device(id string) string { return f[id] }

// enrolAt mints a real enrolment token for device and redeems it at
// host via TryEnrol, exactly the way the listener gate would on a real
// "mikroview-enrol <token>" line -- the one path tests should use to
// put a device's AcceptedIP in place, rather than poking the field
// directly, so these tests exercise the real mint/hash/redeem sequence.
func enrolAt(t *testing.T, r *Registry, device, host string) {
	t.Helper()
	token, _, err := r.MintEnrolment(device, time.Now())
	if err != nil {
		t.Fatalf("MintEnrolment(%q): %v", device, err)
	}
	line := []byte(`<30>Jan  1 00:00:00 router mikroview-enrol ` + token)
	if !r.TryEnrol(host, line) {
		t.Fatalf("TryEnrol(%q, ...) = false, want the freshly minted token to redeem", host)
	}
}

// TestListServesTheStoredName is issue #600's requirement at the layer
// it is stored: a rename one person saved is what every reader of List
// gets -- GET /api/devices, the wizard's command blocks, the silence
// flag's wording -- rather than the name only that person's browser
// knows about.
func TestListServesTheStoredName(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"}})
	now := time.Now()
	r.Resolve("192.168.1.1", now)
	r.Ensure("lab-crs", now)
	r.SetNames(fixedNames{"lab-crs": "lab crs"})

	got := map[string]string{}
	for _, info := range r.List() {
		got[info.ID] = info.Name
	}
	if got["lab-crs"] != "lab crs" {
		t.Errorf("pushing device name = %q, want the stored rename", got["lab-crs"])
	}
	if got["core"] != "Core Router" {
		t.Errorf("configured device name = %q, want config.yaml's own name", got["core"])
	}
}

// A declaration with no name, and a registry with no resolver at all,
// both still display as something: the raw id. Nothing renders an empty
// device cell, and removing a label restores the id the editor promised.
func TestListFallsBackToTheRawID(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "unnamed", SourceIP: "192.168.1.2"}})
	r.Resolve("192.168.1.2", time.Now())

	list := r.List()
	if len(list) != 1 || list[0].Name != "unnamed" {
		t.Fatalf("List() = %+v, want the id as the displayed name", list)
	}

	r.SetNames(fixedNames{"unnamed": "Spare"})
	if list = r.List(); list[0].Name != "Spare" {
		t.Errorf("Name = %q, want the stored rename once one exists", list[0].Name)
	}
}

// Order is stable, configured first then by id. Scenarios and the setup
// wizard read devices[0] as "the router this instance watches"; under
// the old map order a second device made that answer change from one
// request to the next.
func TestListOrdersConfiguredFirstThenByID(t *testing.T) {
	r := NewRegistry([]config.Device{
		{ID: "zeta", Name: "Zeta", SourceIP: "192.168.1.9"},
		{ID: "alpha", Name: "Alpha", SourceIP: "192.168.1.1"},
	})
	now := time.Now()
	r.Ensure("rb5009", now)
	r.Ensure("hap-ax3", now)

	for i := 0; i < 20; i++ {
		var ids []string
		for _, info := range r.List() {
			ids = append(ids, info.ID)
		}
		want := []string{"alpha", "zeta", "hap-ax3", "rb5009"}
		for j := range want {
			if ids[j] != want[j] {
				t.Fatalf("List() ids = %v, want %v", ids, want)
			}
		}
	}
}
