// SPDX-License-Identifier: AGPL-3.0-only
//
// #1283: preferencesState is the one thing that talks to
// GET/PUT /api/me/preferences. These pin its own contract --
// register/get/set, the debounce, flush, reset -- and the one-time
// localStorage migration, in isolation from any of the nine modules
// that actually use it (each of those keeps its own test, seeded
// through preferencesState.seedForTest() the same way this file seeds
// through a mocked fetchMyPreferences()).

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
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { colorway: 'pulse' } })
    const seen: unknown[] = []
    preferencesState.register('colorway', (v) => seen.push(v))

    await preferencesState.ensureLoaded()

    expect(fetchMyPreferences).toHaveBeenCalledTimes(1)
    expect(seen).toEqual(['pulse'])
    expect(preferencesState.get('colorway')).toBe('pulse')
  })

  it('hydrates a module registered after the record already loaded', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { colorway: 'mono' } })
    await preferencesState.ensureLoaded()

    const seen: unknown[] = []
    preferencesState.register('colorway', (v) => seen.push(v))

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
    localStorage.setItem('mikroview-colorway', 'pulse')
    vi.mocked(fetchMyPreferences).mockRejectedValue(new Error('network down'))
    const seen: unknown[] = []
    preferencesState.register('colorway', (v) => seen.push(v))

    await preferencesState.ensureLoaded()

    expect(seen).toEqual([undefined])
    expect(saveMyPreferences).not.toHaveBeenCalled()
    // The legacy key is left alone -- an unreachable API is not the
    // same thing as "the server record is genuinely empty".
    expect(localStorage.getItem('mikroview-colorway')).toBe('pulse')
  })
})

describe('preferencesState.get/set', () => {
  it('set() is readable back immediately, ahead of the debounced write', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
    await preferencesState.ensureLoaded()
    preferencesState.set('colorway', 'nebula')

    expect(preferencesState.get('colorway')).toBe('nebula')
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
    preferencesState.set('colorway', 'pulse')
    preferencesState.set('groupMode', true)

    await vi.advanceTimersByTimeAsync(499)
    expect(saveMyPreferences).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1)
    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { colorway: 'pulse', groupMode: true } }, {})
  })

  it('a change restarts the debounce rather than firing twice', async () => {
    vi.useFakeTimers()
    preferencesState.set('colorway', 'pulse')
    await vi.advanceTimersByTimeAsync(300)
    preferencesState.set('colorway', 'nebula')
    await vi.advanceTimersByTimeAsync(300)
    expect(saveMyPreferences).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(200)
    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { colorway: 'nebula' } }, {})
  })

  it('flush() sends immediately and cancels the pending timer', async () => {
    preferencesState.set('colorway', 'pulse')

    await preferencesState.flush()

    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
  })

  it('flush() is a no-op when nothing changed since the last flush', async () => {
    await preferencesState.flush()
    expect(saveMyPreferences).not.toHaveBeenCalled()
  })

  it('flush(keepalive) forwards the option through to the PUT, for a pagehide flush', async () => {
    preferencesState.set('colorway', 'pulse')

    await preferencesState.flush({ keepalive: true })

    expect(saveMyPreferences).toHaveBeenCalledWith({ version: 1, prefs: { colorway: 'pulse' } }, { keepalive: true })
  })
})

