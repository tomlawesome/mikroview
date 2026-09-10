// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/flags"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// -- #641: an expected verdict writes to the watchlist ----------------

// pairedFlag raises an internal_recon flag carrying evidence pairs --
// the shape #641 works on: a device, and the (destination, port)
// combinations it was actually seen making.
func pairedFlag(t *testing.T, s *Server, target string, pairs ...flags.HostPort) flags.Flag {
	t.Helper()
	return pairedFlagSized(t, s, target, 3, pairs...)
}

// pairedFlagSized is pairedFlag with the firing's own size stated, for a
// test that needs a *second* firing of the same pair: an expectation
// recorded from a size-less flag absorbs everything forever (#640,
// Exclusion.Absorbs), so without a size there is no way back into the
// inbox to judge twice.
func pairedFlagSized(t *testing.T, s *Server, target string, size int, pairs ...flags.HostPort) flags.Flag {
	t.Helper()
	s.Flags.AddEmission(flags.TypeInternalRecon, target,
		"3 distinct internal destinations in 60s", nil,
		flags.Evidence{Pairs: pairs, PairsTotal: len(pairs)}, "", false, &size, time.Now())
	for _, f := range s.Flags.List() {
		if f.Target == target {
			return f
		}
	}
	t.Fatalf("no flag raised for %s", target)
	return flags.Flag{}
}

func invertedEntries(t *testing.T, s *Server) []watchlist.Entry {
	t.Helper()
	entries, err := s.Definitions.ListExpectations()
	if err != nil {
		t.Fatalf("ListExpectations: %v", err)
	}
	out := make([]watchlist.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Invert {
			out = append(out, e)
		}
	}
	return out
}

func undoVerdict(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestVerdictExpectedCreatesAnObservingEntryAndPermitsThePairs is the
// issue's own "creating an observing entry where none exists": the
// device had no inverted entry, so one appears holding exactly the
// flag's pairs -- and it is observing, because nothing may fire from a
// step the operator never asked for.
func TestVerdictExpectedCreatesAnObservingEntryAndPermitsThePairs(t *testing.T) {
	s, _ := newTestServer(t)
	f := pairedFlag(t, s, "192.168.1.50",
		flags.HostPort{Host: "192.168.1.10", Port: 445},
		flags.HostPort{Host: "192.168.1.11", Port: 445})

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()

	resp := postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	entries := invertedEntries(t, s)
	if len(entries) != 1 {
		t.Fatalf("expected one inverted entry, got %+v", entries)
	}
	e := entries[0]
	if e.Source.IP != "192.168.1.50" || e.Source.MAC != "" {
		t.Errorf("entry Source = %+v, want the flag's device by IP", e.Source)
	}
	if !e.Observing {
		t.Error("an entry created by an automatic step must start observing, so nothing fires from it")
	}
	want := []watchlist.PermittedDest{{DestIP: "192.168.1.10", Port: 445}, {DestIP: "192.168.1.11", Port: 445}}
	if len(e.Permitted) != len(want) {
		t.Fatalf("Permitted = %+v, want %+v", e.Permitted, want)
	}
	for i, d := range want {
		if e.Permitted[i] != d {
			t.Errorf("Permitted[%d] = %+v, want %+v", i, e.Permitted[i], d)
		}
	}

	// The ledger's half (#640 part C): what was added, by which verdict,
	// and when -- read off the expectation the same verdict recorded.
	ex, ok := s.Flags.Expectation(flags.TypeInternalRecon, "192.168.1.50")
	if !ok {
		t.Fatal("the expected verdict recorded no expectation to hang the permission on")
	}
	if len(ex.Permitted) != 1 {
		t.Fatalf("Exclusion.Permitted = %+v, want one record", ex.Permitted)
	}
	rec := ex.Permitted[0]
	if rec.EntryID != e.ID {
		t.Errorf("record EntryID = %q, want the entry it wrote to (%q)", rec.EntryID, e.ID)
	}
	if !rec.CreatedEntry {
		t.Error("the record must say this verdict created the entry, or undo cannot tell it from the operator's own")
	}
	if rec.Verdict != flags.VerdictExpected {
		t.Errorf("record Verdict = %q, want expected", rec.Verdict)
	}
	if rec.At.IsZero() {
		t.Error("record At must say when")
	}
	if len(rec.Dests) != 2 {
		t.Errorf("record Dests = %+v, want both pairs", rec.Dests)
	}
}

// TestVerdictExpectedPermitsOnTheDeviceExistingEntry pins the other
// branch: a device that already has an inverted entry gets its pairs
// promoted onto that one, and no second entry appears alongside it.
func TestVerdictExpectedPermitsOnTheDeviceExistingEntry(t *testing.T) {
	s, _ := newTestServer(t)
	existing := watchlist.Entry{
		ID:        "entry-existing",
		Name:      "the workstation",
		Source:    matchlog.Identity{IP: "192.168.1.50"},
		Invert:    true,
		Observing: false,
		Permitted: []watchlist.PermittedDest{{DestIP: "192.168.1.1", Port: 53}},
		CreatedAt: time.Now(),
	}
	if err := s.Definitions.UpsertExpectation(existing); err != nil {
		t.Fatal(err)
	}
	f := pairedFlag(t, s, "192.168.1.50", flags.HostPort{Host: "192.168.1.10", Port: 445})

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	resp := postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected")
	resp.Body.Close()

	entries := invertedEntries(t, s)
	if len(entries) != 1 || entries[0].ID != existing.ID {
		t.Fatalf("expected the device's own entry and no second one, got %+v", entries)
	}
	if got := entries[0].Permitted; len(got) != 2 || got[1].DestIP != "192.168.1.10" {
		t.Errorf("Permitted = %+v, want the operator's own pair kept and the flag's appended", got)
	}
	if entries[0].Observing {
		t.Error("permitting onto an existing entry must not put it back into observe mode")
	}
	ex, _ := s.Flags.Expectation(flags.TypeInternalRecon, "192.168.1.50")
	if len(ex.Permitted) != 1 || ex.Permitted[0].CreatedEntry {
		t.Errorf("record = %+v, want one that does not claim to have created the entry", ex.Permitted)
	}
}

// TestVerdictUndoWithdrawsThePermissionAndTheEntryItCreated is the
// reversibility the whole automatic step rests on: undo takes the
// destinations back, and the entry that existed only to hold them goes
// with them.
func TestVerdictUndoWithdrawsThePermissionAndTheEntryItCreated(t *testing.T) {
	s, _ := newTestServer(t)
	f := pairedFlag(t, s, "192.168.1.50", flags.HostPort{Host: "192.168.1.10", Port: 445})

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected").Body.Close()
	if len(invertedEntries(t, s)) != 1 {
		t.Fatal("setup: the expected verdict should have created an entry")
	}

	resp := undoVerdict(t, ts.URL+"/api/flags/verdict/"+f.ID)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status = %d, want 200", resp.StatusCode)
	}
	if got := invertedEntries(t, s); len(got) != 0 {
		t.Errorf("entries after undo = %+v, want none: the entry existed only to hold a permission that has been withdrawn", got)
	}
	if _, ok := s.Flags.Expectation(flags.TypeInternalRecon, "192.168.1.50"); ok {
		t.Error("undo should have withdrawn the expectation too (#640)")
	}
}

