// SPDX-License-Identifier: AGPL-3.0-only

// Package blcatalogue is the blocklist builder's catalogue (#1360): the
// known-bad address lists MikroView offers to load onto a router, with
// what each card says about them and the record of lists considered and
// left out. Researched in docs/design/screens/blocklist/round-2/
// catalogue.md (2026-09-30) and ratified with the owner's answers on
// #1360: 1b and 1c admit four more lists beside the three that passed
// the strict bar, each labelled with a caveat on its card.
//
// Names, URLs, terms and parse recipes only. The router fetches each
// list from its source on its own schedule; MikroView never serves,
// copies or vendors list data (AGENTS.md, "List and lookup data"). A
// feed's attribution still travels with the data on the router --
// Spamhaus asks for credit and its © text in each entry, which part 4's
// loader writes into the address-list comment.
//
// What every list shares, so it is not a field: the drop goes in raw
// prerouting, placed first (owner, 11a); logging the drops defaults to
// yes (owner, 12a; BUILD.md for the four new lists); lists are named
// mv-bl-<key> (ListNames).
//
// The four lists the owner admitted on 2026-10-01 were read at the
// source on 2026-10-01, the day this package was written:
//
//   - blocklist.de strongips, https://lists.blocklist.de/lists/strongips.txt:
//     385 addresses, one per line, no header, 5 KB. The export page
//     (https://www.blocklist.de/en/export.html) states no licence; its
//     only terms are "These files are as they are, and to be used at
//     your own risk", a warranty disclaimer rather than a grant.
//   - GreenSnow, https://blocklist.greensnow.co/greensnow.txt: 4,935
//     addresses, one per line, no header, 70 KB. https://greensnow.co/
//     states no licence and reads "Copyright © 2013-2026 GreenSnow.co.
//     All rights reserved. Reproduction or republication strictly
//     prohibited." The router fetching it for its own use is neither;
//     MikroView republishes nothing.
//   - DShield top 20, https://feeds.dshield.org/block.txt (the header's
//     "primary URL"; www.dshield.org/block.txt serves the same file):
//     20 /24 networks, tab-separated "start end mask attacks name
//     country email" after a # header, 2 KB. The header: "(c) $year
//     DShield.org, some rights reserved. Details
//     http://creativecommons.org/licenses/by-nc-sa/2.5/ -- use on your own
//     risk", and "the top 20 attacking class C (/24) subnets over the
//     last three days".
//   - Binary Defense banlist, https://binarydefense.com/banlist.txt
//     (www.binarydefense.com answers 301 to it, so the bare host saves
//     a redirect on a router below 7.22): 1,514 addresses, one per line
//     after a # header, 22 KB. The header: "Note that this is for public
//     use only. The ATIF feed may not be used for commercial resale or
//     in products that are charging fees for such services. Use of these
//     feeds for commerical (having others pay for a service) use is
//     strictly prohibited."
//
// The three that passed the strict bar are as catalogue.md records them
// (2026-09-30); their URLs were re-fetched on 2026-10-01 and all answer
// 200 directly.
package blcatalogue

import "slices"

// Format is how a list's file is laid out, which picks the loader's
// parse recipe (BUILD.md part 4).
type Format string

const (
	// FormatSpamhausJSON is one JSON object per line,
	// {"cidr":"1.10.16.0/20","sblid":"SBL256894","rir":"apnic"}; a line
	// without cidr (the file's metadata line) is skipped.
	FormatSpamhausJSON Format = "spamhaus-json"
	// FormatOnePerLine is one address or prefix per line; blank lines and
	// lines starting # or ; are comments.
	FormatOnePerLine Format = "one-per-line"
	// FormatDShield is DShield's tab-separated "start end mask ..." rows
	// after a # header, loaded as start/mask.
	FormatDShield Format = "dshield"
)

// Direction is which traffic a list's rules drop.
type Direction string

const (
	// DirectionFrom drops traffic from a listed address.
	DirectionFrom Direction = "from"
	// DirectionBoth also drops traffic to one: a second rule matching
	// the destination.
	DirectionBoth Direction = "both"
)

// Refresh is how often the router fetches a list again.
type Refresh string

const (
	RefreshHourly   Refresh = "hourly"
	RefreshSixHours Refresh = "6h"
	RefreshDaily    Refresh = "daily"
	// RefreshWeekdays is daily, Monday to Friday -- the scheduler's
	// days=, which needs RouterOS 7.24 (routeros.Features.SchedulerDays).
	RefreshWeekdays Refresh = "weekdays"
	RefreshWeekly   Refresh = "weekly"
)

