// SPDX-License-Identifier: AGPL-3.0-only

package dossier

import (
	"sort"
	"strings"
	"time"
)

// Signal is one named thing observed about a host: a protocol it spoke,
// a shape of its traffic, or a fact read off its hardware address.
// Signals exist so the identity table below can be a table -- rows
// naming signals -- instead of a nest of conditions nobody can review.
type Signal string

const (
	SigMQTT      Signal = "mqtt"
	SigNTP       Signal = "ntp"
	SigDNS       Signal = "dns"
	SigMDNS      Signal = "mdns"
	SigSSDP      Signal = "ssdp"
	SigSMB       Signal = "smb"
	SigNetBIOS   Signal = "netbios"
	SigRDP       Signal = "rdp"
	SigLDAP      Signal = "ldap"
	SigKerberos  Signal = "kerberos"
	SigSSH       Signal = "ssh"
	SigTelnet    Signal = "telnet"
	SigVNC       Signal = "vnc"
	SigNFS       Signal = "nfs"
	SigISCSI     Signal = "iscsi"
	SigAFP       Signal = "afp"
	SigPrintRaw  Signal = "printer-raw"
	SigIPP       Signal = "ipp"
	SigLPD       Signal = "lpd"
	SigRTSP      Signal = "rtsp"
	SigONVIF     Signal = "onvif-discovery"
	SigSIP       Signal = "sip"
	SigModbus    Signal = "modbus"
	SigBACnet    Signal = "bacnet"
	SigSyslog    Signal = "syslog"
	SigSMTP      Signal = "smtp"
	SigDLNA      Signal = "dlna"
	SigWebServed Signal = "web-served"

	// Shape signals, read off the traffic rather than one port.
	SigFewInternetPeers Signal = "few-internet-peers"
	SigLANOnly          Signal = "lan-only"

	// Address signals, read off the MAC.
	SigLocallyAdministered Signal = "locally-administered-mac"
	SigVendorEmbedded      Signal = "vendor-embedded"
	SigVendorSBC           Signal = "vendor-single-board"
	SigVendorPrinter       Signal = "vendor-printer"
	SigVendorCamera        Signal = "vendor-camera"
	SigVendorNAS           Signal = "vendor-nas"
	SigVendorNetworkGear   Signal = "vendor-network-gear"
	SigVendorHypervisor    Signal = "vendor-hypervisor"
	SigVendorPhone         Signal = "vendor-voip-phone"
)

// portSignal is one row of the port-to-signal table: which port, on
// which protocol ("" matches any), in which direction ("in", "out" or
// "" for either), means which signal.
//
// Direction matters more than it looks. A host *reached on* 445 is
// serving SMB; a host *reaching* 445 is a client of somebody else's
// file server. Both are worth knowing and they say different things
// about what the host is, so the table names the direction rather than
// treating a port as a property of the host.
type portSignal struct {
	port      int
	protocol  string
	direction string
	signal    Signal
}

var portSignals = []portSignal{
	// Device-to-service protocols: the ones that separate an appliance
	// from a general-purpose computer.
	{port: 1883, direction: "out", signal: SigMQTT},
	{port: 8883, direction: "out", signal: SigMQTT},
	{port: 1883, direction: "in", signal: SigMQTT},
	{port: 123, direction: "out", signal: SigNTP},
	{port: 53, direction: "out", signal: SigDNS},
	{port: 5353, signal: SigMDNS},
	{port: 1900, signal: SigSSDP},

	// Windows and directory services.
	{port: 445, signal: SigSMB},
	{port: 139, signal: SigNetBIOS},
	{port: 137, signal: SigNetBIOS},
	{port: 3389, signal: SigRDP},
	{port: 389, signal: SigLDAP},
	{port: 636, signal: SigLDAP},
	{port: 88, signal: SigKerberos},

	// Remote access.
	{port: 22, signal: SigSSH},
	{port: 23, signal: SigTelnet},
	{port: 5900, signal: SigVNC},

	// Storage.
	{port: 2049, signal: SigNFS},
	{port: 3260, signal: SigISCSI},
	{port: 548, signal: SigAFP},

	// Printing.
	{port: 9100, signal: SigPrintRaw},
	{port: 631, signal: SigIPP},
	{port: 515, signal: SigLPD},

	// Cameras and voice.
	{port: 554, signal: SigRTSP},
	{port: 3702, signal: SigONVIF},
	{port: 5060, signal: SigSIP},
	{port: 5061, signal: SigSIP},

	// Building and industrial control.
	{port: 502, signal: SigModbus},
	{port: 47808, signal: SigBACnet},

	// Infrastructure and media.
	{port: 514, signal: SigSyslog},
	{port: 25, direction: "out", signal: SigSMTP},
	{port: 587, direction: "out", signal: SigSMTP},
	{port: 32400, signal: SigDLNA},
	{port: 8200, signal: SigDLNA},

	// Something reached a web port on this host: it has an interface
	// worth opening, which is the most useful thing the probe line can
	// offer.
	{port: 80, direction: "in", signal: SigWebServed},
	{port: 443, direction: "in", signal: SigWebServed},
	{port: 8080, direction: "in", signal: SigWebServed},
	{port: 8443, direction: "in", signal: SigWebServed},
}

