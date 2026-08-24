// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { matchesPortQuery } from './portMatch'

describe('matchesPortQuery', () => {
  it('matches an exact port number on either side', () => {
    expect(matchesPortQuery('443', [{ port: 443 }])).toBe(true)
    expect(matchesPortQuery('443', [{ port: 8443 }])).toBe(false)
  })

  it('an empty query matches everything', () => {
    expect(matchesPortQuery('', [{ port: 443 }])).toBe(true)
    expect(matchesPortQuery('  ', [])).toBe(true)
  })

  it('matches a well-known service name (lib/commonPorts.ts)', () => {
    expect(matchesPortQuery('https', [{ port: 443 }])).toBe(true)
    expect(matchesPortQuery('HTTPS', [{ port: 443 }])).toBe(true)
    expect(matchesPortQuery('ssh', [{ port: 443 }])).toBe(false)
  })

  it('matches the operator-configured label (#413) even where it differs from the well-known name', () => {
    expect(matchesPortQuery('nas-share', [{ port: 445, portName: 'nas-share' }])).toBe(true)
    // The well-known name for 445 (SMB) still matches too -- the box
    // matches every label a port might be shown under, not just the one
    // in use for this particular event.
    expect(matchesPortQuery('smb', [{ port: 445, portName: 'nas-share' }])).toBe(true)
  })

  it('a text query with no port label anywhere matches nothing, rather than being ignored', () => {
    // Deliberately unlike the pre-#438 numeric-only box, where a
    // non-numeric value was silently treated as "no filter" -- see
    // state.svelte.test.ts's updated port coverage for why that changed.
    expect(matchesPortQuery('nonesuch', [{ port: 443 }, { port: 22 }])).toBe(false)
  })

  it('a candidate with no port at all (e.g. an ICMP event) never matches, numeric or text', () => {
    expect(matchesPortQuery('443', [{}])).toBe(false)
    expect(matchesPortQuery('http', [{}])).toBe(false)
  })

  it('any candidate matching is enough (source or destination)', () => {
    expect(matchesPortQuery('443', [{ port: 51512 }, { port: 443 }])).toBe(true)
    expect(matchesPortQuery('https', [{ port: 51512 }, { port: 443 }])).toBe(true)
  })
})
