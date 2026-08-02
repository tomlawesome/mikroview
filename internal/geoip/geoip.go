// Package geoip resolves public IPs to a country code using an optional,
// locally-mounted MaxMind GeoLite2-Country database. There is no bundled
// database and no network calls to MaxMind at runtime -- MaxMind's license
// requires a free account to obtain a database yourself, and this package
// deliberately treats the whole feature as optional: if no database is
// configured, or the configured one can't be opened, every lookup just
// reports "unknown" rather than erroring, so GeoIP is a nice-to-have that
// degrades to "no flags shown" instead of a hard startup dependency.
package geoip

import (
	"net"

	"github.com/oschwald/geoip2-golang"
)

// Lookup wraps an optional MaxMind DB reader. The zero value is a valid,
// permanently-disabled Lookup (every Country call reports ok=false), so
// callers never need to branch on "was GeoIP configured."
type Lookup struct {
	db *geoip2.Reader
}

// Open loads path as a MaxMind GeoLite2/GeoIP2 Country (or City, which is
// a superset) database. An empty path is the expected "not configured"
// case and returns a disabled Lookup with a nil error. A non-empty path
// that fails to open/parse also returns a (disabled) usable Lookup, but
// with the error so the caller can log it -- either way the returned
// Lookup is always safe to use unconditionally.
func Open(path string) (*Lookup, error) {
	if path == "" {
		return &Lookup{}, nil
	}
	db, err := geoip2.Open(path)
	if err != nil {
		return &Lookup{}, err
	}
	return &Lookup{db: db}, nil
}

// Close releases the underlying database file, if one is open.
func (l *Lookup) Close() {
	if l.db != nil {
		l.db.Close()
	}
}

// Country returns the ISO 3166-1 alpha-2 country code for ipStr, and
// whether a code was found at all. It reports ok=false (never an error)
// for: GeoIP not configured, an unparseable IP, a private/loopback/
// link-local address (meaningless to geolocate), or an IP with no match
// in the database.
func (l *Lookup) Country(ipStr string) (code string, ok bool) {
	if l.db == nil || ipStr == "" {
		return "", false
	}
	ip := net.ParseIP(ipStr)
	if ip == nil || !isPublic(ip) {
		return "", false
	}
	rec, err := l.db.Country(ip)
	if err != nil || rec.Country.IsoCode == "" {
		return "", false
	}
	return rec.Country.IsoCode, true
}

func isPublic(ip net.IP) bool {
	return !ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}
