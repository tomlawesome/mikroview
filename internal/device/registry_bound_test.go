// SPDX-License-Identifier: AGPL-3.0-only

package device

import (
	"fmt"
	"testing"
	"time"

	"github.com/tomlawesome/mikroview/internal/config"
)

// TestDiscoveredDevicesAreBounded: Resolve mints an entry for any
// unseen syslog source IP, and over UDP that address is trivially
// spoofable -- no connection, no handshake, no credentials. Unbounded,
// a flood of forged sources exhausts memory. Proven before the fix:
// 200,000 spoofed IPs produced 200,000 retained entries.
func TestDiscoveredDevicesAreBounded(t *testing.T) {
	prev := maxDiscoveredDevices
	maxDiscoveredDevices = 100
	t.Cleanup(func() { maxDiscoveredDevices = prev })

	r := NewRegistry(nil)
	now := time.Now()
	for i := 0; i < 5000; i++ {
		r.Resolve(fmt.Sprintf("10.%d.%d.%d", byte(i>>16), byte(i>>8), byte(i)), now.Add(time.Duration(i)*time.Millisecond))
	}

	if got := len(r.List()); got > maxDiscoveredDevices {
		t.Errorf("registry holds %d discovered devices, want <= %d", got, maxDiscoveredDevices)
	}
}

// TestConfiguredDevicesSurviveASpoofFlood is the important half: the
// cap must never evict a router the operator actually declared.
// Otherwise an attacker could push the real devices out of the fleet
// view with forged packets -- the attack succeeding by another route.
func TestConfiguredDevicesSurviveASpoofFlood(t *testing.T) {
	prev := maxDiscoveredDevices
	maxDiscoveredDevices = 10
	t.Cleanup(func() { maxDiscoveredDevices = prev })

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
		t.Error("the configured router was evicted by a flood of spoofed sources; configured devices must never be evicted")
	}
}
