// SPDX-License-Identifier: AGPL-3.0-only

package watchlist

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/ingest"
	"github.com/tomlawesome/mikroview/internal/matchlog"
)

func logs(r ingest.FilterRule) ingest.FilterRule {
	r.Log = true
	return r
}

func device(rules ...ingest.FilterRule) map[string][]ingest.FilterRule {
	return map[string][]ingest.FilterRule{"router-a": rules}
}

// The default is silence. A deployment that never set up the optional
// router push must not be told its entries are broken.
func TestCoverageSaysNothingWithoutPushedRules(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}}
	if got := Coverage(entry, nil); got != CoverageUnknown {
		t.Errorf("with no pushed tables = %v, want %v", got, CoverageUnknown)
	}
	if got := Coverage(entry, map[string][]ingest.FilterRule{}); got != CoverageUnknown {
		t.Errorf("with an empty map = %v, want %v", got, CoverageUnknown)
	}
	// A router with no filter rules at all is a real state that says
	// nothing about intent.
	if got := Coverage(entry, device()); got != CoverageUnknown {
		t.Errorf("with an empty table = %v, want %v", got, CoverageUnknown)
	}
}

// The cheapest definite answer, and the most useful: rules exist and not
// one of them logs, so nothing can feed the watchlist or the live view.
func TestCoverageDetectsNoRuleLogsAtAll(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}}
	rules := device(
		ingest.FilterRule{Chain: "forward", Action: "drop"},
		ingest.FilterRule{Chain: "input", Action: "accept"},
	)
	if got := Coverage(entry, rules); got != CoverageNoLogging {
		t.Errorf("= %v, want %v", got, CoverageNoLogging)
	}
}

// A rule with logging on but no log-prefix still produces events (as
// action "unknown"), which is half of why guessing from the prefix was
// rejected.
func TestCoverageCountsALoggingRuleWithNoPrefix(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}}
	rules := device(logs(ingest.FilterRule{Chain: "forward", Action: "drop"}))
	if got := Coverage(entry, rules); got != CoverageOK {
		t.Errorf("= %v, want %v -- a logging rule with no prefix still feeds events", got, CoverageOK)
	}
}

func TestCoverageOnPorts(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22, 2222}}

	covered := device(logs(ingest.FilterRule{DstPort: "20-30"}))
	if got := Coverage(entry, covered); got != CoverageOK {
		t.Errorf("port in range = %v, want %v", got, CoverageOK)
	}

	// Every logging rule scopes to ports this entry does not watch.
	out := device(
		logs(ingest.FilterRule{DstPort: "80"}),
		logs(ingest.FilterRule{DstPort: "443,8443"}),
	)
	if got := Coverage(entry, out); got != CoverageOutOfScope {
		t.Errorf("no rule covers either port = %v, want %v", got, CoverageOutOfScope)
	}

	// One rule with no port condition matches every port, so the entry
	// is covered regardless of the others.
	mixed := device(
		logs(ingest.FilterRule{DstPort: "80"}),
		logs(ingest.FilterRule{}),
	)
	if got := Coverage(entry, mixed); got != CoverageOK {
		t.Errorf("an unscoped rule = %v, want %v", got, CoverageOK)
	}
}

func TestCoverageOnDestinationAddress(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}, DestIP: "10.1.2.3"}

	if got := Coverage(entry, device(logs(ingest.FilterRule{DstAddress: "10.0.0.0/8"}))); got != CoverageOK {
		t.Errorf("inside the rule's prefix = %v, want %v", got, CoverageOK)
	}
	if got := Coverage(entry, device(logs(ingest.FilterRule{DstAddress: "192.168.0.0/16"}))); got != CoverageOutOfScope {
		t.Errorf("outside every rule = %v, want %v", got, CoverageOutOfScope)
	}
	// Negation, which a naive containment check answers backwards.
	if got := Coverage(entry, device(logs(ingest.FilterRule{DstAddress: "!10.0.0.0/8"}))); got != CoverageOutOfScope {
		t.Errorf("negated prefix containing the address = %v, want %v", got, CoverageOutOfScope)
	}
	if got := Coverage(entry, device(logs(ingest.FilterRule{DstAddress: "!192.168.0.0/16"}))); got != CoverageOK {
		t.Errorf("negated prefix not containing the address = %v, want %v", got, CoverageOK)
	}
}

// One router logging the right traffic is enough. Entries are not scoped
// to a device, so five routers that cannot feed an entry do not make it
// uncovered.
func TestCoverageIsSatisfiedByAnyDevice(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}}
	rules := map[string][]ingest.FilterRule{
		"router-a": {logs(ingest.FilterRule{DstPort: "80"})},
		"router-b": {logs(ingest.FilterRule{DstPort: "22"})},
		"router-c": {ingest.FilterRule{DstPort: "22"}}, // not logging
	}
	if got := Coverage(entry, rules); got != CoverageOK {
		t.Errorf("= %v, want %v", got, CoverageOK)
	}
}

// Anything unreadable makes the answer Unknown rather than a confident
// negative -- the unreadable rule might have been the covering one.
func TestCoverageStaysSilentOnAnythingItCannotRead(t *testing.T) {
	entry := Entry{ID: "e", Ports: []int{22}, DestIP: "10.1.2.3"}

	// A dst-address naming an address list rather than an address.
	unreadable := device(
		logs(ingest.FilterRule{DstPort: "80"}),
		logs(ingest.FilterRule{DstAddress: "mgmt"}),
	)
	if got := Coverage(entry, unreadable); got != CoverageUnknown {
		t.Errorf("unreadable dst-address = %v, want %v", got, CoverageUnknown)
	}

	// A rule scoping by src-address-list, whose contents are not pushed.
	bySource := Entry{ID: "e", Ports: []int{22}, Source: matchlog.Identity{IP: "192.168.1.50"}}
	listScoped := device(logs(ingest.FilterRule{SrcAddressList: "mgmt"}))
	if got := Coverage(bySource, listScoped); got != CoverageUnknown {
		t.Errorf("src-address-list = %v, want %v", got, CoverageUnknown)
	}
}

// A rule carries no MAC condition, so a MAC-scoped entry cannot be
// narrowed by address -- any logging rule might be carrying that
// device's traffic.
func TestCoverageCannotNarrowAMACScopedEntry(t *testing.T) {
	entry := Entry{
		ID:     "e",
		Ports:  []int{22},
		Source: matchlog.Identity{MAC: "52:55:0A:00:02:02"},
	}
	rules := device(logs(ingest.FilterRule{SrcAddress: "192.168.99.0/24"}))
	if got := Coverage(entry, rules); got != CoverageOK {
		t.Errorf("= %v, want %v -- rules carry no MAC condition to narrow by", got, CoverageOK)
	}
}

// An inverted entry watches every port its device touches, so port
// scoping must not be applied to it.
func TestCoverageIgnoresPortsForAnInvertedEntry(t *testing.T) {
	entry := Entry{
		ID:     "e",
		Invert: true,
		Source: matchlog.Identity{IP: "192.168.1.50"},
	}
	rules := device(logs(ingest.FilterRule{DstPort: "9999", SrcAddress: "192.168.1.0/24"}))
	if got := Coverage(entry, rules); got != CoverageOK {
		t.Errorf("= %v, want %v -- an inverted entry is not port-scoped", got, CoverageOK)
	}

	out := device(logs(ingest.FilterRule{SrcAddress: "10.0.0.0/8"}))
	if got := Coverage(entry, out); got != CoverageOutOfScope {
		t.Errorf("source outside every rule = %v, want %v", got, CoverageOutOfScope)
	}
}