// TestVerdictUndoKeepsAnEntryTheOperatorAlreadyHad is the other half of
// the same reversal: an entry the verdict did not create keeps
// everything that was not this verdict's doing.
func TestVerdictUndoKeepsAnEntryTheOperatorAlreadyHad(t *testing.T) {
	s, _ := newTestServer(t)
	existing := watchlist.Entry{
		ID:        "entry-existing",
		Source:    matchlog.Identity{IP: "192.168.1.50"},
		Invert:    true,
		Permitted: []watchlist.PermittedDest{{DestIP: "192.168.1.1", Port: 53}},
		CreatedAt: time.Now(),
	}
	if err := s.Definitions.UpsertExpectation(existing); err != nil {
		t.Fatal(err)
	}
	f := pairedFlag(t, s, "192.168.1.50", flags.HostPort{Host: "192.168.1.10", Port: 445})

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected").Body.Close()
	undoVerdict(t, ts.URL+"/api/flags/verdict/"+f.ID).Body.Close()

	entries := invertedEntries(t, s)
	if len(entries) != 1 {
		t.Fatalf("the operator's own entry must survive an undo, got %+v", entries)
	}
	want := []watchlist.PermittedDest{{DestIP: "192.168.1.1", Port: 53}}
	if len(entries[0].Permitted) != 1 || entries[0].Permitted[0] != want[0] {
		t.Errorf("Permitted = %+v, want only what the operator permitted themselves (%+v)", entries[0].Permitted, want)
	}
}

// TestRejudgingAwayFromExpectedWithdrawsThePermission mirrors #640's
// rule for the expectation itself: changing one's mind is an undo, not a
// second judgement stacked on the first, so the permission goes too.
func TestRejudgingAwayFromExpectedWithdrawsThePermission(t *testing.T) {
	s, _ := newTestServer(t)
	f := pairedFlag(t, s, "192.168.1.50", flags.HostPort{Host: "192.168.1.10", Port: 445})

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected").Body.Close()
	postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "checked").Body.Close()

	if got := invertedEntries(t, s); len(got) != 0 {
		t.Errorf("entries after re-judging = %+v, want none", got)
	}
}

