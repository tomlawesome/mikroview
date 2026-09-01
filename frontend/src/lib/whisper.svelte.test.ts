// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import { whisperState } from './whisper.svelte'
import { appState } from './state.svelte'

const MIN = 60_000

beforeEach(() => {
  whisperState.fenceRange = null
  whisperState.seekMs = null
  appState.autoscroll = true
})

describe('whisperState.seek', () => {
  it('records the clicked minute and turns autoscroll off (round 22: "click to seek... autoscroll off")', () => {
    whisperState.seek(10 * MIN)
    expect(whisperState.seekMs).toBe(10 * MIN)
    expect(appState.autoscroll).toBe(false)
  })

  it('reseeks on every call without needing a second one', () => {
    whisperState.seek(10 * MIN)
    whisperState.seek(11 * MIN)
    expect(whisperState.seekMs).toBe(11 * MIN)
  })

  it('clears a drawn fence -- #717\'s whole "clearing" story is a click that also seeks', () => {
    whisperState.setFenceRange(1 * MIN, 2 * MIN)
    expect(whisperState.fenceRange).not.toBeNull()

    whisperState.seek(5 * MIN)
    expect(whisperState.fenceRange).toBeNull()
  })
})

describe('whisperState.setFenceRange', () => {
  it('closes a range, order-independent', () => {
    whisperState.setFenceRange(12 * MIN, 10 * MIN)
    expect(whisperState.fenceRange).toEqual({ start: 10 * MIN, end: 12 * MIN })
  })

  it('does not touch Autoscroll on its own -- only a plain seek does that', () => {
    appState.autoscroll = true
    whisperState.setFenceRange(1 * MIN, 2 * MIN)
    expect(appState.autoscroll).toBe(true)
  })
})

describe('whisperState.clearFence', () => {
  it('drops the range rather than leaving it dimmed and un-editable', () => {
    whisperState.setFenceRange(1 * MIN, 2 * MIN)
    expect(whisperState.fenceRange).not.toBeNull()

    whisperState.clearFence()
    expect(whisperState.fenceRange).toBeNull()
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
