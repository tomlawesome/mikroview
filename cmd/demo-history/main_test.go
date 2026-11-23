// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// #1451: the demo-history generator had no test at all.

var fixedNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func TestNightsForFollowsEachStory(t *testing.T) {
	count := func(ns []watchlist.Night, s watchlist.NightState) int {
		n := 0
		for _, x := range ns {
			if x.State == s {
				n++
			}
		}
		return n
	}
	healthy := nightsFor(storyHealthy, fixedNow)
	if len(healthy) != watchlist.MaxNights || count(healthy, watchlist.NightKept) != len(healthy) {
		t.Errorf("healthy: %d nights, %d kept; want all %d kept", len(healthy), count(healthy, watchlist.NightKept), watchlist.MaxNights)
	}
	for _, st := range []story{storyHeld, storyNoCoverage} {
		ns := nightsFor(st, fixedNow)
		if len(ns) == 0 || count(ns, watchlist.NightUnobserved) != len(ns) {
			t.Errorf("story %d: want every night unobserved, got %+v", st, ns)
		}
	}
	broken := nightsFor(storyBrokenRing, fixedNow)
	if len(broken) < 3 || count(broken, watchlist.NightEmpty) != 2 {
		t.Fatalf("broken ring: %d nights, %d empty; want exactly the last two empty", len(broken), count(broken, watchlist.NightEmpty))
	}
	if broken[len(broken)-1].State != watchlist.NightEmpty || broken[len(broken)-2].State != watchlist.NightEmpty || broken[0].State != watchlist.NightKept {
		t.Errorf("broken ring order wrong: %+v", broken)
	}
	k := keptNight(watchlist.Occurrence{Open: fixedNow})
	if k.State != watchlist.NightKept || k.Count != 4 || !k.First.After(fixedNow) {
		t.Errorf("keptNight = %+v", k)
	}
}

func TestBuildDefinitionsStoresEveryDemoEntry(t *testing.T) {
	doc, err := buildDefinitions(fixedNow)
	if err != nil {
		t.Fatalf("buildDefinitions: %v", err)
	}
	// Reload through the real store: the document must be one it accepts.
	path := filepath.Join(t.TempDir(), "defs.json")
	if err := os.WriteFile(path, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	defs, err := engine.OpenDefinitionsStore(path)
	if err != nil {
		t.Fatalf("reopening the generated document: %v", err)
	}
	for _, d := range demoWatchlist {
		got, ok := defs.Get(d.id)
		if !ok {
			t.Errorf("entry %q missing from the document", d.id)
			continue
		}
		if got.Definition.Enabled == d.paused {
			t.Errorf("entry %q: Enabled = %v, want %v (paused=%v)", d.id, got.Definition.Enabled, !d.paused, d.paused)
		}
	}
}

func TestTrafficGeneratorsStayInsideTheirBounds(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		if f := diurnalPick(rng); f < 0 || f >= 1 {
			t.Fatalf("diurnalPick = %v, want [0,1)", f)
		}
	}
	day := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	bg := backgroundTrafficFor(day, rng)
	if len(bg) != 220 {
		t.Errorf("backgroundTrafficFor produced %d records, want 220", len(bg))
	}
	for _, r := range bg {
		if r.ReceivedAt.Before(day) || !r.ReceivedAt.Before(day.Add(24*time.Hour)) {
			t.Fatalf("background record at %v is outside its day", r.ReceivedAt)
		}
	}

	d := demoWatchlist[0]
	occ := nightlyWindow.ClosedSince(time.Time{}, fixedNow, 1)[0]
	m := matchingTrafficFor(d, occ, rng)
	if len(m) < 3 || len(m) > 5 {
		t.Errorf("matchingTrafficFor produced %d records, want 3-5", len(m))
	}
	for _, r := range m {
		if r.Event.DstIP != d.ip {
			t.Errorf("matching record to %q, want the entry's address %q", r.Event.DstIP, d.ip)
		}
		okPort := false
		for _, p := range d.ports {
			okPort = okPort || p == r.Event.DstPort
		}
		if !okPort {
			t.Errorf("matching record to port %d, not one of %v", r.Event.DstPort, d.ports)
		}
		if r.ReceivedAt.Before(occ.Open) || r.ReceivedAt.After(occ.Close) {
			t.Errorf("matching record at %v is outside the night %v-%v", r.ReceivedAt, occ.Open, occ.Close)
		}
	}
}

func TestSyntheticRecordCarriesItsRawLine(t *testing.T) {
	at := fixedNow
	r := syntheticRecord(at, "192.0.2.1", "aa:bb", "203.0.113.5", 443, store.ActionDrop, "forward")
	if r.Event.SrcIP != "192.0.2.1" || r.Event.DstPort != 443 || r.Event.Action != store.ActionDrop || !r.ReceivedAt.Equal(at) {
		t.Errorf("record = %+v", r)
	}
	if !strings.Contains(r.Event.Raw, "203.0.113.5:443") || !strings.HasPrefix(r.Event.Raw, "forward:") {
		t.Errorf("Raw = %q", r.Event.Raw)
	}
}

