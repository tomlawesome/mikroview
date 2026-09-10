// SPDX-License-Identifier: AGPL-3.0-only

package hosts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/persist"
)

func TestRegistersMirrorsTheMapsOwnRule(t *testing.T) {
	cases := []struct {
		iface, ip string
		want      bool
		why       string
	}{
		{"bridge-lan", "10.0.10.5", true, "a private source on an inbound interface is exactly what the map draws as a host"},
		{"bridge-lan", "192.168.1.20", true, "192.168/16 is private"},
		{"bridge-lan", "172.16.4.4", true, "172.16-31 is private"},
		{"bridge-lan", "172.32.4.4", false, "172.32 is outside the private range, so it is public and not a host"},
		{"bridge-lan", "127.0.0.1", true, "loopback reads as not-public in the browser rule, so the map counts it and so must this"},
		{"bridge-lan", "203.0.113.9", false, "a public source is the far side of the boundary, not a host on it"},
		{"bridge-lan", "999.1.2.3", false, "the browser rule range-checks nothing, so an impossible octet reads as public"},
		{"", "10.0.10.5", false, "no inbound interface means no boundary for the host to stand on"},
		{"bridge-lan", "", false, "no source address means no host"},
		{"bridge-lan", strings.Repeat("a", 200), false, "a key too long to be addressed by the mark endpoints is not worth holding"},
		{"bridge-lan", "10.0.0.1\n", false, "a control character in the address would flow into the UI and the audit trail"},
	}
	for _, c := range cases {
		if got := Registers(c.iface, c.ip); got != c.want {
			t.Errorf("Registers(%q, %q) = %v, want %v -- %s", c.iface, c.ip, got, c.want, c.why)
		}
	}
}

// TestObserveCountsNonIPv4AsAHost pins the consequence of porting the
// browser's isPublicIp exactly rather than substituting net.ParseIP:
// anything that is not a dotted quad counts as a host, because that is
// what the map does today.
func TestObserveCountsNonIPv4AsAHost(t *testing.T) {
	r := newTestRegister(t)
	if !r.Observe("bridge-lan", "fd00::5", "", time.Now()) {
		t.Error("an IPv6 source was not registered -- the browser's isPublicIp reports every non-dotted-quad as not public, and this must match it")
	}
}

func TestObserveRegistersAndRefreshes(t *testing.T) {
	r := newTestRegister(t)
	first := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	if !r.Observe("bridge-lan", "10.0.10.5", "", first) {
		t.Fatal("first observation was not registered")
	}
	h, ok := r.Get("bridge-lan|10.0.10.5")
	if !ok {
		t.Fatal("the host is not in the register under its own key")
	}
	if h.Iface != "bridge-lan" || h.IP != "10.0.10.5" {
		t.Errorf("host = %+v, want the interface and address split out of the key", h)
	}
	if h.Events != 1 || !h.FirstSeen.Equal(first) || !h.LastSeen.Equal(first) {
		t.Errorf("host = %+v, want one event and both stamps at %v", h, first)
	}
	if h.Label != "" {
		t.Errorf("label = %q, want empty -- a host nothing names is still a host", h.Label)
	}

	later := first.Add(time.Minute)
	r.Observe("bridge-lan", "10.0.10.5", "printer", later)
	h, _ = r.Get("bridge-lan|10.0.10.5")
	if h.Events != 2 {
		t.Errorf("events = %d, want 2", h.Events)
	}
	if !h.FirstSeen.Equal(first) {
		t.Errorf("firstSeen = %v, want it pinned at the first sighting %v", h.FirstSeen, first)
	}
	if !h.LastSeen.Equal(later) {
		t.Errorf("lastSeen = %v, want %v", h.LastSeen, later)
	}
	if h.Label != "printer" {
		t.Errorf("label = %q, want the last hostname seen", h.Label)
	}

	// An out-of-order event (a retransmit, a router with a skewed clock)
	// must not drag LastSeen backwards.
	r.Observe("bridge-lan", "10.0.10.5", "", first)
	h, _ = r.Get("bridge-lan|10.0.10.5")
	if !h.LastSeen.Equal(later) {
		t.Errorf("lastSeen = %v after an older event, want it left at %v", h.LastSeen, later)
	}
	if h.Label != "printer" {
		t.Errorf("label = %q, want the empty hostname on the later event to leave the known one alone", h.Label)
	}
}

