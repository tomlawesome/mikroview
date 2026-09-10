// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { nextSort, ariaSort, sortGlyph, type SortState } from './tableSort'

type Key = 'time' | 'actor' | 'what'

describe('nextSort', () => {
  it('reverses direction when the same key is clicked again', () => {
    const current: SortState<Key> = { key: 'time', dir: 'desc' }
    expect(nextSort(current, 'time', 'desc')).toEqual({ key: 'time', dir: 'asc' })
    expect(nextSort({ key: 'time', dir: 'asc' }, 'time', 'desc')).toEqual({ key: 'time', dir: 'desc' })
  })

  it('resets to the given default direction when a new key is clicked', () => {
    const current: SortState<Key> = { key: 'time', dir: 'asc' }
    expect(nextSort(current, 'actor', 'asc')).toEqual({ key: 'actor', dir: 'asc' })
  })

  it('lets each key carry its own default, e.g. time defaults desc while others default asc', () => {
    const current: SortState<Key> = { key: 'actor', dir: 'desc' }
    const defaultDirFor = (key: Key): 'asc' | 'desc' => (key === 'time' ? 'desc' : 'asc')

    expect(nextSort(current, 'time', defaultDirFor('time'))).toEqual({ key: 'time', dir: 'desc' })
    expect(nextSort(current, 'what', defaultDirFor('what'))).toEqual({ key: 'what', dir: 'asc' })
  })

  it('always defaults to desc for a table where every column resets that way', () => {
    const current: SortState<Key> = { key: 'actor', dir: 'asc' }
    expect(nextSort(current, 'what', 'desc')).toEqual({ key: 'what', dir: 'desc' })
  })
})

describe('ariaSort', () => {
  it('reports the active key\'s direction', () => {
    expect(ariaSort({ key: 'time', dir: 'asc' }, 'time')).toBe('ascending')
    expect(ariaSort({ key: 'time', dir: 'desc' }, 'time')).toBe('descending')
  })

  it('reports none for every key that is not the active one', () => {
    expect(ariaSort({ key: 'time', dir: 'asc' }, 'actor')).toBe('none')
    expect(ariaSort({ key: 'time', dir: 'asc' }, 'what')).toBe('none')
  })
})

describe('sortGlyph', () => {
  it('shows an up or down glyph for the active key', () => {
    expect(sortGlyph({ key: 'time', dir: 'asc' }, 'time')).toBe('▲')
    expect(sortGlyph({ key: 'time', dir: 'desc' }, 'time')).toBe('▼')
  })

  it('is blank for an inactive key', () => {
    expect(sortGlyph({ key: 'time', dir: 'asc' }, 'actor')).toBe('')
  })
})
