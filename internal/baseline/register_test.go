// SPDX-License-Identifier: AGPL-3.0-only

package baseline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// noon is a fixed reference instant. Tests that care about calendar days
// use it plus a whole number of days, so a test never straddles a real
// midnight and starts failing overnight.
var noon = time.Date(2026, 9, 1, 12, 0, 0, 0, time.Local)

func day(n int) time.Time { return noon.AddDate(0, 0, n) }

func newTestRegister(t *testing.T) *Register {
	t.Helper()
	r, err := Open("", DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

func TestKeyForIsTheAgreedShape(t *testing.T) {
	cases := []struct {
		src, dst string
		port     int
		proto    string
		want     string
		why      string
	}{
		{"10.0.0.5", "10.0.0.1", 443, "tcp", "10.0.0.5|10.0.0.1|443|tcp",
			"the four-field key the Go side, the API and the browser all address a line by"},
		{"10.0.0.5", "10.0.0.1", 0, "icmp", "10.0.0.5|10.0.0.1||icmp",
			"no destination port leaves the field empty rather than writing 0, which is not a port"},
		{"10.0.0.5", "10.0.0.1", 53, "", "10.0.0.5|10.0.0.1|53|",
			"an unknown protocol leaves the field empty rather than guessing one"},
	}
	for _, c := range cases {
		if got := KeyFor(c.src, c.dst, c.port, c.proto); got != c.want {
			t.Errorf("KeyFor(%q, %q, %d, %q) = %q, want %q -- %s",
				c.src, c.dst, c.port, c.proto, got, c.want, c.why)
		}
	}
}

func TestRegistersDefersToTheHostRuleAndNeedsADestination(t *testing.T) {
	cases := []struct {
		iface, src, dst string
		port            int
		proto           string
		want            bool
		why             string
	}{
		{"bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", true,
			"a private source on an inbound interface, talking to somewhere: the ordinary case"},
		{"bridge-lan", "10.0.10.5", "203.0.113.9", 443, "tcp", true,
			"a public destination is deliberately allowed -- a private host reaching somewhere new on the Internet is the case the sieve exists for"},
		{"bridge-lan", "203.0.113.9", "10.0.0.1", 443, "tcp", false,
			"a public source is not a host by hosts.Registers, so it is not the near end of a line either"},
		{"", "10.0.10.5", "10.0.0.1", 443, "tcp", false,
			"no inbound interface means no boundary, exactly as for the host register"},
		{"bridge-lan", "", "10.0.0.1", 443, "tcp", false,
			"no source address means no line"},
		{"bridge-lan", "10.0.10.5", "", 443, "tcp", false,
			"no destination means there is no line to draw, only a host"},
		{"bridge-lan", "10.0.10.5", strings.Repeat("a", 200), 443, "tcp", false,
			"a key too long to be addressed by the expected endpoints is not worth holding"},
		{"bridge-lan", "10.0.10.5", "10.0.0.1\n", 443, "tcp", false,
			"a control character would flow into the UI and the audit trail"},
	}
	for _, c := range cases {
		if got := Registers(c.iface, c.src, c.dst, c.port, c.proto); got != c.want {
			t.Errorf("Registers(%q, %q, %q, %d, %q) = %v, want %v -- %s",
				c.iface, c.src, c.dst, c.port, c.proto, got, c.want, c.why)
		}
	}
}

// TestThresholdIsNOfM is the ratified 3-of-14 rule, exercised across the
// boundary in both directions and against a non-default threshold.
//
// The dates are the point: the same number of *observations* spread over
// a different number of *days* has to answer differently, because
// recurrence is the whole claim the baseline makes.
func TestThresholdIsNOfM(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		// seenOn is which day offsets (relative to day 0) the line was
		// seen on. The line is then judged as of onDay.
		seenOn []int
		onDay  int
		want   bool
		why    string
	}{
		{"two of the last fourteen is not established", DefaultConfig(),
			[]int{0, 1}, 1, false,
			"two distinct days is under the threshold of three"},
		{"three of the last fourteen is established", DefaultConfig(),
			[]int{0, 1, 2}, 2, true,
			"three distinct days is the ratified threshold, met exactly"},
		{"many sightings on one day is not established", DefaultConfig(),
			[]int{0, 0, 0, 0, 0, 0}, 0, false,
			"volume is not recurrence -- six events in a day is still one day, and this is the distinction the whole feature rests on"},
		{"three days spread over the window is established", DefaultConfig(),
			[]int{0, 6, 13}, 13, true,
			"the days need not be consecutive, only distinct and inside the window"},
		{"days that have aged out of the window no longer count", DefaultConfig(),
			[]int{0, 1, 2}, 20, false,
			"judged on day 20, days 0-2 are outside the 14-day window, so a once-established line has lapsed"},
		{"a longer window keeps them in", Config{Days: 3, Of: 28},
			[]int{0, 1, 2}, 20, true,
			"the same history against a 28-day window still holds all three days"},
		{"a stricter threshold refuses the same history", Config{Days: 5, Of: 14},
			[]int{0, 1, 2}, 2, false,
			"three days does not meet a threshold of five"},
		{"a threshold of one establishes on first sight", Config{Days: 1, Of: 14},
			[]int{0}, 0, true,
			"one day of one is met the moment the line appears"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Open("", c.cfg)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			for _, d := range c.seenOn {
				if !r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(d)) {
					t.Fatalf("observation on day %d was not registered", d)
				}
			}
			l, ok := r.Get(KeyFor("10.0.10.5", "10.0.0.1", 443, "tcp"))
			if !ok {
				t.Fatal("the line was not registered at all")
			}
			if got := l.Established(c.cfg.Normalise(), day(c.onDay)); got != c.want {
				t.Errorf("Established = %v, want %v (seen on days %v, judged on day %d, %d-of-%d) -- %s",
					got, c.want, c.seenOn, c.onDay, c.cfg.Days, c.cfg.Of, c.why)
			}
		})
	}
}

