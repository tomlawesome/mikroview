// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { ACTIONS, ACTION_LABELS, ACTION_FILTER_OPTIONS } from './actions'
import type { Action } from './types'

// The type system covers ACTION_LABELS (a Record<Action, string> stops
// compiling when the union grows). It does not cover the two things
// below, which are arrays: a new action can be added to the union, land
// a label, render a badge -- and still be unfilterable, because nobody
// added it to the pick-list. That is the failure #437 would have shipped
// silently.
describe('action vocabulary', () => {
  it('lists every action exactly once, with unknown last', () => {
    expect(new Set(ACTIONS).size).toBe(ACTIONS.length)
    expect(ACTIONS.at(-1)).toBe('unknown')
    // Every labelled action is listed, and vice versa.
    expect([...ACTIONS].sort()).toEqual(Object.keys(ACTION_LABELS).sort())
  })

  it('offers every action in the filter pick-list', () => {
    const values = ACTION_FILTER_OPTIONS.map((o) => o.value)
    expect(values[0]).toBe('') // "Any action"
    for (const action of ACTIONS) {
      expect(values).toContain(action)
    }
    expect(values.length).toBe(ACTIONS.length + 1)
  })

  it('keeps the raw action as the filter value, never the label', () => {
    for (const option of ACTION_FILTER_OPTIONS) {
      if (option.value === '') continue
      expect(ACTIONS).toContain(option.value)
      expect(option.label).toBeTruthy()
    }
  })

  it('classifies the non-filter rule kinds #437 added', () => {
    // Named explicitly rather than derived, so removing one of these
    // from the union is a failing test and not a quietly shorter list.
    const nonFilter: Action[] = ['marked', 'natted', 'log']
    for (const action of nonFilter) {
      expect(ACTIONS).toContain(action)
    }
    // Both new labels name RouterOS's own term, so the option is
    // findable by someone looking at their router's configuration.
    const label = (v: Action) => ACTION_FILTER_OPTIONS.find((o) => o.value === v)?.label ?? ''
    expect(label('marked')).toMatch(/mangle/i)
    expect(label('natted')).toMatch(/nat/i)
  })
})
