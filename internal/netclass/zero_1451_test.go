// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"reflect"
	"testing"
	"time"
)

// #1451: paths no other test reached.

func TestReadyOnceASourceHasBeenFetched(t *testing.T) {
	c := New(nil, testLog())
	if c.Ready() {
		t.Error("Ready() before any fetch")
	}
	c.mu.Lock()
	c.fetchedAt[SourceTor] = time.Now()
	c.mu.Unlock()
	if !c.Ready() {
		t.Error("Ready() = false after a source was fetched")
	}
}

func TestEnabledSourcesAreSortedAndACopy(t *testing.T) {
	c := New(nil, testLog())
	c.order = []Source{SourceX4BVPN, SourceTor}
	got := c.EnabledSources()
	if !reflect.DeepEqual(got, []Source{SourceTor, SourceX4BVPN}) {
		t.Errorf("EnabledSources() = %v, want sorted [tor x4b_vpn]", got)
	}
	got[0] = "mutated"
	if c.order[0] != SourceX4BVPN {
		t.Error("EnabledSources() handed out the classifier's own slice")
	}
}

func TestKnownSourcesListsTheWholeRegistryInOrder(t *testing.T) {
	got := KnownSources()
	if len(got) != len(feedRegistry) || len(got) == 0 {
		t.Fatalf("KnownSources() has %d entries, registry has %d", len(got), len(feedRegistry))
	}
	seen := map[Source]bool{}
	for i, s := range got {
		if s != feedRegistry[i].Source || seen[s] {
			t.Errorf("entry %d = %q (duplicate=%v)", i, s, seen[s])
		}
		seen[s] = true
	}
	for _, d := range DefaultSources {
		if !seen[Source(d)] {
			t.Errorf("default source %q is not in the known list", d)
		}
	}
}

func TestParseX4BReadsPlainCIDRsAndSkipsJunkAndReservedRanges(t *testing.T) {
	body := []byte("# comment\n203.0.114.0/24\nnot a cidr\n10.0.0.0/24\n203.0.114.128/25\n\n")
	got := parseX4B(body)
	if len(got) != 2 {
		t.Fatalf("parseX4B gave %d prefixes, want 2: %v", len(got), got)
	}
	if got[0].prefix.String() != "203.0.114.0/24" || got[1].prefix.String() != "203.0.114.128/25" {
		t.Errorf("prefixes = %v, %v", got[0].prefix, got[1].prefix)
	}
	if len(parseX4B(nil)) != 0 {
		t.Error("an empty body gave prefixes")
	}
}
