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
	Chain     string `json:"chain"`

	InInterface  string `json:"inInterface,omitempty"`
	OutInterface string `json:"outInterface,omitempty"`
	ConnState    string `json:"connState,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	SrcMAC       string `json:"srcMac,omitempty"`

	SrcIP   string `json:"srcIp,omitempty"`
	SrcPort int    `json:"srcPort,omitempty"`
	DstIP   string `json:"dstIp,omitempty"`
	DstPort int    `json:"dstPort,omitempty"`

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
}
