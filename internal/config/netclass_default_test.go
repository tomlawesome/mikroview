// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"slices"
	"testing"

	"github.com/tomlawesome/mikroview/internal/netclass"
)

// The two default source lists must agree.
//
// internal/config keeps its own literal copy deliberately, so the
// package stays a dependency-free leaf -- but nothing held the copy to
// the original, and they drifted: config's list omitted
// apple_private_relay. main.go wires netclass.New with config's value,
// so netclass.DefaultSources was dead code and a fresh install silently
// shipped without Apple's own ranges. Since x4b_vpn's upstream data
// covers those same ranges, that left ordinary iPhone/iPad/Mac traffic
// classified as a VPN exit -- exactly what having the list on by default
// exists to prevent, and what docs/configuration.md says it does.
//
// An external test (package config) so the import stays out of the
// package proper and the leaf property is unaffected for consumers.
func TestNetClassDefaultMatchesNetclassPackage(t *testing.T) {
	got := defaults().NetClass.Sources
	want := netclass.DefaultSources

	if !slices.Equal(got, want) {
		t.Errorf("config default netClass.sources = %v, netclass.DefaultSources = %v -- these must agree, since main.go wires the former and documents the latter", got, want)
	}
}
