// SPDX-License-Identifier: AGPL-3.0-only

package hosts

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/ipscope"
)

// Assumed API: SetHomeScope(*ipscope.Scope), package state like the
// engine's. The register mirrors the browser's rule, which honours
// networks.home (#1481), so a home source must count as a host here too.
func TestRegistersTreatsAConfiguredHomeRangeAsAHost(t *testing.T) {
	t.Cleanup(func() { SetHomeScope(nil) })
	if Registers("vlan10", "198.51.100.66") {
		t.Fatal("with no home scope, 198.51.100.66 is public and must not register")
	}
	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	SetHomeScope(sc)
	if !Registers("vlan10", "198.51.100.66") {
		t.Error("with home 198.51.100.0/24, 198.51.100.66 must register as a host")
	}
	if Registers("ether1", "203.0.113.44") {
		t.Error("203.0.113.44 must stay public with the home scope set")
	}
}
