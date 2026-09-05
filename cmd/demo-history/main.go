// SPDX-License-Identifier: AGPL-3.0-only

// Command demo-history builds a mikroview backup envelope (internal/backup)
// carrying seven nights of watchlist history and a matching week of retained
// corpus, for `-restore` to load into a fresh demo instance -- issue #959.
//
// Why a Go command and not a scripts/seed-demo.py mode: the corpus half has
// to be encrypted the same way internal/retention's own daily files are
// (AES-256-GCM under a key derived with HKDF, internal/retention.Key.Derive),
// and the watchlist half has to produce exactly the JSON shape
// internal/engine's definitions store persists (definitionsDocument,
// watchlistNonInvertedParamSchema's windowJSON/nightsJSON/ringJSON). Both are
// this module's own internal packages -- reusing them directly is one
// function call each; reimplementing either one in Python to match byte for
// byte would be a second, drifting copy of code this binary already has.
// scripts/seed-demo.py keeps the job of everything that goes in over the
// HTTP API (push, entities, accounts) -- this command only builds the file
// `-restore` reads, per AGENTS.md's "Demos the owner reviews" recipe.
//
// The watchlist entries mirror scripts/seed-demo.py's WATCHLIST_ENTRIES
// (#738) by name, IP and ports, so the story started here is the one the
// live feeder continues once it starts. The two tables have no shared
// source of truth across the language boundary -- keep them in step by
// hand if one changes.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	mathrand "math/rand"
	"os"
	"time"

	"github.com/tomlawesome/mikroview/internal/backup"
	"github.com/tomlawesome/mikroview/internal/engine"
	"github.com/tomlawesome/mikroview/internal/matchlog"
	"github.com/tomlawesome/mikroview/internal/store"
	"github.com/tomlawesome/mikroview/internal/watchlist"
)

// retainedRecord is the wire shape of one event inside the envelope's
// "retained_events" store: plain (unencrypted) JSON, deliberately -- see
// backup_cli.go's handling of this store name for why. It mirrors
// internal/retention/file.go's own unexported record type field-for-field
// (json tags "e"/"r") because that is the shape backup_cli.go's restore
// path decodes, not because the two packages share it.
type retainedRecord struct {
	Event      store.Event `json:"e"`
	ReceivedAt time.Time   `json:"r"`
}

// retainedEventsStore must match backup_cli.go's own constant of the same
// name. Not imported from there: backup_cli.go is part of the `main`
// package at the module root, a different `main` from this command's, so
// there is nothing to import -- the two are kept in step by the comment on
// each.
const retainedEventsStore = "retained_events"

// nightlyWindow is every demo entry's watch window: 22:00-06:00 UTC, every
// day. One shared window keeps the story simple -- there is nothing in
// #959's brief asking for varied windows -- and UTC avoids a zone this
// generator would otherwise have to invent.
var nightlyWindow = watchlist.Window{Start: 22 * 60, End: 6 * 60}

// story is which of the seven-night shapes a demo watchlist entry gets.
type story int

const (
	// storyHealthy: every night kept, ring unbroken -- the ordinary case.
	storyHealthy story = iota
	// storyHeld: paused throughout, so mikroview was not evaluating it --
	// every night "not observed", never "empty".
	storyHeld
	// storyNoCoverage: no pushed firewall rule logs this pathway (the
	// build-server-db-exposure story scripts/seed-demo.py's WATCHLIST_ENTRIES
	// already notes) -- every night "not observed" for the same reason
	// storyHeld is, but because of coverage rather than the pause switch.
	storyNoCoverage
	// storyBrokenRing: kept until the last two nights, which are empty --
	// a run currently broken, for the "watched" entries BRIEF.md's city
	// view gives a tall building to (#738 item 3's round-40 pair).
	storyBrokenRing
)

// demoEntry is one of scripts/seed-demo.py's WATCHLIST_ENTRIES, plus the
// seven-night story this command gives it.
type demoEntry struct {
	id     string
	name   string
	ip     string
	ports  []int
	paused bool
	story  story
}