func TestObserveSkipsWhatIsNotAHost(t *testing.T) {
	r := newTestRegister(t)
	if r.Observe("ether1", "203.0.113.9", "", time.Now()) {
		t.Error("a public source was registered -- the register must not be broader than the map's rule")
	}
	if len(r.List()) != 0 {
		t.Errorf("register = %+v, want empty", r.List())
	}
}

func TestListIsSortedByKey(t *testing.T) {
	r := newTestRegister(t)
	now := time.Now()
	for _, ip := range []string{"10.0.10.9", "10.0.10.2", "10.0.10.5"} {
		r.Observe("bridge-lan", ip, "", now)
	}
	list := r.List()
	if len(list) != 3 {
		t.Fatalf("len(list) = %d, want 3", len(list))
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].Key >= list[i].Key {
			t.Errorf("list is not sorted by key: %q then %q", list[i-1].Key, list[i].Key)
		}
	}
}

// TestCapEvictsTheOldestUnmarkedHost pins the bound the package comment
// justifies: at MaxHosts, a newly-seen host displaces the least recently
// seen entry that carries no operator decision.
func TestCapEvictsTheOldestUnmarkedHost(t *testing.T) {
	r := newTestRegister(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < MaxHosts; i++ {
		ip := testIP(i)
		if !r.Observe("bridge-lan", ip, "", base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("filling the register failed at %d (%s)", i, ip)
		}
	}
	if n := len(r.List()); n != MaxHosts {
		t.Fatalf("len = %d, want the register full at %d", n, MaxHosts)
	}

	oldest := KeyFor("bridge-lan", testIP(0))
	if _, ok := r.Get(oldest); !ok {
		t.Fatalf("test setup: %s should be the oldest entry", oldest)
	}

	newcomer := "10.99.99.99"
	if !r.Observe("bridge-lan", newcomer, "", base.Add(time.Hour)) {
		t.Fatal("a newly-seen host was not registered against a full register")
	}
	if _, ok := r.Get(oldest); ok {
		t.Errorf("%s survived -- the oldest LastSeen without a mark is what the cap evicts", oldest)
	}
	if _, ok := r.Get(KeyFor("bridge-lan", newcomer)); !ok {
		t.Error("the newly-seen host is not in the register")
	}
	if n := len(r.List()); n != MaxHosts {
		t.Errorf("len = %d, want the register still capped at %d", n, MaxHosts)
	}
}

// TestCapNeverEvictsAMark is the other half: an operator's decision is
// the one thing here that cannot be rebuilt from the feed, so it is
// preferred over a newly-seen host.
func TestCapNeverEvictsAMark(t *testing.T) {
	r := newTestRegister(t)
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < MaxHosts; i++ {
		r.Observe("bridge-lan", testIP(i), "", base.Add(time.Duration(i)*time.Second))
	}
	// The oldest entry -- the one eviction would otherwise take -- is
	// marked, so the second oldest must go instead.
	oldest := KeyFor("bridge-lan", testIP(0))
	if _, err := r.Mark(oldest, MarkIntended, "the lab NAS, off most of the time", "admin"); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	r.Observe("bridge-lan", "10.99.99.99", "", base.Add(time.Hour))

	if _, ok := r.Get(oldest); !ok {
		t.Error("the marked host was evicted -- a mark is the one thing the feed cannot rebuild")
	}
	if _, ok := r.Get(KeyFor("bridge-lan", testIP(1))); ok {
		t.Error("the second-oldest unmarked host survived -- eviction should have taken it instead")
	}
}

// TestCapShedsWhenEverythingIsMarked pins the last-resort branch: with
// nothing evictable the new observation is dropped rather than an
// operator's decision.
func TestCapShedsWhenEverythingIsMarked(t *testing.T) {
	r := newTestRegister(t)
	// A smaller ceiling would need MaxHosts to be a var; marking ten
	// thousand hosts is cheap enough to do honestly instead.
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < MaxHosts; i++ {
		ip := testIP(i)
		r.Observe("bridge-lan", ip, "", base.Add(time.Duration(i)*time.Second))
		if _, err := r.Mark(KeyFor("bridge-lan", ip), MarkIntended, "on purpose", "admin"); err != nil {
			t.Fatalf("Mark %s: %v", ip, err)
		}
	}

	if r.Observe("bridge-lan", "10.99.99.99", "", base.Add(time.Hour)) {
		t.Error("a new host was registered with nothing evictable -- it must be shed instead")
	}
	if n := len(r.List()); n != MaxHosts {
		t.Errorf("len = %d, want the register still capped at %d", n, MaxHosts)
	}
}

// TestDismissedMarkClearsOnReappearance is the issue's own promise: "if
// it reappears in the feed it comes back by itself".
func TestDismissedMarkClearsOnReappearance(t *testing.T) {
	r := newTestRegister(t)
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	r.Observe("bridge-lan", "10.0.10.5", "", now)

	key := "bridge-lan|10.0.10.5"
	if _, err := r.Mark(key, MarkDismissed, "", "admin"); err != nil {
		t.Fatalf("Mark: %v", err)
	}
	h, _ := r.Get(key)
	if h.Mark == nil || h.Mark.Kind != MarkDismissed {
		t.Fatalf("mark = %+v, want dismissed", h.Mark)
	}

	r.Observe("bridge-lan", "10.0.10.5", "", now.Add(time.Hour))
	h, _ = r.Get(key)
	if h.Mark != nil {
		t.Errorf("mark = %+v after the host spoke again, want it cleared -- a host that is back is not dismissed", h.Mark)
	}
	if h.Events != 2 {
		t.Errorf("events = %d, want the reappearance counted like any other event", h.Events)
	}
}

// TestIntendedMarkSurvivesReappearance is the opposite promise: "say why
// once and it stays said", the same as a coverage declaration.
func TestIntendedMarkSurvivesReappearance(t *testing.T) {
	r := newTestRegister(t)
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	r.Observe("bridge-lan", "10.0.10.5", "", now)

	key := "bridge-lan|10.0.10.5"
	if _, err := r.Mark(key, MarkIntended, "the lab NAS, powered on twice a month", "admin"); err != nil {
		t.Fatalf("Mark: %v", err)
	}

	r.Observe("bridge-lan", "10.0.10.5", "", now.Add(time.Hour))
	h, _ := r.Get(key)
	if h.Mark == nil || h.Mark.Kind != MarkIntended {
		t.Fatalf("mark = %+v, want the intended mark left in place", h.Mark)
	}
	if h.Mark.Reason != "the lab NAS, powered on twice a month" {
		t.Errorf("reason = %q, want it unchanged", h.Mark.Reason)
	}
}

func TestMarkSetsActorAndStampServerSide(t *testing.T) {
	r := newTestRegister(t)
	r.Observe("bridge-lan", "10.0.10.5", "", time.Now())

	h, err := r.Mark("bridge-lan|10.0.10.5", MarkIntended, "on purpose", "alice")
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}
	if h.Mark.By != "alice" {
		t.Errorf("by = %q, want the actor the caller took from the session", h.Mark.By)
	}
	if h.Mark.At.IsZero() {
		t.Error("at is zero -- the stamp is set server-side, never by the request")
	}
}