// Caveat labels: the fact a card carries for a list admitted under the
// owner's 1b and 1c (2026-10-01).
const (
	CaveatNoLicence   = "no licence stated"
	CaveatNotBusiness = "not for business use"
)

// List is one entry in the catalogue -- one card on the page.
type List struct {
	// Key names the list everywhere MikroView writes it on the router:
	// the address list, script and scheduler mv-bl-<Key>, and the rule
	// comment "mikroview blocklist: <Key> (from|to)".
	Key string
	// Name is the card's title; Short is the list's name in a ledger row
	// or a receipt.
	Name  string
	Short string
	// URL is the file the router fetches; URL6 its IPv6 counterpart, set
	// only where IPv6 is true.
	URL  string
	URL6 string
	// Format picks the loader's parse recipe.
	Format Format
	// Terms is what the source says about using the list, in brief.
	Terms string
	// Caveat is the card's labelled fact for a list that did not pass the
	// strict bar (CaveatNoLicence, CaveatNotBusiness), and "" for one that
	// did.
	Caveat string
	// Default is whether the list is chosen when the page opens.
	Default bool
	// DefaultDirection is the "block" choice the page opens with.
	DefaultDirection Direction
	// IPv6 is whether the list offers an IPv6 file -- on the drawn card,
	// "ipv6 too", chosen by default. Only Spamhaus has one.
	IPv6 bool
	// Refresh is the choices offered, in the order drawn; RefreshDefault
	// is the one the page opens with (RefreshDefaultFor for a router
	// without scheduler days).
	Refresh        []Refresh
	RefreshDefault Refresh
	// Facts is the card's facts line and Guide its sentence of guidance.
	// The counts in Facts are the measured samples' (catalogue.md for the
	// three, the package comment above for the four), not live numbers.
	// A **phrase** is the one the drawn card sets bold (BUILD.md,
	// "Catalogue copy and defaults"): the page renders it so and shows
	// the asterisks nowhere. A labelled list's Facts ends on its Caveat,
	// verbatim, in the slot the three carry their terms word.
	Facts string
	Guide string
	// FlaggedByMikroView is true where internal/blocklist also flags from
	// the list, so what the router drops and what MikroView flags agree.
	FlaggedByMikroView bool
}

// LeftOut is one list considered and not offered, with why.
type LeftOut struct {
	Name string
	Why  string
}

// Reviewed is the day the catalogue was last read against its sources:
// catalogue.md's research on 2026-09-30, and the four admitted lists read
// at their sources on 2026-10-01 (the package comment). The page quotes
// it beside the considered-and-left-out record.
const Reviewed = "2026-10-01"

// listPrefix is what every address list, script and scheduler the
// builder writes is named under.
const listPrefix = "mv-bl-"