var demoWatchlist = []demoEntry{
	{"demo-kitchen-cam-web-only", "kitchen-cam-web-only", "192.168.20.31", []int{80, 443}, false, storyHealthy},
	{"demo-guest-phone-smb-watch", "guest-phone-smb-watch", "192.168.30.50", []int{445}, true, storyHeld},
	{"demo-front-desk-phone-voip", "front-desk-phone-voip", "192.168.50.20", []int{5060}, false, storyHealthy},
	{"demo-build-server-db-exposure", "build-server-db-exposure", "10.10.0.11", []int{3306}, false, storyNoCoverage},
	{"demo-cam-porch-smb-watch", "cam-porch-smb-watch", "10.0.30.31", []int{445}, false, storyBrokenRing},
	{"demo-nas-shares-watch", "nas-shares-watch", "10.0.20.10", []int{445, 5001}, false, storyHealthy},
}

// backgroundHost is a non-watchlist identity the corpus's background
// traffic runs between, reusing scripts/seed-demo.py's ROUTERS/HOSTS
// naming (#738) so the corpus and the live feeder that follows it talk
// about the same estate.
type backgroundHost struct {
	ip  string
	mac string
}

var backgroundHosts = []backgroundHost{
	{"192.0.2.10", "aa:bb:cc:01:01:01"}, // core-switch, border-rb5009/core
	{"192.0.2.21", "aa:bb:cc:01:01:02"}, // home-nas, border-rb5009/core
	{"192.168.10.15", "aa:bb:cc:01:02:01"}, // tom-laptop, border-rb5009/staff
	{"192.168.40.12", "aa:bb:cc:02:01:01"}, // office-hex/office, unnamed
	{"172.16.5.5", "aa:bb:cc:03:01:01"},    // lab-crs/mgmt, unnamed
}

var wanTargets = []string{"203.0.113.5", "8.8.8.8", "1.1.1.1", "93.184.216.34"}

func main() {
	out := flag.String("out", "", "path to write the backup envelope to (required)")
	days := flag.Int("days", watchlist.MaxNights, "how many nights/days of history to generate")
	appVersion := flag.String("app-version", "demo-history-generator", "value stamped into the envelope's appVersion field")
	force := flag.Bool("force", false, "overwrite -out if it already exists")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: demo-history -out <envelope-file> [-days N] [-force]")
		os.Exit(2)
	}
	if *days <= 0 || *days > watchlist.MaxNights {
		fmt.Fprintf(os.Stderr, "-days must be between 1 and %d (watchlist.MaxNights) -- the definitions store never remembers more nights than that anyway\n", watchlist.MaxNights)
		os.Exit(2)
	}

	now := time.Now().UTC()

	definitionsDoc, err := buildDefinitions(now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "building the definitions store: %v\n", err)
		os.Exit(1)
	}
	retainedJSON, eventCount, err := buildRetainedEvents(now, *days)
	if err != nil {
		fmt.Fprintf(os.Stderr, "building the retained corpus: %v\n", err)
		os.Exit(1)
	}

	stores := map[string][]byte{
		"definitions":       definitionsDoc,
		retainedEventsStore: retainedJSON,
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if *force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(*out, flags, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "opening %s: %v\n", *out, err)
		os.Exit(1)
	}
	if err := backup.Write(f, *appVersion, stores); err != nil {
		f.Close()
		fmt.Fprintf(os.Stderr, "writing the envelope: %v\n", err)
		os.Exit(1)
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "closing %s: %v\n", *out, err)
		os.Exit(1)
	}

	info, _ := os.Stat(*out)
	var size int64
	if info != nil {
		size = info.Size()
	}
	fmt.Printf("wrote %s: %d watchlist entries, %d retained events across %d days, %d bytes on disk\n",
		*out, len(demoWatchlist), eventCount, *days, size)
}

