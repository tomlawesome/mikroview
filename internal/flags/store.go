// SPDX-License-Identifier: AGPL-3.0-only

// Package flags stores manually-clearable behavioral flags raised by
// internal/detect (port scans, per-source activity spikes, repeated
// critical-port attempts, global volume spikes).
//
// This is the one deliberate exception to mikroview's otherwise
// in-memory-only design (see SECURITY.md): a flag exists specifically to
// stay visible until a human looks at it and clears it, so unlike every
// other piece of state it's persisted to a small JSON file rather than
// reset on every restart. Persistence is optional (empty StorePath keeps
// flags in-memory only, same as everything else) so a deployment that
// hasn't mounted a volume for it still works, just without the
// survives-a-restart guarantee.
package flags

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tomlawesome/mikroview/internal/reputation"
	"sort"
	"sync"
	"time"

	"github.com/tomlawesome/mikroview/internal/evict"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/persist"
)

var persistLog = logging.New("flags")

type Type string

const (
	TypePortScan              Type = "port_scan"
	TypeActivitySpike         Type = "activity_spike"
	TypeCriticalPort          Type = "critical_port"
	TypeGlobalSpike           Type = "global_spike"
	TypeDistributedBruteForce Type = "distributed_brute_force"
	TypeOutboundAnomaly       Type = "outbound_anomaly"
	TypeInternalRecon         Type = "internal_recon"
	TypeRuleSpike             Type = "rule_spike"
	TypeRepeatedDrops         Type = "repeated_drops"
	// TypeLowSlowScan (issue #20): a port scan deliberately paced to stay
	// under TypePortScan's short-burst threshold -- judged over hours by
	// several independent signals (destination breadth, per-source EMA
	// baseline, drop/reject ratio, reputation) rather than one count. See
	// internal/detect/low_slow_scan.go.
	TypeLowSlowScan Type = "low_slow_scan"
	// TypeOffHoursActivity (issue #104): a host active during a clock
	// window it has no established history of being active in --
	// deliberately *not* "any activity inside a fixed off-hours window,"
	// since a naive version of that fires on trivial noise (a phone
	// syncing, a scheduled job) with nothing to judge it against. Judged
	// per hour-of-day against that specific host's own EMA baseline *for
	// that specific hour*, gated by both an absolute count floor and a
	// minimum number of distinct prior days of history at that hour --
	// see internal/detect/off_hours.go.
	TypeOffHoursActivity Type = "off_hours_activity"
	// TypeDeviceSilence (issue #98): a configured RouterOS device that
	// should be sending syslog has gone quiet for longer than
	// detect.Config.DeviceStaleAfter -- absence of events, not a pattern
	// within them, so it's raised by a periodic sweep (see
	// internal/detect/device_silence.go) rather than any per-event path.
	// Target is the device's configured ID, not an IP. Deliberately the
	// same string value as detect.DetectorDeviceSilence, like every
	// other detector/flag-type pair in this codebase (see
	// DetectorName's doc comment).
	TypeDeviceSilence Type = "device_silence"
	// TypeNewDevice (issue #103 phase 1): raised the first time
	// mikroview ever sees a given store.Event.SrcMAC, per
	// internal/device.MACRegistry's persisted history -- deterministic,
	// fires once per genuinely-new MAC, no threshold/window to tune.
	// Raised directly from the ingest path (main.go), not from
	// internal/detect like every other Type here, since it needs no
	// rolling-window state, just a yes/no "seen before" lookup.
	TypeNewDevice Type = "new_device"
	// TypeStaleRule (issue #102): a firewall rule that fired at some
	// point (recorded in internal/rules' long-lived usage record) but
	// hasn't fired again in a long time -- either dead weight or an
	// unnecessary hole, worth a human's attention either way. Target is
	// the rule label, not an IP, same non-IP-target precedent
	// TypeGlobalSpike already sets with "global". See
	// internal/detect/stale_rule.go.
	TypeStaleRule Type = "stale_rule"
	// TypeUnexpectedMailSender (issue #108): a LAN source originating an
	// outbound connection to an external destination on an SMTP port
	// (25, 465, 587) that isn't tagged "trusted-mail-sender" in
	// internal/entities' store -- a host with no established, admin-
	// acknowledged reason to send mail suddenly doing so is a strong,
	// simple, deterministic compromised-device/spambot signal, distinct
	// from TypeOutboundAnomaly (dest_spread.go), which only fires on
	// distinct-destination-*count* spread over a window -- a single new
	// SMTP connection to one destination wouldn't trip that on its own.
	// Deterministic like TypeNewDevice/TypeStaleRule above -- no
	// threshold or window to tune, just "has this untagged source ever
	// done this before." See internal/detect/mail_sender.go.
	TypeUnexpectedMailSender Type = "unexpected_mail_sender"
	// TypeKnownBadIP (issue #113 Part B): a source IP matching a
	// locally-cached CIDR range from a vetted, curated threat-intel feed
	// (Spamhaus DROP by default -- see internal/blocklist's doc
	// comment for the full menu and why an arbitrary user-supplied URL
	// isn't offered instead). Raised directly, independent of any
	// behavioral threshold -- presence on a list Spamhaus is confident
	// is entirely malicious-controlled is itself the signal, not a
	// byproduct of volume or pattern. Target is the source IP, same
	// convention as TypePortScan/TypeActivitySpike/TypeCriticalPort.
	//
	// Also feeds RaiseConfidenceFloor for every other currently-active
	// source-IP-keyed flag on the same target (see
	// internal/detect/known_bad_ip.go's knownBadReinforcedTypes) -- the
	// same reinforcement role internal/detect's async AbuseIPDB-informed
	// checks already play for those flags (see maybeCheckReputation),
	// just synchronous, since a local lookup
	// needs no network round-trip to resolve. Raised directly from
	// internal/detect.Observe, not gated by DetectorName/Scope like
	// every per-event behavioral detector above -- same "no matching
	// detector-settings entry" exception new_device/stale_rule already
	// established (see frontend/src/lib/types.ts's FlagType doc
	// comment): there's no threshold to tune and no scope narrower than
	// "this exact list membership check."
	TypeKnownBadIP Type = "known_bad_ip"
)

// maxFlags bounds the store the same way every other buffer in mikroview
// has an explicit ceiling (see internal/store's ring buffer, the
// frontend's MAX_CLIENT_EVENTS). Flags are raised far less often than
// raw events, so this is a generous safety net rather than a limit
// expected to be hit in normal use. A var rather than a const so tests
// can shrink it without creating 1000+ flags.
var maxFlags = 1000

// maxFlagsHardCeiling bounds the store even when every flag is still
// active. maxFlags above is a soft target that only sheds *cleared*
// flags; without a hard stop, unauthenticated input can grow the active
// set without limit (see pruneLocked). A var so tests can shrink it.
var maxFlagsHardCeiling = 5000

// flagTimeSeriesMinutes is how much history Store.TimeSeries covers, at
// 1-minute resolution -- same window/resolution as
// internal/store/ring.go's Stats.TimeSeries, for visual/temporal
// consistency between EventsChart and FlagsChart on the frontend.
const flagTimeSeriesMinutes = 60

// Evidence is structured supporting detail beyond the free-text Detail
// string -- which detector populates which field:
//   - Ports: port_scan's distinct ports touched (capped, see
//     internal/detect's maxEvidencePorts).
//   - Hosts: distributed_brute_force's distinct source IPs,
//     outbound_anomaly's/internal_recon's distinct destinations
//     (capped, see internal/detect's maxEvidenceHosts).
//   - NAT: repeated_drops' triggering event's NAT translation info,
//     when present.
//   - Pairs/PairsTotal/PairsTotalIsFloor (#654): the distinct
//     (destination host, destination port) combinations actually seen
//     together -- critical_port's, and since #641 outbound_anomaly's and
//     internal_recon's, which is what makes an expected verdict able to
//     permit exactly what a device was seen doing rather than the cross
//     product of two lists. Capped for display at internal/engine's maxEvidencePairs
//     (== maxEvidencePorts, see that constant's own doc comment for why).
//     Ports and Hosts above are independent sets -- crossing them implies
//     every combination was seen, which for a detector recording many of
//     each is almost never true (this is the #641 watchlist-draft problem
//     the issue names: 20 hosts x 50 ports reading as up to 1000
//     permitted connections a device never made). Pairs is what a caller
//     should build a "permit exactly this" draft from instead of
//     Ports x Hosts. PairsTotal is the distinct-pair count before the
//     display cap, present whenever Pairs is truncated by it, so a
//     caller can say "50 of 214 pairs" rather than showing 50 and
//     reading as complete -- the same "never silently truncate" rule
//     #379 already established for a wrong Detail sentence, applied
//     here to a structured list instead. That count itself is bounded
//     for the same resource-safety reason the display list is
//     (internal/engine's maxEvidencePairsTracked -- attacker-controlled
//     traffic must never size an unbounded map), so past that second,
//     larger ceiling PairsTotal stops being exact and PairsTotalIsFloor
//     is true; a caller must then render it as a lower bound ("50 of
//     200+"), never as the precise-looking flat number it would
//     otherwise look like.
//   - SrcMAC (#654): the triggering event's source MAC address, present
//     only where the detector declared it (currently port_scan and
//     repeated_drops -- see internal/engine's EvidenceMAC) and the
//     source was a local device. A flag identifying its subject only by
//     IP silently stops matching that device the moment its DHCP lease
//     changes; SrcMAC lets a consumer key on the same MAC-preferred
//     identity matchlog.Identity already uses instead.
//
// Zero value (all fields empty/nil) is valid and common -- most
// detectors (activity_spike, rule_spike, global_spike, stale_rule,
// unexpected_mail_sender) have nothing here at all, since their Detail
// string already says everything there is to say.
//
// Pairs, PairsTotal, PairsTotalIsFloor and SrcMAC are additive fields,
// following Provisional's own precedent on Flag (see that field's doc
// comment): they round-trip through JSON like every other field here,
// need no migration, and are simply absent (the Go zero value) on a flag
// persisted before #654 -- there is nothing to backfill, since Evidence
// is always overwritten wholesale on a re-fire (see add(), "f.Evidence =
// evidence") rather than accumulated, so no existing flag's evidence is
// retroactively wrong for lacking values #654 didn't exist to record yet.
type Evidence struct {
	Ports             []int      `json:"ports,omitempty"`
	Hosts             []string   `json:"hosts,omitempty"`
	NAT               *NATInfo   `json:"nat,omitempty"`
	Pairs             []HostPort `json:"pairs,omitempty"`
	PairsTotal        int        `json:"pairsTotal,omitempty"`
	PairsTotalIsFloor bool       `json:"pairsTotalIsFloor,omitempty"`
	SrcMAC            string     `json:"srcMac,omitempty"`
}