// TestASecondExpectedVerdictIsUndoneOnItsOwn is why the record is one
// per verdict rather than one per expectation: a firing that came back
// past the ceiling and was judged normal again permits whatever *it*
// saw, and undoing that second judgement must take back only its own
// additions -- exactly as #640's undo restores the expectation to the
// size the first verdict left it at, not to nothing.
func TestASecondExpectedVerdictIsUndoneOnItsOwn(t *testing.T) {
	s, _ := newTestServer(t)
	f := pairedFlag(t, s, "192.168.1.50", flags.HostPort{Host: "192.168.1.10", Port: 445})

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected").Body.Close()

	// The same pair fires again, reaching somewhere new, and is judged
	// normal a second time. Past the recorded size by more than the
	// tolerance, or the expectation would absorb it and there would be no
	// second flag to judge (#640).
	size := 40
	s.Flags.AddEmission(flags.TypeInternalRecon, "192.168.1.50", "40 distinct internal destinations in 60s", nil,
		flags.Evidence{Pairs: []flags.HostPort{
			{Host: "192.168.1.10", Port: 445},
			{Host: "192.168.1.12", Port: 139},
		}}, "", false, &size, time.Now())
	postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected").Body.Close()

	ex, _ := s.Flags.Expectation(flags.TypeInternalRecon, "192.168.1.50")
	if len(ex.Permitted) != 2 {
		t.Fatalf("Exclusion.Permitted = %+v, want one record per verdict", ex.Permitted)
	}

	undoVerdict(t, ts.URL+"/api/flags/verdict/"+f.ID).Body.Close()

	entries := invertedEntries(t, s)
	if len(entries) != 1 {
		t.Fatalf("the entry still holds the first verdict's permission, so it must survive: %+v", entries)
	}
	want := watchlist.PermittedDest{DestIP: "192.168.1.10", Port: 445}
	if len(entries[0].Permitted) != 1 || entries[0].Permitted[0] != want {
		t.Errorf("Permitted = %+v, want only the first verdict's %+v", entries[0].Permitted, want)
	}
}

// TestVerdictExpectedWithoutPairsWritesNothingToTheWatchlist: a flag
// whose detector records no pairs permits nothing. Ports and Hosts are
// two independent sets, and crossing them would permit combinations the
// device never made -- the fabrication Evidence.Pairs exists to rule out
// (#654), so the absence of pairs is a refusal, not a fallback.
func TestVerdictExpectedWithoutPairsWritesNothingToTheWatchlist(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.AddWithDetail(flags.TypePortScan, "192.168.1.50", "20 distinct ports in 60s", 50,
		flags.Evidence{Ports: []int{22, 23, 25}, Hosts: []string{"192.168.1.10", "192.168.1.11"}}, "", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	resp := postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "expected")
	defer resp.Body.Close()

	var f flags.Flag
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		t.Fatal(err)
	}
	if !f.Cleared || f.Verdict != flags.VerdictExpected {
		t.Errorf("the verdict itself must still land: %+v", f)
	}
	if got := invertedEntries(t, s); len(got) != 0 {
		t.Errorf("entries = %+v, want none: there were no observed pairs to permit", got)
	}
	ex, _ := s.Flags.Expectation(flags.TypePortScan, "192.168.1.50")
	if len(ex.Permitted) != 0 {
		t.Errorf("Exclusion.Permitted = %+v, want nothing recorded", ex.Permitted)
	}
}

// TestVerdictExpectedOnATargetThatIsNoDeviceWritesNothing: an inverted
// entry is a policy about one device, and a target that is not an
// address does not name one (a rule label, a port, "global"). Nothing is
// written rather than an entry scoped to a string that can never match.
func TestVerdictExpectedOnATargetThatIsNoDeviceWritesNothing(t *testing.T) {
	s, _ := newTestServer(t)
	f := pairedFlag(t, s, "port 445", flags.HostPort{Host: "192.168.1.10", Port: 445})

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	postVerdict(t, ts.URL+"/api/flags/"+f.ID+"/verdict", "expected").Body.Close()

	if got := invertedEntries(t, s); len(got) != 0 {
		t.Errorf("entries = %+v, want none", got)
	}
}

// TestVerdictExpectedPrefersTheEvidenceMAC: where the evidence carries a
// MAC, the entry is scoped by it -- an IP-bound entry stops matching the
// device the moment its DHCP lease changes, which is the whole reason
// matchlog.Identity is MAC-preferred.
func TestVerdictExpectedPrefersTheEvidenceMAC(t *testing.T) {
	s, _ := newTestServer(t)
	s.Flags.AddWithDetail(flags.TypeInternalRecon, "192.168.1.50", "3 distinct internal destinations in 60s", 40,
		flags.Evidence{
			Pairs:  []flags.HostPort{{Host: "192.168.1.10", Port: 445}},
			SrcMAC: "52:55:0a:00:02:02",
		}, "", time.Now())
	id := s.Flags.List()[0].ID

	ts := httptest.NewServer(asAdmin(s.mux()))
	defer ts.Close()
	postVerdict(t, ts.URL+"/api/flags/"+id+"/verdict", "expected").Body.Close()

	entries := invertedEntries(t, s)
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %+v", entries)
	}
	if entries[0].Source.MAC != "52:55:0a:00:02:02" {
		t.Errorf("Source = %+v, want the device identified by its MAC", entries[0].Source)
	}
}
