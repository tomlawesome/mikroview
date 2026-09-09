// SPDX-License-Identifier: AGPL-3.0-only

// Package oui turns a hardware address into the organisation IEEE
// assigned its 24-bit prefix to -- the "vendor" half of identifying an
// unknown host (issue #410) -- and reports the one case where asking
// that question is a category error: a locally-administered address,
// which no vendor ever registered.
//
// It rides the same fetch-and-refresh machinery internal/blocklist and
// internal/netclass use: nothing is compiled into the binary, the
// operator's own instance fetches the registry at runtime, a failed or
// not-yet-completed fetch degrades to "no vendor data yet" rather than
// to an error, and a fetch that fails later keeps the last good data
// rather than dropping to empty.
//
// The one thing it adds to that shape is an on-disk cache. The other
// two feeds hold their data in memory only and re-download on every
// restart; this registry is ~4MB and changes by a handful of rows a
// week, so a restart re-reading its own cache -- and then confirming it
// with a conditional GET that normally answers 304 -- is both kinder to
// IEEE and the difference between "vendors are there when the operator
// opens the first dossier" and "vendors appear a minute later".
package oui

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// RefreshInterval is fixed rather than configurable, the same decision
// internal/blocklist.RefreshInterval documents and for the same reason:
// a knob here can only be turned one useful way (down), and thousands of
// self-hosted instances polling a public registry harder than daily is a
// cost IEEE pays for a benefit nobody gets -- assignments are made in
// batches, weeks apart. The daily poll is a conditional GET, so an
// unchanged registry costs a few hundred bytes, not 4MB.
const RefreshInterval = 24 * time.Hour

// StaleAfter is when a successfully-loaded registry stops being
// presented as current. It is not an expiry -- stale vendor data is
// still overwhelmingly right, because an assignment made in 1998 is
// still that company's today -- so the data keeps serving and the
// staleness is *stated* instead, per this issue's "a stale registry is
// stated, not hidden".
const StaleAfter = 30 * 24 * time.Hour

// SourceURL is the IEEE MA-L (MAC Address Block Large) public listing:
// the registry that maps a 24-bit OUI to the organisation holding it.
//
// Terms of use, checked 2026-09-09 before this feed was wired up.
// IEEE publishes the MA-L public listing for direct download at
// https://standards.ieee.org/products-programs/regauth/ ("in order to
// download the entire public listing for a registry, please select
// either the text or CSV file below"), free of charge, without
// registration, and with no stated licence, usage condition or
// restriction attached to the file -- the only condition the download
// page states at all is technical, that the CSV is UTF-8. Nothing in
// those terms restricts an operator downloading it and looking
// addresses up in it, which is the entire use here.
//
// What is *not* granted is redistribution: no licence accompanies the
// file, and IEEE asserts copyright over its published material
// generally, so shipping a copy inside mikroview would be redistributing
// someone else's data with no permission to do so. mikroview therefore
// never bundles it -- the operator's own instance fetches it from IEEE
// directly, into the operator's own data directory. That is the same
// conclusion the house rule against vendoring lookup datasets reaches
// from the other direction, and it also keeps the data as fresh as the
// operator's last refresh rather than as fresh as the release.
//
// Attribution rides along with the data: Status reports this URL and
// the fetch time, so an operator reading a vendor name can see whose
// registry said so and how old the answer is.
const SourceURL = "https://standards-oui.ieee.org/oui/oui.csv"

// ieeeItself is how the MA-L file spells a block IEEE has kept in its
// own name, which it does for the parent OUIs that MA-M and MA-S (the
// medium and small blocks, 28- and 36-bit) are carved out of. A MAC
// from one of those blocks matches the parent row and would otherwise
// be reported as made by "IEEE Registration Authority", which is
// nonsense as a device vendor. There are a few hundred such rows, and
// the real assignee is in the MA-M/MA-S files this feed does not fetch,
// so the honest answer is "this prefix is sub-delegated and the vendor
// is not in this registry" -- named as its own state rather than
// printed as a vendor.
const ieeeItself = "IEEE Registration Authority"

