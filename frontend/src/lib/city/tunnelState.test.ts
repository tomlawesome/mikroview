// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { bridgeStateFor, bridgeStateLabel } from './tunnelState'

describe('city bridges: state from API state and events, never guessed', () => {
  it('is up only when the API says up and something crossed in the window', () => {
    expect(bridgeStateFor('up', 3)).toBe('up')
    expect(bridgeStateFor('up', 1)).toBe('up')
  })

  it('is quiet -- lit but empty -- when up with no events in the window', () => {
    expect(bridgeStateFor('up', 0)).toBe('quiet')
  })

  it('is down when the API says down, regardless of stale events', () => {
    expect(bridgeStateFor('down', 0)).toBe('down')
    expect(bridgeStateFor('down', 5)).toBe('down')
  })

  it('is unknown when the API says unknown (kind never pushed)', () => {
    expect(bridgeStateFor('unknown', 0)).toBe('unknown')
    expect(bridgeStateFor('unknown', 9)).toBe('unknown')
  })

  it('is unknown, never a guessed down, for a tunnel seen only in events', () => {
    // null: no pushed table names this interface at all -- events are
    // the only reason the city knows it exists.
    expect(bridgeStateFor(null, 4)).toBe('unknown')
    expect(bridgeStateFor(null, 0)).toBe('unknown')
  })

  it('labels each state with the ratified wording', () => {
    expect(bridgeStateLabel('up')).toBe('UP')
    expect(bridgeStateLabel('down')).toBe('DOWN')
    expect(bridgeStateLabel('quiet')).toBe('QUIET')
    expect(bridgeStateLabel('unknown')).toBe('state not pushed')
  })
})
