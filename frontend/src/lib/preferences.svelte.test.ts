// SPDX-License-Identifier: AGPL-3.0-only
//
// #1283: preferencesState is the one thing that talks to
// GET/PUT /api/me/preferences. These pin its own contract --
// register/get/set, the debounce, flush, reset -- and the one-time
// localStorage migration, in isolation from any of the eight modules
// that actually use it (each of those keeps its own test, seeded
// through preferencesState.seedForTest() the same way this file seeds
// through a mocked fetchMyPreferences()).
//
// 'demoPref' below is not a real preference module -- it stands in for
// an arbitrary key wherever this file is exercising the generic
// register/get/set/flush plumbing rather than the legacy-key migration
// itself.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  fetchMyPreferences: vi.fn(),
  saveMyPreferences: vi.fn(),
}))

import { fetchMyPreferences, saveMyPreferences } from './api'
import { preferencesState } from './preferences.svelte'

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(saveMyPreferences).mockResolvedValue(null)
  preferencesState.reset()
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('preferencesState.ensureLoaded', () => {
  it('fetches once and hydrates every registered module', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { demoPref: 'pulse' } })
    const seen: unknown[] = []
    preferencesState.register('demoPref', (v) => seen.push(v))

    await preferencesState.ensureLoaded()

    expect(fetchMyPreferences).toHaveBeenCalledTimes(1)
    expect(seen).toEqual(['pulse'])
    expect(preferencesState.get('demoPref')).toBe('pulse')
  })

  it('hydrates a module registered after the record already loaded', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { demoPref: 'mono' } })
    await preferencesState.ensureLoaded()

    const seen: unknown[] = []
    preferencesState.register('demoPref', (v) => seen.push(v))

    expect(seen).toEqual(['mono'])
  })

  it('is idempotent -- a second call does not re-fetch', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })

    await preferencesState.ensureLoaded()
    await preferencesState.ensureLoaded()

    expect(fetchMyPreferences).toHaveBeenCalledTimes(1)
  })

  it('joins an in-flight load rather than firing a second fetch', async () => {
    let resolve!: (v: { version: number; prefs: Record<string, unknown> }) => void
    vi.mocked(fetchMyPreferences).mockReturnValue(
      new Promise((r) => {
        resolve = r
      }),
    )

    const first = preferencesState.ensureLoaded()
    const second = preferencesState.ensureLoaded()
    resolve({ version: 1, prefs: {} })
    await Promise.all([first, second])

    expect(fetchMyPreferences).toHaveBeenCalledTimes(1)
  })

  it('falls back to empty defaults, without attempting migration, when the API is unreachable', async () => {
    localStorage.setItem('mikroview-demoPref', 'pulse')
    vi.mocked(fetchMyPreferences).mockRejectedValue(new Error('network down'))
    const seen: unknown[] = []
    preferencesState.register('demoPref', (v) => seen.push(v))

    await preferencesState.ensureLoaded()

    expect(seen).toEqual([undefined])
    expect(saveMyPreferences).not.toHaveBeenCalled()
    // The legacy key is left alone -- an unreachable API is not the
    // same thing as "the server record is genuinely empty".
    expect(localStorage.getItem('mikroview-demoPref')).toBe('pulse')
  })

  it('never flushes a change made against a failed load -- the server keeps whatever else it had', async () => {
    vi.mocked(fetchMyPreferences).mockRejectedValue(new Error('network down'))
    await preferencesState.ensureLoaded()

    preferencesState.set('demoPref', 'pulse')
    await preferencesState.flush()

    // Nothing sent at all: there was never a real baseline to save
    // against, so this session's one changed key must not go out and
    // risk standing in for -- or racing against -- whatever the server
    // actually holds for every other key.
    expect(saveMyPreferences).not.toHaveBeenCalled()
  })

  it('retries the load on the next ensureLoaded(), and then sends the change made while it was failing', async () => {
    vi.mocked(fetchMyPreferences).mockRejectedValueOnce(new Error('network down'))
    await preferencesState.ensureLoaded()
    preferencesState.set('demoPref', 'pulse')

    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { retention: 30 } })
    await preferencesState.ensureLoaded()

    expect(fetchMyPreferences).toHaveBeenCalledTimes(2)
    // The retry's own record is kept, and the pending local change rides
    // along on top of it rather than being lost.
    expect(preferencesState.get('retention')).toBe(30)
    expect(preferencesState.get('demoPref')).toBe('pulse')
    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { demoPref: 'pulse' } }, {})
  })
})

describe('preferencesState.get/set', () => {
  it('set() is readable back immediately, ahead of the debounced write', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
    await preferencesState.ensureLoaded()
    preferencesState.set('demoPref', 'nebula')

    expect(preferencesState.get('demoPref')).toBe('nebula')
    expect(saveMyPreferences).not.toHaveBeenCalled()
  })
})

