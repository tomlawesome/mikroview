// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { RECENT_WINDOW_MS, recentCount, setupEcho } from './fleet'
import type { ClientEvent, Device } from './types'

function device(setup?: Device['setup']): Device {
  return {
    id: 'core',
    name: 'Core',
    sourceIp: '192.168.1.1',
    configured: true,
    firstSeen: '2026-09-15T10:00:00Z',
    lastSeen: '2026-09-15T10:05:00Z',
    eventCount: 12,
    status: 'live',
    setup,
  }
}

// #1241: the router reports the logging setup the wizard left on it, and
// the card says one thing about it -- the fuller upgrade notice is
// #1240's.
describe('setupEcho', () => {
  it('names the remedy while a router is behind', () => {
    expect(setupEcho(device({ standing: 'behind', scriptVersion: 4, currentVersion: 5 }))).toBe(
      'setup behind · paste Trust the certificate again',
    )
  })

  it('says so when a router has never reported its setup', () => {
    expect(setupEcho(device({ standing: 'never reported', scriptVersion: 0, currentVersion: 5 }))).toBe(
      'setup never reported',
    )
  })

  it('says nothing at all when the setup is current', () => {
    expect(setupEcho(device({ standing: 'current', scriptVersion: 5, currentVersion: 5 }))).toBeNull()
  })

  it('says nothing when the server served no setup answer', () => {
    expect(setupEcho(device())).toBeNull()
  })
})

function clientEvt(overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: 1,
    time: '2026-01-01T00:00:00Z',
    deviceId: 'core',
    sourceIp: '10.0.0.1',
    action: 'accept',
    ruleLabel: 'lan-wan',
    chain: 'forward',
    raw: 'A|lan-wan|forward: ...',
    receivedAt: 0,
    ...overrides,
  }
}

describe('recentCount', () => {
  it('counts only the named device\'s events inside the recent window', () => {
    const now = 1_000_000
    const events = [
      clientEvt({ deviceId: 'core', receivedAt: now - 1_000 }),
      clientEvt({ deviceId: 'edge', receivedAt: now - 1_000 }),
      // Outside RECENT_WINDOW_MS -- must not count.
      clientEvt({ deviceId: 'core', receivedAt: now - RECENT_WINDOW_MS - 1_000 }),
    ]
    expect(recentCount(events, 'core', now)).toBe(1)
    expect(recentCount(events, 'edge', now)).toBe(1)
    expect(recentCount(events, 'never-seen', now)).toBe(0)
  })

  // #1304 E3: each router card called recentCount once per device, and
  // every call rescanned the whole buffer -- N cards meant N full scans
  // of the same buffer on the same tick. Fixed by counting every device
  // in one pass the first time a given (events, nowMs) pair is asked
  // about, and reusing that for the rest of that tick's cards. Proven by
  // instrumenting reads of `deviceId` (the field the scan tests) rather
  // than timing, so this cannot flake on a slow runner.
  it('scans the buffer once per (events, nowMs) pair, not once per device asked about', () => {
    const now = 2_000_000
    let reads = 0
    const events: ClientEvent[] = Array.from({ length: 500 }, (_, i) => {
      const base = clientEvt({ deviceId: i % 3 === 0 ? 'core' : 'edge', receivedAt: now - 1_000 })
      return new Proxy(base, {
        get(target, prop, receiver) {
          if (prop === 'deviceId') reads++
          return Reflect.get(target, prop, receiver)
        },
      })
    })

    recentCount(events, 'core', now) // the first card's call -- the one real pass
    reads = 0
    // Three more cards' worth of calls against the identical buffer and
    // clock reading. A rescan-per-call implementation would read
    // deviceId another 500 times for each of these.
    recentCount(events, 'edge', now)
    recentCount(events, 'core', now)
    recentCount(events, 'never-seen', now)

    expect(reads).toBe(0)
  })
})