// buildDefinitions writes demoWatchlist into a scratch definitions store
// through the real engine.DefinitionsStore/engine.ExpectationDefinitionFor
// path -- the same code the live app and backup_definitions_roundtrip_test.go
// use -- and returns the resulting document's bytes, so this command never
// hand-rolls the JSON shape internal/engine's definitionsDocument owns.
func buildDefinitions(now time.Time) ([]byte, error) {
	tmp, err := os.CreateTemp("", "mikroview-demo-definitions-*.json")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	tmp.Close()
	// Removed rather than left empty: OpenDefinitionsStore treats a
	// missing file as the expected first-run case but an existing,
	// zero-byte one as a document that failed to parse -- see
	// persist.Open's fail-closed contract (#378).
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	defer os.Remove(path)

	defs, err := engine.OpenDefinitionsStore(path)
	if err != nil {
		return nil, fmt.Errorf("opening a scratch definitions store: %w", err)
	}

	for _, d := range demoWatchlist {
		nights := nightsFor(d.story, now)
		e := watchlist.Entry{
			ID:        d.id,
			Name:      d.name,
			Source:    matchlog.Identity{IP: d.ip},
			Ports:     d.ports,
			Window:    nightlyWindow,
			Nights:    nights,
			Ring:      watchlist.UpdateRing(nights, nightlyWindow),
			CreatedAt: now.Add(-8 * 24 * time.Hour),
		}
		def, err := engine.ExpectationDefinitionFor(e)
		if err != nil {
			return nil, fmt.Errorf("converting %q: %w", d.name, err)
		}
		def.Enabled = !d.paused
		if err := defs.Upsert(def); err != nil {
			return nil, fmt.Errorf("storing %q: %w", d.name, err)
		}
	}
	if err := defs.Flush(context.Background()); err != nil {
		return nil, fmt.Errorf("flushing the scratch definitions store: %w", err)
	}
	return os.ReadFile(path)
}

// keptNight is a night that closed with at least one match attributed to
// the entry -- see watchlist.NightKept.
func keptNight(o watchlist.Occurrence) watchlist.Night {
	return watchlist.Night{Opened: o.Open, State: watchlist.NightKept, First: o.Open.Add(37 * time.Minute), Count: 4}
}

// nightsFor builds up to watchlist.MaxNights nights ending at the most
// recent occurrence of nightlyWindow that had already closed when now was
// captured, per the demo entry's story.
func nightsFor(st story, now time.Time) []watchlist.Night {
	occs := nightlyWindow.ClosedSince(time.Time{}, now, watchlist.MaxNights)
	nights := make([]watchlist.Night, 0, len(occs))
	for i, o := range occs {
		switch st {
		case storyHealthy:
			nights = append(nights, keptNight(o))
		case storyHeld, storyNoCoverage:
			nights = append(nights, watchlist.Night{Opened: o.Open, State: watchlist.NightUnobserved})
		case storyBrokenRing:
			if i >= len(occs)-2 {
				nights = append(nights, watchlist.Night{Opened: o.Open, State: watchlist.NightEmpty})
			} else {
				nights = append(nights, keptNight(o))
			}
		}
	}
	return nights
}

// buildRetainedEvents synthesises days of background traffic plus, on
// every night a demo entry's story keeps, a handful of matching events at
// that entry's own address and port -- so an operator drilling from a kept
// night into the corpus finds traffic that actually explains it.
//
// Volume is modest on purpose: #958's envelope memory bound is sized for a
// router-backup vault, and this is a demo for one estate, not a load test --
// see the doc comment on internal/backup.MaxDecompressed.
func buildRetainedEvents(now time.Time, days int) ([]byte, int, error) {
	rng := mathrand.New(mathrand.NewSource(cryptoSeed()))
	var records []retainedRecord

	// Ends yesterday, never today: today belongs to the live feeder that
	// starts right after -restore (AGENTS.md's demo recipe), and a
	// retained event timestamped later than "now" would be a corpus
	// claiming to have seen the future.
	dayStart := now.Truncate(24*time.Hour).AddDate(0, 0, -days)
	for day := 0; day < days; day++ {
		base := dayStart.AddDate(0, 0, day)
		records = append(records, backgroundTrafficFor(base, rng)...)
	}

	occs := nightlyWindow.ClosedSince(time.Time{}, now, watchlist.MaxNights)
	for _, d := range demoWatchlist {
		nights := nightsFor(d.story, now)
		for i, n := range nights {
			if n.State != watchlist.NightKept || i >= len(occs) {
				continue
			}
			records = append(records, matchingTrafficFor(d, occs[i], rng)...)
		}
	}

	sortRecords(records)

	buf, err := marshalRecords(records)
	return buf, len(records), err
}

