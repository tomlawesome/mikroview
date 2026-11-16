// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"testing"

	"github.com/tomlawesome/mikroview/internal/ipscope"
)

// Assumed API: lookupAddr(scope *ipscope.Scope, ipStr string) -- the ingest
// filter takes the scope as its first argument (nil = today's filter).
func TestLookupAddrSkipsAConfiguredHomeRange(t *testing.T) {
	if _, ok := lookupAddr(nil, "198.51.100.66"); !ok {
		t.Fatal("with no scope, 198.51.100.66 must pass the ingest filter")
	}
	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupAddr(sc, "198.51.100.66"); ok {
		t.Error("with home 198.51.100.0/24, 198.51.100.66 must be filtered out of geolocation")
	}
	if _, ok := lookupAddr(sc, "203.0.113.44"); !ok {
		t.Error("203.0.113.44 must still pass the ingest filter")
	}
}
