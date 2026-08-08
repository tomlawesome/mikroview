// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"encoding/json"
	"net/netip"
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
	SourceTor    Source = "tor"
	SourceX4BVPN Source = "x4b_vpn"
	SourceX4BDC  Source = "x4b_datacenter"
	SourceAWS    Source = "aws"
	SourceGCP    Source = "gcp"
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
// two high-precision lists, not the broad datacenter feeds. An operator
// who wants full cloud attribution opts the rest in. Tightening the
// default is what keeps the feature from firing on ordinary traffic the
// day it is switched on.
var DefaultSources = []string{string(SourceTor), string(SourceX4BVPN)}

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