// HostPort mirrors internal/engine.HostPort's shape (one destination
// host/port combination actually observed together) -- kept as this
// package's own type rather than importing the engine one, the same
// reason NATInfo just below is its own copy rather than
// internal/engine.NATInfo: the store's persisted shape belongs to this
// package, not to whichever evaluator happens to produce it today.
type HostPort struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// NATInfo is one event's NAT translation detail (store.Event's
// NatIP/NatPort/NatRaw), attached to a flag's Evidence when the
// triggering event had one.
type NATInfo struct {
	IP   string `json:"ip,omitempty"`
	Port int    `json:"port,omitempty"`
	Raw  string `json:"raw,omitempty"`
}

// Flag is one raised, human-clearable signal.
type Flag struct {
	ID        string    `json:"id"`
	Type      Type      `json:"type"`
	Target    string    `json:"target"` // source IP, "global" for TypeGlobalSpike, a device ID for TypeDeviceSilence, or a rule label for TypeRuleSpike/TypeStaleRule
	Detail    string    `json:"detail"` // human-readable specifics, e.g. "23 distinct ports in 60s"
	Count     int       `json:"count"`  // times this detector has re-fired for this target since the flag was (re-)raised
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
	Cleared   bool      `json:"cleared"`
	ClearedAt time.Time `json:"clearedAt,omitzero"`
	// Confidence is 0-100, set only by detectors that make a statistical
	// judgment call rather than a deterministic threshold crossing (e.g.
	// the per-host activity baseline -- see internal/detect/host_baseline.go).
	// nil (omitted from JSON) means "not scored" -- a plain threshold
	// detector is exactly as confident as the count it reports, so
	// attaching a number there would be noise, not signal.
	Confidence *int `json:"confidence,omitempty"`
	// ReputationFloor is the last reputation-informed minimum confidence
	// applied to this flag (see RaiseConfidenceFloor, internal/detect's
	// async AbuseIPDB-informed check) -- reapplied against Confidence on
	// every subsequent re-fire so a later, purely behavioral confidence
	// recompute never silently discards reputation evidence gathered
	// earlier in the same episode. nil means no reputation floor has
	// been applied (either not configured, not looked up yet, or the
	// target has no reputation data).
	ReputationFloor *int `json:"reputationFloor,omitempty"`
	// Reputation is a snapshot of the target's reputation *as of when
	// this episode's async lookup resolved*, not fetched live on read --
	// the point is "what did this look like when it fired." Only ever
	// set for single-IP detectors (see internal/detect's
	// maybeCheckReputation) -- group detectors' async check still
	// informs ReputationFloor/Confidence the same way, but there's no
	// single coherent snapshot to attach when many different IPs were
	// sampled.
	Reputation *reputation.Result `json:"reputation,omitempty"`
	// Country is the target's ISO 3166-1 alpha-2 code, from the same
	// GeoIP lookup already performed at ingest time
	// (store.Event.SrcCountry) -- captured synchronously at raise time,
	// no extra lookup. Empty for an internal target or when GeoIP isn't
	// configured.
	Country string `json:"country,omitempty"`
	// Evidence is structured supporting detail beyond Detail -- see
	// Evidence's own doc comment.
	Evidence Evidence `json:"evidence,omitzero"`
	// Provisional marks a flag raised while its judgement's baseline had
	// not yet cleared its history floor -- internal/engine.Baseline's
	// warm-up gating (docs/decisions/evaluation-engine.md section 1,
	// #368's fix made a chassis contract). false (the default, omitted
	// from JSON) is correct for every flag raised today: nothing wires a
	// detector onto internal/engine.Baseline yet -- that is #405's job.
	// This field, and its matchlog.Record counterpart, land now
	// (additive, no migration needed) so the persisted shape and its
	// round trip are proven ahead of anything setting it to true.
	Provisional bool `json:"provisional,omitempty"`
	// Size is this firing's own size: the measure the detector compares
	// against its threshold (#640) -- distinct ports for port_scan,
	// events in the window for activity_spike, and so on. Supplied by
	// whatever raised the flag; see internal/engine's shipped size
	// declarations for what each detector calls its size.
	//
	// nil (omitted) means the detector declares no size, the same
	// "nil means not scored" convention Confidence above already uses.
	// It is what an expectation on this pair records, and what a later
	// firing is judged against -- see Exclusion.Absorbs.
	Size *int `json:"size,omitempty"`
	// ExpectedSize is the size an expectation for this (Type, Target)
	// had recorded at the moment this flag was raised past it -- set
	// only on a firing that an existing expectation refused to absorb,
	// so a card can read "expected up to 30, saw 120" from
	// ExpectedSize and Size together.
	//
	// nil (omitted) is the ordinary case: no expectation exists for this
	// pair, so nothing was expected and nothing was exceeded.
	ExpectedSize *int `json:"expectedSize,omitempty"`
	// Verdict is an operator's judgement of this flag (#640, replacing
	// #638's expected/noise/real): expected (normal for this host, and
	// an expectation recorded for it), checked (looked at, fine this
	// time), investigate (of concern, being looked at) or resolved
	// (dealt with, normally a firewall change). Empty (omitted from
	// JSON) means unjudged -- same "empty is the common, unset case"
	// convention Provisional above follows. Set only through SetVerdict,
	// which also owns VerdictBy/VerdictAt and, for the three that clear,
	// reuses clearLocked rather than duplicating it.
	Verdict Verdict `json:"verdict,omitempty"`
	// PriorVerdict is the checked-or-resolved judgement this pair
	// carried when it was last cleared, kept across the revival that
	// wipes Verdict (#640). It is what makes a returning flag able to
	// say "you checked this on 2 Sept and found it fine" or "resolved on
	// 2 Sept -- it's back": both verdicts clear the flag and suppress
	// nothing, so the only trace they leave is this memory.
	//
	// Only checked and resolved are remembered. Expected leaves an
	// expectation instead, and a flag that returns past one already says
	// so with Size/ExpectedSize; investigate never clears, so there is
	// no revival to carry it over.
	PriorVerdict Verdict `json:"priorVerdict,omitempty"`
	// PriorVerdictAt is when PriorVerdict was given -- the date the
	// returning card reads. Zero (omitted) exactly when PriorVerdict is
	// empty, same omitzero convention as VerdictAt below.
	PriorVerdictAt time.Time `json:"priorVerdictAt,omitzero"`
	// VerdictBy is the account that set Verdict -- empty exactly when
	// Verdict is empty.
	VerdictBy string `json:"verdictBy,omitempty"`
	// VerdictAt is when Verdict was set -- zero (omitted, same
	// omitzero convention as ClearedAt) exactly when Verdict is empty.
	VerdictAt time.Time `json:"verdictAt,omitzero"`
	// verdictCleared records whether the most recent SetVerdict call's
	// own clearLocked call is what cleared this flag -- as opposed to
	// the flag already being cleared beforehand (a plain Clear, or an
	// earlier verdict). UndoVerdict reads this to decide whether
	// undoing must re-open the flag: it must not, if the flag was
	// already-cleared before the verdict it's undoing, since that
	// clear wasn't the verdict's doing and undo has no business
	// touching it.
	//
	// Deliberately unexported, so it carries no json tag and is simply
	// skipped by encoding/json -- it does not survive a persist/reload
	// round trip, and that is the right call, not an oversight. Its
	// only reader is UndoVerdict, called (if at all) moments after
	// SetVerdict, on the same running process and the same *Flag the
	// map already holds -- persistence is rate-limited/best-effort
	// even for the fields that IS json-tagged (see persist.WriteBehind),
	// so leaning on it for a same-process, few-seconds-later decision
	// would be building on ground shakier than just keeping the bit in
	// memory. A restart in that narrow window already drops the
	// undo affordance client-side along with everything else in
	// flight; nothing here makes that worse, and persisting one more
	// bit doesn't fix it either.
	verdictCleared bool
	// expectationBefore is what this pair's expectation looked like just
	// before the current expected verdict recorded one -- nil when the
	// current verdict is anything else, or when this process never set
	// it. UndoVerdict reads it to put the expectation back.
	//
	// Unexported for exactly the reasons verdictCleared above gives: no
	// json tag, no persistence, one reader, called moments later in the
	// same process. A verdict that outlived its process is undoable as a
	// verdict; the expectation it recorded is then the ledger's to prune
	// (#640 part C), not this bit's to guess at.
	expectationBefore *expectationSnapshot
}

