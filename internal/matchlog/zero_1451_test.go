// SPDX-License-Identifier: AGPL-3.0-only

package matchlog

import "testing"

// #1451: Key is the bucket key engine's inverted dispatch indexes by, so
// two identities that match each other must share it.
func TestIdentityKeyPrefersMACAndIgnoresCase(t *testing.T) {
	a := Identity{MAC: "AA:BB:CC:00:11:22", IP: "192.0.2.1"}
	b := Identity{MAC: "aa:bb:cc:00:11:22", IP: "192.0.2.99"} // same device, new lease
	if a.Key() != b.Key() {
		t.Errorf("same MAC, different case/IP: %q != %q", a.Key(), b.Key())
	}
	if a.Key() != "mac:aa:bb:cc:00:11:22" {
		t.Errorf("Key() = %q", a.Key())
	}
	ip := Identity{IP: "192.0.2.1"}
	if ip.Key() != "ip:192.0.2.1" {
		t.Errorf("IP-only Key() = %q", ip.Key())
	}
	if ip.Key() == (Identity{IP: "192.0.2.2"}).Key() {
		t.Error("different IPs share a key")
	}
	if !a.MatchesSource(b) || a.Key() != b.Key() {
		t.Error("identities that MatchesSource each other must share a Key")
	}
}