func TestMarkRejectsWhatValidationMustCatch(t *testing.T) {
	r := newTestRegister(t)
	r.Observe("bridge-lan", "10.0.10.5", "", time.Now())
	key := "bridge-lan|10.0.10.5"

	cases := []struct {
		name    string
		key     string
		kind    MarkKind
		reason  string
		wantErr error
	}{
		{name: "unknown kind", key: key, kind: "hidden", reason: "x", wantErr: ErrInvalidMark},
		{name: "empty kind", key: key, kind: "", reason: "x", wantErr: ErrInvalidMark},
		{name: "empty key", key: "", kind: MarkIntended, reason: "x", wantErr: ErrInvalidMark},
		{name: "over-long key", key: strings.Repeat("k", maxKeyLength+1), kind: MarkIntended, reason: "x", wantErr: ErrInvalidMark},
		{name: "control character in key", key: "bridge-lan|10.0.10.5\n", kind: MarkIntended, reason: "x", wantErr: ErrInvalidMark},
		{name: "invalid utf-8 in key", key: "bridge-lan|\xff\xfe", kind: MarkIntended, reason: "x", wantErr: ErrInvalidMark},
		{name: "bidi override in reason", key: key, kind: MarkIntended, reason: "safe‮gnorw", wantErr: ErrInvalidMark},
		{name: "over-long reason", key: key, kind: MarkIntended, reason: strings.Repeat("r", maxReasonLength+1), wantErr: ErrInvalidMark},
		{name: "intended with no reason", key: key, kind: MarkIntended, reason: "", wantErr: ErrInvalidMark},
		{name: "unknown host", key: "bridge-lan|10.0.10.99", kind: MarkIntended, reason: "x", wantErr: ErrUnknownHost},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := r.Mark(c.key, c.kind, c.reason, "admin"); err != c.wantErr {
				t.Errorf("Mark err = %v, want %v", err, c.wantErr)
			}
		})
	}

	// A dismissal needs no reason: it says "take this off my map", which
	// is actionable without a justification.
	if _, err := r.Mark(key, MarkDismissed, "", "admin"); err != nil {
		t.Errorf("dismissing with no reason = %v, want it accepted", err)
	}
}