// expectationSnapshot is the before-picture recordExpectationLocked
// takes so undo has something exact to restore: whether an expectation
// existed at all, and what size it carried if it did.
type expectationSnapshot struct {
	existed bool
	size    *int
}

// Verdict is an operator's judgement of a flag -- see Flag.Verdict.
type Verdict string

// The four verdicts (#640). Every flag ends as one of them: either
// mikroview is told this traffic is acceptable at these characteristics
// (expected), or a human says what looking at it concluded (checked,
// investigate, resolved). Noise and the plain clear are gone -- there is
// no way to dismiss a flag without a judgement.
const (
	// VerdictExpected records an expectation from the flag: normal for
	// this host, at this size. Clears, and suppresses further firings of
	// the same pair within ExpectationTolerance -- see Exclusion.
	VerdictExpected Verdict = "expected"
	// VerdictChecked is "looked suspicious, checked, fine this time".
	// Clears, suppresses nothing, and is remembered on PriorVerdict so a
	// re-fire can say when it was checked.
	VerdictChecked Verdict = "checked"
	// VerdictInvestigate is "of concern, being looked at". The one
	// verdict that leaves the flag open; the row then offers expected or
	// resolved.
	VerdictInvestigate Verdict = "investigate"
	// VerdictResolved is "dealt with", normally by a firewall change.
	// Clears, and is deliberately not a suppression: a line reaches
	// mikroview only if the firewall let it get that far, so a correct
	// fix makes the lines stop. If the same circumstances recur the flag
	// returns, saying when it was called resolved.
	VerdictResolved Verdict = "resolved"
)

// Valid reports whether v is one of the four recognised verdicts --
// used by the API handler to reject anything else with 400 before it
// ever reaches the store.
func (v Verdict) Valid() bool {
	switch v {
	case VerdictExpected, VerdictChecked, VerdictInvestigate, VerdictResolved:
		return true
	default:
		return false
	}
}

// Clears reports whether v clears the flag it is given to. Everything
// but investigate does: investigate's whole purpose is a flag that
// stays open while someone works on it.
func (v Verdict) Clears() bool {
	return v != VerdictInvestigate
}

// Remembered reports whether v is carried across a revival on
// PriorVerdict, so a returning flag can say what was concluded last
// time -- see Flag.PriorVerdict for why only two verdicts are.
func (v Verdict) Remembered() bool {
	return v == VerdictChecked || v == VerdictResolved
}

// FlagTimeBucket is one point in Store.TimeSeries: counts of newly-raised
// episodes by Type for a single one-minute window -- same shape
// convention as internal/store/ring.go's TimeBucket. ByType omits types
// with a zero count for that minute rather than listing every known
// Type every time. Counts episode starts (Add/AddWithConfidence/
// AddWithDetail's isNew), not every re-fire -- see bumpTimeSeriesLocked's
// doc comment for why.
type FlagTimeBucket struct {
	Time   time.Time       `json:"time"`
	ByType map[Type]uint64 `json:"byType"`
}

// ExpectationTolerance is the one number an expectation is allowed to
// grow by before it stops absorbing (#640): a firing whose size is
// within 1.5x the recorded size is normal for this host, and anything
// above that is the behaviour having changed enough to be worth a
// human's attention again.
//
// Deliberately a single package-level constant rather than a per-
// expectation or per-detector knob. The owner's design (#640) states one
// factor, shown on the ledger row, and a tolerance an operator can tune
// per entry is a threshold by another name -- the same global-threshold
// raise that issue rejected, just spelled locally. If this ever needs to
// vary it becomes a real design question, not a field quietly added.
const ExpectationTolerance = 1.5

// Exclusion is one permanently-excluded (Type, Target) pair -- see
// Store.Exclude's doc comment. ID is the same flagID(Type, Target) key
// flags themselves use, included so a caller (the admin exclusions API)
// can operate on an entry without recomputing it and without ambiguity
// from splitting it back apart (Target can itself contain ":", e.g. an
// IPv6 address, which would make that split ambiguous).
//
// #640 grew it from a flat "never flag this again" into a *sized*
// expectation: "this much of this, from this host, is normal." The key
// is unchanged -- still (Type, Target) -- and every added field is
// additive JSON with an omit-when-unset tag, so an exclusion written
// before this change reads back with Size nil and keeps meaning exactly
// what it always meant. There is no migration and none is needed.
type Exclusion struct {
	ID     string `json:"id"`
	Type   Type   `json:"type"`
	Target string `json:"target"`
	// Size is the measure recorded when this expectation was made --
	// whatever the detector declares its size to be (distinct ports for
	// port_scan, events in the window for activity_spike, and so on: see
	// internal/engine's shipped size declarations). Compared against a
	// later firing's own size through Absorbs.
	//
	// nil means "this expectation has no size", which is both the
	// pre-#640 shape read back off disk and what a detector that
	// declares no size produces. It reads as the original, blunter
	// meaning: ignore this host on this detector outright. That is a
	// deliberate, documented fallback, not a degraded case -- some
	// detectors genuinely have no count to be normal at (device_silence
	// is an absence, known_bad_ip is list membership), and pretending
	// otherwise would put a made-up number on the ledger.
	Size *int `json:"size,omitempty"`
	// Absorbed counts firings this expectation has suppressed. It is the
	// evidence the entry is earning its place -- the ledger (#640 part C)
	// shows it, and an expectation absorbing nothing is a candidate for
	// pruning.
	Absorbed uint64 `json:"absorbed,omitempty"`
	// Since is when this expectation was first recorded. Zero (omitted)
	// for an exclusion written before #640, which has no such record --
	// the ledger renders that absence rather than inventing a date.
	Since time.Time `json:"since,omitzero"`
	// Permitted is what each expected verdict on this pair wrote onto the
	// watchlist as well as here (#641): the device's evidence pairs,
	// recorded as permitted destinations on its inverted entry. One
	// record per verdict, appended in the order the verdicts were given.
	//
	// It lives on the expectation rather than in a store of its own
	// because the two halves are one act and the ledger (#640 part C)
	// shows them together: the detector learned a size, and the device
	// was allowed these destinations. Part C is what prunes them; this
	// package only has to be able to say what was added, by which
	// verdict, and when -- see PermittedRecord.
	//
	// Additive, omitted when empty, so an expectation recorded before
	// #641 reads back with nothing here and means exactly what it did.
	Permitted []PermittedRecord `json:"permitted,omitempty"`
}

// PermittedRecord is one expected verdict's write onto the watchlist --
// the reversible half of an automatic step (#641).
//
// Written by the API layer through Store.RecordPermitted rather than by
// SetVerdict itself: this store holds flags, and the entry the
// destinations landed on is an expectation definition in
// internal/engine's store, which this package neither imports nor should.
// What it does own is the *record* -- undo has to know exactly what to
// take back, and the ledger has to be able to show it.
type PermittedRecord struct {
	// EntryID is the inverted watchlist entry the destinations were
	// written to.
	EntryID string `json:"entryId"`
	// Dests is what was permitted, in the flag's own evidence order.
	// Stored as this package's HostPort rather than
	// watchlist.PermittedDest for the same reason HostPort exists at all:
	// the persisted shape belongs here, not to whichever package happens
	// to consume it.
	Dests []HostPort `json:"dests,omitempty"`
	// CreatedEntry records that this verdict is what brought the entry
	// into existence -- there was no inverted entry for the device, so
	// one was created in its observing state to hold the permission.
	// Undo removes an entry it created and nothing else has touched;
	// without this bit it could not tell that entry from one the
	// operator made themselves.
	CreatedEntry bool `json:"createdEntry,omitempty"`
	// Verdict is the judgement that wrote this. Always VerdictExpected
	// today -- it is the only verdict that permits anything -- and stated
	// rather than assumed so the ledger reads what happened off the
	// record instead of inferring it from the record's existence.
	Verdict Verdict `json:"verdict,omitempty"`
	// At is when it was written.
	At time.Time `json:"at,omitzero"`
}

// Absorbs reports whether a firing of the given observed size is still
// covered by this expectation -- true means suppress it and count it,
// false means the behaviour has outgrown what was recorded and the flag
// must be raised again carrying both numbers.
//
// Three cases, in the order they are decided:
//
//   - e.Size nil: a size-less expectation absorbs everything, forever.
//     That is the pre-#640 exclusion's meaning kept intact, and the
//     meaning of an expectation on a detector that declares no size.
//   - observed nil against a sized expectation: absorb. This should not
//     arise (the expectation's own size came from a firing of the same
//     detector, so that detector does declare one), but if a detector's
//     declaration ever changes underneath a stored entry, the honest
//     answer is that nothing measurable grew. Re-raising would mean
//     printing "expected up to 30, saw <nothing>", which is a claim we
//     cannot support.
//   - both sized: absorbed while observed is within ExpectationTolerance
//     times the recorded size, inclusive.
func (e Exclusion) Absorbs(observed *int) bool {
	if e.Size == nil || observed == nil {
		return true
	}
	return float64(*observed) <= float64(*e.Size)*ExpectationTolerance
}

