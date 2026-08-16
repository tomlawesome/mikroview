// SPDX-License-Identifier: AGPL-3.0-only

package engine

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// fakeLists answers address-list membership from a fixed map keyed
// "device\x00list" -- the same fixture shape internal/watchlist's own
// tests use, restated here because that one is unexported there.
type fakeLists map[string][]string

func (f fakeLists) InAddressList(device, list, ip string) bool {
	for _, member := range f[device+"\x00"+list] {
		if member == ip {
			return true
		}
	}
	return false
}

// recordedObservation is one RecordObservation call, captured.
type recordedObservation struct {
	entryID string
	destIP  string
	port    int
	at      time.Time
}

type fakeObservations struct{ calls []recordedObservation }

func (f *fakeObservations) RecordObservation(entryID, destIP string, port int, t time.Time) {
	f.calls = append(f.calls, recordedObservation{entryID, destIP, port, t})
}

// buildExpectations is the test-side shorthand for the production
// assembly: entries in, the two registered Evaluateds out, with every
// emission captured.
func buildExpectations(t *testing.T, entries []watchlist.Entry, members AddressListMembership, obs ObservationRecorder) (*DeclarativeSet, *InvertedExpectations, *[]RoutedEmission) {
	t.Helper()
	var got []RoutedEmission
	set, inv, err := BuildExpectations(entries, ExpectationDeps{
		Members:      members,
		Sink:         func(r RoutedEmission) { got = append(got, r) },
		Observations: obs,
	})
	if err != nil {
		t.Fatalf("BuildExpectations: %v", err)
	}
	return set, inv, &got
}

// --- non-inverted: every matching axis, through the engine -------------

// TestExpectationNonInvertedMatchesEveryAxis is the axis-by-axis
// equivalent of what internal/watchlist's own end-to-end pass covered,
// driven through the built definitions instead of a bespoke evaluator:
// each entry scopes exactly one axis, and only the events that satisfy
// that axis emit.
func TestExpectationNonInvertedMatchesEveryAxis(t *testing.T) {
	entries := []watchlist.Entry{
		{ID: "by-port", Ports: []int{22}},
		{ID: "by-mac", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, Ports: []int{8080}},
		{ID: "by-destip", DestIP: "10.0.0.9", Ports: []int{443}},
		{ID: "by-addrlist", SourceList: watchlist.AddressListRef{Device: "core", List: "mgmt"}, Ports: []int{9999}},
	}
	lists := fakeLists{"core\x00mgmt": {"192.168.1.200"}}
	set, _, got := buildExpectations(t, entries, lists, nil)

	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		event   store.Event
		wantHit string
	}{
		{"port match", store.Event{SrcIP: "203.0.113.1", SrcMAC: "11:11:11:11:11:11", DstIP: "10.0.0.1", DstPort: 22}, "by-port"},
		{"port mismatch", store.Event{SrcIP: "203.0.113.1", SrcMAC: "11:11:11:11:11:11", DstIP: "10.0.0.1", DstPort: 23}, ""},
		{"mac match", store.Event{SrcMAC: "aa:bb:cc:dd:ee:ff", SrcIP: "192.168.1.5", DstIP: "10.0.0.2", DstPort: 8080}, "by-mac"},
		{"mac mismatch", store.Event{SrcMAC: "ff:ff:ff:ff:ff:ff", SrcIP: "192.168.1.5", DstIP: "10.0.0.2", DstPort: 8080}, ""},
		{"destip match", store.Event{SrcIP: "203.0.113.2", SrcMAC: "22:22:22:22:22:22", DstIP: "10.0.0.9", DstPort: 443}, "by-destip"},
		{"destip mismatch", store.Event{SrcIP: "203.0.113.2", SrcMAC: "22:22:22:22:22:22", DstIP: "10.0.0.10", DstPort: 443}, ""},
		{"addrlist member", store.Event{SrcIP: "192.168.1.200", DstIP: "10.0.0.3", DstPort: 9999}, "by-addrlist"},
		{"addrlist non-member", store.Event{SrcIP: "192.168.1.201", DstIP: "10.0.0.3", DstPort: 9999}, ""},
		{"established traffic is not an attempt", store.Event{SrcIP: "203.0.113.1", SrcMAC: "11:11:11:11:11:11", DstIP: "10.0.0.1", DstPort: 22, ConnState: "established"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := len(*got)
			e := tc.event
			e.ReceivedAt = now
			now = now.Add(time.Second)
			set.Evaluate(e)

			fired := (*got)[before:]
			if tc.wantHit == "" {
				if len(fired) != 0 {
					t.Fatalf("expected no emission, got %d (%q)", len(fired), fired[0].Expectation.EntryID)
				}
				return
			}
			if len(fired) != 1 {
				t.Fatalf("expected exactly one emission, got %d", len(fired))
			}
			w := fired[0].Expectation
			if w == nil {
				t.Fatal("an expectation definition emitted a detection")
			}
			if w.EntryID != tc.wantHit {
				t.Errorf("emission EntryID = %q, want %q", w.EntryID, tc.wantHit)
			}
		})
	}
}