// TestDayRolloverShiftsRatherThanRecomputing pins the mechanism, not
// just the answer: the bitmap is shifted forward when the date changes,
// and the per-day counters reset with it.
func TestDayRolloverShiftsRatherThanRecomputing(t *testing.T) {
	r := newTestRegister(t)
	key := KeyFor("10.0.10.5", "10.0.0.1", 443, "tcp")

	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(0))
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(0).Add(time.Hour))

	l, _ := r.Get(key)
	if l.CountToday != 2 {
		t.Errorf("CountToday = %d after two sightings on one day, want 2", l.CountToday)
	}
	if l.Days != 0b1 {
		t.Errorf("Days = %b after one day, want 1 -- bit 0 is the anchor day", l.Days)
	}

	// Next calendar day: the bitmap shifts, yesterday stays recorded, and
	// today's counters start again.
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(1))
	l, _ = r.Get(key)
	if l.Days != 0b11 {
		t.Errorf("Days = %b after a second, consecutive day, want 11", l.Days)
	}
	if l.CountToday != 1 {
		t.Errorf("CountToday = %d after the rollover, want 1 -- yesterday's count is not today's", l.CountToday)
	}
	if !l.FirstSeenToday.Equal(day(1)) {
		t.Errorf("FirstSeenToday = %v after the rollover, want %v", l.FirstSeenToday, day(1))
	}
	if !l.FirstSeen.Equal(day(0)) {
		t.Errorf("FirstSeen = %v, want %v -- the all-time first-seen must survive a rollover", l.FirstSeen, day(0))
	}

	// A three-day gap leaves the skipped days unset rather than filled.
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(4))
	l, _ = r.Get(key)
	if l.Days != 0b11001 {
		t.Errorf("Days = %b after a gap, want 11001 -- days nothing was seen on must stay unset", l.Days)
	}
	if got := l.DistinctDays(DefaultConfig(), day(4)); got != 3 {
		t.Errorf("DistinctDays = %d, want 3", got)
	}

	// A gap wider than the bitmap clears it: a line unseen for MaxDays
	// has no recurrence left inside any valid window.
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(4+MaxDays))
	l, _ = r.Get(key)
	if l.Days != 0b1 {
		t.Errorf("Days = %b after a gap wider than the bitmap, want 1 -- only today survives", l.Days)
	}
}

// TestReadsRollForwardWithoutBeingObserved covers the half a midnight
// sweep would otherwise have to do: a line last seen days ago must
// answer correctly without anything having touched it since.
func TestReadsRollForwardWithoutBeingObserved(t *testing.T) {
	r := newTestRegister(t)
	for d := range 3 {
		r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(d))
	}
	l, _ := r.Get(KeyFor("10.0.10.5", "10.0.0.1", 443, "tcp"))

	if !l.Established(DefaultConfig(), day(2)) {
		t.Fatal("three distinct days should be established on the day of the last sighting")
	}
	if !l.Established(DefaultConfig(), day(13)) {
		t.Error("still inside the 14-day window on day 13, so it should still be established without having been re-observed")
	}
	if l.Established(DefaultConfig(), day(30)) {
		t.Error("all three days are long outside the window by day 30, so the line has lapsed -- and nothing re-observed it to make that true")
	}
	if l.SeenOn(day(30)) {
		t.Error("SeenOn must be false for a day the line was not seen on")
	}
}

