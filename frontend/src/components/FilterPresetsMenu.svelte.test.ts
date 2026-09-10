// SPDX-License-Identifier: AGPL-3.0-only
//
// Round 37 gave the saved filters a home at last: a `saved ▾` trigger
// inside the filter box's own right end, opening a short list in the
// account menu's dress. The component itself predates round 30 and was
// never mounted -- #683 left it unmounted rather than invent a place for
// it -- so this is the first coverage of it, and it covers the menu's
// behaviour (open, apply, forget, save, close) rather than its dress.
import { beforeEach, describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import { fireEvent } from '@testing-library/dom'
import { flushSync } from 'svelte'
import FilterPresetsMenu from './FilterPresetsMenu.svelte'
import { appState } from '../lib/state.svelte'
import { presetState, type FilterPreset } from '../lib/presets.svelte'
import { emptyFilters, type Filters } from '../lib/types'

function preset(name: string, filters: Partial<Filters> = {}): FilterPreset {
  return { name, filters: { ...emptyFilters(), ...filters } }
}

// Both of these are module-level singletons loaded once from
// localStorage, so a preset saved or forgotten by one test is still
// saved or forgotten in the next one without this.
beforeEach(() => {
  presetState.presets = [
    preset('drops only', { action: 'drop' }),
    preset('the guest vlan', { srcQuery: '10.20.0.0/16' }),
  ]
  appState.filters = emptyFilters()
})

function renderMenu() {
  const { container } = render(FilterPresetsMenu)
  const trigger = container.querySelector<HTMLButtonElement>('.fsaved')
  if (!trigger) throw new Error('saved trigger not found')
  return {
    container,
    trigger,
    menu: () => container.querySelector('.fpmenu'),
    rows: () => [...container.querySelectorAll<HTMLButtonElement>('.fpname')],
    forgets: () => [...container.querySelectorAll<HTMLButtonElement>('.fpx')],
  }
}

async function open(trigger: HTMLButtonElement) {
  await fireEvent.click(trigger)
  flushSync()
}

describe('the saved-filters trigger', () => {
  it('reads `saved ▾` and opens onto nothing until it is asked to', () => {
    const { trigger, menu } = renderMenu()

    expect(trigger.textContent?.trim()).toBe('saved ▾')
    expect(menu()).toBeNull()
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
  })

  it('opens the menu, closes it again on a second click, and reports which it is', async () => {
    const { trigger, menu } = renderMenu()

    await open(trigger)
    expect(menu()).not.toBeNull()
    // aria-expanded is the only thing telling a screen reader the list is
    // there at all -- the panel is drawn, not announced.
    expect(trigger.getAttribute('aria-expanded')).toBe('true')

    await open(trigger)
    expect(menu()).toBeNull()
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
  })

  it('Escape closes it, without needing the pointer back on the trigger', async () => {
    const { trigger, menu } = renderMenu()
    await open(trigger)

    await fireEvent.keyDown(document, { key: 'Escape' })
    flushSync()

    expect(menu()).toBeNull()
  })
})

describe('the saved list', () => {
  it('lists every saved filter, each with its own way to forget it', async () => {
    const { trigger, rows, forgets } = renderMenu()
    await open(trigger)

    expect(rows().map((r) => r.textContent?.trim())).toEqual(['drops only', 'the guest vlan'])
    expect(forgets()).toHaveLength(2)
    expect(forgets()[0].textContent?.trim()).toBe('×')
  })

  it('taking a saved filter applies it and gets out of the way', async () => {
    const { trigger, rows, menu } = renderMenu()
    await open(trigger)

    await fireEvent.click(rows()[1])
    flushSync()

    expect(appState.filters.srcQuery).toBe('10.20.0.0/16')
    // Reaching for a saved filter is one decision, already made -- leaving
    // the list open would ask the operator to close it themselves.
    expect(menu()).toBeNull()
  })

  it('forgetting one row removes only that one and leaves the list open', async () => {
    const { trigger, rows, forgets, menu } = renderMenu()
    await open(trigger)

    await fireEvent.click(forgets()[0])
    flushSync()

    expect(presetState.presets.map((p) => p.name)).toEqual(['the guest vlan'])
    // Forgetting is housekeeping, often more than one at a time, so the
    // menu stays where it is rather than closing after each `×`.
    expect(menu()).not.toBeNull()
    expect(rows().map((r) => r.textContent?.trim())).toEqual(['the guest vlan'])
  })

  it('an empty list still says what is missing rather than opening onto a bare save item', async () => {
    presetState.presets = []
    appState.filters = { ...emptyFilters(), action: 'drop' }
    const { trigger, container, rows } = renderMenu()
    await open(trigger)

    expect(rows()).toHaveLength(0)
    expect(container.querySelector('.fpnone')?.textContent?.trim()).toBe('No saved filters yet.')
  })
})

describe('save this filter as…', () => {
  it('is absent with no filter set, because it is an offer the app could not keep', async () => {
    const { trigger, container } = renderMenu()
    await open(trigger)

    expect(appState.hasActiveFilters).toBe(false)
    // Absent rather than disabled: a greyed row says the same thing more
    // quietly and still occupies the list.
    expect(container.querySelector('.fpsave')).toBeNull()
  })

  it('appears as soon as there is a filter worth saving', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    const { trigger, container } = renderMenu()
    await open(trigger)

    expect(appState.hasActiveFilters).toBe(true)
    expect(container.querySelector('.fpsave')?.textContent?.trim()).toBe('save this filter as…')
  })
})