// Ceiling is the largest size this expectation still absorbs -- the
// recorded size scaled by ExpectationTolerance and truncated, since a
// size is a whole count. Reports ok=false for a size-less expectation,
// which has no ceiling because it absorbs everything. For display and
// for callers that want the number without re-deriving the tolerance.
func (e Exclusion) Ceiling() (int, bool) {
	if e.Size == nil {
		return 0, false
	}
	return int(float64(*e.Size) * ExpectationTolerance), true
}

// persistedState is the on-disk JSON shape written by persistLocked and
// read back by Open: an object holding both the flags and the permanent
// exclusions.
type persistedState struct {
	Flags    []Flag      `json:"flags"`
	Excluded []Exclusion `json:"excluded,omitempty"`
}

// Store holds every known flag, active and cleared, keyed by a stable ID
// derived from (Type, Target) -- there is at most one entry per detector
// per target, ever; re-firing updates it in place, and clearing just
// marks it rather than deleting it, so recent history stays visible. The
// zero value is not usable; construct with Open.
type Store struct {
	mu sync.RWMutex
	// wb is nil when persistence isn't configured, same "nil means
	// off" convention every field it replaced (backend/version/
	// lastPersist) used to follow individually -- see persist.WriteBehind
	// for what it now owns: write-behind, the backend deadline, the
	// after-write-stamped rate limit/back-off, and version bookkeeping.
	// Every method on it is a safe no-op on a nil receiver.
	wb   *persist.WriteBehind
	byID map[string]*Flag
	// clearedCount tracks how many entries in byID are Cleared, so
	// pruneLocked can skip its scan entirely when there is nothing
	// evictable. Under a flood the store sits full of *active* flags, so
	// that scan otherwise ran on every Add and found nothing.
	clearedCount int
	// shedActive counts active, never-reviewed flags dropped at the hard
	// ceiling over this process's lifetime. Losing one is a real loss of
	// evidence, so it is worth being able to say how many rather than
	// only that it happened -- see pruneLocked.
	shedActive uint64

	// excluded holds every permanently-excluded (Type, Target) pair, same
	// flagID key as byID -- see Store.Exclude's doc comment. Checked at
	// the top of add() so an excluded pair never raises again.
	excluded map[string]Exclusion

	// minuteBuckets/minuteBucketTime implement the same lazily-reset
	// rolling-bucket trick as internal/store/ring.go's Store.minuteBuckets
	// (bucket i holds counts for unix minute minuteBucketTime[i], reset
	// the next time that slot is reused for a new minute), but counting
	// newly-raised flag episodes broken down by Type instead of raw
	// events by Action. A map per slot rather than ring.go's fixed
	// actionSlots array: Action has five, permanently fixed, values and
	// Insert is a hot path, so ring.go trades a bit of flexibility for
	// avoiding a map allocation on every call; Type already has 14
	// values, keeps growing as detectors are added, and add() is called
	// far less often (a detector firing, not every raw event), so a map
	// is the better trade here. Populated directly in add() itself, not
	// via the onRaise hook -- see bumpTimeSeriesLocked's doc comment for
	// why that distinction matters.
	minuteBuckets    [flagTimeSeriesMinutes]map[Type]uint64
	minuteBucketTime [flagTimeSeriesMinutes]int64

	// onRaise, if set via WithOnRaise, is called after a new flag episode
	// is raised (first-ever raise or a revival from Cleared -- the same
	// "isNew" Add/AddWithConfidence/AddWithDetail already report) --
	// never on a plain re-fire of an already-active flag, so a noisy
	// detector doesn't re-alert on every event. Must not block: see
	// internal/notify.Dispatcher.Enqueue's contract, the intended caller.
	onRaise func(Flag)
}

// WithOnRaise sets the hook called on every new flag episode -- see
// onRaise's own doc comment. Chainable, mirroring
// internal/detect.Detector.WithReputation's shape.
func (s *Store) WithOnRaise(fn func(Flag)) *Store {
	s.onRaise = fn
	return s
}

// Open loads path if it exists (a missing file is the expected first-run
// case, not an error) and returns a Store that persists to it from then
// on. An empty path is the expected "persistence not configured" case:
// a fully usable, in-memory-only Store is returned. A document that
// exists but cannot be read or parsed is a hard error (issue #378): the
// caller gets (nil, err) rather than a store whose live backend would
// overwrite that document on the first write. See persist.Open.
func Open(path string) (*Store, error) {
	if path == "" {
		return OpenWithBackend(nil)
	}
	return OpenWithBackend(persist.NewFileBackend(path))
}