// privateListing is how the file spells an assignment whose holder
// exercised IEEE's private-listing option: the block is registered, but
// the assignee's identity is deliberately withheld. That is a different
// fact from "not in the registry" and the dossier says so.
const privateListing = "Private"

// Vendor is the answer to "who registered this prefix". Known is false
// for every way of not having an answer, each of which carries its own
// Reason in words, because "we have no registry yet", "this address has
// no vendor by construction" and "the registry withholds this one" are
// three different next steps for an operator, not one blank field.
type Vendor struct {
	Known bool `json:"known"`
	// Name is the organisation IEEE lists, verbatim -- "Espressif
	// Inc.", "Raspberry Pi Trading Ltd". Empty unless Known.
	Name string `json:"name,omitempty"`
	// OUI is the prefix that was looked up, uppercase hex.
	OUI string `json:"oui,omitempty"`
	// Registry is the IEEE registry the row came from: "MA-L".
	Registry string `json:"registry,omitempty"`
	// Private reports the withheld-assignee case above.
	Private bool `json:"private,omitempty"`
	// SubDelegated reports the MA-M/MA-S parent case above.
	SubDelegated bool `json:"subDelegated,omitempty"`
	// Reason says, in plain words, why Known is false. Empty when it is
	// true.
	Reason string `json:"reason,omitempty"`
}

// Status is what the registry can say about itself: where its data came
// from, when, and how much of it there is. The dossier prints it beside
// any vendor answer so a name is never shown without its provenance.
type Status struct {
	Source string `json:"source"`
	// Loaded is false until the first successful fetch or cache read.
	Loaded    bool      `json:"loaded"`
	Entries   int       `json:"entries"`
	FetchedAt time.Time `json:"fetchedAt,omitempty"`
	// FromCache reports that the currently-served data came off disk
	// and has not yet been confirmed against IEEE in this process.
	FromCache bool `json:"fromCache,omitempty"`
	// Stale is FetchedAt older than StaleAfter. Stated, not hidden, and
	// not a reason to stop serving.
	Stale bool `json:"stale,omitempty"`
	// Note is the human sentence for the current state, including the
	// "no vendor data yet" one.
	Note string `json:"note,omitempty"`
}

// Registry holds the current OUI table and serves Lookup. Safe for
// concurrent use: Lookup takes a read lock, Refresh builds a whole new
// table and swaps it under the write lock, so a lookup never sees a
// half-loaded registry.
type Registry struct {
	mu        sync.RWMutex
	entries   map[string]string // OUI -> organisation, verbatim
	fetchedAt time.Time
	fromCache bool
	etag      string

	url       string
	cachePath string
	client    *fetchClient
	log       *slog.Logger
}

// New builds a Registry for the configured source URL and cache path.
// No network access happens here: the cache is read (so a restart
// serves vendors immediately), and Lookup reports "no vendor data yet"
// until either that read or the first Refresh succeeds.
func New(url, cachePath string, log *slog.Logger) *Registry {
	if url == "" {
		url = SourceURL
	}
	r := &Registry{
		url:       url,
		cachePath: cachePath,
		client:    newFetchClient(),
		log:       log,
	}
	r.loadCache()
	return r
}

// Enabled reports whether this registry has somewhere to fetch from --
// main.go uses it to decide whether to start a refresh ticker at all,
// the same way blocklist.HasFeeds and netclass.HasSources are used.
func (r *Registry) Enabled() bool { return r != nil && r.url != "" }