// lists is the catalogue in the drawn order: the three that passed the
// strict bar, then the four the owner admitted with a caveat.
var lists = []List{
	{
		Key:   "spamhaus",
		Name:  "Spamhaus DROP",
		Short: "Spamhaus DROP",
		URL:   "https://www.spamhaus.org/drop/drop_v4.json",
		URL6:  "https://www.spamhaus.org/drop/drop_v6.json",
		// The JSON files are what Spamhaus now recommends; "in time, these
		// [text files] will be deprecated" (catalogue.md).
		Format:             FormatSpamhausJSON,
		Terms:              `free, attribution required: "credit must be given to Spamhaus Project, and the date and © text should remain with the file and data"; no automatic fetch more than once an hour`,
		Default:            true,
		DefaultDirection:   DirectionFrom,
		IPv6:               true,
		Refresh:            []Refresh{RefreshSixHours, RefreshDaily, RefreshWeekly},
		RefreshDefault:     RefreshDaily,
		Facts:              "**1,692** ranges + 91 IPv6 · 100 KB · barely moves · false positives **rare** · spamhaus.org · credited",
		Guide:              "Whole netblocks Spamhaus says carry **no legitimate traffic** — hijacked space and criminal hosting. MikroView already flags from it, so what the router drops and what MikroView flags stay the same.",
		FlaggedByMikroView: true,
	},
	{
		Key:                "et",
		Name:               "Emerging Threats compromised IPs",
		Short:              "Emerging Threats",
		URL:                "https://rules.emergingthreats.net/blockrules/compromised-ips.txt",
		Format:             FormatOnePerLine,
		Terms:              "BSD-3-Clause (the ET Open distribution's rules/LICENSE); ET asks for one fetch a day",
		Default:            true,
		DefaultDirection:   DirectionBoth, // owner, 17a
		Refresh:            []Refresh{RefreshDaily, RefreshWeekdays, RefreshWeekly},
		RefreshDefault:     RefreshWeekdays,
		Facts:              "**633** hosts · 9 KB · rebuilt on weekdays · false positives **some** · emergingthreats.net · BSD",
		Guide:              "Single hosts seen attacking in the last few days. Churns, and a cleaned-up host stays listed a while, so a **drop is occasionally an innocent server** — MikroView shows each one. It flags from this list already.",
		FlaggedByMikroView: true,
	},
	{
		Key:    "cins",
		Name:   "CINS Army",
		Short:  "CINS Army",
		URL:    "https://cinsscore.com/list/ci-badguys.txt",
		Format: FormatOnePerLine,
		Terms:  `free, "as a simple text file, with which you can parse and use in any way you see fit"`,
		// Off by default: 15,000 hosts, worth it only on a router that
		// exposes a service (catalogue.md).
		Default: false,
		// "from", 6 h: confirmed by BUILD.md's "Catalogue copy and
		// defaults" (2026-10-01). From, because CINS lists scanners
		// knocking on exposed ports, so inbound is the whole point; 6 h,
		// because the list changes hourly while each load is a minute of
		// adds on a small router.
		DefaultDirection: DirectionFrom,
		Refresh:          []Refresh{RefreshHourly, RefreshSixHours, RefreshDaily},
		RefreshDefault:   RefreshSixHours,
		Facts:            "**15,000** hosts · 213 KB · changes hourly · false positives **some** · cinsscore.com · free",
		Guide:            "Hosts their sensors saw scanning or attacking, kept to addresses **not already on other lists**. Big — a minute or more to load on a small router — and worth it only if the router exposes SSH, a VPN or a web service; with nothing exposed, the default firewall drops these anyway. MikroView does not flag from it.",
	},
	{
		Key:              "blde",
		Name:             "blocklist.de strongips",
		Short:            "blocklist.de",
		URL:              "https://lists.blocklist.de/lists/strongips.txt",
		Format:           FormatOnePerLine,
		Terms:            `no licence stated; "These files are as they are, and to be used at your own risk"`,
		Caveat:           CaveatNoLicence,
		DefaultDirection: DirectionFrom,
		Refresh:          []Refresh{RefreshHourly, RefreshSixHours, RefreshDaily},
		RefreshDefault:   RefreshSixHours,
		Facts:            "**385** hosts · 5 KB · refreshed every 30 minutes · blocklist.de · no licence stated",
		Guide:            "Hosts that fail2ban on many servers reported in the last 48 hours — the small, high-confidence cut of blocklist.de. **No licence stated**: the site says nothing about who may use it, only that it comes \"at your own risk\", so whether that is enough is your call. MikroView does not flag from it.",
	},
	{
		Key:              "greensnow",
		Name:             "GreenSnow",
		Short:            "GreenSnow",
		URL:              "https://blocklist.greensnow.co/greensnow.txt",
		Format:           FormatOnePerLine,
		Terms:            `no licence stated; "All rights reserved. Reproduction or republication strictly prohibited."`,
		Caveat:           CaveatNoLicence,
		DefaultDirection: DirectionFrom,
		Refresh:          []Refresh{RefreshSixHours, RefreshDaily, RefreshWeekly},
		RefreshDefault:   RefreshDaily,
		Facts:            "**4,935** hosts · 70 KB · greensnow.co · no licence stated",
		Guide:            "Attacking hosts tracked by one company; about half are on blocklist.de too. **No licence stated**: the site says nothing about who may use the list and forbids republishing it — a router fetching it for itself is neither, but nobody has said yes, so that is your call. MikroView does not flag from it.",
	},
	{
		Key:              "dshield",
		Name:             "DShield top 20",
		Short:            "DShield",
		URL:              "https://feeds.dshield.org/block.txt",
		Format:           FormatDShield,
		Terms:            "CC BY-NC-SA 2.5: non-commercial",
		Caveat:           CaveatNotBusiness,
		DefaultDirection: DirectionFrom,
		Refresh:          []Refresh{RefreshSixHours, RefreshDaily, RefreshWeekly},
		RefreshDefault:   RefreshDaily,
		// "false positives likely": whole /24s, and one in the sample was
		// Google Cloud -- the one new list the research supports a rating
		// for (BUILD.md, "Catalogue copy and defaults").
		Facts: "**20** /24 networks · 2 KB · the last three days · false positives **likely** · dshield.org · not for business use",
		Guide: "The twenty /24 networks that attacked DShield's sensors most over the last three days — whole networks, so an attacker's neighbours are dropped with it. **Not for business use**: the licence is non-commercial, fine on a home router and not on one a business runs, and MikroView cannot tell which this is. It does not flag from it.",
	},
	{
		Key:              "bindef",
		Name:             "Binary Defense banlist",
		Short:            "Binary Defense",
		URL:              "https://binarydefense.com/banlist.txt",
		Format:           FormatOnePerLine,
		Terms:            `"may not be used for commercial resale or in products that are charging fees for such services"`,
		Caveat:           CaveatNotBusiness,
		DefaultDirection: DirectionFrom,
		Refresh:          []Refresh{RefreshDaily, RefreshWeekly},
		RefreshDefault:   RefreshDaily,
		Facts:            "**1,514** hosts · 22 KB · binarydefense.com · not for business use",
		Guide:            "Attacking hosts on Binary Defense's own ban list. **Not for business use**: the header allows public use only, not commercial — take it on a home router, leave it off one a business runs; MikroView cannot tell which this is. It does not flag from it.",
	},
}