// OpenWithBackend is Open against any persist.Backend -- a JSON file by
// default, or Postgres when configured (issue #131). A nil backend gives
// a usable, in-memory-only store.
func OpenWithBackend(b persist.Backend) (*Store, error) {
	s := &Store{byID: make(map[string]*Flag), excluded: make(map[string]Exclusion)}

	wb, _, err := persist.OpenWriteBehind(context.Background(), b, "the flags store", persist.WriteBehindOptions{
		MinInterval: persistMinInterval,
		OnSaveError: func(msg string) { persistLog.Error(msg) },
		OnConflict:  func(msg string) { persistLog.Warn(msg) },
	}, func(data []byte) error {
		var state persistedState
		if err := json.Unmarshal(data, &state); err != nil {
			return err
		}
		for _, f := range state.Flags {
			// A JSON `null` in this array decodes to a zero-value Flag
			// rather than a nil pointer (the field is []Flag, not
			// []*Flag), so it cannot crash the way the entities loader
			// can -- it just lands an ID-less entry in the map. Dropped
			// here rather than kept as a bogus entry.
			if f.ID == "" {
				continue
			}
			f := f
			s.byID[f.ID] = &f
			if f.Cleared {
				s.clearedCount++
			}
		}
		for _, e := range state.Excluded {
			s.excluded[e.ID] = e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.wb = wb
	return s, nil
}

// Flush forces this store's write-behind writer to persist whatever is
// currently dirty now, without waiting out its usual debounce interval,
// and blocks until that attempt finishes or ctx expires -- for a caller
// that genuinely needs to know a change has reached the backend before
// proceeding (a test, or a `-backup` CLI invocation racing a change made
// moments earlier in a separate, still-running process). Not meant for
// routine use -- persistence off the hot path is the whole point of
// issue #400; this is the deliberate escape hatch, not a replacement for
// it. A store with no backend configured (wb == nil) is a safe no-op.
func (s *Store) Flush(ctx context.Context) error {
	return s.wb.Flush(ctx)
}

// Close stops this store's write-behind writer goroutine, flushing
// whatever is still dirty within persist.SaveTimeout before returning --
// main's shutdown joins on this so a change made right before exit is
// not silently dropped. A store with no backend configured (wb == nil)
// is a safe no-op. Not safe to call any mutating method after Close.
func (s *Store) Close(ctx context.Context) error {
	return s.wb.Close(ctx)
}

func flagID(t Type, target string) string {
	return string(t) + ":" + target
}

// Add raises a flag for (t, target), or updates it in place if one is
// already active. A *cleared* flag for the same (t, target) is revived
// as a fresh episode (FirstSeen and Count reset) rather than left
// cleared -- once a human has dismissed a flag, the behavior recurring
// is worth a new signal, not a silently-suppressed repeat of something
// they already looked at. Reports whether this call started a new
// episode (first-ever raise, or a revival) as opposed to updating an
// already-active flag in place -- internal/detect uses this to avoid
// re-triggering a reputation lookup on every re-fire of an ongoing flag
// (see RaiseConfidenceFloor).
//
// No production detector calls this directly anymore -- every real
// call site was migrated to AddWithConfidence/AddWithDetail once
// confidence scoring landed (issue #39/#57). Kept as the simplest
// lifecycle primitive (raise/re-fire/clear/revive/prune) for tests that
// don't care about confidence scoring, the same reasoning New/
// DefaultConfig are kept in internal/detect for tests that don't need
// their own full configurability.
func (s *Store) Add(t Type, target, detail string, now time.Time) bool {
	isNew, f := s.add(t, target, detail, nil, Evidence{}, "", false, nil, now)
	s.maybeNotify(isNew, f)
	return isNew
}

// AddWithConfidence is Add, but for a detector that can express how
// confident it is in this specific flag (0-100) rather than a simple
// deterministic threshold crossing -- see Flag.Confidence.
func (s *Store) AddWithConfidence(t Type, target, detail string, confidence int, now time.Time) bool {
	isNew, f := s.add(t, target, detail, &confidence, Evidence{}, "", false, nil, now)
	s.maybeNotify(isNew, f)
	return isNew
}

// AddWithDetail is AddWithConfidence, plus structured evidence and a
// country code, for a detector whose behavior is best explained by
// exactly what was touched, not just a count -- see Evidence and
// Flag.Country.
func (s *Store) AddWithDetail(t Type, target, detail string, confidence int, evidence Evidence, country string, now time.Time) bool {
	isNew, f := s.add(t, target, detail, &confidence, evidence, country, false, nil, now)
	s.maybeNotify(isNew, f)
	return isNew
}

// AddProvisional is AddWithDetail, plus marking the raised/re-fired
// episode provisional -- see Flag.Provisional's doc comment. Added by
// #399 alongside the field itself; no production detector calls this
// yet (#405 wires internal/detect onto internal/engine.Baseline's
// warm-up gating, which is what would ever pass provisional=true) -- it
// exists now so the persisted shape, on both backends, and the round
// trip are proven ahead of anything depending on them. See
// TestAddProvisionalPersistsAndSurvivesReload.
func (s *Store) AddProvisional(t Type, target, detail string, confidence int, evidence Evidence, country string, provisional bool, now time.Time) bool {
	isNew, f := s.add(t, target, detail, &confidence, evidence, country, provisional, nil, now)
	s.maybeNotify(isNew, f)
	return isNew
}

// AddEmission is AddProvisional with confidence as an optional value
// rather than a required one: nil means "this detector makes no
// statistical judgement to score," which is what Flag.Confidence's own
// nil already meant and what Add (above) has always produced.
//
// It exists because internal/engine.FlagsSink needs both halves at once.
// Every definition ported onto the chassis before #405's final block
// scored its emissions, so the sink could pass a plain int and default a
// nil to 0; unexpected_mail_sender is the first that genuinely does not
// score -- it is deterministic, like new_device and stale_rule -- and
// defaulting its nil to 0 would silently turn "not scored" into "scored
// zero confidence," which an analyst reads as a judgement rather than as
// its absence. That is the gap FlagsSink's own doc comment said was
// worth widening this API to close rather than papering over; this is
// the widening.
// size is this firing's own size (#640), nil for a definition that
// declares none -- the value an expectation for this (Type, Target) is
// judged against, and recorded by a later expected verdict. Only this
// entry point takes one: it is the only raise path with an Emission
// behind it, and every other Add* above belongs to a caller that has no
// size to offer. They pass nil, which is the same "no size" the
// declares-none case already means.
func (s *Store) AddEmission(t Type, target, detail string, confidence *int, evidence Evidence, country string, provisional bool, size *int, now time.Time) bool {
	isNew, f := s.add(t, target, detail, confidence, evidence, country, provisional, size, now)
	s.maybeNotify(isNew, f)
	return isNew
}

// maybeNotify fires onRaise for a newly-raised episode -- called after
// add() has returned (its deferred unlock has already fired), so the
// hook never runs while s.mu is held.
func (s *Store) maybeNotify(isNew bool, f Flag) {
	if isNew && s.onRaise != nil {
		s.onRaise(f)
	}
}

func (s *Store) add(t Type, target, detail string, confidence *int, evidence Evidence, country string, provisional bool, size *int, now time.Time) (bool, Flag) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	// An expectation for this (Type, Target) is consulted before
	// anything else, and this is the one place that check lives (#640).
	//
	// It sits here, in add(), rather than in internal/engine's flags sink
	// for two reasons. Every raise path in the product funnels through
	// this function -- the engine's sink, main.go's new_device raise,
	// and any future caller -- so one check covers all of them and none
	// can be written that quietly bypasses it. And absorbing a firing
	// means incrementing the expectation's own counter, which is store
	// state under the lock this function already holds; doing it a layer
	// up would mean reaching back into the store for a second, separately
	// locked call on every suppressed firing, with the raise decision and
	// the count that justifies it able to disagree in between.
	//
	// Within tolerance: silently absorbed and counted -- no entry is
	// created, updated, or revived, and the caller sees "not a new
	// episode" so nothing downstream (notifications, reputation lookups)
	// fires either. See Store.Exclude's doc comment for why this is a
	// deliberate no-op rather than a raise that then gets auto-cleared.
	//
	// Above tolerance: the expectation no longer covers what this host is
	// doing, so the flag is raised carrying both numbers (see
	// Flag.ExpectedSize). The expectation is left exactly as it was --
	// raising the recorded size is an operator's judgement, made by
	// saying Expected again (SetVerdict), never something the store
	// does to itself on the strength of the traffic that broke it.
	var expectedSize *int
	if ex, excluded := s.excluded[id]; excluded {
		if ex.Absorbs(size) {
			ex.Absorbed++
			s.excluded[id] = ex
			s.persistLocked()
			return false, Flag{}
		}
		expectedSize = copyIntPtr(ex.Size)
	}

	f, ok := s.byID[id]
	isNew := !ok
	if !ok {
		f = &Flag{ID: id, Type: t, Target: target, FirstSeen: now}
		s.byID[id] = f
	} else if f.Cleared {
		isNew = true
		f.FirstSeen = now
		f.Cleared = false
		s.clearedCount--
		f.ClearedAt = time.Time{}
		f.Count = 0
		f.ReputationFloor = nil // a revived flag starts its confidence history fresh
		f.Reputation = nil      // ...and its detail history, including any stale reputation snapshot
		// ...and its judgement: a past call was about the episode that
		// just got cleared, not about this new one, so it must not
		// silently carry forward and suppress attention on a fresh
		// firing. A checked or resolved verdict is *remembered* first
		// (#640): it suppressed nothing, so this revival is exactly the
		// "you checked this on 2 Sept and found it fine" / "resolved on
		// 2 Sept -- it's back" case, and PriorVerdict is the only trace
		// of it left. Any other verdict clears the memory rather than
		// leaving a stale one standing: it is the last judgement that
		// counts, not the last remembered one.
		if f.Verdict.Remembered() {
			f.PriorVerdict = f.Verdict
			f.PriorVerdictAt = f.VerdictAt
		} else {
			f.PriorVerdict = ""
			f.PriorVerdictAt = time.Time{}
		}
		f.Verdict = ""
		f.VerdictBy = ""
		f.VerdictAt = time.Time{}
		f.verdictCleared = false // this Clear=false transition is the revival's doing, not any verdict's
	}
	f.Detail = detail
	f.Confidence = mergeConfidence(confidence, f.ReputationFloor)
	f.Evidence = evidence
	f.Country = country
	// Size/ExpectedSize describe this firing, so they are refreshed on
	// every call like Detail above, not fixed at episode start: a re-fire
	// that has grown further past an expectation must say so with its own
	// current numbers, not the ones the episode opened with.
	f.Size = copyIntPtr(size)
	f.ExpectedSize = expectedSize
	// Provisional is fixed at episode start (isNew: first-ever raise, or
	// a revival from Cleared), same as FirstSeen just above -- not
	// overwritten on a plain re-fire of an already-active episode.
	// #642's own requirement: a baseline warming past its floor mid-
	// episode must not silently convert an already-raised provisional
	// flag into a settled one in place, since that is exactly the kind
	// of unmarked status change #616's honesty vocabulary rules out --
	// an operator who already saw this flag hatched/labelled provisional
	// would see it silently turn solid. The provisional flag instead
	// stays provisional for the rest of its active episode; a genuinely
	// settled judgement is a new episode, which only exists once this
	// one is cleared (by a human, or an auto-clear) and the condition
	// fires again through the isNew-revival branch above, where a false
	// provisional argument does take effect. See
	// TestAddDoesNotConvertActiveProvisionalFlagInPlace and
	// TestAddProvisionalNewEpisodeAfterClearCanSettle.
	if isNew {
		f.Provisional = provisional
	}
	f.LastSeen = now
	f.Count++

	if isNew {
		s.bumpTimeSeriesLocked(t, now)
	}

	s.pruneLocked()
	s.persistLocked()
	return isNew, *f
}

// bumpTimeSeriesLocked records one new-or-revived episode of Type t at
// time now in the rolling per-minute bucket history (see
// minuteBuckets/minuteBucketTime's doc comment). Called directly from
// add() itself -- store-internal bookkeeping, not routed through the
// pluggable onRaise callback -- because onRaise is a single-slot hook
// already claimed by internal/notify.Dispatcher.Enqueue (see main.go's
// fs.WithOnRaise(dispatcher.Enqueue)); wiring this counter through
// WithOnRaise a second time would silently overwrite that assignment
// (whichever caller sets it last wins, with no compile-time or runtime
// signal) and stop every SMTP/Pushover/webhook notification. Must be
// called with s.mu already held.
func (s *Store) bumpTimeSeriesLocked(t Type, now time.Time) {
	minute := now.Unix() / 60
	idx := minute % flagTimeSeriesMinutes
	if idx < 0 {
		idx += flagTimeSeriesMinutes
	}
	if s.minuteBucketTime[idx] != minute {
		s.minuteBucketTime[idx] = minute
		s.minuteBuckets[idx] = make(map[Type]uint64, 1)
	}
	s.minuteBuckets[idx][t]++
}

// TimeSeries returns the last flagTimeSeriesMinutes minutes of newly-
// raised-episode counts by Type, oldest first, one entry per minute
// including empty ones -- the same fixed-width-window shape convention
// as internal/store/ring.go's Stats.TimeSeries, for FlagsChart. Like
// ring.go's rolling buckets, this only ever reflects activity since this
// Store was constructed: no historical backfill/persistence across a
// restart (see this package's own doc comment on what mikroview does
// and doesn't persist).
func (s *Store) TimeSeries() []FlagTimeBucket {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.timeSeriesLocked()
}

func (s *Store) timeSeriesLocked() []FlagTimeBucket {
	nowMinute := time.Now().Unix() / 60
	out := make([]FlagTimeBucket, flagTimeSeriesMinutes)
	for i := 0; i < flagTimeSeriesMinutes; i++ {
		minute := nowMinute - int64(flagTimeSeriesMinutes-1-i)
		idx := minute % flagTimeSeriesMinutes
		if idx < 0 {
			idx += flagTimeSeriesMinutes
		}
		byType := make(map[Type]uint64, len(s.minuteBuckets[idx]))
		if s.minuteBucketTime[idx] == minute {
			for typ, c := range s.minuteBuckets[idx] {
				if c > 0 {
					byType[typ] = c
				}
			}
		}
		out[i] = FlagTimeBucket{Time: time.Unix(minute*60, 0).UTC(), ByType: byType}
	}
	return out
}

// mergeConfidence combines a detector's freshly computed confidence
// with any previously applied reputation floor -- the floor must
// survive a plain re-fire's confidence recompute, or a later, purely
// behavioral update would silently discard reputation evidence gathered
// earlier in the same episode.
func mergeConfidence(fresh, floor *int) *int {
	switch {
	case floor == nil:
		return fresh
	case fresh == nil || *floor > *fresh:
		v := *floor
		return &v
	default:
		return fresh
	}
}

// copyIntPtr returns an independent copy of p, so a *int stored on a
// Flag or an Exclusion is never aliased to a caller's variable that
// could change underneath it -- the same guard internal/engine's own
// copyIntPtr (router.go) applies when translating an Emission.
func copyIntPtr(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// RaiseConfidenceFloor raises id's confidence to at least floor, if the
// flag is still known and floor is higher than its current score or
// previously applied floor. Never lowers an existing score -- a
// clean/unavailable reputation result is absence of evidence, not
// evidence of innocence. Called asynchronously by internal/detect, well
// after the triggering event -- safe to call from any goroutine, same
// as every other Store method.
func (s *Store) RaiseConfidenceFloor(t Type, target string, floor int) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		return
	}

	changed := false
	if f.ReputationFloor == nil || floor > *f.ReputationFloor {
		v := floor
		f.ReputationFloor = &v
		changed = true
	}
	if f.Confidence == nil || floor > *f.Confidence {
		v := floor
		f.Confidence = &v
		changed = true
	}
	if changed {
		s.persistLocked()
	}
}

// ApplyReputationSnapshot records target's reputation lookup result on
// the flag (see Flag.Reputation) and, if it includes an AbuseIPDB
// score, raises the confidence floor from it too -- same floor-raise-
// only reasoning as RaiseConfidenceFloor: a clean/absent score is
// absence of evidence, not evidence of innocence. Only meaningful for
// the single-IP reputation path (see internal/detect's
// maybeCheckReputation) -- the group path has no single snapshot to
// attach and keeps using RaiseConfidenceFloor directly. No-ops if the
// flag is no longer known.
func (s *Store) ApplyReputationSnapshot(t Type, target string, snapshot reputation.Result) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		return
	}

	f.Reputation = &snapshot

	// The applied floor is the strongest of two independent signals:
	// AbuseIPDB's abuse score, and its IsTor/UsageType fields (issue
	// #58) -- a Tor exit node or hosting-provider address is worth
	// floor-raising even when the target has no abuse reports at all,
	// since those are compositionally different kinds of evidence.
	floor := -1
	if snapshot.AbuseScore != nil {
		floor = *snapshot.AbuseScore
	}
	if riskFloor, ok := snapshot.RiskFloor(); ok && riskFloor > floor {
		floor = riskFloor
	}
	if floor >= 0 {
		if f.ReputationFloor == nil || floor > *f.ReputationFloor {
			v := floor
			f.ReputationFloor = &v
		}
		if f.Confidence == nil || floor > *f.Confidence {
			v := floor
			f.Confidence = &v
		}
	}
	s.persistLocked()
}