// TestExpectationTupleCarriesTheEventsOwnIdentity pins what
// matchNonInverted's own doc comment made explicit and #406 must not
// lose: the recorded Tuple always carries the *event's* real identity,
// never the entry's (possibly unscoped) source scoping, so one unscoped
// expectation watching many devices produces one record per device
// rather than one shared record they all collapse into.
func TestExpectationTupleCarriesTheEventsOwnIdentity(t *testing.T) {
	set, _, got := buildExpectations(t, []watchlist.Entry{{ID: "unscoped", Ports: []int{22}}}, nil, nil)

	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	set.Evaluate(store.Event{SrcMAC: "AA:BB:CC:DD:EE:FF", SrcIP: "192.168.1.5", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now})
	set.Evaluate(store.Event{SrcIP: "192.168.1.6", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now.Add(time.Second)})

	if len(*got) != 2 {
		t.Fatalf("expected 2 emissions, got %d", len(*got))
	}
	first := (*got)[0].Expectation
	if first.Tuple.Source.MAC != "AA:BB:CC:DD:EE:FF" || first.Tuple.Source.IP != "192.168.1.5" {
		t.Errorf("first tuple source = %+v, want the event's own MAC and IP verbatim", first.Tuple.Source)
	}
	if first.Tuple.DestIP != "10.0.0.1" || first.Tuple.Port != 22 {
		t.Errorf("first tuple = %+v, want 10.0.0.1:22", first.Tuple)
	}
	if first.Event.SrcIP != "192.168.1.5" {
		t.Errorf("emission did not carry the triggering event as evidence: %+v", first.Event)
	}
	second := (*got)[1].Expectation
	if second.Tuple.Source.IP != "192.168.1.6" || second.Tuple.Source.MAC != "" {
		t.Errorf("second tuple source = %+v, want the second device's own identity", second.Tuple.Source)
	}
}

// TestExpectationSourceIdentityPrefersMACOverIP pins the half of the
// identity rule FieldSourceAddress could not express: an IP-scoped
// expectation must not match an event that carries a source MAC, even
// when the address is right, because matchlog collapses on the
// MAC-preferred key. This is matchlog.Identity.MatchesSource's rule,
// reached through a condition.
func TestExpectationSourceIdentityPrefersMACOverIP(t *testing.T) {
	entries := []watchlist.Entry{
		{ID: "ip-scoped", Source: matchlog.Identity{IP: "192.168.1.5"}, Ports: []int{22}},
	}
	set, _, got := buildExpectations(t, entries, nil, nil)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	set.Evaluate(store.Event{SrcIP: "192.168.1.5", SrcMAC: "aa:bb:cc:dd:ee:ff", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now})
	if len(*got) != 0 {
		t.Errorf("an IP-scoped expectation matched a MAC-identified event: %+v", (*got)[0].Expectation.Tuple)
	}

	set.Evaluate(store.Event{SrcIP: "192.168.1.5", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now.Add(time.Second)})
	if len(*got) != 1 {
		t.Fatalf("expected the same expectation to match the same address with no MAC, got %d emissions", len(*got))
	}
}