func TestUnmark(t *testing.T) {
	r := newTestRegister(t)
	r.Observe("bridge-lan", "10.0.10.5", "", time.Now())
	key := "bridge-lan|10.0.10.5"

	if r.Unmark(key) {
		t.Error("Unmark on an unmarked host = true, want false so the API can answer 404")
	}
	if r.Unmark("bridge-lan|10.0.10.99") {
		t.Error("Unmark on an unknown key = true, want false")
	}

	if _, err := r.Mark(key, MarkIntended, "on purpose", "admin"); err != nil {
		t.Fatal(err)
	}
	if !r.Unmark(key) {
		t.Fatal("Unmark on a marked host = false, want true")
	}
	h, _ := r.Get(key)
	if h.Mark != nil {
		t.Errorf("mark = %+v, want it gone", h.Mark)
	}
}

// TestListDoesNotLeakTheStoredMark pins the copy in copyHost: a caller
// mutating a listed Host's Mark must not reach into the register.
func TestListDoesNotLeakTheStoredMark(t *testing.T) {
	r := newTestRegister(t)
	r.Observe("bridge-lan", "10.0.10.5", "", time.Now())
	if _, err := r.Mark("bridge-lan|10.0.10.5", MarkIntended, "on purpose", "admin"); err != nil {
		t.Fatal(err)
	}

	list := r.List()
	list[0].Mark.Reason = "tampered"

	h, _ := r.Get("bridge-lan|10.0.10.5")
	if h.Mark.Reason != "on purpose" {
		t.Errorf("reason = %q after a caller edited its own copy, want the stored one untouched", h.Mark.Reason)
	}
}

// TestPersistenceRoundTrip proves an empty path is in-memory only and a
// configured one survives a reopen, marks included.
func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	r.Observe("bridge-lan", "10.0.10.5", "printer", now)
	if _, err := r.Mark("bridge-lan|10.0.10.5", MarkIntended, "on purpose", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	r2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	h, ok := r2.Get("bridge-lan|10.0.10.5")
	if !ok {
		t.Fatal("the host did not survive a reopen")
	}
	if h.Label != "printer" || h.Events != 1 || !h.LastSeen.Equal(now) {
		t.Errorf("host = %+v, want the observation restored intact", h)
	}
	if h.Mark == nil || h.Mark.Kind != MarkIntended || h.Mark.Reason != "on purpose" {
		t.Errorf("mark = %+v, want the intended mark restored", h.Mark)
	}
}

func TestOpenWithNoPathIsInMemoryOnly(t *testing.T) {
	r, err := Open("")
	if err != nil {
		t.Fatalf("Open(\"\"): %v", err)
	}
	if !r.Observe("bridge-lan", "10.0.10.5", "", time.Now()) {
		t.Error("an in-memory-only register refused an observation")
	}
	if err := r.Flush(context.Background()); err != nil {
		t.Errorf("Flush on an in-memory-only register = %v, want a no-op", err)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Errorf("Close on an in-memory-only register = %v, want a no-op", err)
	}
}

