// SPDX-License-Identifier: AGPL-3.0-only
import { describe, it, expect } from 'vitest'
import type { ServedPort } from './api'
import { hostBadge, servingTally } from './serving'

function port(o: Partial<ServedPort> = {}): ServedPort {
  return { port: 445, proto: 'tcp', events: 1, ...o }
}

describe('servingTally (#1320)', () => {
  it('reads "N of M hosts answer"', () => {
    expect(servingTally(7, 41)).toBe('7 of 41 hosts answer')
  })

  it('keeps the plural at one host', () => {
    expect(servingTally(1, 1)).toBe('1 of 1 host answer')
  })

  it('is null where the surface knows of no host at all -- not a "0 of 0" claim', () => {
    expect(servingTally(0, 0)).toBeNull()
  })
})

describe('hostBadge (#1320)', () => {
  it('joins several ports under one trailing protocol when they share it', () => {
    expect(hostBadge([port({ port: 445 }), port({ port: 139 }), port({ port: 22 })], 0)).toBe('445, 139, 22/tcp')
  })

  it('appends "+N more" past the badge cap', () => {
    expect(hostBadge([port({ port: 445 })], 3)).toBe('445/tcp · +3 more')
  })

  it('gives each port its own protocol when they differ', () => {
    expect(hostBadge([port({ port: 445, proto: 'tcp' }), port({ port: 53, proto: 'udp' })], 0)).toBe('445/tcp, 53/udp')
  })

  it('reads only the overflow when nothing else made the cap', () => {
    expect(hostBadge([], 2)).toBe('+2 more')
  })

  it('is empty when there is nothing to say at all', () => {
    expect(hostBadge([], 0)).toBe('')
  })
})