func TestOffTodayReturnsOnlyTodaysUnestablishedLines(t *testing.T) {
	r := newTestRegister(t)

	// An established line: three days running, seen again today.
	for d := range 3 {
		r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(d))
	}
	// A brand-new line, today only.
	r.Observe("bridge-lan", "10.0.10.5", "203.0.113.9", 8443, "tcp", OutcomeAccept, day(2))
	// A line off pattern but not seen today: it was novel yesterday and
	// is nobody's business now.
	r.Observe("bridge-lan", "10.0.10.6", "203.0.113.9", 22, "tcp", OutcomeDrop, day(1))

	off := r.OffToday(day(2))
	if len(off) != 1 {
		var got []string
		for _, l := range off {
			got = append(got, l.Key)
		}
		t.Fatalf("OffToday returned %d lines (%v), want exactly 1 -- the established line and yesterday's novelty both belong out", len(off), got)
	}
	if want := KeyFor("10.0.10.5", "203.0.113.9", 8443, "tcp"); off[0].Key != want {
		t.Errorf("OffToday returned %q, want %q", off[0].Key, want)
	}
}

func TestWorstOutcomeOfTheDayIsKept(t *testing.T) {
	r := newTestRegister(t)
	key := KeyFor("10.0.10.5", "10.0.0.1", 22, "tcp")

	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 22, "tcp", OutcomeAccept, day(0))
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 22, "tcp", OutcomeDrop, day(0).Add(time.Minute))
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 22, "tcp", OutcomeAccept, day(0).Add(2*time.Minute))

	l, _ := r.Get(key)
	if l.OutcomeToday != OutcomeDrop {
		t.Errorf("OutcomeToday = %q, want %q -- a line refused once has been refused, and a later accept does not unsay it", l.OutcomeToday, OutcomeDrop)
	}

	// The next day starts clean: yesterday's refusal is not today's.
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 22, "tcp", OutcomeAccept, day(1))
	l, _ = r.Get(key)
	if l.OutcomeToday != OutcomeAccept {
		t.Errorf("OutcomeToday = %q after the rollover, want %q", l.OutcomeToday, OutcomeAccept)
	}
}

func TestExpectedMarkEstablishesImmediately(t *testing.T) {
	r := newTestRegister(t)
	key := KeyFor("10.0.10.5", "203.0.113.9", 8443, "tcp")

	r.Observe("bridge-lan", "10.0.10.5", "203.0.113.9", 8443, "tcp", OutcomeAccept, day(0))
	l, _ := r.Get(key)
	if l.Established(DefaultConfig(), day(0)) {
		t.Fatal("a line seen on one day is not established -- the test's premise is wrong")
	}

	if _, err := r.Expect(key, "nightly offsite backup", "tom"); err != nil {
		t.Fatalf("Expect: %v", err)
	}
	l, _ = r.Get(key)
	if !l.Established(DefaultConfig(), day(0)) {
		t.Error("an expected line must read established immediately -- it is the only way a line leaves the bright state early")
	}
	if l.Expected == nil || l.Expected.Reason != "nightly offsite backup" || l.Expected.By != "tom" {
		t.Errorf("Expected = %+v, want the reason and actor as given", l.Expected)
	}
	if l.Expected.At.IsZero() {
		t.Error("the mark must record when it was made")
	}
	if len(r.OffToday(day(0))) != 0 {
		t.Error("an expected line must drop out of the off-baseline answer")
	}

	// And it stays established on a day it was not even seen.
	if !l.Established(DefaultConfig(), day(90)) {
		t.Error("an expected mark is unconditional -- recurrence lapsing does not withdraw it")
	}

	if !r.Unexpect(key) {
		t.Fatal("Unexpect reported nothing to remove")
	}
	l, _ = r.Get(key)
	if l.Established(DefaultConfig(), day(0)) {
		t.Error("withdrawing the mark must put the line back to what its own recurrence says")
	}
	if r.Unexpect(key) {
		t.Error("Unexpect on an already-unmarked line must report false, which the API turns into a 404")
	}
}