// clearLocked marks f cleared, if it isn't already -- the one place that
// touches Cleared/ClearedAt/clearedCount, called under s.mu by both
// ClearAll and SetVerdict (every verdict but investigate clears via this
// same path rather than a parallel one -- see SetVerdict's doc
// comment). A no-op on an already-cleared flag.
//
// Reports whether it actually changed anything -- false on the no-op
// path -- so a caller that needs to know whether *it* was the one that
// cleared f (SetVerdict, for UndoVerdict's benefit) can tell that apart
// from "f was already cleared by something else." unclearLocked is the
// symmetric un-clear, and is the only place that decrements
// clearedCount, for the same "one place touches the count" reason this
// doc comment gives for clearLocked incrementing it.
func (s *Store) clearLocked(f *Flag, now time.Time) bool {
	if f.Cleared {
		return false
	}
	f.Cleared = true
	s.clearedCount++
	f.ClearedAt = now
	return true
}

// unclearLocked reverses clearLocked: re-opens f, if it's currently
// cleared. A no-op otherwise, symmetric with clearLocked's own no-op
// case. The only place clearedCount is decremented, matching
// clearLocked as the only place it's incremented -- keeping both edges
// of that counter's bookkeeping next to each other rather than letting
// a future caller decrement it without the corresponding invariant
// clearLocked's own callers already get for free.
func (s *Store) unclearLocked(f *Flag) {
	if !f.Cleared {
		return
	}
	f.Cleared = false
	f.ClearedAt = time.Time{}
	s.clearedCount--
}

// SetVerdict records an operator's judgement of id (#640) and reports
// the updated flag plus whether id was known at all -- an unknown ID is
// the one failure case (the handler maps it to 404); the caller is
// expected to have already rejected an unrecognised Verdict value via
// Verdict.Valid() before this is reached, so an invalid v here is a
// programmer error, not a runtime one.
//
// This is the only way a flag ever leaves the inbox one at a time: the
// plain clear is gone (#640), so every dismissal carries a judgement.
// expected, checked and resolved all clear the flag through the same
// clearLocked path ClearAll uses -- not a parallel clear -- so there is
// only ever one place a flag transitions to Cleared. investigate records
// the verdict and leaves Cleared untouched: a flag someone is working on
// stays open.
//
// expected additionally records the expectation itself -- "this much of
// this, from this host, is normal" -- in the same atomic step under the
// same lock, so a flag can never be cleared as expected without the
// expectation that justifies it landing too. See
// recordExpectationLocked for what it records and what a repeat call
// does.
//
// Re-judging an already-judged flag overwrites the previous verdict --
// there's no history kept of a changed mind, same as every other
// in-place mutation this store does. verdictCleared is recomputed on
// every call (reset to false, then set only if this call's own
// clearLocked reports a real change) rather than ever OR'd with its
// previous value, so UndoVerdict always reflects whether the *current*
// verdict is what's holding the flag cleared -- see verdictCleared's
// own doc comment on Flag.
func (s *Store) SetVerdict(id string, v Verdict, by string, now time.Time) (Flag, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		return Flag{}, false
	}
	// Changing one's mind away from expected withdraws the expectation
	// that verdict recorded, exactly as undoing it would. Without this,
	// re-judging an expected flag as checked would leave a suppression
	// standing that nothing on screen still claims, and the pair would
	// go quiet for a reason the operator had just retracted.
	if f.Verdict == VerdictExpected && v != VerdictExpected {
		s.undoExpectationLocked(f)
	}
	f.Verdict = v
	f.VerdictBy = by
	f.VerdictAt = now
	f.verdictCleared = false
	f.expectationBefore = nil
	if v.Clears() {
		f.verdictCleared = s.clearLocked(f, now)
	}
	if v == VerdictExpected {
		s.recordExpectationLocked(f, now)
	}
	s.persistLocked()
	return *f, true
}

// UndoVerdict reverses SetVerdict (#638's undo affordance, now a real
// server call rather than a client-side deferred timer -- see the
// issue comment on why: a PWA service worker re-issuing every request
// through itself strips the keepalive guarantee a page-unload-time
// POST relied on, so a verdict judged just before a reload could be
// lost before it ever reached the server).
//
// Resets Verdict/VerdictBy/VerdictAt to their zero values, and re-opens
// the flag only if the verdict being undone is what cleared it --
// f.verdictCleared, set by SetVerdict -- never if the flag was already
// cleared beforehand by something else (an earlier verdict later
// overwritten by this one, say). That is the one subtlety here: undo
// must not resurrect a flag that undoing the verdict had no part in
// clearing.
//
// Undoing an expected verdict also puts its expectation back the way it
// found it (#640) -- removed if that verdict created it, restored to its
// old size if it raised one. An undo that reopened the flag while
// leaving the suppression standing would be the worst of both: a flag
// visibly back in the inbox and a store quietly absorbing every further
// firing of it. See undoExpectationLocked.
//
// Reports whether id was known at all, same true/false contract as
// every other id-keyed mutator here. Undoing an unjudged flag (empty
// Verdict, verdictCleared false) is a deliberate no-op, not an error:
// the caller may be a stale undo affordance racing a page that already
// moved on, and it can't always know which is which.
func (s *Store) UndoVerdict(id string) (Flag, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.byID[id]
	if !ok {
		return Flag{}, false
	}
	if f.verdictCleared {
		s.unclearLocked(f)
	}
	if f.Verdict == VerdictExpected {
		s.undoExpectationLocked(f)
	}
	f.Verdict = ""
	f.VerdictBy = ""
	f.VerdictAt = time.Time{}
	f.verdictCleared = false
	s.persistLocked()
	return *f, true
}

