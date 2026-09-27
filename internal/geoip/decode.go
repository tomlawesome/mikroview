// SPDX-License-Identifier: AGPL-3.0-only

package geoip

import (
	"strconv"
	"strings"

	"github.com/oschwald/maxminddb-golang/v2"
)

// The three sources use two record layouts:
//
//   - MaxMind GeoLite2 and DB-IP Lite: nested, {"country": {"iso_code":
//     "GB", ...}, ...} -- DB-IP publishes its Lite MMDB in MaxMind's
//     layout so existing readers work unchanged (schema and sample
//     record at https://db-ip.com/db/format/ip-to-country-lite/mmdb.html,
//     read 2026-09-27; not checked against a downloaded file).
//   - IPinfo Lite: flat, {"country_code": "GB", "country": "United
//     Kingdom", "asn": "AS13335", "as_name": "Cloudflare, Inc.", ...}.
//     Note "country" is a *string* here, not a map. Field list and
//     sample record from https://ipinfo.io/lite, read 2026-09-27.
//
// Decoded by path with plain maxminddb rather than into one struct: a
// struct with a nested Country field fails to decode IPinfo's string
// "country" at all, and a per-source struct would make a source that
// changed layout read as "no country" for every address. Trying the flat
// path first and the nested one second tolerates either layout from any
// source, at the cost of one extra path walk on a nested-layout miss.

// countryFrom reads the ISO country code from a lookup result in either
// layout. "" when the address is not in the file or the record has none.
func countryFrom(res maxminddb.Result) string {
	if !res.Found() {
		return ""
	}
	var code string
	if err := res.DecodePath(&code, "country_code"); err == nil && code != "" {
		return strings.ToUpper(code)
	}
	code = ""
	if err := res.DecodePath(&code, "country", "iso_code"); err == nil && code != "" {
		return strings.ToUpper(code)
	}
	return ""
}

// ownerFrom reads the network owner from an IPinfo Lite record. IPinfo
// writes the ASN as a string with its "AS" prefix -- its published
// sample record reads "asn": "AS13335" -- and a bare number is accepted
// too, and so is MaxMind's GeoLite2-ASN spelling
// (autonomous_system_number / autonomous_system_organization), so a
// layout change reads as the same answer rather than as nothing.
func ownerFrom(res maxminddb.Result) (uint, string) {
	if !res.Found() {
		return 0, ""
	}
	var raw any
	if err := res.DecodePath(&raw, "asn"); err != nil || raw == nil {
		raw = nil
		_ = res.DecodePath(&raw, "autonomous_system_number")
	}
	asn := parseASN(raw)
	if asn == 0 {
		return 0, ""
	}
	var name string
	if err := res.DecodePath(&name, "as_name"); err != nil || name == "" {
		name = ""
		_ = res.DecodePath(&name, "autonomous_system_organization")
	}
	return asn, name
}

// parseASN turns whatever the record held into an AS number, 0 when it
// is not one.
func parseASN(v any) uint {
	switch n := v.(type) {
	case string:
		s := strings.TrimSpace(n)
		if len(s) > 2 && strings.EqualFold(s[:2], "AS") {
			s = s[2:]
		}
		u, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return 0
		}
		return uint(u)
	case uint64:
		if n > 1<<32-1 {
			return 0
		}
		return uint(n)
	case uint32:
		return uint(n)
	case uint16:
		return uint(n)
	case int:
		if n <= 0 || n > 1<<32-1 {
			return 0
		}
		return uint(n)
	}
	return 0
}