describe('preferencesState.reset', () => {
  it('drops the record from memory without flushing', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
    await preferencesState.ensureLoaded()
    preferencesState.set('colorway', 'pulse')

    preferencesState.reset()

    expect(preferencesState.get('colorway')).toBeUndefined()
    expect(saveMyPreferences).not.toHaveBeenCalled()
  })

  it('a module registered before reset is not re-hydrated by reset() itself', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { colorway: 'pulse' } })
    const seen: unknown[] = []
    // Not yet loaded, so register() itself does not call this -- only
    // ensureLoaded() below does.
    preferencesState.register('colorway', (v) => seen.push(v))
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
    localStorage.setItem('mikroview-colorway', 'pulse')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: { retention: 30 } })

    await preferencesState.ensureLoaded()

    expect(saveMyPreferences).not.toHaveBeenCalled()
    expect(preferencesState.get('colorway')).toBeUndefined()
    expect(preferencesState.get('retention')).toBe(30)
    // Left alone -- migration only ever runs against a genuinely empty
    // record, so a browser with both an old key and a real server
    // record keeps the key (harmless; the next release removes the
    // shim regardless).
    expect(localStorage.getItem('mikroview-colorway')).toBe('pulse')
  })

  it('does nothing when the server record is empty and no legacy key exists either', async () => {
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })

    await preferencesState.ensureLoaded()

    expect(saveMyPreferences).not.toHaveBeenCalled()
    expect(preferencesState.get('colorway')).toBeUndefined()
  })

  it('builds a record from whatever legacy keys are present, uploads it, and clears every mikroview* key', async () => {
    localStorage.setItem('mikroview-filter-presets', JSON.stringify([{ name: 'drops', filters: { action: 'drop' } }]))
    localStorage.setItem('mikroview-colorway', 'pulse')
    localStorage.setItem('mikroview:topography-altitude', 'region')
    localStorage.setItem('mikroview-column-widths-v8', JSON.stringify([140, 150]))
    localStorage.setItem('mikroview-column-visibility-v1', JSON.stringify({ mac: false }))
    localStorage.setItem('mikroview:group', '1')
    localStorage.setItem('mikroview-max-age-seconds', '30')
    localStorage.setItem('mikroview-metrics-view', 'table')
    localStorage.setItem('mikroview-deck-order', JSON.stringify(['live', 'fall']))
    // Not one of the nine, but still mikroview*-prefixed -- the sweep is
    // blanket, not itemised.
    localStorage.setItem('mikroview-something-future', '1')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })

    await preferencesState.ensureLoaded()

    expect(saveMyPreferences).toHaveBeenCalledTimes(1)
    const [record] = vi.mocked(saveMyPreferences).mock.calls[0]
    expect(record).toEqual({
      version: 1,
      prefs: {
        presets: [{ name: 'drops', filters: { action: 'drop' } }],
        colorway: 'pulse',
        altitudeStop: 'region',
        columns: { widths: [140, 150], visible: { mac: false } },
        groupMode: true,
        retention: 30,
        metrics: 'table',
        deckOrder: ['live', 'fall'],
      },
    })

    expect(preferencesState.get('colorway')).toBe('pulse')
    expect(preferencesState.get('groupMode')).toBe(true)

    for (const k of Object.keys(localStorage)) {
      expect(k.startsWith('mikroview')).toBe(false)
    }
  })

  it('treats a stored "null" retention as no limit, and "0" groupMode as off', async () => {
    localStorage.setItem('mikroview-max-age-seconds', 'null')
    localStorage.setItem('mikroview:group', '0')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })

    await preferencesState.ensureLoaded()

    expect(preferencesState.get('retention')).toBeNull()
    expect(preferencesState.get('groupMode')).toBe(false)
  })

  it('keeps the legacy keys and still applies the migrated values in memory when the PUT fails', async () => {
    localStorage.setItem('mikroview-colorway', 'pulse')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
    vi.mocked(saveMyPreferences).mockResolvedValue('the server could not do that (500)')

    await preferencesState.ensureLoaded()

    expect(preferencesState.get('colorway')).toBe('pulse')
    expect(localStorage.getItem('mikroview-colorway')).toBe('pulse')
  })

  it('never touches sessionStorage -- the wizard history key and the sign-out flag live there, not in this sweep', async () => {
    sessionStorage.setItem('mikroview-wizard-history-key', 'a-key')
    sessionStorage.setItem('mikroview.justSignedOut', '1')
    localStorage.setItem('mikroview-colorway', 'pulse')
    vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })

    await preferencesState.ensureLoaded()

    expect(sessionStorage.getItem('mikroview-wizard-history-key')).toBe('a-key')
    expect(sessionStorage.getItem('mikroview.justSignedOut')).toBe('1')
    sessionStorage.clear()
  })
})