// signalForPort looks one port up in the table above.
func signalForPort(port int, protocol, direction string) (Signal, bool) {
	for _, ps := range portSignals {
		if ps.port != port {
			continue
		}
		if ps.protocol != "" && !strings.EqualFold(ps.protocol, protocol) {
			continue
		}
		if ps.direction != "" && ps.direction != direction {
			continue
		}
		return ps.signal, true
	}
	return "", false
}

// vendorHint maps part of an IEEE-registered organisation name to what
// that vendor mostly makes. It is a hint and nothing more: Espressif
// sells chips that end up in doorbells and in laboratory instruments
// alike, and a vendor row alone never names a device -- it only adds
// weight to a profile the traffic already matched.
//
// Matching is on a lowercased substring of the registered name, which
// is why the fragments are distinctive ones rather than whole names:
// IEEE records "Espressif Inc." today and could record "Espressif
// Systems (Shanghai) Co., Ltd." tomorrow.
type vendorHint struct {
	fragment string
	signal   Signal
}

var vendorHints = []vendorHint{
	{"espressif", SigVendorEmbedded},
	{"tuya", SigVendorEmbedded},
	{"shelly", SigVendorEmbedded},
	{"sonoff", SigVendorEmbedded},
	{"raspberry pi", SigVendorSBC},
	{"beagleboard", SigVendorSBC},
	{"hewlett packard", SigVendorPrinter},
	{"brother industries", SigVendorPrinter},
	{"seiko epson", SigVendorPrinter},
	{"canon", SigVendorPrinter},
	{"axis communications", SigVendorCamera},
	{"hangzhou hikvision", SigVendorCamera},
	{"dahua", SigVendorCamera},
	{"synology", SigVendorNAS},
	{"qnap", SigVendorNAS},
	{"mikrotik", SigVendorNetworkGear},
	{"ubiquiti", SigVendorNetworkGear},
	{"tp-link", SigVendorNetworkGear},
	{"netgear", SigVendorNetworkGear},
	{"vmware", SigVendorHypervisor},
	{"parallels", SigVendorHypervisor},
	{"xensource", SigVendorHypervisor},
	{"yealink", SigVendorPhone},
	{"grandstream", SigVendorPhone},
	{"polycom", SigVendorPhone},
}

// Confidence is how much weight to put on a suggestion, in words. Words
// rather than a number on purpose: a percentage invites arithmetic the
// evidence cannot support, and an operator acts on "worth a look"
// versus "near certain", not on 0.62.
type Confidence string

const (
	Weak   Confidence = "weak"
	Fair   Confidence = "fair"
	Strong Confidence = "strong"
)

// down lowers a confidence one step, for the thin-evidence rule.
func (c Confidence) down() Confidence {
	switch c {
	case Strong:
		return Fair
	case Fair:
		return Weak
	}
	return Weak
}

// Profile is one row of the identity table: the signals that must be
// present, the signals at least one of which must be, the signals whose
// presence rules it out, and the most this row may ever claim.
type Profile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// All must every one be present.
	All []Signal `json:"-"`
	// Any, when non-empty, needs at least one present.
	Any []Signal `json:"-"`
	// None rules the profile out if any is present.
	None []Signal `json:"-"`
	// Ceiling is the most this row may claim on perfect evidence. The
	// thin-evidence rule can only lower it.
	Ceiling Confidence `json:"-"`
	// Because is the one-sentence reason this combination reads the way
	// it does -- shown to the operator, so it is written for them.
	Because string `json:"because"`
}

