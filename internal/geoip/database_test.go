// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// The rest of this package's tests never open a database, so the decode
// path -- geoip2.Open, Reader.Country, and reading the ISO code out of
// the record -- was covered by nothing CI runs (#1110). mikroview bundles
// no database by design and geoip2-golang ships no fixture, so this
// builds one with MaxMind's own writer library instead. The reader and
// the writer are both MaxMind's, so what is under test is the format,
// which is the thing that would break under an upgrade -- #1081 being
// the worked example.
//
// The countries are invented and the networks are the RFC 5737 and RFC
// 3849 documentation ranges: no real address space, and nobody's data.
func writeFixtureDB(t *testing.T) string {
	t.Helper()

	w, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoLite2-Country",
		// mmdbwriter classes the documentation ranges as reserved and
		// refuses to insert into one without this, with "attempt to
		// insert 203.0.113.0/24 into 203.0.113.0/24, which is a
		// reserved network". It fails loudly rather than writing an
		// empty database, so the fixture cannot go hollow unnoticed.
		IncludeReservedNetworks: true,
	})
	if err != nil {
		t.Fatalf("mmdbwriter.New: %v", err)
	}

	for _, e := range []struct{ cidr, iso string }{
		{"203.0.113.0/24", "NZ"},
		{"198.51.100.0/24", "JP"},
		{"2001:db8::/32", "IS"},
	} {
		_, network, err := net.ParseCIDR(e.cidr)
		if err != nil {
			t.Fatalf("net.ParseCIDR(%s): %v", e.cidr, err)
		}
		record := mmdbtype.Map{
			"country": mmdbtype.Map{"iso_code": mmdbtype.String(e.iso)},
		}
		if err := w.Insert(network, record); err != nil {
			t.Fatalf("Insert(%s): %v", e.cidr, err)
		}
	}

	path := filepath.Join(t.TempDir(), "fixture-country.mmdb")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("os.Create(%s): %v", path, err)
	}
	defer f.Close()
	if _, err := w.WriteTo(f); err != nil {
		t.Fatalf("WriteTo(%s): %v", path, err)
	}
	return path
}

func TestCountryAgainstRealDatabase(t *testing.T) {
	l, err := Open(writeFixtureDB(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	cases := []struct {
		name   string
		ip     string
		want   string
		wantOK bool
	}{
		{"ipv4 in a mapped network", "203.0.113.5", "NZ", true},
		{"a different mapped network", "198.51.100.1", "JP", true},
		{"ipv6 in a mapped network", "2001:db8::1", "IS", true},
		// The v1 -> v2 migration (#1081) turned this into a real
		// question: net.ParseIP produced one form for both, netip does
		// not, so Country unmaps before looking up. Nothing proved that
		// end to end against a database until now.
		{"ipv4-in-ipv6 resolves as its ipv4 form", "::ffff:203.0.113.5", "NZ", true},
		{"public address the database has no entry for", "192.0.2.7", "", false},
		{"private address is never looked up", "192.168.1.1", "", false},
		{"loopback is never looked up", "127.0.0.1", "", false},
		{"malformed input", "not-an-ip", "", false},
		{"empty input", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, ok := l.Country(c.ip)
			if code != c.want || ok != c.wantOK {
				t.Errorf("Country(%q) = %q, %v; want %q, %v", c.ip, code, ok, c.want, c.wantOK)
			}
		})
	}
}
