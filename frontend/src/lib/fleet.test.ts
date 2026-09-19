// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { setupEcho } from './fleet'
import type { Device } from './types'

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