describe('preferencesState debounce and flush', () => {
  beforeEach(async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
    await preferencesState.ensureLoaded()
  })

  it('sends one PUT of the whole record 500ms after the last change', async () => {
    vi.useFakeTimers()
    preferencesState.set('demoPref', 'pulse')
    preferencesState.set('groupMode', true)

    await vi.advanceTimersByTimeAsync(499)
    expect(saveMyPreferences).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { demoPref: 'pulse', groupMode: true } }, {})
  })

  it('a change restarts the debounce rather than firing twice', async () => {
    vi.useFakeTimers()
    preferencesState.set('demoPref', 'pulse')
    await vi.advanceTimersByTimeAsync(300)
    preferencesState.set('demoPref', 'nebula')
    await vi.advanceTimersByTimeAsync(300)
    expect(saveMyPreferences).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(200)
    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { demoPref: 'nebula' } }, {})
  })

  it('flush() sends immediately and cancels the pending timer', async () => {
    preferencesState.set('demoPref', 'pulse')

    await preferencesState.flush()

    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
  })

  it('sends only the key changed since the last successful save, not the whole record', async () => {
    preferencesState.set('demoPref', 'pulse')
    await preferencesState.flush()
    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { demoPref: 'pulse' } }, {})

    preferencesState.set('groupMode', true)
    await preferencesState.flush()

    // A second, unrelated change must not re-send demoPref -- only
    // groupMode changed since the last successful flush (#1283's "save
    // only what changed" ruling; two tabs each saving a different key
    // must not stomp on each other on the server).
    expect(saveMyPreferences).toHaveBeenLastCalledWith({ version: 1, prefs: { groupMode: true } }, {})
  })

  it('flush() is a no-op when nothing changed since the last flush', async () => {
    await preferencesState.flush()
    expect(saveMyPreferences).not.toHaveBeenCalled()
  })

  it('flush(keepalive) forwards the option through to the PUT, for a pagehide flush', async () => {
    preferencesState.set('demoPref', 'pulse')

    await preferencesState.flush({ keepalive: true })

    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { demoPref: 'pulse' } }, { keepalive: true })
  })
})

describe('preferencesState.reset', () => {
  it('drops the record from memory without flushing', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
    await preferencesState.ensureLoaded()
    preferencesState.set('demoPref', 'pulse')

    preferencesState.reset()

    expect(preferencesState.get('demoPref')).toBeUndefined()
    expect(saveMyPreferences).not.toHaveBeenCalled()
  })

  it('a module registered before reset is not re-hydrated by reset() itself', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { demoPref: 'pulse' } })
    const seen: unknown[] = []
    // Not yet loaded, so register() itself does not call this -- only
    // ensureLoaded() below does.
    preferencesState.register('demoPref', (v) => seen.push(v))
    await preferencesState.ensureLoaded()
    expect(seen).toEqual(['pulse'])

    preferencesState.reset()

    // reset() clears the record but calls no hydrator -- a stale value
    // sitting in a module's own $state is left alone until the next
    // ensureLoaded() (which the reload after a real sign-out makes
    // moot, see auth.svelte.ts's clearSessionState comment).
    expect(seen).toEqual(['pulse'])
  })

  it('lets the next ensureLoaded() fetch again', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
    await preferencesState.ensureLoaded()
    preferencesState.reset()

    await preferencesState.ensureLoaded()

    expect(fetchMyPreferences).toHaveBeenCalledTimes(2)
  })
})