// Lookup returns what the registry knows about m's prefix.
//
// A locally-administered address is answered without consulting the
// table at all: there is no assignment to find, and saying so is the
// useful answer rather than a failed lookup. A miss with no data
// loaded, and a miss against a loaded registry, are likewise different
// answers with different Reasons -- the first says come back later, the
// second says this prefix genuinely is not assigned.
func (r *Registry) Lookup(m MAC) Vendor {
	if m.Address == "" {
		return Vendor{Reason: "no MAC address to look up"}
	}
	if m.LocallyAdministered {
		return Vendor{
			OUI:    m.OUI,
			Reason: "locally administered address -- no vendor exists to look up",
		}
	}
	if r == nil {
		return Vendor{OUI: m.OUI, Reason: "no vendor data yet -- the OUI registry is not configured"}
	}

	r.mu.RLock()
	name, ok := r.entries[m.OUI]
	loaded := len(r.entries) > 0
	r.mu.RUnlock()

	switch {
	case !loaded:
		return Vendor{OUI: m.OUI, Reason: "no vendor data yet -- the OUI registry has not been fetched"}
	case !ok:
		return Vendor{OUI: m.OUI, Reason: "prefix is not in the IEEE MA-L registry"}
	case name == ieeeItself:
		return Vendor{
			OUI:          m.OUI,
			Registry:     "MA-L",
			SubDelegated: true,
			Reason:       "prefix is sub-delegated by IEEE (an MA-M or MA-S block); the vendor is not in the MA-L registry",
		}
	case name == privateListing:
		return Vendor{
			OUI:      m.OUI,
			Registry: "MA-L",
			Private:  true,
			Reason:   "the assignee chose a private listing, so IEEE withholds the name",
		}
	}
	return Vendor{
		Known:    true,
		Name:     name,
		OUI:      m.OUI,
		Registry: "MA-L",
	}
}

// LookupMAC is Lookup for callers holding a raw address string. An
// unparseable address is reported as exactly that rather than as an
// absent vendor.
func (r *Registry) LookupMAC(mac string) (MAC, Vendor) {
	m, ok := Parse(mac)
	if !ok {
		return MAC{}, Vendor{Reason: fmt.Sprintf("%q is not a hardware address this can read", mac)}
	}
	return m, r.Lookup(m)
}

// Status describes the currently-served data. It is safe to call before
// anything has been loaded, which is the state it exists to describe.
func (r *Registry) Status() Status {
	if r == nil || r.url == "" {
		return Status{Note: "the OUI registry feed is switched off, so no vendor names are available"}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	st := Status{
		Source:    r.url,
		Entries:   len(r.entries),
		FetchedAt: r.fetchedAt,
		FromCache: r.fromCache,
	}
	if len(r.entries) == 0 {
		st.Note = "no vendor data yet -- the OUI registry has not been fetched; vendor names appear after the first refresh"
		return st
	}
	st.Loaded = true
	age := time.Since(r.fetchedAt)
	st.Stale = age > StaleAfter
	switch {
	case st.Stale:
		st.Note = fmt.Sprintf("%d prefixes, last fetched %s ago -- older than %s, so a recently-registered vendor may be missing",
			st.Entries, age.Round(time.Hour), StaleAfter)
	case r.fromCache:
		st.Note = fmt.Sprintf("%d prefixes, read from the local cache (fetched %s ago) and not yet re-checked against IEEE",
			st.Entries, age.Round(time.Minute))
	default:
		st.Note = fmt.Sprintf("%d prefixes, fetched from IEEE %s ago", st.Entries, age.Round(time.Minute))
	}
	return st
}

// entryCount reports how many prefixes are currently served, for tests
// and log lines.
func (r *Registry) entryCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// describe is the one-line summary the refresh path logs.
func describe(entries int, from string) string {
	return fmt.Sprintf("IEEE MA-L registry: %s, %d prefixes", from, entries)
}

// looksLikeCSV is a cheap sanity check on a fetched body before it is
// allowed to replace a working table: the MA-L file always starts with
// its header row, so a captive portal's login page or a provider error
// page is rejected as a parse failure rather than parsed into an empty
// registry.
func looksLikeCSV(body []byte) bool {
	head := body
	if len(head) > 256 {
		head = head[:256]
	}
	return strings.HasPrefix(strings.TrimLeft(string(head), "\ufeff \t\r\n"), "Registry,")
}