// recordExpectationLocked records an expectation from a flag -- "this
// much of this, from this host, is normal" for its (Type, Target) going
// forward. Called by SetVerdict under the lock it already holds, so the
// clear and the expectation that justifies it land together or not at
// all; there is no separate entry point, since an expectation only ever
// comes from an operator saying expected about a flag they looked at.
//
// The expectation's size is the flag's own Size -- the firing the
// operator just looked at and judged normal, not a number typed in --
// so what gets suppressed is bounded by what was actually seen. A flag
// from a detector that declares no size records a size-less expectation,
// which is the older, blunter "ignore this host on this detector"
// (see Exclusion.Size).
//
// Calling it again on a pair that already has an expectation is how the
// recorded size is *raised*: a firing that broke the old ceiling and was
// judged normal anyway becomes the new normal. Three deliberate
// restrictions on that:
//
//   - The size only ever goes up. A quieter firing later does not
//     silently narrow an expectation the operator widened on purpose.
//   - A size-less expectation stays size-less. It already absorbs
//     everything, so attaching a size would *narrow* it -- turning
//     "ignore this outright" into a ceiling the operator never asked
//     for, which is a suppression quietly becoming a re-raise.
//   - Absorbed and Since are kept. They are the entry's history, and the
//     ledger's evidence that it has been earning its place; a raise is
//     the same expectation grown, not a new one.
//
// What the entry looked like before this call is snapshotted onto the
// flag (expectationBefore) so UndoVerdict can put it back -- see
// undoExpectationLocked.
func (s *Store) recordExpectationLocked(f *Flag, now time.Time) {
	if existing, already := s.excluded[f.ID]; already {
		f.expectationBefore = &expectationSnapshot{existed: true, size: copyIntPtr(existing.Size)}
		if existing.Size != nil && f.Size != nil && *f.Size > *existing.Size {
			existing.Size = copyIntPtr(f.Size)
			s.excluded[f.ID] = existing
		}
		return
	}
	f.expectationBefore = &expectationSnapshot{}
	s.excluded[f.ID] = Exclusion{
		ID:     f.ID,
		Type:   f.Type,
		Target: f.Target,
		Size:   copyIntPtr(f.Size),
		Since:  now,
	}
}

// undoExpectationLocked reverses recordExpectationLocked for the verdict
// UndoVerdict is undoing: an expectation this verdict created is
// removed, and one it raised goes back to the size it had. Absorbed and
// Since are left alone -- firings absorbed in between really were
// absorbed, and rewriting that count would make the ledger lie about
// what happened.
//
// Silently does nothing when there is no snapshot to work from: the
// verdict was set in an earlier process (expectationBefore is in-memory
// only, same reasoning as verdictCleared's own doc comment), and
// inventing a removal on the strength of an entry that might predate it
// would delete an expectation this undo has no claim on.
func (s *Store) undoExpectationLocked(f *Flag) {
	snap := f.expectationBefore
	f.expectationBefore = nil
	if snap == nil {
		return
	}
	if !snap.existed {
		delete(s.excluded, f.ID)
		return
	}
	existing, ok := s.excluded[f.ID]
	if !ok {
		return
	}
	existing.Size = copyIntPtr(snap.size)
	s.excluded[f.ID] = existing
}

// ClearAll clears every currently-active (not yet Cleared) flag in one
// pass, returning how many it cleared. Regular clears only -- it must
// never record an expectation (see SetVerdict's expected verdict for
// that); "Clear all" on the frontend (issue #198) has no expectation-
// recording variant and none is planned, since a single click-again
// confirm is not the amount of intent a bulk suppression should
// require.
//
// One lock for the whole pass rather than one Clear call per flag: a
// concurrent Add landing mid-sweep either sees the old state and gets
// cleared too, or the new state and is skipped -- both are acceptable,
// but N separate lock/unlock cycles would let a caller observe a
// partially-cleared set mid-call, which a bulk action should not expose.
func (s *Store) ClearAll(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cleared := 0
	for _, f := range s.byID {
		if f.Cleared {
			continue
		}
		f.Cleared = true
		f.ClearedAt = now
		s.clearedCount++
		cleared++
	}
	if cleared > 0 {
		s.persistLocked()
	}
	return cleared
}

// Exclude permanently marks (t, target) as excluded -- from this call
// on, add() (and so every Add/AddWithConfidence/AddWithDetail call) is a
// silent no-op for that exact pair, forever, until a matching
// RemoveExclusion/RemoveExclusionByID call. This is deliberately
// permanent, not a timer: an earlier "snooze with expiry" design was
// rejected as pointless -- it either re-fires once the snooze ends
// (nothing was solved) or it doesn't (permanent exclusion was what was
// wanted all along). Idempotent: excluding an already-excluded pair is a
// no-op.
//
// This is the size-less form (see Exclusion.Size): it records no size
// and so absorbs every future firing of that pair regardless of how far
// the behaviour grows. An expected verdict is what records a *sized*
// expectation from a flag the operator actually looked at (#640); this
// entry point has no flag to take a size from, and inventing one would
// put a number on the ledger nobody measured.
func (s *Store) Exclude(t Type, target string) {
	id := flagID(t, target)

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, already := s.excluded[id]; already {
		return
	}
	s.excluded[id] = Exclusion{ID: id, Type: t, Target: target}
	// Clear any entry that is already active for this pair.
	//
	// add() skips excluded pairs before it touches s.byID, so an
	// existing active flag would otherwise sit in List() as
	// Cleared:false forever, frozen -- every later update silently
	// no-op'd, and no path to clear it but RemoveExclusion. Not reachable
	// through the API today, since an expected verdict clears first, but that
	// makes this a landmine for the next caller of Exclude rather than a
	// non-issue: the method's own contract says the pair goes silent from
	// this call on, and an entry stuck visible is the opposite.
	if f, ok := s.byID[id]; ok && !f.Cleared {
		f.Cleared = true
	}
	s.persistLocked()
}

// RemoveExclusion reverses Exclude for (t, target), letting that pair
// raise again going forward -- existing flag history (if any) is
// untouched either way. Reports whether an exclusion was actually
// present, same true/false contract as every other id-keyed mutator here.
func (s *Store) RemoveExclusion(t Type, target string) bool {
	return s.RemoveExclusionByID(flagID(t, target))
}

// RemoveExclusionByID is RemoveExclusion, keyed directly by the same ID
// Exclusion.ID/Flag.ID already use -- what a caller holding listed
// Exclusion values, rather than raw (Type, Target) pairs, has on hand to
// act on. That is the ledger's prune (#640 part C) and UndoVerdict's own
// reversal; the admin exclusions API that used to call it is gone.
func (s *Store) RemoveExclusionByID(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.excluded[id]; !ok {
		return false
	}
	delete(s.excluded, id)
	s.persistLocked()
	return true
}

// Excluded reports whether (t, target) is currently permanently
// excluded -- exposed for callers/tests that want to check without
// going through Add's side effects.
func (s *Store) Excluded(t Type, target string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.excluded[flagID(t, target)]
	return ok
}

// Expectation returns the recorded expectation for (t, target) and
// whether one exists at all -- the sized counterpart to Excluded above,
// for a caller (the ledger, a test) that needs the recorded size,
// absorbed count and since-when rather than only the yes/no. Returns a
// copy: the Size pointer it carries is independent of the stored entry,
// so a caller cannot mutate store state through it.
func (s *Store) Expectation(t Type, target string) (Exclusion, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.excluded[flagID(t, target)]
	if !ok {
		return Exclusion{}, false
	}
	return copyExclusion(e), true
}

// copyExclusion detaches an Exclusion from store state before it is
// handed out: an independent Size pointer, and an independent Permitted
// slice (whose own Dests slices are copied too). Without it a caller
// holding a listed entry could mutate what the store is about to
// consult, which for Permitted matters more than for Size -- undo reads
// it to decide what to take off the watchlist.
func copyExclusion(e Exclusion) Exclusion {
	e.Size = copyIntPtr(e.Size)
	if len(e.Permitted) > 0 {
		recs := make([]PermittedRecord, len(e.Permitted))
		for i, r := range e.Permitted {
			r.Dests = append([]HostPort(nil), r.Dests...)
			recs[i] = r
		}
		e.Permitted = recs
	}
	return e
}

// Get returns one flag by id, and whether it exists -- the read every
// caller that has to know a flag's state *before* changing it needs
// (internal/api's verdict handler, which reads the verdict it is about
// to replace so it can reverse what that verdict wrote onto the
// watchlist). A copy, like every other read here.
func (s *Store) Get(id string) (Flag, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	f, ok := s.byID[id]
	if !ok {
		return Flag{}, false
	}
	return *f, true
}

