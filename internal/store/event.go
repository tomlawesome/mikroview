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
	ID       uint64    `json:"id"`
	Time     time.Time `json:"time"`
	DeviceID string    `json:"deviceId"`
	SourceIP string    `json:"sourceIp"`

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

	Length int    `json:"length,omitempty"`
	Flags  string `json:"flags,omitempty"` // TCP flags (e.g. "SYN"), or ICMP "type X, code Y"

	Raw string `json:"raw"`
}
