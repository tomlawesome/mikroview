// SPDX-License-Identifier: AGPL-3.0-only
//
// #1218 audit finding 13: this component used to be two separate copies
// -- FilterBar.svelte's mobile drawer and Whisper.svelte's desktop
// popover each carried their own ColumnChoice/PLAIN_COLUMNS/etc and
// their own columnCheckbox/columnCheckboxes snippets, byte-for-byte
// identical. Only Whisper.svelte.test.ts covered any of it; FilterBar's
// copy had no test at all. These tests exercise the shared component
// directly, and FilterBar.svelte.test.ts/Whisper.svelte.test.ts each
// check their own mount site wires it up, rather than re-covering the
// checkbox logic in three places.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import ColumnToggles from './ColumnToggles.svelte'
import { COLUMNS, PINNED_COLUMNS, columnState } from '../lib/columns.svelte'

// columnState is a module-level singleton (shared with columns.svelte.test.ts,
// FilterBar.svelte.test.ts, Whisper.svelte.test.ts and LiveTable.svelte.test.ts),
// so every test here restores it rather than leaking a toggle into
// whichever test runs next.
beforeEach(() => {
  columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
})

afterEach(() => {
  columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
})

describe('ColumnToggles', () => {
  it('offers a checkbox for every optional column, and none for the pinned two', () => {
    render(ColumnToggles)

    for (const key of PINNED_COLUMNS) {
      const label = COLUMNS.find((c) => c.key === key)?.label as string
      expect(screen.queryByRole('checkbox', { name: `${label} column` })).toBeNull()
    }

    // 15 columns, 2 pinned -- 13 checkboxes total.
    expect(screen.getAllByRole('checkbox').length).toBe(COLUMNS.length - PINNED_COLUMNS.size)
    expect(screen.getByRole('checkbox', { name: 'Device column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Chain column' })).toBeTruthy()
  })

  // #710: "Address column" used to name two different checkboxes (source's
  // and destination's) -- each carries its own disambiguated aria-label
  // even though the two read identically on screen.
  it('disambiguates the address/port/MAC checkboxes that repeat visually, by aria-label', () => {
    render(ColumnToggles)

    expect(screen.getByRole('checkbox', { name: 'Source address column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Destination address column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Source port column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Destination port column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Source MAC column' })).toBeTruthy()
  })

  it('draws the bare column name on screen, grouped under source/destination headings for the repeated ones', () => {
    const { container } = render(ColumnToggles)

    const labelTexts = Array.from(container.querySelectorAll('.col-toggle')).map((el) => el.textContent?.trim())
    expect(labelTexts).toContain('device')
    expect(labelTexts).toContain('NAT')
    expect(labelTexts.filter((t) => t === 'address').length).toBe(2)

    const headings = Array.from(container.querySelectorAll('.col-group-heading')).map((el) => el.textContent?.trim())
    expect(headings).toEqual(['source', 'destination'])
  })

  it('defaults every checkbox to checked -- the shipped default stays all fifteen columns', () => {
    render(ColumnToggles)

    expect(screen.getByRole('checkbox', { name: 'Device column' })).toHaveProperty('checked', true)
  })

  it('unchecking a column writes through to columnState', async () => {
    render(ColumnToggles)

    const device = screen.getByRole('checkbox', { name: 'Device column' })
    await fireEvent.click(device)
    flushSync()

    expect(columnState.isColumnVisible('device')).toBe(false)
  })

  // touch (issue #85): the mobile drawer's own 44px row / larger type,
  // a prop rather than an ancestor `.drawer` selector -- see the
  // component's own comment on why a parent's CSS can no longer reach
  // in once this was its own component.
  it('does not add the touch class by default', () => {
    const { container } = render(ColumnToggles)

    expect(container.querySelector('.col-toggle.touch')).toBeNull()
    expect(container.querySelector('.col-group-heading.touch')).toBeNull()
  })

  it('adds the touch class to every row and heading when asked', () => {
    const { container } = render(ColumnToggles, { props: { touch: true } })

    const toggles = container.querySelectorAll('.col-toggle')
    expect(toggles.length).toBeGreaterThan(0)
    expect(container.querySelectorAll('.col-toggle.touch').length).toBe(toggles.length)
    const headings = container.querySelectorAll('.col-group-heading')
    expect(container.querySelectorAll('.col-group-heading.touch').length).toBe(headings.length)
  })
})
