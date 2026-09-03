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
	before.Resolve("10.0.0.5", firstSeen.Add(2*time.Hour))

	raw, taken := exportFrom(t, before)

	after := NewRegistry(configured)
	if err := after.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}

	devices := after.List()
	if len(devices) != 2 {
		t.Fatalf("registry holds %d devices (%+v), want the configured one and the discovered one", len(devices), devices)
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

	discovered, ok := find(devices, "10.0.0.5")
	if !ok {
		t.Fatalf("the auto-discovered device is missing after the restore")
	}
	if discovered.Configured {
		t.Errorf("the discovered device came back configured: %+v", discovered)
	}
	if discovered.ID != "10.0.0.5" || discovered.Name != "10.0.0.5" {
		t.Errorf("discovered identity = %+v, want the address, exactly as Resolve mints it", discovered)
	}
	if discovered.EventCount != 1 {
		t.Errorf("EventCount = %d, want 1", discovered.EventCount)
	}
}

// TestResolveKeepsCountingFromTheRestoredTotal checks the restore leaves
// the registry in a state the ingest path can carry on from, rather than
// one that only reads correctly until the next event.
func TestResolveKeepsCountingFromTheRestoredTotal(t *testing.T) {
	before := NewRegistry(nil)
	before.Resolve("10.0.0.5", time.Now().Add(-time.Hour))
	before.Resolve("10.0.0.5", time.Now().Add(-time.Hour))
	raw, taken := exportFrom(t, before)

	after := NewRegistry(nil)
	if err := after.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	now := time.Now()
	if id := after.Resolve("10.0.0.5", now); id != "10.0.0.5" {
		t.Errorf("Resolve = %q, want the restored device's own ID", id)
	}

	got, ok := find(after.List(), "10.0.0.5")
	if !ok {
		t.Fatalf("device missing")
	}
	if got.EventCount != 3 {
		t.Errorf("EventCount = %d, want the 2 restored plus the 1 just resolved", got.EventCount)
	}
	if !got.LastSeen.Equal(now) {
		t.Errorf("LastSeen = %v, want the event just resolved at %v", got.LastSeen, now)
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

// TestAPreviouslyDiscoveredDeviceTakesTheConfiguredIdentity is the
// opposite direction: the operator has since declared a router that had
// been auto-discovered, so config.yaml supplies the identity and the
// snapshot supplies its history.
func TestAPreviouslyDiscoveredDeviceTakesTheConfiguredIdentity(t *testing.T) {
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
		t.Errorf("history = %+v, want the discovered device's first-seen and count", got)
	}
}

// TestImportRespectsTheDiscoveryCap: a snapshot must not be able to put
// more discovered devices in the registry than a running one would hold,
// since its contents ultimately come from whoever can reach the syslog
// listener.
func TestImportRespectsTheDiscoveryCap(t *testing.T) {
	orig := maxDiscoveredDevices
	maxDiscoveredDevices = 50
	defer func() { maxDiscoveredDevices = orig }()

	taken := time.Now()
	devices := make([]Info, 0, 500)
	for i := 0; i < 500; i++ {
		ip := fmt.Sprintf("10.1.%d.%d", i/256, i%256)
		devices = append(devices, Info{
			ID:         ip,
			Name:       ip,
			SourceIP:   ip,
			FirstSeen:  taken.Add(-time.Duration(i) * time.Minute),
			LastSeen:   taken.Add(-time.Duration(i) * time.Minute),
			EventCount: 1,
		})
	}
	raw, err := json.Marshal(registryState{Devices: devices})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	r := NewRegistry(nil)
	if err := r.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if held := len(r.List()); held > maxDiscoveredDevices {
		t.Errorf("restored %d discovered devices against a cap of %d", held, maxDiscoveredDevices)
	}
}

// TestImportClampsTimestampsToWhenTheSnapshotWasTaken: nothing in a
// snapshot can be newer than the snapshot, and a future LastSeen would
// make an entry outlive every genuine device, since the discovery cap
// evicts by oldest LastSeen.
func TestImportClampsTimestampsToWhenTheSnapshotWasTaken(t *testing.T) {
	taken := time.Now().Truncate(time.Second)
	raw, err := json.Marshal(registryState{Devices: []Info{{
		ID:         "10.0.0.5",
		Name:       "10.0.0.5",
		SourceIP:   "10.0.0.5",
		FirstSeen:  taken.Add(-time.Hour),
		LastSeen:   taken.Add(365 * 24 * time.Hour),
		EventCount: 1,
	}}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	r := NewRegistry(nil)
	if err := r.SnapshotPart().Import(raw, taken, time.Now()); err != nil {
		t.Fatalf("Import: %v", err)
	}
	got, ok := find(r.List(), "10.0.0.5")
	if !ok {
		t.Fatalf("device missing")
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
	if len(r.List()) != 1 {
		t.Errorf("the refused import still changed the registry: %+v", r.List())
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