// profiles is the whole identity table, most specific first. Order is
// the tie-break between rows that match equally strongly, so the more
// specific reading of the same evidence wins.
//
// It is deliberately small and deliberately hedged: every label ends in
// "-ish" or names a role rather than a product, because the traffic can
// support "this behaves like a Windows machine" and cannot support
// "this is a Windows 11 laptop". Rows are added when a combination
// genuinely narrows the hunt, not to increase coverage.
var profiles = []Profile{
	{
		ID:      "windows-domain",
		Label:   "Windows-ish host in a domain",
		All:     []Signal{SigSMB, SigKerberos},
		Any:     []Signal{SigLDAP, SigRDP, SigNetBIOS},
		Ceiling: Strong,
		Because: "Kerberos beside SMB is what a domain-joined Windows machine does and almost nothing else does",
	},
	{
		ID:      "windows",
		Label:   "Windows-ish host",
		All:     []Signal{SigSMB, SigRDP},
		Ceiling: Strong,
		Because: "SMB and RDP together are a Windows desktop or server; other systems speak one or the other, rarely both",
	},
	{
		ID:      "nas",
		Label:   "file server or NAS",
		All:     []Signal{SigSMB},
		Any:     []Signal{SigNFS, SigISCSI, SigAFP, SigVendorNAS},
		None:    []Signal{SigRDP},
		Ceiling: Strong,
		Because: "SMB alongside a second file-sharing protocol is a storage appliance rather than a desktop",
	},
	{
		ID:      "printer",
		Label:   "printer",
		Any:     []Signal{SigPrintRaw, SigIPP, SigLPD, SigVendorPrinter},
		None:    []Signal{SigRDP},
		Ceiling: Strong,
		Because: "raw printing, IPP or LPD is served by printers and print servers and by nothing else worth confusing them with",
	},
	{
		ID:      "camera",
		Label:   "IP camera or video recorder",
		Any:     []Signal{SigRTSP, SigONVIF, SigVendorCamera},
		Ceiling: Fair,
		Because: "RTSP and ONVIF discovery are the video-surveillance protocols; the vendor row, on its own, is only a hint",
	},
	{
		ID:      "voip",
		Label:   "VoIP phone or PBX",
		Any:     []Signal{SigSIP, SigVendorPhone},
		Ceiling: Fair,
		Because: "SIP is voice signalling, so this is a handset, a gateway or the PBX itself",
	},
	{
		ID:      "industrial",
		Label:   "building or industrial controller",
		Any:     []Signal{SigModbus, SigBACnet},
		Ceiling: Strong,
		Because: "Modbus and BACnet are plant and building-automation protocols that only controllers speak",
	},
	{
		ID:      "iot",
		Label:   "IoT-ish device",
		All:     []Signal{SigMQTT},
		Any:     []Signal{SigNTP, SigFewInternetPeers, SigVendorEmbedded, SigLANOnly},
		None:    []Signal{SigRDP, SigSMB},
		Ceiling: Fair,
		Because: "MQTT is how small devices talk to a broker, and a clock check plus a handful of fixed endpoints is the rest of an appliance's whole life",
	},
	{
		ID:      "media",
		Label:   "media server or player",
		Any:     []Signal{SigDLNA},
		Ceiling: Weak,
		Because: "DLNA/UPnP media ports are shared by servers, players and televisions, so this narrows the kind and not the device",
	},
	{
		ID:      "unix-server",
		Label:   "Unix-ish server",
		All:     []Signal{SigSSH},
		Any:     []Signal{SigWebServed, SigSyslog, SigVendorSBC},
		None:    []Signal{SigRDP},
		Ceiling: Fair,
		Because: "an SSH service beside something it serves is a machine someone administers, not an appliance",
	},
	{
		ID:      "network-gear",
		Label:   "network equipment",
		Any:     []Signal{SigVendorNetworkGear},
		None:    []Signal{SigRDP, SigSMB},
		Ceiling: Weak,
		Because: "the hardware vendor makes network equipment, which is a hint about the box and not a reading of its traffic",
	},
	{
		ID:      "virtual",
		Label:   "virtual or randomised interface",
		All:     []Signal{SigLocallyAdministered},
		Ceiling: Fair,
		Because: "the address was made up locally, so this is a VM, a container, or a device randomising its MAC -- ask which host made it, not which vendor made it",
	},
	{
		ID:      "embedded",
		Label:   "embedded device",
		Any:     []Signal{SigVendorEmbedded, SigVendorSBC},
		None:    []Signal{SigRDP, SigSMB},
		Ceiling: Weak,
		Because: "the hardware vendor sells microcontrollers or single-board computers, which is a hint about what it is built from",
	},
	{
		ID:      "quiet-endpoint",
		Label:   "appliance-ish endpoint",
		All:     []Signal{SigNTP},
		Any:     []Signal{SigFewInternetPeers, SigLANOnly},
		None:    []Signal{SigRDP, SigSMB, SigSSH},
		Ceiling: Weak,
		Because: "it checks the time and talks to almost nothing else, which is the shape of an appliance rather than of a computer somebody uses",
	},
}

// Evidence is one line behind a suggestion: the signal, and what was
// actually observed to record it.
type Evidence struct {
	Signal string `json:"signal"`
	Detail string `json:"detail"`
}