// leftOut is the "considered and left out" record: one entry per list,
// in the drawn order, with the drawn reason. Ten, now that blocklist.de,
// GreenSnow, DShield and Binary Defense are offered (BUILD.md part 3);
// the drawing's paired rows (Feodo and SSLBL, ThreatFox and URLhaus)
// are one entry per list here, each with its own half of the reason.
var leftOut = []LeftOut{
	{Name: "ET Block-IPs", Why: "98% of it is Spamhaus DROP; the rest is DShield's non-commercial data"},
	{Name: "FireHOL level 1", Why: "holds 10.0.0.0/8 and 192.168.0.0/16 — dropped first in raw, it would drop your LAN"},
	{Name: "abuse.ch Feodo Tracker", Why: "empty since Operation Endgame"},
	{Name: "abuse.ch SSL Blacklist", Why: "deprecated 2025"},
	{Name: "abuse.ch ThreatFox", Why: "needs a personal key, not-for-profit terms; domains, not addresses"},
	{Name: "abuse.ch URLhaus", Why: "needs a personal key, not-for-profit terms; URLs, not addresses"},
	{Name: "ipsum", Why: "re-publishes thirty other lists under a licence it cannot grant; lags a day"},
	{Name: "Team Cymru bogons", Why: "routing hygiene with private space in it, not a known-bad list"},
	{Name: "Tor exit list", Why: "policy, not known-bad — blocking it outbound breaks Tor for your own devices"},
	{Name: "Spamhaus ASN-DROP", Why: "lists networks by number; a router cannot act on those"},
}

// Lists returns the catalogue in the drawn order. A copy: the caller may
// change it without changing the catalogue.
func Lists() []List {
	out := make([]List, len(lists))
	for i, l := range lists {
		l.Refresh = slices.Clone(l.Refresh)
		out[i] = l
	}
	return out
}

// Lookup returns the list with key, and whether there is one.
func Lookup(key string) (List, bool) {
	for _, l := range Lists() {
		if l.Key == key {
			return l, true
		}
	}
	return List{}, false
}

// LeftOutLists returns the considered-and-left-out record, a copy.
func LeftOutLists() []LeftOut {
	return slices.Clone(leftOut)
}

// ListName is the address list a list loads into: mv-bl-<key>.
func ListName(key string) string { return listPrefix + key }

// ListName6 is the IPv6 address list a list with IPv6 loads into:
// mv-bl-<key>6.
func ListName6(key string) string { return listPrefix + key + "6" }

// ListNames returns every address list the builder can write, in
// catalogue order with each IPv6 list after its IPv4 one. It is the
// fixed set the push's address-list-count block counts, so it changes
// only when the catalogue does -- never with what a router has on.
func ListNames() []string {
	var names []string
	for _, l := range lists {
		names = append(names, ListName(l.Key))
		if l.IPv6 {
			names = append(names, ListName6(l.Key))
		}
	}
	return names
}

// RefreshDefaultFor is the refresh the page opens with on a router that
// does or does not have the scheduler's days= (RouterOS 7.24). Without
// it, weekdays cannot be written, and the drawn 7.19.4 card opens on
// daily instead.
func (l List) RefreshDefaultFor(schedulerDays bool) Refresh {
	if l.RefreshDefault == RefreshWeekdays && !schedulerDays {
		return RefreshDaily
	}
	return l.RefreshDefault
}

// RefreshChoicesFor is the refresh choices offered on such a router:
// weekdays only with days=.
func (l List) RefreshChoicesFor(schedulerDays bool) []Refresh {
	var out []Refresh
	for _, r := range l.Refresh {
		if r == RefreshWeekdays && !schedulerDays {
			continue
		}
		out = append(out, r)
	}
	return out
}
