// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"encoding/json"
	"fmt"
	"net"
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
	// Sources is the unattributed syslog sources (#1170), written for
	// the same reason the devices are: "first seen" is a claim about
	// the past that nothing else can recover, and an address nobody has
	// claimed is exactly the one an operator is likely to be part-way
	// through investigating when the process restarts.
	Sources []Source `json:"sources,omitempty"`
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
	devices := make([]Info, 0, len(p.r.byID))
	for _, info := range p.r.byID {
		devices = append(devices, *info)
	}
	sources := make([]Source, 0, len(p.r.sources))
	for _, src := range p.r.sources {
		sources = append(sources, *src)
	}
	p.r.mu.RUnlock()

	// Sorted so the same registry produces the same bytes twice: map
	// iteration order is randomised, and a document that differs run to
	// run is needlessly hard to diff when someone is working out what a
	// snapshot actually held.
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	sort.Slice(sources, func(i, j int) bool { return sources[i].Address < sources[j].Address })

	return json.Marshal(registryState{Devices: devices, Sources: sources})
}

// Import merges a snapshot into the registry NewRegistry has just built
// from config.yaml. config.yaml wins on identity, the snapshot supplies
// the history:
//
//   - A device the config still declares, or one an ingest token named
//     on a push before the restart, keeps its current identity and
//     takes back its first/last-seen times and event count.
//   - A device the snapshot recorded as configured but config.yaml no
//     longer declares is dropped. Removing a device from config.yaml is
//     deliberate, and a warm restart that quietly puts it back would
//     read as the removal having failed. If the router is still
//     sending, its syslog is attributed afresh, which is the truth:
//     nothing has been received under that identity since the operator
//     dropped it.
//   - An unattributed source comes back unattributed, with its lines
//     and its dates, subject to the same cap a running registry holds
//     it to.
//
// One migration, and the reason it is here rather than in a version
// field: a snapshot written before #1170 holds every syslog source as a
// *device* named after its own IP, which is precisely the row that
// issue removed. Such a record is restored as the unattributed source
// it always was -- history kept, the claim that it is a router dropped
// -- rather than resurrecting a row the app no longer has anywhere to
// put. A device an ingest token named is told apart from one of those
// by its id: a token device name is not an IP address.
//
// Like the store's part, it refuses a registry that has already seen
// traffic: the only correct time to restore is at boot, before ingest
// starts, and merging over live counts would inflate them.
// AcceptedIP/EnrolledAt (issue #1281) are deliberately left untouched by
// this whole merge, on every branch: OpenRegistryWithBackend's own
// persisted document is the durable source for those, loaded earlier in
// main.go's boot sequence than this warm-restart snapshot ever runs
// (see main.go's ordering comment beside RunPeriodicSnapshot's caller),
// so an existing device already carries its real enrolment by the time
// Import sees it, and the byAcceptedIP index behind it is already
// correct. A device this merge creates fresh (the "snapshot recorded it,
// OpenRegistry's own document did not" case -- in practice, an upgrade
// from before #1281 existed) has never been enrolled either way, so an
// empty AcceptedIP is simply the truth.
func (p registryPart) Import(raw json.RawMessage, taken, now time.Time) error {
	var state registryState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}

	p.r.mu.Lock()
	defer p.r.mu.Unlock()

	for _, info := range p.r.byID {
		if info.EventCount > 0 {
			return fmt.Errorf("device: refusing to restore over a registry that has already seen traffic (%s) -- a snapshot is only ever loaded at boot", info.ID)
		}
	}
	if len(p.r.sources) > 0 {
		return fmt.Errorf("device: refusing to restore over a registry that has already seen traffic (%d unattributed sources) -- a snapshot is only ever loaded at boot", len(p.r.sources))
	}

	for _, record := range state.Devices {
		if record.ID == "" {
			continue
		}
		firstSeen := notAfter(record.FirstSeen, taken)
		lastSeen := notAfter(record.LastSeen, taken)
		if existing, ok := p.r.byID[record.ID]; ok {
			existing.FirstSeen = firstSeen
			existing.LastSeen = lastSeen
			existing.EventCount = record.EventCount
			continue
		}
		if record.Configured {
			continue // dropped from config.yaml since the snapshot -- see the doc comment
		}
		if net.ParseIP(record.ID) != nil {
			// Pre-#1170 discovery: a syslog source the old registry
			// minted a device for. It is a source, and always was --
			// unless config.yaml has since declared that address, in
			// which case the operator has answered the question the
			// old row was guessing at and the declared device takes
			// the history.
			p.r.restoreAddressLocked(normalizeIP(record.ID), record.EventCount, firstSeen, lastSeen)
			continue
		}
		p.r.byID[record.ID] = &Info{
			ID:         record.ID,
			Name:       record.Name,
			SourceIP:   normalizeIP(record.SourceIP),
			FirstSeen:  firstSeen,
			LastSeen:   lastSeen,
			EventCount: record.EventCount,
		}
	}

	for _, record := range state.Sources {
		key := normalizeIP(record.Address)
		if key == "" {
			continue
		}
		// Claimants are deliberately not restored: they are a reading
		// of pushed address tables, which a restart has just emptied.
		p.r.restoreAddressLocked(key, record.Lines, notAfter(record.FirstSeen, taken), notAfter(record.LastSeen, taken))
	}

	// One prune after the whole merge rather than one per entry: the
	// batched shed leaves headroom, so a restored registry is in exactly
	// the state a running one would be in after the same traffic.
	p.r.pruneLocked()
	return nil
}

// restoreAddressLocked puts one address's history back where it now
// belongs: on the device config.yaml has since declared for it, else on
// the unattributed source itself. Merges rather than replaces, since
// one document can hold the same address twice -- a pre-#1170 device
// row and a source row for it.
func (r *Registry) restoreAddressLocked(address string, lines uint64, firstSeen, lastSeen time.Time) {
	info, ok := r.byIP[address]
	if !ok {
		// #1281: an address may instead belong to a device enrolled by
		// token rather than declared in config.yaml -- same merge either
		// way, just a different map to have found it in.
		info, ok = r.byAcceptedIP[address]
	}
	if ok {
		info.EventCount += lines
		if !firstSeen.IsZero() && (info.FirstSeen.IsZero() || firstSeen.Before(info.FirstSeen)) {
			info.FirstSeen = firstSeen
		}
		if lastSeen.After(info.LastSeen) {
			info.LastSeen = lastSeen
		}
		return
	}
	existing, ok := r.sources[address]
	if !ok {
		r.sources[address] = &Source{Address: address, Lines: lines, FirstSeen: firstSeen, LastSeen: lastSeen}
		return
	}
	existing.Lines += lines
	if !firstSeen.IsZero() && (existing.FirstSeen.IsZero() || firstSeen.Before(existing.FirstSeen)) {
		existing.FirstSeen = firstSeen
	}
	if lastSeen.After(existing.LastSeen) {
		existing.LastSeen = lastSeen
	}
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
