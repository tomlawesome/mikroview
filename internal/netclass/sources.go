// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"encoding/json"
	"net/netip"
	"strings"
)

// Category is the kind of network a range belongs to. It is deliberately
// coarse and display-oriented: mikroview shows it as context on an IP,
// it does not (in this package) turn it into a score. The research on
// #114 found that a datacenter match alone covers >10% of routable IPv4,
// so any scoring belongs behind direction and per-category weighting
// decided elsewhere -- not baked into the classifier.
type Category string

const (
	// CategoryVPN is a commercial VPN exit. High precision: X4BNet's VPN
	// list covers ~0.08% of IPv4, two orders of magnitude tighter than
	// its datacenter list.
	CategoryVPN Category = "vpn"
	// CategoryDatacenter is cloud/hosting/datacenter space -- broad, and
	// on its own weak. Useful as attribution ("DigitalOcean"), noisy as
	// a signal.
	CategoryDatacenter Category = "datacenter"
	// CategoryTor is a Tor exit node, published by the Tor Project
	// itself. The highest-precision anonymity signal on the menu.
	CategoryTor Category = "tor"
	// CategoryPrivacyRelay is Apple iCloud Private Relay / Cloudflare
	// WARP -- shown so an operator understands what they are looking at,
	// and explicitly separated because it is ordinary consumer traffic
	// that must never read as "VPN exit".
	CategoryPrivacyRelay Category = "privacy-relay"
)

// Source identifies one feed on the vetted menu. As with internal/
// blocklist, this is not an arbitrary user URL: an operator enabling a
// source is trusting mikroview's vetting of it.
type Source string

const (
	SourceTor               Source = "tor"
	SourceApplePrivateRelay Source = "apple_private_relay"
	SourceX4BVPN            Source = "x4b_vpn"
	SourceX4BDC             Source = "x4b_datacenter"
	SourceAWS               Source = "aws"
	SourceGCP               Source = "gcp"
)

// feedDef is one menu entry. Category is the classification every prefix
// from this feed carries; Parse turns the raw body into (prefix, detail)
// pairs, where detail is an optional finer label (an AWS region, say)
// shown alongside the category.
type feedDef struct {
	Source   Source
	Category Category
	Label    string // human name, e.g. "X4BNet VPN"
	URL      string
	Parse    func([]byte) []classifiedPrefix
}

// classifiedPrefix is one parsed entry: a prefix plus an optional detail
// string (region/provider) that refines the feed's category.
type classifiedPrefix struct {
	prefix netip.Prefix
	detail string
}

// feedRegistry is the whole vetted menu. Order is priority order for the
// combined-entry budget, tightest-and-most-trusted first: Tor (tiny,
// first-party), then the precise VPN list, then the broad datacenter and
// cloud lists.
//
// Azure is deliberately absent: its range document lives behind a
// date-stamped filename that is deleted within ~2 weeks and 403s to a
// plain client (measured, #114 finding 5), so no stable fetch URL
// exists. Its space is already covered by the X4BNet datacenter list via
// ASN.
var feedRegistry = []feedDef{
	{
		Source:   SourceTor,
		Category: CategoryTor,
		Label:    "Tor exit nodes",
		URL:      "https://check.torproject.org/torbulkexitlist",
		Parse:    parseTorList,
	},
	{
		// Positioned before SourceX4BVPN deliberately: X4BNet's own build
		// pipeline pulls Apple's list into their VPN feed verbatim (#114's
		// research comment names their fetch-apple-privacy-relay.yml
		// workflow), so the same prefixes exist in both. buildTable
		// resolves an exact-prefix tie by priority order, so listing the
		// authoritative source first is what makes a Private Relay egress
		// address classify as CategoryPrivacyRelay rather than shadowing
		// into CategoryVPN -- the single false positive #114's research
		// called out as mattering most ("every iPhone/iPad/Mac with
		// iCloud+ ... telling an operator 'this is a known VPN exit'
		// about their family's normal Safari browsing destroys trust in
		// the signal on day one").
		Source:   SourceApplePrivateRelay,
		Category: CategoryPrivacyRelay,
		Label:    "Apple Private Relay",
		URL:      "https://mask-api.icloud.com/egress-ip-ranges.csv",
		Parse:    parseApplePrivateRelay,
	},
	{
		Source:   SourceX4BVPN,
		Category: CategoryVPN,
		Label:    "X4BNet VPN",
		URL:      "https://raw.githubusercontent.com/X4BNet/lists_vpn/main/output/vpn/ipv4.txt",
		Parse:    parseX4B,
	},
	{
		Source:   SourceX4BDC,
		Category: CategoryDatacenter,
		Label:    "X4BNet datacenter",
		URL:      "https://raw.githubusercontent.com/X4BNet/lists_vpn/main/output/datacenter/ipv4.txt",
		Parse:    parseX4B,
	},
	{
		Source:   SourceAWS,
		Category: CategoryDatacenter,
		Label:    "AWS",
		URL:      "https://ip-ranges.amazonaws.com/ip-ranges.json",
		Parse:    parseAWS,
	},
	{
		Source:   SourceGCP,
		Category: CategoryDatacenter,
		Label:    "Google Cloud",
		URL:      "https://www.gstatic.com/ipranges/cloud.json",
		Parse:    parseGCP,
	},
}

