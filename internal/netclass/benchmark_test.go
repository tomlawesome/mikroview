// SPDX-License-Identifier: AGPL-3.0-only

package netclass

import (
	"fmt"
	"math/rand"
	"net/netip"
	"testing"
)

// BenchmarkLookup records the per-lookup cost of the netipx-backed class
// order (#1313), so a later change to buildSets/Lookup/lookupDetail
// cannot make it slow unnoticed. #1313's own measurement, on 100k random
// prefixes spread over six classes, was ~0.12us/lookup for the removed
// bart trie against ~1.6us/lookup for one netipx.IPSet per class checked
// in ClassOrder -- this benchmark is that same shape, so its number is
// directly comparable to both.
func BenchmarkLookup(b *testing.B) {
	const totalPrefixes = 100_000
	prefixesPerSource := totalPrefixes / len(ClassOrder)

	rng := rand.New(rand.NewSource(1))
	bySrc := make(map[Source][]classifiedPrefix, len(ClassOrder))
	for _, src := range ClassOrder {
		ps := make([]classifiedPrefix, prefixesPerSource)
		for i := range ps {
			addr := netip.AddrFrom4([4]byte{
				byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 0,
			})
			bits := 16 + rng.Intn(16) // /16 .. /31, a realistic feed spread
			ps[i] = classifiedPrefix{
				prefix: netip.PrefixFrom(addr, bits).Masked(),
				detail: fmt.Sprintf("region-%d", i%20),
			}
		}
		bySrc[src] = ps
	}

	c := New(nil, testLog())
	for _, src := range ClassOrder {
		c.sources[src] = registryBySource[src]
	}
	c.order = append([]Source{}, ClassOrder...)
	c.sets, c.details, c.detailStrings = buildSets(bySrc)

	// Random addresses, mostly misses -- the same shape as a live syslog
	// stream, where most source addresses match no feed at all.
	addrs := make([]string, 4096)
	for i := range addrs {
		addrs[i] = netip.AddrFrom4([4]byte{
			byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)),
		}).String()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Lookup(addrs[i%len(addrs)])
	}
}