func TestExpectRefusesWhatItShould(t *testing.T) {
	r := newTestRegister(t)
	key := KeyFor("10.0.10.5", "10.0.0.1", 443, "tcp")
	r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(0))

	cases := []struct {
		name, key, reason string
		wantErr           error
	}{
		{"an empty reason", key, "", ErrInvalidExpected},
		{"a reason of only control characters", key, "\n\n", ErrInvalidExpected},
		{"an over-long reason", key, strings.Repeat("x", maxReasonLength+1), ErrInvalidExpected},
		{"an empty key", "", "fine", ErrInvalidExpected},
		{"a key no event has registered", "10.9.9.9|10.0.0.1|443|tcp", "fine", ErrUnknownLine},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := r.Expect(c.key, c.reason, "tom"); err != c.wantErr {
				t.Errorf("Expect = %v, want %v", err, c.wantErr)
			}
		})
	}

	// The reason is the substance of the mark, so a refused one must not
	// have left a mark behind.
	l, _ := r.Get(key)
	if l.Expected != nil {
		t.Error("a refused Expect must not have recorded anything")
	}
}

// TestEvictionNeverDropsAnExpectedLine is the promise that an operator's
// own statement outlives capacity pressure: everything else in the
// register can be rebuilt from the feed, and that cannot.
func TestEvictionNeverDropsAnExpectedLine(t *testing.T) {
	r := newTestRegister(t)

	// One line, marked expected, and deliberately the oldest thing in the
	// register -- so a naive oldest-first eviction would take it first.
	precious := KeyFor("10.0.0.1", "10.0.0.2", 443, "tcp")
	r.Observe("bridge-lan", "10.0.0.1", "10.0.0.2", 443, "tcp", OutcomeAccept, day(0))
	if _, err := r.Expect(precious, "the one thing that must survive", "tom"); err != nil {
		t.Fatalf("Expect: %v", err)
	}

	// Fill well past the cap with newer, unmarked lines.
	for i := range MaxLines + 500 {
		r.Observe("bridge-lan", "10.0.10.5", testIP(i), 443, "tcp", OutcomeAccept, day(1))
	}

	if len(r.List()) > MaxLines {
		t.Errorf("the register holds %d lines, over the cap of %d", len(r.List()), MaxLines)
	}
	l, ok := r.Get(precious)
	if !ok {
		t.Fatal("the expected line was evicted under capacity pressure -- it is the one entry that must never be")
	}
	if l.Expected == nil {
		t.Error("the expected line survived but lost its mark")
	}
}

func TestEvictionTakesTheOldestUnmarkedLine(t *testing.T) {
	r := newTestRegister(t)

	// A destination outside the 10/8 range testIP walks, so the filler
	// below cannot accidentally re-observe this very line and refresh the
	// last-seen time the test is about.
	oldest := KeyFor("10.0.10.5", "192.168.99.99", 443, "tcp")
	r.Observe("bridge-lan", "10.0.10.5", "192.168.99.99", 443, "tcp", OutcomeAccept, day(0))
	for i := range MaxLines {
		r.Observe("bridge-lan", "10.0.10.5", testIP(i), 443, "tcp", OutcomeAccept, day(1))
	}

	if _, ok := r.Get(oldest); ok {
		t.Error("the oldest unmarked line should have been evicted to make room")
	}
}

