// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
)

// TestUnattributedSourcesAreBounded: Resolve records an entry for any
// unseen syslog source IP, and the syslog listener takes no credentials
// -- anyone who can reach the port can add their own address.
// Unbounded, a flood of sources exhausts memory. Proven before the fix:
// 200,000 addresses produced 200,000 retained entries.
func TestUnattributedSourcesAreBounded(t *testing.T) {
	prev := maxUnattributedSources
	maxUnattributedSources = 100
	t.Cleanup(func() { maxUnattributedSources = prev })

	r := NewRegistry(nil)
	now := time.Now()
	for i := 0; i < 5000; i++ {
		r.Resolve(fmt.Sprintf("10.%d.%d.%d", byte(i>>16), byte(i>>8), byte(i)), now.Add(time.Duration(i)*time.Millisecond))
	}

	if got := len(r.Unattributed()); got > maxUnattributedSources {
		t.Errorf("registry holds %d unattributed sources, want <= %d", got, maxUnattributedSources)
	}
	if got := r.List(); len(got) != 0 {
		t.Errorf("List() = %+v, want no devices: none of those addresses is a router", got)
	}
}

// TestConfiguredDevicesSurviveASpoofFlood is the important half: the
// cap must never evict a router the operator actually declared.
// Otherwise an attacker could push the real devices out of the fleet
// view with a flood of their own -- the attack succeeding by another
// route. Since #1170 declared devices are not in the capped map at all,
// which is what makes that structural rather than careful.
func TestConfiguredDevicesSurviveASpoofFlood(t *testing.T) {
	prev := maxUnattributedSources
	maxUnattributedSources = 10
	t.Cleanup(func() { maxUnattributedSources = prev })

	r := NewRegistry([]config.Device{{ID: "core", Name: "Core router", SourceIP: "192.168.1.1"}})
	now := time.Now()

	// The real router is seen once, early -- making it the
	// least-recently-seen entry by the end of the flood.
	r.Resolve("192.168.1.1", now)

	for i := 0; i < 2000; i++ {
		r.Resolve(fmt.Sprintf("203.0.113.%d", i%256), now.Add(time.Duration(i+1)*time.Second))
	}

	var found bool
	for _, info := range r.List() {
		if info.SourceIP == "192.168.1.1" {
			found = true
			if !info.Configured {
				t.Error("the configured device lost its Configured flag")
			}
		}
	}
	if !found {
		t.Error("the configured router was evicted by a flood of unclaimed sources; configured devices must never be evicted")
	}
}

// TestUnattributedSourcesShedLeavesHeadroomForTheNextSource: pruneLocked
// used to evict back to exactly maxUnattributedSources, which left the
// registry full -- so the very next newly-seen source overflowed
// again and paid the whole walk-and-sort once more, and so did every
// one after that. Resolve runs synchronously on the single ingest
// goroutine, keyed on the source IP off an unauthenticated TLS syslog
// connection, so that is a state a flood can hold the registry in
// indefinitely.
//
// The property that stops it is that a shed leaves headroom. Asserting
// on the headroom rather than on timings keeps this a contract test
// rather than a benchmark that fails on a busy machine. Mirrors
// mac_registry_test.go's TestMACRegistryShedsABatchSoTheNextNewMACIsFree.
// Fails against the pre-#370 code (which lands at exactly the cap, no
// headroom) and passes with the batched evict.DownTo shed. See #370.
func TestUnattributedSourcesShedLeavesHeadroomForTheNextSource(t *testing.T) {
	prev := maxUnattributedSources
	maxUnattributedSources = 800
	t.Cleanup(func() { maxUnattributedSources = prev })

	r := NewRegistry(nil)
	now := time.Now()
	for i := 0; i <= maxUnattributedSources; i++ { // one past the cap, forcing a shed
		r.Resolve(fmt.Sprintf("10.%d.%d.%d", byte(i>>16), byte(i>>8), byte(i)), now.Add(time.Duration(i)*time.Second))
	}

	after := len(r.Unattributed())
	if after >= maxUnattributedSources {
		t.Fatalf("the shed left the registry at %d against a cap of %d -- no headroom, so the next new source sheds again",
			after, maxUnattributedSources)
	}

	// Every insertion up to the headroom must now be free of a shed.
	headroom := maxUnattributedSources - after
	for i := 0; i < headroom; i++ {
		r.Resolve(fmt.Sprintf("172.16.%d.%d", byte(i>>8), byte(i)), now.Add(time.Hour))
	}
	if got := len(r.Unattributed()); got != maxUnattributedSources {
		t.Errorf("filling the %d-entry headroom gave %d entries, want %d -- a shed ran that should not have",
			headroom, got, maxUnattributedSources)
	}
}
