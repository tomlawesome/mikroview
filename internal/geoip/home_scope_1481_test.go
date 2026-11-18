// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
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

// TestManagerSkipsAHomeRangeFromOptionsHome goes through New(Options{Home})
// and the public Country lookup with a real loaded database: an address in
// the home range is not geolocated, an outside one is.
func TestManagerSkipsAHomeRangeFromOptionsHome(t *testing.T) {
	sc, err := ipscope.New([]string{"198.51.100.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		home *ipscope.Scope
		want bool
	}{{"no home", nil, true}, {"home set", sc, false}} {
		t.Run(c.name, func(t *testing.T) {
			fp, srv := newFakeProvider(t)
			m := New(Options{CacheDir: filepath.Join(t.TempDir(), "geoip"), Home: c.home, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
			t.Cleanup(m.Close)
			pointAt(m, srv)
			fp.set("/dbip-2026-09", gz(t, buildDB(t, nested, "NZ")))
			m.RefreshDue(context.Background())
			if m.Source() == "" {
				t.Fatal("no source loaded")
			}
			if _, ok := m.Country("198.51.100.66"); ok != c.want {
				t.Errorf("Country(198.51.100.66) ok = %v, want %v", ok, c.want)
			}
			if code, ok := m.Country("203.0.113.44"); !ok || code != "NZ" {
				t.Errorf("Country(203.0.113.44) = %q, %v; want NZ, true", code, ok)
			}
		})
	}
}