// RecordPermitted attaches rec to the expectation recorded for flagID --
// what an expected verdict permitted on the watchlist, alongside the
// size it learned (#641). Reports whether there was an expectation to
// attach it to: there is nothing to record against a flag that never
// recorded one, and inventing an entry here would put a permission on
// the ledger with no expectation beside it.
//
// Appends rather than replaces. A second expected verdict on the same
// pair (the firing came back past the ceiling and was judged normal
// again) permits whatever that firing saw, which may be pairs the first
// never did -- and undoing the second must take back only its own
// additions. One record per verdict is what makes that exact.
func (s *Store) RecordPermitted(flagID string, rec PermittedRecord) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.excluded[flagID]
	if !ok {
		return false
	}
	rec.Dests = append([]HostPort(nil), rec.Dests...)
	e.Permitted = append(e.Permitted, rec)
	s.excluded[flagID] = e
	s.persistLocked()
	return true
}

// WithdrawPermitted removes and returns the most recent PermittedRecord
// for flagID -- the one the verdict now being undone or re-judged wrote
// -- so its caller can take those destinations back off the watchlist.
// Reports false when there is nothing recorded, which is the ordinary
// case for every flag that was never judged expected and for one whose
// evidence carried no pairs to permit.
//
// Called before the verdict itself is reversed, deliberately: undoing an
// expected verdict that created the expectation deletes the expectation
// outright (see undoExpectationLocked), and this record goes with it.
func (s *Store) WithdrawPermitted(flagID string) (PermittedRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.excluded[flagID]
	if !ok || len(e.Permitted) == 0 {
		return PermittedRecord{}, false
	}
	last := e.Permitted[len(e.Permitted)-1]
	e.Permitted = e.Permitted[:len(e.Permitted)-1]
	if len(e.Permitted) == 0 {
		e.Permitted = nil
	}
	s.excluded[flagID] = e
	s.persistLocked()
	return last, true
}

// ListExclusions returns every recorded expectation, sorted by ID for a
// stable display order -- the read surface the ledger (#640 part C) is
// built on: every expectation with its recorded size, absorbed count and
// since-when, so it can be reviewed and pruned.
func (s *Store) ListExclusions() []Exclusion {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Exclusion, 0, len(s.excluded))
	for _, e := range s.excluded {
		// Detached, same reason Expectation detaches: a listed entry must
		// not be a handle onto store state.
		out = append(out, copyExclusion(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// List returns every known flag, active and cleared, most-recently-
// active first.
func (s *Store) List() []Flag {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

func (s *Store) listLocked() []Flag {
	out := make([]Flag, 0, len(s.byID))
	for _, f := range s.byID {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// pruneLocked evicts the oldest *cleared* flags once the store is over
// maxFlags, oldest-cleared-first. Active flags are never evicted --  in
// the (extremely unlikely) case that active flags alone exceed the cap,
// the store is simply allowed to hold more than maxFlags rather than
// discarding something a human hasn't looked at yet.
func (s *Store) pruneLocked() {
	over := len(s.byID) - maxFlags
	if over <= 0 {
		return
	}

	// Only pay for the cleared-flag scan if there are any. Once the
	// store sits above maxFlags with everything active -- which is the
	// steady state under a flood, since active flags are kept -- this
	// scan-and-sort ran on *every* Add and never found anything to
	// evict. Measured at 911ns/Add below maxFlags versus 416us at the
	// ceiling: a 457x permanent tax, not an occasional one.
	// clearedCount lets this skip the scan entirely in the steady state
	// under a flood -- everything active, nothing to evict -- instead of
	// walking every flag on every Add to rediscover that. That scan alone
	// measured ~127us per Add at the ceiling.
	if s.clearedCount > 0 {
		cleared := make([]*Flag, 0, s.clearedCount)
		for _, f := range s.byID {
			if f.Cleared {
				cleared = append(cleared, f)
			}
		}
		sort.Slice(cleared, func(i, j int) bool { return cleared[i].ClearedAt.Before(cleared[j].ClearedAt) })
		for i := 0; i < over && i < len(cleared); i++ {
			delete(s.byID, cleared[i].ID)
			s.clearedCount--
		}
	}

	// Hard ceiling. Preferring to evict cleared flags is right -- an
	// active flag is something a human still needs to see, so it should
	// outlive reviewed noise. But "never evict an active flag" is only
	// safe if the number of active flags is bounded, and it is not:
	// flag targets come from unauthenticated syslog, so an attacker can
	// mint distinct ones as fast as they can send packets.
	//
	// Sheds a batch rather than the exact overflow, for the same reason
	// internal/evict documents: evicting one at a time means a full scan
	// and sort per Add forever. A batch amortizes it across the next
	// several thousand insertions.
	if len(s.byID) <= maxFlagsHardCeiling {
		return
	}
	target := evict.Target(maxFlagsHardCeiling)
	all := make([]*Flag, 0, len(s.byID))
	for _, f := range s.byID {
		all = append(all, f)
	}
	// Order matters more than it looks. This used to shed by FirstSeen
	// ascending -- earliest-raised first -- which is precisely backwards:
	// the first flag of a real incident is the most valuable thing in the
	// store, and an attacker only has to mint maxFlagsHardCeiling junk
	// targets to push it out. Reproduced on the old code: one genuine
	// active flag, then 5,001 `new_device` flags (any unseen src-mac, no
	// threshold to cross) sent as ~600 KB of syslog in about 7 ms, and
	// the genuine flag was gone from both byID and List() permanently.
	//
	// Cleared flags go first (a human has reviewed them). Among active
	// ones, Count -- how many times this detector has re-fired for this
	// target -- is the best available "this is noise" signal: minted
	// flags fire once each, a real incident re-fires. LastSeen breaks
	// ties, which is also what SECURITY.md already advertises for the
	// detector-side caps.
	//
	// This does not make the store immune, and cannot: a bounded store
	// under an unbounded flood must drop something. What it does is stop
	// the eviction order from actively selecting for the evidence worth
	// keeping, and make the loss visible rather than silent. See #285.
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Cleared != b.Cleared {
			return a.Cleared
		}
		if a.Count != b.Count {
			return a.Count < b.Count
		}
		return a.LastSeen.Before(b.LastSeen)
	})
	shedActive := 0
	for i := 0; i < len(all) && len(s.byID) > target; i++ {
		if all[i].Cleared {
			s.clearedCount--
		} else {
			shedActive++
		}
		delete(s.byID, all[i].ID)
	}
	if shedActive > 0 {
		// An active flag is something nobody has looked at yet, so
		// dropping one is worth saying out loud. Rate-limited by the
		// batch shed itself: this can only fire once per batch, not once
		// per evicted flag.
		s.shedActive += uint64(shedActive)
		persistLog.Warn(fmt.Sprintf(
			"flag store hit its hard ceiling of %d and dropped %d active (unreviewed) flags, %d in total so far -- "+
				"this happens when far more distinct flag targets arrive than a real network produces, "+
				"which is itself worth investigating",
			maxFlagsHardCeiling, shedActive, s.shedActive))
	}
}

// persistMinInterval rate-limits persistLocked's actual disk writes --
// a detector re-firing on an active flag calls this on every single
// matching event for as long as the condition holds, not once per
// episode, so sustained high-rate traffic (a real port scan, the exact
// condition detection exists to catch) used to mean a full JSON
// marshal + atomic rename on every event, directly on the detection
// hot path. The trade-off: the very latest state is only durably
// persisted once another persistLocked call arrives after this
// interval elapses, so a change made right before mikroview crashes or
// is killed can be lost for up to this long -- an explicit, bounded
// version of the same "best-effort, not critical state" trade-off this
// package's own doc comment already makes for flags persistence in
// general (in-memory state, which every read goes through, is always
// immediately correct regardless).
//
// A var rather than a const so tests that need every call to persist
// immediately (e.g. a round-trip test with no delay between calls) can
// shrink it, same convention as maxFlags/maxTrackedSources/
// maxTCPConnections elsewhere in this codebase. Now persist.WriteBehind's
// MinInterval -- see OpenWithBackend -- rather than a field this type
// checks itself; the rate-limiting/back-off logic that used to live
// here, and its #377 stall-under-load defect, both moved to that type
// (issue #400).
var persistMinInterval = time.Second

// persistLocked encodes the current state -- flags and exclusions alike,
// see persistedState -- and hands it to the write-behind writer (see
// persist.WriteBehind), which coalesces it with whatever else is
// pending and persists it off this goroutine, under its own deadline and
// rate limit. Marshal failures are swallowed rather than surfaced to
// Add/Clear/Exclude's callers: the in-memory state (which every read
// goes through) stays correct either way, so a transient disk issue
// degrades to "won't survive a restart right now" rather than breaking
// live use. Must be called with s.mu already held -- the "lock covers
// the in-memory mutation and an encode/snapshot, nothing past that"
// contract issue #400 asks for; MarkDirty itself never touches the
// backend.
func (s *Store) persistLocked() {
	if s.wb == nil {
		return
	}

	excluded := make([]Exclusion, 0, len(s.excluded))
	for _, e := range s.excluded {
		excluded = append(excluded, e)
	}
	sort.Slice(excluded, func(i, j int) bool { return excluded[i].ID < excluded[j].ID })

	data, err := json.MarshalIndent(persistedState{Flags: s.listLocked(), Excluded: excluded}, "", "  ")
	if err != nil {
		persistLog.Error(fmt.Sprintf("encoding flags for persistence failed: %v -- this change exists only in memory and will be lost on restart", err))
		return
	}
	s.wb.MarkDirty(data)
}