func TestSortRecordsAndMarshalRecords(t *testing.T) {
	t0 := fixedNow
	recs := []retainedRecord{
		syntheticRecord(t0.Add(2*time.Hour), "192.0.2.1", "", "203.0.113.5", 80, store.ActionAccept, "forward"),
		syntheticRecord(t0, "192.0.2.2", "", "203.0.113.5", 80, store.ActionAccept, "forward"),
		syntheticRecord(t0.Add(time.Hour), "192.0.2.3", "", "203.0.113.5", 80, store.ActionAccept, "forward"),
	}
	sortRecords(recs)
	for i := 1; i < len(recs); i++ {
		if recs[i].ReceivedAt.Before(recs[i-1].ReceivedAt) {
			t.Fatalf("not sorted: %v", recs)
		}
	}
	b, err := marshalRecords(recs)
	if err != nil {
		t.Fatal(err)
	}
	var back []retainedRecord
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("output is not a JSON array of records: %v", err)
	}
	if len(back) != 3 || back[0].Event.SrcIP != "192.0.2.2" || !back[0].ReceivedAt.Equal(t0) {
		t.Errorf("round trip = %+v", back)
	}
	if empty, _ := marshalRecords(nil); string(empty) != "[]" {
		t.Errorf("marshalRecords(nil) = %q, want []", empty)
	}
}

func TestBuildRetainedEventsIsChronologicalAndNeverLaterThanNow(t *testing.T) {
	const days = 3
	b, n, err := buildRetainedEvents(fixedNow, days)
	if err != nil {
		t.Fatal(err)
	}
	var recs []retainedRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != n || n < days*220 {
		t.Fatalf("%d records decoded, count reported %d, want at least %d", len(recs), n, days*220)
	}
	for i, r := range recs {
		if i > 0 && r.ReceivedAt.Before(recs[i-1].ReceivedAt) {
			t.Fatalf("record %d out of order", i)
		}
		if r.ReceivedAt.After(fixedNow) {
			t.Fatalf("record at %v is in the future", r.ReceivedAt)
		}
	}

	// Background traffic (the only kind with a source MAC) ends before
	// today; matching traffic may fall in this morning's closed window.
	today := fixedNow.Truncate(24 * time.Hour)
	demoIPs := map[string]bool{}
	for _, d := range demoWatchlist {
		demoIPs[d.ip] = true
	}
	last := nightlyWindow.ClosedSince(time.Time{}, fixedNow, 1)[0]
	matches := 0
	for _, r := range recs {
		if r.Event.SrcMAC != "" && !r.ReceivedAt.Before(today) {
			t.Fatalf("background record at %v runs into today (%v)", r.ReceivedAt, today)
		}
		if demoIPs[r.Event.DstIP] && !r.ReceivedAt.Before(last.Open) && !r.ReceivedAt.After(last.Close) {
			matches++
		}
	}
	if matches < 1 {
		t.Errorf("no matching event in the last closed window %v-%v", last.Open, last.Close)
	}
}

func TestCryptoSeedDiffers(t *testing.T) {
	a, b := cryptoSeed(), cryptoSeed()
	if a == b {
		t.Error("two seeds were equal")
	}
}

// main is exercised by re-running this test binary as the command, since
// it calls os.Exit.
func TestMainWritesAnEnvelopeAndRefusesToOverwrite(t *testing.T) {
	if os.Getenv("DEMO_HISTORY_AS_MAIN") == "1" {
		os.Args = append([]string{"demo-history"}, strings.Fields(os.Getenv("DEMO_HISTORY_ARGS"))...)
		main()
		return
	}
	run := func(args string) (string, int) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainWritesAnEnvelopeAndRefusesToOverwrite$")
		cmd.Env = append(os.Environ(), "DEMO_HISTORY_AS_MAIN=1", "DEMO_HISTORY_ARGS="+args)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	path := filepath.Join(t.TempDir(), "demo.mvbak")

	if out, code := run(""); code != 2 || !strings.Contains(out, "usage") {
		t.Errorf("no -out: code %d, out %q; want 2 and a usage line", code, out)
	}
	if _, code := run("-out " + path + " -days 0"); code != 2 {
		t.Errorf("-days 0: code %d, want 2", code)
	}
	out, code := run("-out " + path + " -days 2 -app-version demo-test")
	if code != 0 || !strings.Contains(out, "wrote "+path) || !strings.Contains(out, "6 watchlist entries") {
		t.Fatalf("first run: code %d, out %q", code, out)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	env, err := backup.Read(f)
	if err != nil {
		t.Fatalf("the written file is not a readable envelope: %v", err)
	}
	if env.AppVersion != "demo-test" || len(env.Stores["definitions"]) == 0 || len(env.Stores[retainedEventsStore]) == 0 {
		t.Errorf("envelope = version %q, stores %v", env.AppVersion, len(env.Stores))
	}
	if _, code := run("-out " + path); code != 1 {
		t.Errorf("second run without -force: code %d, want 1", code)
	}
	if _, code := run("-out " + path + " -days 1 -force"); code != 0 {
		t.Errorf("run with -force: code %d, want 0", code)
	}
}
