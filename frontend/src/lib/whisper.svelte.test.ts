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

  // Reversed by round 36, deliberately: "a seek or a fence turns it
  // off". A fence is a window on the past, so lines arriving now would
  // scroll the reader away from the range they have just drawn. Before
  // this, fencing left the stream following and the fenced rows walked
  // off the top of the screen while the reader watched.
  it('stops the stream following, as a seek does -- a fence is a window on the past', () => {
    appState.autoscroll = true
    whisperState.setFenceRange(1 * MIN, 2 * MIN)
    expect(appState.autoscroll).toBe(false)
  })
})

// #749: seeking set autoscroll false and nothing in the app ever set it
// back, so the stream stopped following for the rest of the session and
// only a reload recovered it. Round 36 draws the way back as the same
// pill the state is read from.
describe('whisperState.resumeFollowing', () => {
  it('follows again after a seek -- the one thing #749 had no way to do', () => {
    whisperState.seek(10 * MIN)
    expect(appState.autoscroll).toBe(false)

    whisperState.resumeFollowing()
    expect(appState.autoscroll).toBe(true)
  })

  it('clears the cursor and the window it is leaving, not just the toggle', () => {
    whisperState.setFenceRange(1 * MIN, 2 * MIN)
    whisperState.seekMs = 5 * MIN

    whisperState.resumeFollowing()
    expect(whisperState.seekMs).toBeNull()
    expect(whisperState.fenceRange).toBeNull()
  })

  it('follows again after a fence too, not only after a seek', () => {
    whisperState.setFenceRange(1 * MIN, 2 * MIN)
    expect(appState.autoscroll).toBe(false)

    whisperState.resumeFollowing()
    expect(appState.autoscroll).toBe(true)
  })
})

describe('whisperState.stopFollowing', () => {
  it('stops the stream without forgetting where the reader was looking', () => {
    whisperState.seek(10 * MIN)
    whisperState.resumeFollowing()
    whisperState.setFenceRange(1 * MIN, 2 * MIN)
    appState.autoscroll = true

    whisperState.stopFollowing()
    expect(appState.autoscroll).toBe(false)
    // "Stop moving", not "forget the window" -- the drawn fence stands.
    expect(whisperState.fenceRange).toEqual({ start: 1 * MIN, end: 2 * MIN })
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
