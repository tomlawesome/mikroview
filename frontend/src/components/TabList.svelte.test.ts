// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
// Vite's own `?raw` suffix (declared ambiently by the "vite/client" types
// already in tsconfig.app.json) -- reads the component's source as a
// plain string rather than compiling it, with no Node `fs`/`path` types
// needed, which the rest of this test suite deliberately does without.
import tabListSource from './TabList.svelte?raw'
import TabList from './TabList.svelte'

// The house tablist (DESIGN.md: "Tabs inside pages are the house
// tablist (arrow keys)"), exercised directly rather than only through
// Flags/Watchlist -- the keyboard behaviour is the same control in both
// places, so one focused test suite here is worth more than duplicating
// it per consumer.

describe('TabList roles and structure', () => {
  it('renders a tablist with one tab per entry, aria-selected on the active one', () => {
    render(TabList, {
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta' },
      ],
      selected: 'a',
      onselect: () => {},
      label: 'Test tabs',
    })
    expect(screen.getByRole('tablist', { name: 'Test tabs' })).toBeTruthy()
    const alpha = screen.getByRole('tab', { name: 'Alpha' })
    const beta = screen.getByRole('tab', { name: 'Beta' })
    expect(alpha.getAttribute('aria-selected')).toBe('true')
    expect(beta.getAttribute('aria-selected')).toBe('false')
  })

  // Roving tabindex: only the selected tab is a Tab stop, so pressing
  // Tab once from outside the tablist lands on the active tab, not on
  // every tab in turn.
  it('gives only the selected tab a 0 tabindex', () => {
    render(TabList, {
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta' },
      ],
      selected: 'b',
      onselect: () => {},
      label: 'Test tabs',
    })
    expect(screen.getByRole('tab', { name: 'Alpha' }).getAttribute('tabindex')).toBe('-1')
    expect(screen.getByRole('tab', { name: 'Beta' }).getAttribute('tabindex')).toBe('0')
  })

  it('renders a count when one is given, and none of the rail-badge markup when it is not', () => {
    render(TabList, {
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta', count: 3 },
      ],
      selected: 'a',
      onselect: () => {},
      label: 'Test tabs',
    })
    const beta = screen.getByRole('tab', { name: 'Beta 3' })
    const count = beta.querySelector('.count')
    expect(count?.textContent).toBe('3')

    const alpha = screen.getByRole('tab', { name: 'Alpha' })
    expect(alpha.querySelector('.count')).toBeNull()
  })

  // Outlined, never alarm-filled: the bottom bar's one alarm-filled count
  // is Flags' own open-count (BottomBar.svelte's `.count`), and the
  // record is explicit that in-page counts like this one are outlined
  // instead.
  // This component's stylesheet is the one place that could regress
  // that by reaching for --alarm, so it is asserted directly rather than
  // through a jsdom computed-style read, which does not reliably resolve
  // custom-property-based backgrounds.
  it('never styles its count with the rail alarm colour', () => {
    expect(tabListSource).not.toContain('--alarm')
  })
})

describe('TabList keyboard navigation', () => {
  it('ArrowRight moves selection to the next tab and wraps at the end', async () => {
    let selected = 'a'
    const { rerender } = render(TabList, {
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta' },
        { id: 'c', label: 'Gamma' },
      ],
      selected,
      onselect: (id: string) => {
        selected = id
      },
      label: 'Test tabs',
    })

    const alpha = screen.getByRole('tab', { name: 'Alpha' })
    alpha.focus()
    await fireEvent.keyDown(alpha, { key: 'ArrowRight' })
    expect(selected).toBe('b')
    await rerender({
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta' },
        { id: 'c', label: 'Gamma' },
      ],
      selected,
      onselect: (id: string) => {
        selected = id
      },
      label: 'Test tabs',
    })
    // Focus moved with selection -- automatic activation, per the WAI-ARIA
    // tabs pattern the record asks for ("arrow keys").
    expect(document.activeElement).toBe(screen.getByRole('tab', { name: 'Beta' }))

    const gamma = screen.getByRole('tab', { name: 'Gamma' })
    gamma.focus()
    await fireEvent.keyDown(gamma, { key: 'ArrowRight' })
    expect(selected).toBe('a')
  })

  it('ArrowLeft moves selection to the previous tab and wraps at the start', async () => {
    let selected = 'a'
    render(TabList, {
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta' },
        { id: 'c', label: 'Gamma' },
      ],
      selected,
      onselect: (id: string) => {
        selected = id
      },
      label: 'Test tabs',
    })
    const alpha = screen.getByRole('tab', { name: 'Alpha' })
    alpha.focus()
    await fireEvent.keyDown(alpha, { key: 'ArrowLeft' })
    expect(selected).toBe('c')
  })

  it('Home and End jump to the first and last tab', async () => {
    let selected = 'b'
    render(TabList, {
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta' },
        { id: 'c', label: 'Gamma' },
      ],
      selected,
      onselect: (id: string) => {
        selected = id
      },
      label: 'Test tabs',
    })
    const beta = screen.getByRole('tab', { name: 'Beta' })
    beta.focus()
    await fireEvent.keyDown(beta, { key: 'End' })
    expect(selected).toBe('c')
    await fireEvent.keyDown(beta, { key: 'Home' })
    expect(selected).toBe('a')
  })

  it('clicking a tab selects it directly, same as arrow navigation', async () => {
    let selected = 'a'
    render(TabList, {
      tabs: [
        { id: 'a', label: 'Alpha' },
        { id: 'b', label: 'Beta' },
      ],
      selected,
      onselect: (id: string) => {
        selected = id
      },
      label: 'Test tabs',
    })
    await fireEvent.click(screen.getByRole('tab', { name: 'Beta' }))
    expect(selected).toBe('b')
  })
})
