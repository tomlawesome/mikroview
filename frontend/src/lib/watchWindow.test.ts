// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { countNights, crossesMidnight, hasWindow, nightlySummary, windowLabel } from './watchWindow'
import type { WatchNight } from './types'

function night(state: WatchNight['state'], opened = '2026-09-01T21:00:00Z'): WatchNight {
  return { opened, state }
}

describe('windowLabel (#680)', () => {
  it('reads "always" for an entry with no window', () => {
    expect(windowLabel({})).toBe('always')
    expect(windowLabel({ window: {} })).toBe('always')
  })

  it('reads "always" for a zero-length window, which is the absence of one', () => {
    expect(windowLabel({ window: { start: '06:00', end: '06:00', zone: 'Europe/London' } })).toBe('always')
  })

  it('names the clock range and the zone', () => {
    expect(windowLabel({ window: { start: '22:00', end: '06:00', zone: 'Europe/London' } })).toBe(
      '22:00–06:00 Europe/London',
    )
  })

  // 00:00 is a clock time's zero value, so the server omits it. A missing
  // start is midnight, never "no window" -- getting this wrong would make
  // every quiet-hours watch read as "always".
  it('reads an omitted start as midnight rather than as no window', () => {
    expect(hasWindow({ end: '06:00' })).toBe(true)
    expect(windowLabel({ window: { end: '06:00', zone: 'Europe/London' } })).toBe('00:00–06:00 Europe/London')
  })

  it('names UTC explicitly when no zone is stored', () => {
    expect(windowLabel({ window: { start: '22:00', end: '06:00' } })).toBe('22:00–06:00 UTC')
  })

  it('lists the days a window opens on, when it does not open every day', () => {
    expect(windowLabel({ window: { start: '22:00', end: '06:00', days: [6, 0], zone: 'UTC' } })).toBe(
      'Sat, Sun 22:00–06:00 UTC',
    )
  })

  it('knows a window that runs into the following date', () => {
    expect(crossesMidnight({ start: '22:00', end: '06:00' })).toBe(true)
    expect(crossesMidnight({ start: '09:00', end: '17:00' })).toBe(false)
    expect(crossesMidnight(undefined)).toBe(false)
  })
})

describe('nightlySummary (#680)', () => {
  it('says nothing at all when no nights are recorded yet', () => {
    expect(nightlySummary(undefined)).toBeNull()
    expect(nightlySummary([])).toBeNull()
  })

  // The ratified copy, unchanged: no second clause when there is nothing
  // to put in it.
  it('is one clause when every night was kept', () => {
    expect(nightlySummary(Array.from({ length: 7 }, () => night('kept')))).toBe('seven kept nights')
  })

  it('is the ratified two clauses for five kept and two empty', () => {
    const nights = [...Array.from({ length: 5 }, () => night('kept')), night('empty'), night('empty')]
    expect(nightlySummary(nights)).toBe('five kept nights · two empty')
  })

  // The clause the owner ratified on top of the mockup: it appears only
  // when a night could not be observed.
  it('grows a third clause when a night was not observed', () => {
    const nights = [...Array.from({ length: 5 }, () => night('kept')), night('empty'), night('not observed')]
    expect(nightlySummary(nights)).toBe('five kept nights · one empty · one not observed')
  })

  it('drops the third clause again when every night was observed', () => {
    const nights = [...Array.from({ length: 6 }, () => night('kept')), night('empty')]
    expect(nightlySummary(nights)).toBe('six kept nights · one empty')
    expect(nightlySummary(nights)).not.toContain('not observed')
  })

  // The rule the whole feature exists for: a night mikroview did not
  // observe is never counted as, or worded as, empty.
  it('never counts an unobserved night as empty', () => {
    const nights = Array.from({ length: 7 }, () => night('not observed'))
    const summary = nightlySummary(nights)
    expect(summary).toBe('seven nights not observed')
    expect(summary).not.toContain('empty')
    expect(summary).not.toContain('kept')
    expect(countNights(nights)).toEqual({ kept: 0, empty: 0, unobserved: 7, total: 7 })
  })

  it('leads with the unobserved count rather than "no kept nights" when nothing was seen', () => {
    expect(nightlySummary([night('not observed')])).toBe('one night not observed')
  })

  it('says "no kept nights" when the window really was watched and really was silent', () => {
    expect(nightlySummary(Array.from({ length: 3 }, () => night('empty')))).toBe('no kept nights · three empty')
  })

  it('is singular for one night', () => {
    expect(nightlySummary([night('kept')])).toBe('one kept night')
  })

  it('counts an unrecognised state as unobserved rather than as evidence', () => {
    const odd = [{ opened: '2026-09-01T21:00:00Z', state: 'from-a-newer-binary' } as unknown as WatchNight]
    expect(countNights(odd)).toEqual({ kept: 0, empty: 0, unobserved: 1, total: 1 })
  })
})