// TestPersistenceRoundTrip is the point of the whole store: a baseline
// that resets on restart would read every established line as new, which
// is exactly the state the feature exists to distinguish from.
func TestPersistenceRoundTrip(t *testing.T) {
	// Every call persists immediately, so the test never waits out the
	// debounce -- same convention internal/hosts' tests use.
	old := persistMinInterval
	persistMinInterval = 0
	t.Cleanup(func() { persistMinInterval = old })

	path := filepath.Join(t.TempDir(), "baseline.json")
	key := KeyFor("10.0.10.5", "10.0.0.1", 443, "tcp")
	marked := KeyFor("10.0.10.5", "203.0.113.9", 8443, "tcp")

	r, err := Open(path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for d := range 3 {
		r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(d))
	}
	r.Observe("bridge-lan", "10.0.10.5", "203.0.113.9", 8443, "tcp", OutcomeDrop, day(2))
	if _, err := r.Expect(marked, "nightly offsite backup", "tom"); err != nil {
		t.Fatalf("Expect: %v", err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen, as a restart would.
	again, err := Open(path, DefaultConfig())
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}

	l, ok := again.Get(key)
	if !ok {
		t.Fatal("the established line did not survive the restart -- every line would read as new, which is the failure this store exists to prevent")
	}
	if l.Days != 0b111 {
		t.Errorf("Days = %b after reload, want 111 -- the recurrence bitmap is the state that matters", l.Days)
	}
	if !l.Established(DefaultConfig(), day(2)) {
		t.Error("a line established before the restart must still be established after it")
	}
	if !l.FirstSeen.Equal(day(0)) {
		t.Errorf("FirstSeen = %v after reload, want %v", l.FirstSeen, day(0))
	}

	m, ok := again.Get(marked)
	if !ok {
		t.Fatal("the expected line did not survive the restart")
	}
	if m.Expected == nil || m.Expected.Reason != "nightly offsite backup" || m.Expected.By != "tom" {
		t.Errorf("Expected = %+v after reload, want the reason and actor intact", m.Expected)
	}
	if m.OutcomeToday != OutcomeDrop {
		t.Errorf("OutcomeToday = %q after reload, want %q", m.OutcomeToday, OutcomeDrop)
	}
}

// TestLoadDropsAReasonlessMark guards the one corrupt record that would
// otherwise be permanent: a mark with no reason would hold a line
// established and exempt from eviction forever, with nothing on it to
// say who decided that or why.
func TestLoadDropsAReasonlessMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	doc := storeFile{Lines: []*Line{
		{Key: "10.0.10.5|10.0.0.1|443|tcp", SrcIP: "10.0.10.5", DstIP: "10.0.0.1", Port: 443, Proto: "tcp",
			Days: 1, Anchor: 20000, Expected: &Expected{By: "tom"}},
		nil,
		{Key: ""},
	}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshalling the fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	r, err := Open(path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	l, ok := r.Get("10.0.10.5|10.0.0.1|443|tcp")
	if !ok {
		t.Fatal("the valid line was not loaded")
	}
	if l.Expected != nil {
		t.Error("a stored mark with no reason must be dropped on load, not honoured")
	}
	if len(r.List()) != 1 {
		t.Errorf("loaded %d lines, want 1 -- a null entry and a keyless one must both be skipped", len(r.List()))
	}
}

func TestConfigNormaliseClampsToSomethingAnswerable(t *testing.T) {
	cases := []struct {
		in   Config
		want Config
		why  string
	}{
		{Config{Days: 3, Of: 14}, Config{Days: 3, Of: 14}, "the ratified default passes through untouched"},
		{Config{Days: 0, Of: 14}, Config{Days: DefaultDays, Of: 14}, "a threshold of zero would establish every line on sight"},
		{Config{Days: -1, Of: 14}, Config{Days: DefaultDays, Of: 14}, "negative is the same mistake as zero"},
		{Config{Days: 3, Of: 0}, Config{Days: 3, Of: DefaultOf}, "a window of zero would establish nothing, ever"},
		{Config{Days: 3, Of: 99}, Config{Days: 3, Of: DefaultOf}, "a window wider than the bitmap cannot be answered, so it falls back rather than silently truncating"},
		{Config{Days: 20, Of: 14}, Config{Days: DefaultDays, Of: 14}, "a line cannot be seen on more days than the window holds"},
		{Config{Days: 5, Of: 2}, Config{Days: 2, Of: 2}, "the default is itself too large for this window, so it clamps to the window"},
	}
	for _, c := range cases {
		if got := c.in.Normalise(); got != c.want {
			t.Errorf("Config%+v.Normalise() = %+v, want %+v -- %s", c.in, got, c.want, c.why)
		}
	}
}

func TestNilRegisterIsSafe(t *testing.T) {
	var r *Register
	if r.Observe("bridge-lan", "10.0.10.5", "10.0.0.1", 443, "tcp", OutcomeAccept, day(0)) {
		t.Error("a nil register must record nothing rather than panic")
	}
	if got := r.OffToday(day(0)); got != nil {
		t.Errorf("OffToday on a nil register = %v, want nil", got)
	}
	if _, err := r.Expect("k", "why", "tom"); err != ErrUnknownLine {
		t.Errorf("Expect on a nil register = %v, want ErrUnknownLine", err)
	}
	if r.Unexpect("k") {
		t.Error("Unexpect on a nil register must report false")
	}
	if _, ok := r.Get("k"); ok {
		t.Error("Get on a nil register must report false")
	}
	if got := r.Config(); got != DefaultConfig() {
		t.Errorf("Config on a nil register = %+v, want the default", got)
	}
}

// testIP spreads i over the 10.0.0.0/8 space, which comfortably holds
// MaxLines distinct private addresses.
func testIP(i int) string {
	return "10." + strconv.Itoa(i/65536%256) + "." + strconv.Itoa(i/256%256) + "." + strconv.Itoa(i%256)
}