// backgroundTrafficFor generates one day's worth of ordinary firewall
// traffic among backgroundHosts and wanTargets, diurnally weighted (more
// during the day, a quiet trickle at night) -- a much coarser version of
// scripts/seed-demo.py's own diurnal_factor (#738 item 2), enough to make a
// week of corpus look lived-in without reimplementing that script's full
// simulation in Go.
func backgroundTrafficFor(day time.Time, rng *mathrand.Rand) []retainedRecord {
	const eventsPerDay = 220
	out := make([]retainedRecord, 0, eventsPerDay)
	for i := 0; i < eventsPerDay; i++ {
		hourFrac := diurnalPick(rng)
		at := day.Add(time.Duration(hourFrac * float64(24*time.Hour)))
		host := backgroundHosts[rng.Intn(len(backgroundHosts))]
		dst := wanTargets[rng.Intn(len(wanTargets))]
		dstPort := []int{443, 80, 53, 123}[rng.Intn(4)]
		action := store.ActionAccept
		if rng.Float64() < 0.15 {
			action = store.ActionDrop
		}
		out = append(out, syntheticRecord(at, host.ip, host.mac, dst, dstPort, action, "forward"))
	}
	return out
}

// diurnalPick returns a fraction of the day, weighted toward waking hours,
// via rejection sampling against a simple two-hump density.
func diurnalPick(rng *mathrand.Rand) float64 {
	for {
		x := rng.Float64()
		hour := x * 24
		weight := 0.3 + 0.7*math.Sin((hour-6)/24*2*math.Pi)*0.5 + 0.35
		if rng.Float64() < weight {
			return x
		}
	}
}

// matchingTrafficFor generates the handful of events inside occ that make
// entry d's kept night an honest claim: traffic to its own address and one
// of its own ports, from a plausible source.
func matchingTrafficFor(d demoEntry, occ watchlist.Occurrence, rng *mathrand.Rand) []retainedRecord {
	span := occ.Close.Sub(occ.Open)
	n := 3 + rng.Intn(3)
	out := make([]retainedRecord, 0, n)
	for i := 0; i < n; i++ {
		at := occ.Open.Add(time.Duration(rng.Float64() * float64(span)))
		port := d.ports[rng.Intn(len(d.ports))]
		src := wanTargets[rng.Intn(len(wanTargets))]
		out = append(out, syntheticRecord(at, src, "", d.ip, port, store.ActionAccept, "forward"))
	}
	return out
}

func syntheticRecord(at time.Time, srcIP, srcMAC, dstIP string, dstPort int, action store.Action, chain string) retainedRecord {
	srcPort := 1024 + int(at.UnixNano()%60000)
	raw := fmt.Sprintf("%s: in:ether1 out:bridge1, connection-state:new proto TCP, %s:%d->%s:%d, len 60",
		chain, srcIP, srcPort, dstIP, dstPort)
	return retainedRecord{
		Event: store.Event{
			Time:      at,
			Action:    action,
			RuleLabel: "demo-history",
			Chain:     chain,
			Protocol:  "tcp",
			SrcMAC:    srcMAC,
			SrcIP:     srcIP,
			SrcPort:   srcPort,
			DstIP:     dstIP,
			DstPort:   dstPort,
			Raw:       raw,
		},
		ReceivedAt: at,
	}
}

func sortRecords(records []retainedRecord) {
	// Insertion order does not have to be chronological for correctness --
	// backup_cli.go's restore path groups by day itself (retention.Store.
	// Flush) -- but a human reading the envelope's JSON benefits from it,
	// and it keeps two runs of this command over the same inputs producing
	// the same bytes.
	for i := 1; i < len(records); i++ {
		for j := i; j > 0 && records[j].ReceivedAt.Before(records[j-1].ReceivedAt); j-- {
			records[j], records[j-1] = records[j-1], records[j]
		}
	}
}

func marshalRecords(records []retainedRecord) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, r := range records {
		if i > 0 {
			buf.WriteByte(',')
		}
		b, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// cryptoSeed reads a real random seed for the traffic generator's PRNG.
// Nothing here is security-sensitive -- this is cosmetic jitter in a demo
// fixture -- crypto/rand is used only because it is already an import-free
// source of entropy on every platform this builds for, not because the
// choice of seed matters.
func cryptoSeed() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.BigEndian.Uint64(b[:]))
}