// TestExpectationMACScopedSurvivesALeaseChange is the other half: a
// MAC-bound expectation keeps matching when its device's address
// changes, which is the entire reason a MAC-preferred identity exists.
func TestExpectationMACScopedSurvivesALeaseChange(t *testing.T) {
	entries := []watchlist.Entry{
		{ID: "mac-scoped", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, Ports: []int{22}},
	}
	set, _, got := buildExpectations(t, entries, nil, nil)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	// Same device, two different leases, and the router's own uppercase
	// MAC casing (#273) against an entry typed in lowercase.
	set.Evaluate(store.Event{SrcMAC: "AA:BB:CC:DD:EE:FF", SrcIP: "192.168.1.5", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now})
	set.Evaluate(store.Event{SrcMAC: "AA:BB:CC:DD:EE:FF", SrcIP: "192.168.1.77", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now.Add(time.Hour)})
	if len(*got) != 2 {
		t.Fatalf("expected both leases to match the MAC-bound expectation, got %d emissions", len(*got))
	}
}

// TestExpectationAddressListIsResolvedLive pins #274 item 2's whole
// point: membership is answered against what the router has pushed at
// the moment the event arrives, not expanded into fixed addresses when
// the entry was created.
func TestExpectationAddressListIsResolvedLive(t *testing.T) {
	lists := fakeLists{"core\x00mgmt": {"192.168.1.200"}}
	entries := []watchlist.Entry{
		{ID: "listed", SourceList: watchlist.AddressListRef{Device: "core", List: "mgmt"}, Ports: []int{22}},
	}
	set, _, got := buildExpectations(t, entries, lists, nil)
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	set.Evaluate(store.Event{SrcIP: "192.168.1.201", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now})
	if len(*got) != 0 {
		t.Fatal("a non-member matched an address-list-scoped expectation")
	}
	// The router adds the address to the list; the very next event is a
	// member, with no change to the entry.
	lists["core\x00mgmt"] = append(lists["core\x00mgmt"], "192.168.1.201")
	set.Evaluate(store.Event{SrcIP: "192.168.1.201", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now.Add(time.Second)})
	if len(*got) != 1 {
		t.Fatalf("expected the newly-listed address to match, got %d emissions", len(*got))
	}
}

// TestExpectationWithNoMembershipResolverMatchesNothing pins the safe
// direction watchlist.MatchWithLists already chose: with no way to
// answer "is this address in that list", the honest answer is not to
// record a match.
func TestExpectationWithNoMembershipResolverMatchesNothing(t *testing.T) {
	entries := []watchlist.Entry{
		{ID: "listed", SourceList: watchlist.AddressListRef{Device: "core", List: "mgmt"}, Ports: []int{22}},
	}
	set, _, got := buildExpectations(t, entries, nil, nil)
	set.Evaluate(store.Event{SrcIP: "192.168.1.200", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: time.Now()})
	if len(*got) != 0 {
		t.Errorf("an entry whose scope cannot be evaluated recorded a match: %+v", (*got)[0])
	}
}

// --- the dispatch pre-index: the reason this port exists ---------------

// TestExpectationsRideTheDispatchIndex is #406's performance mandate as
// a structural assertion rather than a benchmark reading: #397 measured
// the old per-event linear scan at ~2x the ingest budget at 1,000
// entries and ~11x at 5,000, and the ADR's answer is the dispatch
// pre-index. That only pays off if every non-inverted expectation
// actually lands in a discriminating bucket -- one that ends up in the
// always-consulted bucket is scanned for every event, exactly as before.
//
// A non-inverted entry always has at least one port (watchlist.Store
// refuses one without -- ErrNoPorts), and destinationPort is the first
// field discriminantFor tries, so the always-consulted bucket must be
// empty however many entries there are.
func TestExpectationsRideTheDispatchIndex(t *testing.T) {
	var entries []watchlist.Entry
	for i := 0; i < 500; i++ {
		entries = append(entries, watchlist.Entry{
			ID:    "e" + strconv.Itoa(i),
			Ports: []int{2222 + i},
		})
	}
	set, _, _ := buildExpectations(t, entries, nil, nil)

	if got := set.Index().AlwaysConsultedCount(); got != 0 {
		t.Errorf("%d of %d expectations landed in the always-consulted bucket -- every event would scan them, which is the cost this port exists to remove", got, len(entries))
	}
	candidates := set.Index().Candidates(store.Event{DstPort: 2222, ConnState: "new"})
	if len(candidates) != 1 {
		t.Errorf("an event named one watched port and reached %d candidate definitions, want 1", len(candidates))
	}
}

// --- inverted: the state machine, wearing the envelope -----------------