// TestOpenRefusesAnUnreadableDocument is the fail-closed contract (#378):
// a live backend must never silently overwrite a document it could not
// parse.
func TestOpenRefusesAnUnreadableDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("Open on an unparseable document = nil error, want it refused")
	}
}

// TestLoadDropsUnknownMarkKinds: a mark this build cannot render or
// clear is worse than no mark at all.
func TestLoadDropsUnknownMarkKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts.json")
	doc := storeFile{Hosts: []*Host{
		{Key: "bridge-lan|10.0.10.5", Iface: "bridge-lan", IP: "10.0.10.5", Mark: &Mark{Kind: "archived"}},
		nil,
		{Key: ""},
	}}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	list := r.List()
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want the nil and key-less entries skipped", len(list))
	}
	if list[0].Mark != nil {
		t.Errorf("mark = %+v, want an unrecognised kind dropped", list[0].Mark)
	}
}

// newTestRegister is an in-memory-only register with the persistence
// rate limit removed, so a test never depends on wall-clock timing.
func newTestRegister(t *testing.T) *Register {
	t.Helper()
	r, err := Open("")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return r
}

// testIP spreads i over the 10.0.0.0/8 space, which comfortably holds
// MaxHosts distinct private addresses.
func testIP(i int) string {
	return "10." + strconv.Itoa(i/65536%256) + "." + strconv.Itoa(i/256%256) + "." + strconv.Itoa(i%256)
}

// countingSaveBackend is an in-memory persist.Backend that counts Save
// calls -- see device.countingSaveBackend, the twin of this type.
type countingSaveBackend struct {
	mu      sync.Mutex
	payload []byte
	version int64
	saves   int
}

func newCountingSaveBackend() *countingSaveBackend { return &countingSaveBackend{} }

func (b *countingSaveBackend) Load(ctx context.Context) (persist.Snapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return persist.Snapshot{Payload: b.payload, Version: b.version, Exists: b.version != 0}, nil
}

func (b *countingSaveBackend) Save(ctx context.Context, payload []byte, expect int64) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if expect != b.version {
		return 0, persist.ErrConflict
	}
	b.saves++
	b.payload = payload
	b.version++
	return b.version, nil
}

func (b *countingSaveBackend) Close() error     { return nil }
func (b *countingSaveBackend) Describe() string { return "counting test backend" }

func (b *countingSaveBackend) saveCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.saves
}

// flushForTest waits for r's write-behind writer to persist whatever is
// currently dirty -- see device.flushForTest, the twin of this helper.
func flushForTest(t *testing.T, r *Register) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.Flush(ctx); err != nil {
		t.Fatalf("flushForTest: %v", err)
	}
}

// TestObserveInATightLoopProducesFarFewerWritesThanCalls is #1087's
// proof for this store: Observe used to marshal the whole register on
// every call, on the single ingest goroutine, under the register's own
// lock. A sustained stream of Observe calls against the same host (the
// ordinary "one busy device" ingest case) must not turn into anywhere
// near one backend write per call.
func TestObserveInATightLoopProducesFarFewerWritesThanCalls(t *testing.T) {
	b := newCountingSaveBackend()
	r, err := OpenWithBackend(b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		r.Close(ctx)
	}()

	const n = 2000
	now := time.Now()
	for i := 0; i < n; i++ {
		r.Observe("bridge-lan", "10.0.10.5", "printer", now)
	}
	flushForTest(t, r)

	if got := b.saveCount(); got >= n/10 {
		t.Errorf("%d Observe calls in a tight loop against the same host produced %d backend writes, want far fewer than %d", n, got, n)
	}

	h, ok := r.Get(KeyFor("bridge-lan", "10.0.10.5"))
	if !ok {
		t.Fatal("the host observed in the loop is missing from the register")
	}
	if h.Events != n {
		t.Errorf("Events = %d, want %d -- debouncing the write must never drop an in-memory update", h.Events, n)
	}
}
