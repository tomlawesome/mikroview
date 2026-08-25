// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { buildQuery } from './api'
import { emptyFilters } from './types'

// buildQuery's `ip` forwarding is refetchWithFilters()'s only path back to
// internal/store/query.go's server-side narrowing (see the function's own
// doc comment) -- a regression here silently degrades the "actually
// complete" layer state.svelte.ts describes into "the 500 most recent
// events, unfiltered by address", starving out a selective address that
// only appears further back in the retained buffer than that. Pinned here
// because it slipped through review once already (see #438's PR history)
// without a test that would have caught it immediately.
describe('buildQuery: ip forwarding for srcQuery/dstQuery (#438)', () => {
  it('forwards a bare source IP as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.5' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.5')
  })

  it('forwards a bare destination IP as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), dstQuery: '203.0.113.5' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.5')
  })

  it('forwards a source CIDR as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.0/24' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.0/24')
  })

  it('forwards an IPv6 address as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), dstQuery: '2001:db8::1' })
    expect(new URLSearchParams(qs).get('ip')).toBe('2001:db8::1')
  })

  it('when both boxes hold an address, forwards srcQuery (still a valid superset for either side)', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.5', dstQuery: '198.51.100.9' })
    expect(new URLSearchParams(qs).get('ip')).toBe('203.0.113.5')
  })

  // A pasted address routinely carries leading/trailing whitespace. The
  // forwarded value must be trimmed: internal/store/query.go's
  // net.ParseIP/net.ParseCIDR both fail on padding, which drops
  // matchesFilters to its exact-string-equal fallback -- matching no
  // event at all -- while the client-side matcher (which does trim)
  // leaves the already-buffered rows looking fine. Silently wrong, not
  // visibly broken, which is exactly why this is pinned rather than left
  // to be caught by eye.
  it('trims whitespace from the forwarded ip, not just from the srcQuery param', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '  203.0.113.5  ' })
    const params = new URLSearchParams(qs)
    expect(params.get('ip')).toBe('203.0.113.5')
    expect(params.get('ip')).not.toMatch(/\s/)
  })

  it('trims whitespace from a padded CIDR too', () => {
    const qs = buildQuery({ ...emptyFilters(), dstQuery: '\t198.51.100.0/24\n' })
    expect(new URLSearchParams(qs).get('ip')).toBe('198.51.100.0/24')
  })

  it('does not forward a label/name fragment as ip -- no server-side equivalent exists', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: 'nas-basement' })
    expect(new URLSearchParams(qs).get('ip')).toBeNull()
    expect(new URLSearchParams(qs).get('srcQuery')).toBe('nas-basement')
  })

  it('does not forward a malformed CIDR as ip', () => {
    const qs = buildQuery({ ...emptyFilters(), srcQuery: '203.0.113.5/99' })
    expect(new URLSearchParams(qs).get('ip')).toBeNull()
  })

  it('does not set ip when neither box holds an address', () => {
    const qs = buildQuery({ ...emptyFilters() })
    expect(new URLSearchParams(qs).get('ip')).toBeNull()
  })

  it('only forwards a numeric port, never a text service-name search', () => {
    const numeric = buildQuery({ ...emptyFilters(), port: '443' })
    expect(new URLSearchParams(numeric).get('port')).toBe('443')

    const text = buildQuery({ ...emptyFilters(), port: 'https' })
    expect(new URLSearchParams(text).get('port')).toBeNull()
  })
})
