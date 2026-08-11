// SPDX-License-Identifier: AGPL-3.0-only

package store

import "time"

type Action string

const (
	ActionAccept  Action = "accept"
	ActionDrop    Action = "drop"
	ActionReject  Action = "reject"
	ActionLog     Action = "log"
	ActionUnknown Action = "unknown"
)

// Event is a single parsed firewall log line from a RouterOS device.
type Event struct {
	ID uint64 `json:"id"`
	// Time is the RouterOS device's own self-reported timestamp: it is not
	// guaranteed to be monotonic with insertion order (device clocks can be
	// skewed or unsynced), so query windowing uses ReceivedAt instead.
	Time       time.Time `json:"time"`
	ReceivedAt time.Time `json:"-"`
	DeviceID   string    `json:"deviceId"`
	SourceIP   string    `json:"sourceIp"`

	Action    Action `json:"action"`
	RuleLabel string `json:"ruleLabel"`
	// RuleName is a user-configured friendly name for RuleLabel (see
	// internal/naming, internal/config's RuleNames) -- empty if none is
	// configured. RuleLabel itself is always the raw value RouterOS
	// reported (e.g. "r13"); filtering/grouping stays keyed on that, this
	// is display-only.
	RuleName string `json:"ruleName,omitempty"`
	Chain    string `json:"chain"`

	InInterface  string `json:"inInterface,omitempty"`
	OutInterface string `json:"outInterface,omitempty"`
	ConnState    string `json:"connState,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	SrcMAC       string `json:"srcMac,omitempty"`

	SrcIP   string `json:"srcIp,omitempty"`
	SrcPort int    `json:"srcPort,omitempty"`
	DstIP   string `json:"dstIp,omitempty"`
	DstPort int    `json:"dstPort,omitempty"`

	// SrcPortName/DstPortName are user-configured friendly names for
	// SrcPort/DstPort (see internal/naming.Resolver.Port, issue #109) --
	// empty whenever the port is 0 (no port) or has no matching entity.
	// Same display-only relationship to SrcPort/DstPort that RuleName has
	// to RuleLabel.
	SrcPortName string `json:"srcPortName,omitempty"`
	DstPortName string `json:"dstPortName,omitempty"`

	// SrcHostName/DstHostName are user-configured friendly names for
	// SrcIP/DstIP (see internal/naming, internal/config's HostNames) --
	// empty if none is configured. Same display-only relationship to
	// SrcIP/DstIP that RuleName has to RuleLabel.
	SrcHostName string `json:"srcHostName,omitempty"`
	DstHostName string `json:"dstHostName,omitempty"`

	// SrcCountry/DstCountry are ISO 3166-1 alpha-2 codes from an optional
	// GeoIP lookup (see internal/geoip) -- empty whenever GeoIP isn't
	// configured, the address is private, or it has no match.
	SrcCountry string `json:"srcCountry,omitempty"`
	DstCountry string `json:"dstCountry,omitempty"`

	// NatIP/NatPort are the post-translation address for a srcnat/dstnat
	// chain event (which side depends on Chain: srcnat replaces src,
	// dstnat replaces dst). Empty for chains that don't perform NAT.
	// NatRaw is the verbatim "NAT (...)" annotation RouterOS logs, kept
	// alongside the parsed fields in case the structured extraction
	// misreads an unanticipated format variant.
	NatIP   string `json:"natIp,omitempty"`
	NatPort int    `json:"natPort,omitempty"`
	NatRaw  string `json:"natRaw,omitempty"`

	Length int    `json:"length,omitempty"`
	Flags  string `json:"flags,omitempty"` // TCP flags (e.g. "SYN"), or ICMP "type X, code Y"

	Raw string `json:"raw"`
	// RawTruncated marks Raw as having been cut to store.MaxRawBytes.
	// The UI shows it, so "this is the line the router sent" is never
	// quietly untrue -- see MaxRawBytes for why the cap exists.
	RawTruncated bool `json:"rawTruncated,omitempty"`
}

// MaxRawBytes caps the verbatim log line each retained event holds.
//
// Raw is deliberately kept as the router sent it, so an operator can
// compare a row against the original -- which is why it was excluded
// from the parser's 256-byte field clamp. But it was not bounded at all
// beyond the listener's 64 KiB per-message limit, while the memory
// budget mikroview documents (~120 MiB at the default 200,000 events)
// assumes an ordinary line. Both numbers cannot be right: at 64 KiB
// lines the same 200,000 slots hold 12.55 GiB, a 107x overrun, and
// nothing has to authenticate to send them -- anything able to reach the
// syslog port chooses these bytes. validate.go's own 1.47x memory
// warning compounds that rather than catching it.
//
// 2 KiB is roughly five times the longest genuine RouterOS line
// (typical lines run 150-400 bytes), so truncation only ever hits input
// a real router does not produce. What is lost when it fires is
// recorded rather than hidden: RawTruncated marks the event and the UI
// shows it. Owner decision on #285 finding 5.
const MaxRawBytes = 2048

// ClampRaw applies MaxRawBytes, reporting whether it cut anything.
// Byte-oriented like the parser's own clamp: this bounds memory, and a
// split multi-byte sequence at the boundary is displayed, never decoded.
func ClampRaw(raw string) (string, bool) {
	if len(raw) <= MaxRawBytes {
		return raw, false
	}
	return raw[:MaxRawBytes], true
}