var registryBySource = func() map[Source]feedDef {
	m := make(map[Source]feedDef, len(feedRegistry))
	for _, f := range feedRegistry {
		m[f.Source] = f
	}
	return m
}()

// DefaultSources is a conservative starting point (#114 finding 2/3): the
// high-precision lists, not the broad datacenter feeds. An operator who
// wants full cloud attribution opts the rest in. Tightening the default
// is what keeps the feature from firing on ordinary traffic the day it
// is switched on.
//
// SourceApplePrivateRelay is a default alongside SourceX4BVPN, not an
// opt-in extra: SourceX4BVPN's own upstream data includes Apple's ranges
// (see that feed's own comment above), so leaving the corrective source
// disabled by default would leave the exact false positive #114's
// research called out as mattering most -- "this is a known VPN exit"
// on an iPhone's ordinary Safari browsing -- live in the default
// configuration.
var DefaultSources = []string{string(SourceTor), string(SourceApplePrivateRelay), string(SourceX4BVPN)}

// KnownSources returns the full menu in registry order, for config
// validation messages and docs.
func KnownSources() []Source {
	out := make([]Source, len(feedRegistry))
	for i, f := range feedRegistry {
		out[i] = f.Source
	}
	return out
}

func parseTorList(body []byte) []classifiedPrefix {
	prefixes := parsePlainCIDRs(body)
	out := make([]classifiedPrefix, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, classifiedPrefix{prefix: p})
	}
	return out
}

func parseX4B(body []byte) []classifiedPrefix {
	prefixes := parsePlainCIDRs(body)
	out := make([]classifiedPrefix, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, classifiedPrefix{prefix: p})
	}
	return out
}

// parseAWS reads the official ip-ranges.json. The region is kept as the
// detail so a match reads "AWS eu-west-1" rather than just "AWS".
func parseAWS(body []byte) []classifiedPrefix {
	var doc struct {
		Prefixes []struct {
			IPPrefix string `json:"ip_prefix"`
			Region   string `json:"region"`
		} `json:"prefixes"`
		IPv6Prefixes []struct {
			IPPrefix string `json:"ipv6_prefix"`
			Region   string `json:"region"`
		} `json:"ipv6_prefixes"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	out := make([]classifiedPrefix, 0, len(doc.Prefixes)+len(doc.IPv6Prefixes))
	add := func(cidr, region string) {
		p, err := netip.ParsePrefix(cidr)
		if err != nil || !acceptablePrefix(p) {
			return
		}
		out = append(out, classifiedPrefix{prefix: p.Masked(), detail: region})
	}
	for _, e := range doc.Prefixes {
		add(e.IPPrefix, e.Region)
	}
	for _, e := range doc.IPv6Prefixes {
		add(e.IPPrefix, e.Region)
	}
	return out
}

// parseGCP reads the official cloud.json, whose entries carry either an
// ipv4Prefix or an ipv6Prefix and a scope (region).
func parseGCP(body []byte) []classifiedPrefix {
	var doc struct {
		Prefixes []struct {
			IPv4Prefix string `json:"ipv4Prefix"`
			IPv6Prefix string `json:"ipv6Prefix"`
			Scope      string `json:"scope"`
		} `json:"prefixes"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	out := make([]classifiedPrefix, 0, len(doc.Prefixes))
	for _, e := range doc.Prefixes {
		cidr := e.IPv4Prefix
		if cidr == "" {
			cidr = e.IPv6Prefix
		}
		if cidr == "" {
			continue
		}
		p, err := netip.ParsePrefix(cidr)
		if err != nil || !acceptablePrefix(p) {
			continue
		}
		out = append(out, classifiedPrefix{prefix: p.Masked(), detail: e.Scope})
	}
	return out
}

// parseApplePrivateRelay reads Apple's official egress-ip-ranges.csv:
// one prefix per line, `prefix,country,subdivision,city,` -- five
// comma-separated fields with a permanently-empty trailing one,
// verified against the live feed (287,715 lines, every one exactly
// four commas, no blank lines, no embedded commas in a field) rather
// than assumed. City is kept as detail so a match reads "Apple Private
// Relay (Boston)" rather than just the provider name; malformed lines
// are skipped individually; one bad line is not a reason to discard the
// whole feed, the same tolerance every other parser here has.
func parseApplePrivateRelay(body []byte) []classifiedPrefix {
	var out []classifiedPrefix
	for _, raw := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ",", 4)
		if len(fields) < 4 {
			continue
		}
		p, err := netip.ParsePrefix(fields[0])
		if err != nil || !acceptablePrefix(p) {
			continue
		}
		city := fields[3]
		if i := strings.IndexByte(city, ','); i >= 0 {
			city = city[:i]
		}
		out = append(out, classifiedPrefix{prefix: p.Masked(), detail: city})
	}
	return out
}
