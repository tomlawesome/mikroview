// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/tomlawesome/mikroview/internal/snapshot"
)

// snapshotPartName is this part's key in a snapshot document. Stable
// across releases: it is how a later boot finds these bytes again.
const snapshotPartName = "devices"

// registryState is the registry's slice of a snapshot document.
//
// Info is written as-is -- it already carries the JSON tags /api/devices
// serves it under -- so what a restored device knows about itself is
// exactly what the endpoint showed before the restart: when it was first
// and last seen, and how many events it has sent.
type registryState struct {
	Devices []Info `json:"devices"`
}

// SnapshotPart returns this Registry as a warm-restart part, so a
// restart does not reset every device's first-seen date to the moment
// the process came back (#795).
//
// That date is the one number here an operator cannot recover any other
// way: last-seen and the event count refill from the next few events,
// but "first seen" is a claim about the past, and a cold start silently
// replaces months of it with today.
func (r *Registry) SnapshotPart() snapshot.Part { return registryPart{r: r} }

type registryPart struct{ r *Registry }

func (p registryPart) Name() string { return snapshotPartName }

func (p registryPart) Export() (json.RawMessage, error) {
	p.r.mu.RLock()
	devices := make([]Info, 0, len(p.r.byIP))
	for _, info := range p.r.byIP {
		devices = append(devices, *info)
	}
	p.r.mu.RUnlock()

	// Sorted so the same registry produces the same bytes twice: map
	// iteration order is randomised, and a document that differs run to
	// run is needlessly hard to diff when someone is working out what a
	// snapshot actually held.
	sort.Slice(devices, func(i, j int) bool { return devices[i].SourceIP < devices[j].SourceIP })

	return json.Marshal(registryState{Devices: devices})
}

// Import merges a snapshot into the registry NewRegistry has just built
// from config.yaml. config.yaml wins on identity, the snapshot supplies
// the history:
//
//   - A source the config still declares keeps its configured ID, name
//     and Configured flag, and takes back its first/last-seen times and
//     event count.
//   - A source the snapshot only ever auto-discovered comes back
//     discovered, minted exactly as Resolve would mint it (ID and name
//     are the address), subject to the same discovery cap -- so a
//     restored registry cannot hold more discovered devices than a
//     running one.
//   - A source the snapshot recorded as configured but config.yaml no
//     longer declares is dropped. Removing a device from config.yaml is
//     deliberate, and a warm restart that quietly puts it back --
//     whether as its old configured self or re-labelled as a discovery
//     -- would read as the removal having failed. If the router is still
//     sending, the very next event rediscovers it honestly, dated from
//     now, which is the truth: nothing has been received from it under
//     that identity since the operator dropped it.
//
// Like the store's part, it refuses a registry that has already seen
// traffic: the only correct time to restore is at boot, before ingest
// starts, and merging over live counts would inflate them.
func (p registryPart) Import(raw json.RawMessage, taken, now time.Time) error {
	var state registryState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}

	p.r.mu.Lock()
	defer p.r.mu.Unlock()

	for _, info := range p.r.byIP {
		if info.EventCount > 0 {
			return fmt.Errorf("device: refusing to restore over a registry that has already seen traffic (%s) -- a snapshot is only ever loaded at boot", info.SourceIP)
		}
	}

	for _, record := range state.Devices {
		key := normalizeIP(record.SourceIP)
		if key == "" {
			continue
		}
		firstSeen := notAfter(record.FirstSeen, taken)
		lastSeen := notAfter(record.LastSeen, taken)
		if existing, ok := p.r.byIP[key]; ok {
			existing.FirstSeen = firstSeen
			existing.LastSeen = lastSeen
			existing.EventCount = record.EventCount
			continue
		}
		if record.Configured {
			continue // dropped from config.yaml since the snapshot -- see the doc comment
		}
		p.r.byIP[key] = &Info{
			ID:         key,
			Name:       key,
			SourceIP:   key,
			FirstSeen:  firstSeen,
			LastSeen:   lastSeen,
			EventCount: record.EventCount,
		}
	}
	// One prune after the whole merge rather than one per entry: the
	// batched shed leaves headroom, so a restored registry is in exactly
	// the state a running one would be in after the same discoveries.
	p.r.pruneLocked()
	return nil
}

// notAfter clamps a restored timestamp to the moment the snapshot was
// taken. Nothing in a snapshot can honestly be newer than the snapshot,
// and a device dated into the future is not merely wrong on screen: the
// discovery cap evicts by oldest LastSeen, so an entry claiming to have
// been seen next year would outlive every genuine device in the
// registry.
func notAfter(t, limit time.Time) time.Time {
	if t.After(limit) {
		return limit
	}
	return t
}
