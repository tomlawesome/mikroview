// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
)

func find(devices []Info, sourceIP string) (Info, bool) {
	for _, d := range devices {
		if d.SourceIP == sourceIP {
			return d, true
		}
	}
	return Info{}, false
}

func findSource(sources []Source, address string) (Source, bool) {
	for _, s := range sources {
		if s.Address == address {
			return s, true
		}
	}
	return Source{}, false
}

// exportFrom builds the registry a previous process would have had and
// returns its snapshot bytes plus the time the snapshot was taken.
func exportFrom(t *testing.T, r *Registry) (json.RawMessage, time.Time) {
	t.Helper()
	raw, err := r.SnapshotPart().Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	return raw, time.Now()
}

// TestSnapshotRoundTripKeepsFirstSeenAcrossARestart is the point of the
// device part: a restart must not re-date every router to today.
func TestSnapshotRoundTripKeepsFirstSeenAcrossARestart(t *testing.T) {
	configured := []config.Device{{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"}}

	before := NewRegistry(configured)
	firstSeen := time.Now().Add(-90 * 24 * time.Hour).Truncate(time.Second)
	before.Resolve("192.168.1.1", firstSeen)
	before.Resolve("192.168.1.1", firstSeen.Add(time.Hour))
	before.Ensure("hap-ax3", firstSeen.Add(2*time.Hour))
	before.Resolve("10.0.0.5", firstSeen.Add(3*time.Hour))

	raw, taken := exportFrom(t, before)

	after := NewRegistry(configured)
	if err := after.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}

	devices := after.List()
	if len(devices) != 2 {
		t.Fatalf("registry holds %d devices (%+v), want the configured one and the one that pushed", len(devices), devices)
	}

	core, ok := find(devices, "192.168.1.1")
	if !ok {
		t.Fatalf("the configured device is missing after the restore")
	}
	if !core.FirstSeen.Equal(firstSeen) {
		t.Errorf("FirstSeen = %v, want %v -- the one figure a cold start cannot recover", core.FirstSeen, firstSeen)
	}
	if !core.LastSeen.Equal(firstSeen.Add(time.Hour)) {
		t.Errorf("LastSeen = %v, want %v", core.LastSeen, firstSeen.Add(time.Hour))
	}
	if core.EventCount != 2 {
		t.Errorf("EventCount = %d, want 2", core.EventCount)
	}
	if core.ID != "core" || core.Name != "Core Router" || !core.Configured {
		t.Errorf("identity = %+v, want config.yaml's ID, name and configured flag", core)
	}

	var pushed Info
	for _, d := range devices {
		if d.ID == "hap-ax3" {
			pushed = d
		}
	}
	if pushed.ID == "" {
		t.Fatalf("the device an ingest token named is missing after the restore: %+v", devices)
	}
	if pushed.Configured {
		t.Errorf("the pushing device came back configured: %+v", pushed)
	}
	if !pushed.FirstSeen.Equal(firstSeen.Add(2 * time.Hour)) {
		t.Errorf("FirstSeen = %v, want the first push at %v", pushed.FirstSeen, firstSeen.Add(2*time.Hour))
	}

	// The unattributed source keeps its own dates and line count, and
	// is still not a router (#1170).
	src, ok := findSource(after.Unattributed(), "10.0.0.5")
	if !ok {
		t.Fatalf("the unattributed source is missing after the restore: %+v", after.Unattributed())
	}
	if !src.FirstSeen.Equal(firstSeen.Add(3*time.Hour)) || src.Lines != 1 {
		t.Errorf("restored source = %+v, want its own first-seen and one line", src)
	}
}

// A snapshot written before #1170 holds every syslog source as a device
// named after its own IP -- the row that issue removed. The restore
// migrates it to the unattributed source it always was, history kept,
// rather than resurrecting a row nothing in the app has a place for.
func TestAPre1170DiscoveredRowComesBackAsASource(t *testing.T) {
	taken := time.Now().Truncate(time.Second)
	raw, err := json.Marshal(registryState{Devices: []Info{{
		ID:         "172.23.0.1",
		Name:       "172.23.0.1",
		SourceIP:   "172.23.0.1",
		FirstSeen:  taken.Add(-72 * time.Hour),
		LastSeen:   taken.Add(-time.Minute),
		EventCount: 412,
	}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	r := NewRegistry(nil)
	if err := r.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if devices := r.List(); len(devices) != 0 {
		t.Errorf("List() = %+v, want no devices: an address that merely sent lines is not a router", devices)
	}
	src, ok := findSource(r.Unattributed(), "172.23.0.1")
	if !ok {
		t.Fatalf("Unattributed() = %+v, want the migrated source", r.Unattributed())
	}
	if src.Lines != 412 || !src.FirstSeen.Equal(taken.Add(-72*time.Hour)) {
		t.Errorf("migrated source = %+v, want its lines and first-seen carried over", src)
	}
}

// TestResolveKeepsCountingFromTheRestoredTotal checks the restore leaves
// the registry in a state the ingest path can carry on from, rather than
// one that only reads correctly until the next event.
func TestResolveKeepsCountingFromTheRestoredTotal(t *testing.T) {
	configured := []config.Device{{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"}}
	before := NewRegistry(configured)
	before.Resolve("192.168.1.1", time.Now().Add(-time.Hour))
	before.Resolve("192.168.1.1", time.Now().Add(-time.Hour))
	before.Resolve("10.0.0.5", time.Now().Add(-time.Hour))
	before.Resolve("10.0.0.5", time.Now().Add(-time.Hour))
	raw, taken := exportFrom(t, before)

	after := NewRegistry(configured)
	if err := after.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	now := time.Now()
	if id := after.Resolve("192.168.1.1", now); id != "core" {
		t.Errorf("Resolve = %q, want the restored device's own ID", id)
	}
	if id := after.Resolve("10.0.0.5", now); id != "10.0.0.5" {
		t.Errorf("Resolve = %q, want the restored source's own address", id)
	}

	got, ok := find(after.List(), "192.168.1.1")
	if !ok {
		t.Fatalf("device missing")
	}
	if got.EventCount != 3 {
		t.Errorf("EventCount = %d, want the 2 restored plus the 1 just resolved", got.EventCount)
	}
	if !got.LastSeen.Equal(now) {
		t.Errorf("LastSeen = %v, want the event just resolved at %v", got.LastSeen, now)
	}

	src, ok := findSource(after.Unattributed(), "10.0.0.5")
	if !ok {
		t.Fatalf("source missing")
	}
	if src.Lines != 3 || !src.LastSeen.Equal(now) {
		t.Errorf("restored source = %+v, want the 2 restored lines plus the 1 just resolved", src)
	}
}

// TestADeviceDroppedFromConfigIsNotResurrected: removing a router from
// config.yaml is deliberate, so a warm restart must not put it back --
// under its old configured identity or relabelled as a discovery.
func TestADeviceDroppedFromConfigIsNotResurrected(t *testing.T) {
	before := NewRegistry([]config.Device{
		{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"},
		{ID: "edge", Name: "Edge Router", SourceIP: "192.168.1.2"},
	})
	before.Resolve("192.168.1.1", time.Now().Add(-time.Hour))
	before.Resolve("192.168.1.2", time.Now().Add(-time.Hour))
	raw, taken := exportFrom(t, before)

	// config.yaml no longer declares the edge router.
	after := NewRegistry([]config.Device{{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"}})
	if err := after.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if _, ok := find(after.List(), "192.168.1.2"); ok {
		t.Errorf("a device removed from config.yaml came back from the snapshot: %+v", after.List())
	}
	if _, ok := find(after.List(), "192.168.1.1"); !ok {
		t.Errorf("the still-configured device is missing")
	}
}

// TestAPreviouslyUnattributedSourceTakesTheConfiguredIdentity is the
// opposite direction: the operator has since declared the address that
// had been arriving unattributed, so config.yaml supplies the identity
// and the snapshot supplies its history -- the declaration is the
// answer to the question the unattributed row was asking.
func TestAPreviouslyUnattributedSourceTakesTheConfiguredIdentity(t *testing.T) {
	before := NewRegistry(nil)
	seen := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	before.Resolve("10.0.0.5", seen)
	raw, taken := exportFrom(t, before)

	after := NewRegistry([]config.Device{{ID: "branch", Name: "Branch Router", SourceIP: "10.0.0.5"}})
	if err := after.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, ok := find(after.List(), "10.0.0.5")
	if !ok {
		t.Fatalf("device missing after the restore")
	}
	if got.ID != "branch" || got.Name != "Branch Router" || !got.Configured {
		t.Errorf("identity = %+v, want config.yaml's, which wins", got)
	}
	if !got.FirstSeen.Equal(seen) || got.EventCount != 1 {
		t.Errorf("history = %+v, want the source's first-seen and line count", got)
	}
	if len(after.Unattributed()) != 0 {
		t.Errorf("Unattributed() = %+v, want none: the address is declared now", after.Unattributed())
	}
}

// TestImportRespectsTheUnattributedCap: a snapshot must not be able to
// put more unattributed sources in the registry than a running one
// would hold, since its contents ultimately come from whoever can reach
// the syslog listener.
func TestImportRespectsTheUnattributedCap(t *testing.T) {
	orig := maxUnattributedSources
	maxUnattributedSources = 50
	defer func() { maxUnattributedSources = orig }()

	taken := time.Now()
	sources := make([]Source, 0, 500)
	for i := 0; i < 500; i++ {
		ip := fmt.Sprintf("10.1.%d.%d", i/256, i%256)
		sources = append(sources, Source{
			Address:   ip,
			Lines:     1,
			FirstSeen: taken.Add(-time.Duration(i) * time.Minute),
			LastSeen:  taken.Add(-time.Duration(i) * time.Minute),
		})
	}
	raw, err := json.Marshal(registryState{Sources: sources})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	r := NewRegistry(nil)
	if err := r.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if held := len(r.Unattributed()); held > maxUnattributedSources {
		t.Errorf("restored %d unattributed sources against a cap of %d", held, maxUnattributedSources)
	}
}

// TestImportClampsTimestampsToWhenTheSnapshotWasTaken: nothing in a
// snapshot can be newer than the snapshot, and a future LastSeen would
// make an entry outlive every genuine one, since the cap evicts by
// oldest LastSeen.
func TestImportClampsTimestampsToWhenTheSnapshotWasTaken(t *testing.T) {
	taken := time.Now().Truncate(time.Second)
	raw, err := json.Marshal(registryState{Sources: []Source{{
		Address:   "10.0.0.5",
		FirstSeen: taken.Add(-time.Hour),
		LastSeen:  taken.Add(365 * 24 * time.Hour),
		Lines:     1,
	}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	r := NewRegistry(nil)
	if err := r.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	got, ok := findSource(r.Unattributed(), "10.0.0.5")
	if !ok {
		t.Fatalf("source missing")
	}
	if !got.LastSeen.Equal(taken) {
		t.Errorf("LastSeen = %v, want it clamped to the snapshot's own taken time %v", got.LastSeen, taken)
	}
}

func TestImportRefusesARegistryThatHasAlreadySeenTraffic(t *testing.T) {
	r := NewRegistry(nil)
	r.Resolve("10.0.0.9", time.Now())

	raw, err := json.Marshal(registryState{Devices: []Info{{
		ID: "10.0.0.5", Name: "10.0.0.5", SourceIP: "10.0.0.5", EventCount: 400,
	}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := r.SnapshotPart().Import(raw, time.Now(), time.Now()); err == nil {
		t.Errorf("Import over a live registry succeeded, want a refusal -- merging then would inflate live counts")
	}
	if len(r.Unattributed()) != 1 || len(r.List()) != 0 {
		t.Errorf("the refused import still changed the registry: %+v / %+v", r.List(), r.Unattributed())
	}
}

func TestImportRejectsBytesThatAreNotARegistryDocument(t *testing.T) {
	r := NewRegistry(nil)
	if err := r.SnapshotPart().Import(json.RawMessage(`"not a registry"`), time.Now(), time.Now()); err == nil {
		t.Errorf("Import of a foreign document succeeded, want an error so the loader can skip this part")
	}
}

func TestExportIsStableAcrossCalls(t *testing.T) {
	r := NewRegistry([]config.Device{{ID: "core", Name: "Core Router", SourceIP: "192.168.1.1"}})
	for i := 0; i < 20; i++ {
		r.Resolve(fmt.Sprintf("10.0.0.%d", i), time.Now())
		r.Ensure(fmt.Sprintf("router-%d", i), time.Now())
	}
	first, err := r.SnapshotPart().Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := r.SnapshotPart().Export()
		if err != nil {
			t.Fatalf("Export: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("two exports of the same registry differ -- map iteration order is leaking into the document")
		}
	}
}

func TestSnapshotPartNameIsStable(t *testing.T) {
	if got := NewRegistry(nil).SnapshotPart().Name(); got != "devices" {
		t.Errorf("Name() = %q, want %q -- the key a later boot looks the bytes up under", got, "devices")
	}
}