// Identity is the suggested reading of the host, or an explicit refusal
// to suggest one.
type Identity struct {
	Suggested  bool       `json:"suggested"`
	Profile    string     `json:"profile,omitempty"`
	Label      string     `json:"label,omitempty"`
	Confidence Confidence `json:"confidence,omitempty"`
	Because    string     `json:"because,omitempty"`
	// Evidence is what the suggestion rests on -- always present when
	// one is made, and populated with whatever *was* seen even when no
	// suggestion is made, so a card that names nothing still shows its
	// working.
	Evidence []Evidence `json:"evidence,omitempty"`
	// Alternatives are other profiles the same evidence also matched,
	// so a suggestion never hides the reading it beat.
	Alternatives []string `json:"alternatives,omitempty"`
	Note         string   `json:"note"`
}

// Thin evidence: a suggestion drawn from a handful of events over a few
// minutes rests on less than the same suggestion drawn from a day of
// traffic, so its confidence drops a step and the card says why. The
// numbers are round and deliberately unfitted -- they mark "barely
// anything to go on", not a calibrated boundary.
const (
	minEventsForFullConfidence = 20
	minSpanForFullConfidence   = 10 * time.Minute
)

// suggest runs the table against a fingerprint. It is total: no match
// is a first-class outcome with its own wording, never an empty
// suggestion the UI has to interpret.
func suggest(fp fingerprint, mac macFacts) Identity {
	signals := map[Signal]string{}
	for s, detail := range fp.signals {
		signals[s] = detail
	}
	// Address-derived signals join the traffic ones, so one table reads
	// both.
	if mac.parsed {
		if mac.mac.LocallyAdministered {
			signals[SigLocallyAdministered] = "the MAC " + mac.mac.Address + " has the locally-administered bit set, so no vendor registered it"
		}
		if mac.vendor.Known {
			lower := strings.ToLower(mac.vendor.Name)
			for _, h := range vendorHints {
				if strings.Contains(lower, h.fragment) {
					signals[h.signal] = "IEEE registers its MAC prefix to " + mac.vendor.Name
					break
				}
			}
		}
	}

	if len(signals) == 0 {
		return Identity{
			Note: "nothing recognisable enough to suggest an identity: no known protocol, no vendor and no address hint has been seen for this host",
		}
	}

	type scored struct {
		p       Profile
		score   int
		matched []Signal
	}
	var hits []scored
	for _, p := range profiles {
		m, ok := matchProfile(p, signals)
		if !ok {
			continue
		}
		hits = append(hits, scored{p: p, score: len(m), matched: m})
	}

	if len(hits) == 0 {
		return Identity{
			Evidence: evidenceFor(signalList(signals), signals),
			Note:     "the traffic and hardware address do not match any profile in mikroview's table, so no identity is suggested -- the evidence below is still what was seen",
		}
	}

	// Highest score wins; ties go to the earlier (more specific) row.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	best := hits[0]

	id := Identity{
		Suggested:  true,
		Profile:    best.p.ID,
		Label:      best.p.Label,
		Confidence: best.p.Ceiling,
		Because:    best.p.Because,
		Evidence:   evidenceFor(best.matched, signals),
	}
	for _, h := range hits[1:] {
		id.Alternatives = append(id.Alternatives, h.p.Label)
	}

	thin := fp.events < minEventsForFullConfidence || (fp.span > 0 && fp.span < minSpanForFullConfidence)
	if thin {
		id.Confidence = id.Confidence.down()
		id.Note = "confidence is lowered a step because this rests on only " + plural(fp.events, "event") + " over a short window"
	} else {
		id.Note = "a suggestion from mikroview's heuristic table, not a claim -- read the evidence and decide"
	}
	return id
}

// matchProfile reports whether a profile's row is satisfied, and which
// signals did the satisfying.
func matchProfile(p Profile, signals map[Signal]string) ([]Signal, bool) {
	for _, s := range p.None {
		if _, ok := signals[s]; ok {
			return nil, false
		}
	}
	var matched []Signal
	for _, s := range p.All {
		if _, ok := signals[s]; !ok {
			return nil, false
		}
		matched = append(matched, s)
	}
	if len(p.Any) > 0 {
		var anyHit bool
		for _, s := range p.Any {
			if _, ok := signals[s]; ok {
				matched = append(matched, s)
				anyHit = true
			}
		}
		if !anyHit {
			return nil, false
		}
	}
	if len(matched) == 0 {
		return nil, false
	}
	return matched, true
}

func signalList(signals map[Signal]string) []Signal {
	out := make([]Signal, 0, len(signals))
	for s := range signals {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func evidenceFor(sigs []Signal, signals map[Signal]string) []Evidence {
	out := make([]Evidence, 0, len(sigs))
	for _, s := range sigs {
		out = append(out, Evidence{Signal: string(s), Detail: signals[s]})
	}
	return out
}
