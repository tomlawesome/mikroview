// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import { whisperState } from './whisper.svelte'
import { appState } from './state.svelte'

const MIN = 60_000

beforeEach(() => {
  whisperState.fenceOn = false
  whisperState.fenceFirst = null
  whisperState.fenceRange = null
  whisperState.seekMs = null
  appState.autoscroll = true
})

describe('whisperState.clickMinute -- plain (non-fence) mode', () => {
  it('records the clicked minute and turns autoscroll off (round 22: "click to seek... autoscroll off")', () => {
    whisperState.clickMinute(10 * MIN)
    expect(whisperState.seekMs).toBe(10 * MIN)
    expect(appState.autoscroll).toBe(false)
  })

  it('reseeks on every click without needing a second one', () => {
    whisperState.clickMinute(10 * MIN)
    whisperState.clickMinute(11 * MIN)
    expect(whisperState.seekMs).toBe(11 * MIN)
  })
})

describe('whisperState.toggleFence / clickMinute -- fence mode', () => {
  it('takes two clicks to close a range, order-independent', () => {
    whisperState.toggleFence()
    expect(whisperState.fenceOn).toBe(true)

    whisperState.clickMinute(12 * MIN)
    expect(whisperState.fenceRange).toBeNull()
    expect(whisperState.fenceFirst).toBe(12 * MIN)

    whisperState.clickMinute(10 * MIN)
    expect(whisperState.fenceRange).toEqual({ start: 10 * MIN, end: 12 * MIN + MIN })
    expect(whisperState.fenceFirst).toBeNull()
  })

  it('turning the fence off clears the drawn range rather than leaving it dimmed and un-editable', () => {
    whisperState.toggleFence()
    whisperState.clickMinute(1 * MIN)
    whisperState.clickMinute(2 * MIN)
    expect(whisperState.fenceRange).not.toBeNull()

    whisperState.toggleFence()
    expect(whisperState.fenceOn).toBe(false)
    expect(whisperState.fenceRange).toBeNull()
  })

  it('does not touch Autoscroll on its own -- only a plain seek click does that', () => {
    appState.autoscroll = true
    whisperState.toggleFence()
    whisperState.clickMinute(1 * MIN)
    whisperState.clickMinute(2 * MIN)
    expect(appState.autoscroll).toBe(true)
  })
})

describe('whisperState.dimmed', () => {
  it('is false for everything with no fence set', () => {
    expect(whisperState.dimmed(0)).toBe(false)
    expect(whisperState.dimmed(Date.now())).toBe(false)
  })

  it('is true outside the fence range and false inside it, end-exclusive', () => {
    whisperState.fenceRange = { start: 100, end: 200 }
    expect(whisperState.dimmed(50)).toBe(true)
    expect(whisperState.dimmed(100)).toBe(false)
    expect(whisperState.dimmed(199)).toBe(false)
    expect(whisperState.dimmed(200)).toBe(true)
  })
})