// TestInvertedExpectationObserveThenViolate walks the lifecycle through
// the engine: while observing, a destination is recorded as a candidate
// and nothing fires; once observing stops, a permitted destination still
// never fires and an unpromoted one violates.
func TestInvertedExpectationObserveThenViolate(t *testing.T) {
	src := matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}
	obs := &fakeObservations{}
	observing := watchlist.Entry{ID: "device-x", Invert: true, Observing: true, Source: src}
	_, inv, got := buildExpectations(t, []watchlist.Entry{observing}, nil, obs)

	t0 := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC)
	inv.Evaluate(store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.10", DstPort: 80, ReceivedAt: t0})
	if len(*got) != 0 {
		t.Fatalf("an observing expectation fired: %+v", (*got)[0])
	}
	if len(obs.calls) != 1 || obs.calls[0] != (recordedObservation{"device-x", "198.51.100.10", 80, t0}) {
		t.Fatalf("observations = %+v, want one candidate for 198.51.100.10:80 at %v", obs.calls, t0)
	}

	// Observing stopped, one destination promoted.
	enforced := observing
	enforced.Observing = false
	enforced.Permitted = []watchlist.PermittedDest{{DestIP: "198.51.100.10", Port: 80}}
	_, inv, got = buildExpectations(t, []watchlist.Entry{enforced}, nil, obs)

	t1 := t0.Add(time.Minute)
	inv.Evaluate(store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.10", DstPort: 80, ReceivedAt: t1})
	if len(*got) != 0 {
		t.Fatalf("a permitted destination violated: %+v", (*got)[0])
	}

	t2 := t1.Add(time.Minute)
	inv.Evaluate(store.Event{SrcMAC: src.MAC, DstIP: "198.51.100.20", DstPort: 443, ReceivedAt: t2})
	if len(*got) != 1 {
		t.Fatalf("expected one violation for the unpromoted destination, got %d", len(*got))
	}
	w := (*got)[0].Expectation
	if w == nil {
		t.Fatal("an inverted expectation emitted a detection")
	}
	if w.EntryID != "device-x" {
		t.Errorf("violation EntryID = %q, want the entry's own id -- one definition holds every inverted entry, but each routes under its own envelope", w.EntryID)
	}
	if w.Tuple.DestIP != "198.51.100.20" || w.Tuple.Port != 443 {
		t.Errorf("violation tuple = %+v, want 198.51.100.20:443", w.Tuple)
	}
	if w.Tuple.Source.MAC != src.MAC {
		t.Errorf("violation tuple source = %+v, want the device's MAC", w.Tuple.Source)
	}
}

// TestInvertedExpectationsAreIndexedByDevice pins that the inverted set
// consults only the entries scoping the event's own device, rather than
// asking every inverted entry in turn -- the same narrowing the
// declarative half gets from the dispatch index, for a definition whose
// discriminating field is the device rather than the port.
func TestInvertedExpectationsAreIndexedByDevice(t *testing.T) {
	var entries []watchlist.Entry
	for i := 0; i < 50; i++ {
		entries = append(entries, watchlist.Entry{
			ID:     "d" + strconv.Itoa(i),
			Invert: true,
			Source: matchlog.Identity{IP: "192.168.1." + strconv.Itoa(i)},
		})
	}
	obs := &fakeObservations{}
	_, inv, got := buildExpectations(t, entries, nil, obs)
	if inv.Len() != 50 {
		t.Fatalf("inverted set holds %d entries, want 50", inv.Len())
	}

	inv.Evaluate(store.Event{SrcIP: "192.168.1.7", DstIP: "198.51.100.10", DstPort: 80, ReceivedAt: time.Now()})
	if len(*got) != 1 {
		t.Fatalf("expected exactly one entry to answer for one device, got %d", len(*got))
	}
	if (*got)[0].Expectation.EntryID != "d7" {
		t.Errorf("answering entry = %q, want d7", (*got)[0].Expectation.EntryID)
	}

	// A device no entry scopes reaches nothing at all.
	before := len(*got)
	inv.Evaluate(store.Event{SrcIP: "10.9.9.9", DstIP: "198.51.100.10", DstPort: 80, ReceivedAt: time.Now()})
	if len(*got) != before {
		t.Error("an unscoped device reached an inverted expectation")
	}
}

// TestInvertedExpectationsDeclareTheirReplayStatus is #403's contract
// applied to this port: every definition either implements Replay or
// declares itself non-replayable with a stated reason. The inverted
// state machine declares; the declarative half implements, structurally,
// because Replay lives on DeclarativeDefinition itself.
func TestInvertedExpectationsDeclareTheirReplayStatus(t *testing.T) {
	inv, err := NewInvertedExpectations(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	capable, reason, ok := Replayability(inv)
	if !ok {
		t.Fatal("InvertedExpectations resolves to neither Replayable nor NonReplayable")
	}
	if capable {
		t.Error("InvertedExpectations reports itself replayable")
	}
	if reason == "" {
		t.Error("a non-replayable definition must state why")
	}

	entry := watchlist.Entry{ID: "e1", Ports: []int{22}}
	def, err := ExpectationDefinitionFor(entry)
	if err != nil {
		t.Fatal(err)
	}
	dd, err := BuildExpectationDefinition(def, nil)
	if err != nil {
		t.Fatal(err)
	}
	capable, _, ok = Replayability(dd)
	if !ok || !capable {
		t.Errorf("a non-inverted expectation must be replayable like any other declarative definition (capable=%v ok=%v)", capable, ok)
	}
}

// --- the envelope invariants this shape exists to satisfy --------------

// TestEveryWatchlistEntryShapeBuildsADefinition is the structural guard:
// every entry shape watchlist.Store will accept must convert into a
// Definition that passes Validate and then build into something the
// engine actually evaluates. An entry that stores fine but cannot be
// built is an expectation that silently evaluates nothing, which is the
// "absence of detection presented as absence of threat" failure #380's
// first item names.
func TestEveryWatchlistEntryShapeBuildsADefinition(t *testing.T) {
	entries := []watchlist.Entry{
		{ID: "ports-only", Ports: []int{22}},
		{ID: "many-ports", Ports: []int{22, 80, 443}},
		{ID: "mac-scoped", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}, Ports: []int{22}},
		{ID: "ip-scoped", Source: matchlog.Identity{IP: "192.168.1.5"}, Ports: []int{22}},
		{ID: "dest-scoped", DestIP: "10.0.0.9", Ports: []int{22}},
		{ID: "list-scoped", SourceList: watchlist.AddressListRef{Device: "core", List: "mgmt"}, Ports: []int{22}},
		{ID: "everything", Name: "all axes", Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.1.5"}, DestIP: "10.0.0.9", Ports: []int{22, 2222}, CreatedAt: time.Now()},
		{ID: "inverted", Invert: true, Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}},
		{ID: "inverted-noise", Invert: true, Observing: true, IncludeStructuralNoise: true, Source: matchlog.Identity{IP: "192.168.1.9"}},
	}
	for _, e := range entries {
		t.Run(e.ID, func(t *testing.T) {
			def, err := ExpectationDefinitionFor(e)
			if err != nil {
				t.Fatalf("ExpectationDefinitionFor: %v", err)
			}
			if def.ID != e.ID {
				t.Errorf("definition id = %q, want the entry's own id %q -- identity must survive the conversion", def.ID, e.ID)
			}
			if def.Intent != IntentExpectation {
				t.Errorf("intent = %q, want %q: an expectation must never route to the flag lifecycle", def.Intent, IntentExpectation)
			}
			if err := def.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
			if _, _, err := BuildExpectations([]watchlist.Entry{e}, ExpectationDeps{}); err != nil {
				t.Errorf("BuildExpectations: %v", err)
			}
		})
	}
}

// TestExpectationsNeverProduceFlags pins the epic's own decision that
// the two intents stay distinct: an expectation definition's emission
// feeds the match log and nothing else, whatever its kind.
func TestExpectationsNeverProduceFlags(t *testing.T) {
	entries := []watchlist.Entry{
		{ID: "e1", Ports: []int{22}},
		{ID: "d1", Invert: true, Source: matchlog.Identity{IP: "192.168.1.5"}},
	}
	set, inv, got := buildExpectations(t, entries, nil, nil)
	now := time.Now()
	set.Evaluate(store.Event{SrcIP: "203.0.113.1", DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: now})
	inv.Evaluate(store.Event{SrcIP: "192.168.1.5", DstIP: "198.51.100.10", DstPort: 80, ReceivedAt: now})

	if len(*got) != 2 {
		t.Fatalf("expected one emission from each half, got %d", len(*got))
	}
	for i, r := range *got {
		if r.Detection != nil {
			t.Errorf("emission %d produced a flag: %+v", i, r.Detection)
		}
		if r.Expectation == nil {
			t.Errorf("emission %d produced no match-log write", i)
		}
	}
}

// --- the sink ----------------------------------------------------------

// TestMatchlogSinkRecordsAndCollapses drives the whole path against a
// real matchlog.FileStore: a match is appended under the entry's id with
// the event embedded as evidence, and a repeat of the same tuple
// collapses rather than writing a second record.
func TestMatchlogSinkRecordsAndCollapses(t *testing.T) {
	ml, err := matchlog.Open(filepath.Join(t.TempDir(), "matchlog.jsonl"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer ml.Close()

	set, _, err := BuildExpectations([]watchlist.Entry{{ID: "e1", Ports: []int{22}}}, ExpectationDeps{Sink: MatchlogSink(ml)})
	if err != nil {
		t.Fatal(err)
	}

	t0 := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	e := store.Event{SrcMAC: "AA:BB:CC:DD:EE:FF", SrcIP: "192.168.1.5", DstIP: "10.0.0.1", DstPort: 22, Action: store.ActionAccept, ReceivedAt: t0}
	set.Evaluate(e)
	e.ReceivedAt = t0.Add(time.Minute)
	set.Evaluate(e)

	if stats := ml.Stats(); stats.Count != 1 {
		t.Fatalf("match log holds %d records, want 1 (the repeat must collapse)", stats.Count)
	}
	var recorded []matchlog.Record
	if err := ml.Query(context.Background(), matchlog.Query{Source: matchlog.Identity{MAC: "aa:bb:cc:dd:ee:ff"}}, func(r matchlog.Record) bool {
		recorded = append(recorded, r)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if len(recorded) != 1 {
		t.Fatalf("query returned %d records, want 1", len(recorded))
	}
	got := recorded[0]
	if got.EntryID != "e1" {
		t.Errorf("EntryID = %q, want e1", got.EntryID)
	}
	if got.Count != 2 {
		t.Errorf("Count = %d, want 2", got.Count)
	}
	if !got.FirstSeen.Equal(t0) || !got.LastSeen.Equal(t0.Add(time.Minute)) {
		t.Errorf("FirstSeen/LastSeen = %v/%v, want %v/%v", got.FirstSeen, got.LastSeen, t0, t0.Add(time.Minute))
	}
	if got.Event.Action != store.ActionAccept || got.Event.SrcIP != "192.168.1.5" {
		t.Errorf("embedded event = %+v, want the triggering event verbatim", got.Event)
	}
}

// TestMatchlogSinkDropsAnUnattributableMatch pins where
// matchNonInverted's `id.Empty()` guard went: an event carrying neither
// a source MAC nor a source IP cannot be attributed to a device, so
// nothing is recorded -- and it is dropped silently rather than becoming
// a rate-limited "recording a match failed" warning, which is what
// matchlog.ErrEmptyIdentity would otherwise produce at event rate.
func TestMatchlogSinkDropsAnUnattributableMatch(t *testing.T) {
	ml, err := matchlog.Open(filepath.Join(t.TempDir(), "matchlog.jsonl"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer ml.Close()

	set, _, err := BuildExpectations([]watchlist.Entry{{ID: "e1", Ports: []int{22}}}, ExpectationDeps{Sink: MatchlogSink(ml)})
	if err != nil {
		t.Fatal(err)
	}
	set.Evaluate(store.Event{DstIP: "10.0.0.1", DstPort: 22, ReceivedAt: time.Now()})
	if stats := ml.Stats(); stats.Count != 0 {
		t.Errorf("match log holds %d records, want 0", stats.Count)
	}
}

// TestMatchlogSinkIsANoOpWithoutAStore pins the nil contract every sink
// in this package follows: a deployment whose match log failed to open
// evaluates expectations harmlessly rather than panicking per event.
func TestMatchlogSinkIsANoOpWithoutAStore(t *testing.T) {
	sink := MatchlogSink(nil)
	sink(RoutedEmission{Expectation: &MatchlogWrite{EntryID: "e1"}})
}