// #1283's one-time migration: a browser with old localStorage keys and
// an empty server record uploads them once, then clears every
// mikroview* key -- kept for one release (v0.6.1) and then removed.
describe('preferencesState migration from localStorage', () => {
  it('does nothing when the server record already has something in it', async () => {
    localStorage.setItem('mikroview:topography-altitude', 'region')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { retention: 30 } })

    await preferencesState.ensureLoaded()

    expect(saveMyPreferences).not.toHaveBeenCalled()
    expect(preferencesState.get('altitudeStop')).toBeUndefined()
    expect(preferencesState.get('retention')).toBe(30)
    // Left alone -- migration only ever runs against a genuinely empty
    // record, so a browser with both an old key and a real server
    // record keeps the key (harmless; the next release removes the
    // shim regardless).
    expect(localStorage.getItem('mikroview:topography-altitude')).toBe('region')
  })

  it('does nothing when the server record is empty and no legacy key exists either', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })

    await preferencesState.ensureLoaded()

    expect(saveMyPreferences).not.toHaveBeenCalled()
    expect(preferencesState.get('presets')).toBeUndefined()
  })

  it('builds a record from whatever legacy keys are present, uploads it, and clears every mikroview* key', async () => {
    localStorage.setItem('mikroview-filter-presets', JSON.stringify([{ name: 'drops', filters: { action: 'drop' } }]))
    localStorage.setItem('mikroview:topography-altitude', 'region')
    localStorage.setItem('mikroview-column-widths-v8', JSON.stringify([140, 150]))
    localStorage.setItem('mikroview-column-visibility-v1', JSON.stringify({ mac: false }))
    localStorage.setItem('mikroview:group', '1')
    localStorage.setItem('mikroview-max-age-seconds', '30')
    localStorage.setItem('mikroview-metrics-view', 'table')
    localStorage.setItem('mikroview-deck-order', JSON.stringify(['live', 'fall']))
    // Not one of the eight, but still mikroview*-prefixed -- the sweep is
    // blanket, not itemised.
    localStorage.setItem('mikroview-something-future', '1')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {}, userId: 'user-1' })

    await preferencesState.ensureLoaded()

    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
    const [record] = vi.mocked(saveMyPreferences).mock.calls[0]
    expect(record).toEqual({
      version: 1,
      prefs: {
        presets: [{ name: 'drops', filters: { action: 'drop' } }],
        altitudeStop: 'region',
        columns: { widths: [140, 150], visible: { mac: false } },
        groupMode: true,
        retention: 30,
        metrics: 'table',
        deckOrder: ['live', 'fall'],
      },
    })

    expect(preferencesState.get('altitudeStop')).toBe('region')
    expect(preferencesState.get('groupMode')).toBe(true)

    for (const k of Object.keys(localStorage)) {
      expect(k.startsWith('mikroview')).toBe(false)
    }
  })

  it('treats a stored "null" retention as no limit, and "0" groupMode as off', async () => {
    localStorage.setItem('mikroview-max-age-seconds', 'null')
    localStorage.setItem('mikroview:group', '0')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {}, userId: 'user-1' })

    await preferencesState.ensureLoaded()

    expect(preferencesState.get('retention')).toBeNull()
    expect(preferencesState.get('groupMode')).toBe(false)
  })

  it('keeps the legacy keys and still applies the migrated values in memory when the PUT fails', async () => {
    localStorage.setItem('mikroview:topography-altitude', 'region')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {}, userId: 'user-1' })
    vi.mocked(saveMyPreferences).mockResolvedValue('the server could not do that (500)')

    await preferencesState.ensureLoaded()

    expect(preferencesState.get('altitudeStop')).toBe('region')
    expect(localStorage.getItem('mikroview:topography-altitude')).toBe('region')
  })

  it('never touches sessionStorage -- the wizard history key and the sign-out flag live there, not in this sweep', async () => {
    sessionStorage.setItem('mikroview-wizard-history-key', 'a-key')
    sessionStorage.setItem('mikroview.justSignedOut', '1')
    localStorage.setItem('mikroview:topography-altitude', 'region')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {}, userId: 'user-1' })

    await preferencesState.ensureLoaded()

    expect(sessionStorage.getItem('mikroview-wizard-history-key')).toBe('a-key')
    expect(sessionStorage.getItem('mikroview.justSignedOut')).toBe('1')
    sessionStorage.clear()
  })

  // #1283 round 2: on a shared browser, a v0.6.0 install's leftover keys
  // must reach the first account that signs in after the upgrade -- and
  // only that account, even if its own upload never made it and the
  // keys are still sitting there when a second, different account signs
  // in next.
  it('binds the legacy keys to the first account, and refuses to migrate them into a second, different account', async () => {
    localStorage.setItem('mikroview:topography-altitude', 'region')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {}, userId: 'user-a' })
    vi.mocked(saveMyPreferences).mockResolvedValue('the server could not do that (500)')

    // user-a signs in: the upload fails, so the legacy key is left in
    // place, but user-a is now the keys' owner.
    await preferencesState.ensureLoaded()
    expect(preferencesState.get('altitudeStop')).toBe('region')
    expect(localStorage.getItem('mikroview:topography-altitude')).toBe('region')

    // user-a signs out; user-b signs in on the same browser. The server
    // record for user-b is empty too (a genuinely new account), and the
    // legacy key is still sitting in localStorage -- but it belongs to
    // user-a, not user-b.
    preferencesState.reset()
    vi.mocked(saveMyPreferences).mockClear()
    vi.mocked(saveMyPreferences).mockResolvedValue(null)
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {}, userId: 'user-b' })

    await preferencesState.ensureLoaded()

    expect(saveMyPreferences).not.toHaveBeenCalled()
    expect(preferencesState.get('altitudeStop')).toBeUndefined()
    // Still there, untouched, in case user-a signs back in and its own
    // retry can still claim it.
    expect(localStorage.getItem('mikroview:topography-altitude')).toBe('region')
  })
})
