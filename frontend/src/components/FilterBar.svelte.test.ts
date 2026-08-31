// SPDX-License-Identifier: AGPL-3.0-only
//
// The filter row to round 29's ratified shape (#683): one quiet row --
// device · action · chain · proto · source ⇄ destination (scope +
// country) · port · interface · rule -- as dim micro-labels over
// hairline underlines, no boxes, no placeholder prose, with a single ×
// to clear and ▸ to fold back. Presets and Export to CSV are later
// additions round 29 does not draw, so they come off this row (their
// own components/tests are untouched, see FilterPresetsMenu.svelte and
// lib/export.ts) -- this file pins that they are gone from here.

import { beforeEach, describe, expect, it } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// jsdom has no window.matchMedia -- viewport.svelte.ts's ViewportState
// singleton calls it at module-load time (same fix used throughout this
// suite, e.g. AccountMenu.svelte.test.ts).
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
}

const { default: FilterBar } = await import('./FilterBar.svelte')
const { appState } = await import('../lib/state.svelte')
const { emptyFilters } = await import('../lib/types')

async function expandRow() {
  await fireEvent.click(screen.getByRole('button', { name: /Filters/ }))
  flushSync()
}

beforeEach(() => {
  appState.filters = emptyFilters()
  appState.devices = []
  appState.events = []
})

describe('FilterBar, expanded desktop row (#683, ratified round 29)', () => {
  it('folds by default, behind a single trigger', () => {
    render(FilterBar)
    expect(screen.getByRole('button', { name: /Filters/ })).toBeTruthy()
    expect(screen.queryByLabelText('Device')).toBeNull()
  })

  it('names every ratified field, in the ratified order, once expanded', async () => {
    render(FilterBar)
    await expandRow()

    const labels = Array.from(document.querySelectorAll('.fb-label')).map((el) => el.textContent)
    expect(labels).toEqual(['Device', 'Action', 'Chain', 'Proto', 'Source', 'Destination', 'Port', 'Interface', 'Rule'])
  })

  it('does not draw Presets or Export to CSV -- later additions round 29 does not draw', async () => {
    render(FilterBar)
    await expandRow()

    expect(screen.queryByText(/Presets/)).toBeNull()
    expect(screen.queryByText('Export to CSV')).toBeNull()
  })

  it('carries no placeholder prose inside the fields on the desktop row', async () => {
    render(FilterBar)
    await expandRow()

    expect(screen.getByLabelText('Protocol')).toHaveProperty('placeholder', '')
    expect(screen.getByLabelText('Port — number or service')).toHaveProperty('placeholder', '')
    expect(screen.getByLabelText('Interface')).toHaveProperty('placeholder', '')
  })

  it('clears with a single × and folds back with a single ▸', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(FilterBar)
    await expandRow()

    expect(screen.getByLabelText('Clear all filters').textContent?.trim()).toBe('×')
    expect(screen.getByLabelText('Fold filters back into the box').textContent?.trim()).toBe('▸')
  })

  it('has no clear control when nothing is filtered', async () => {
    render(FilterBar)
    await expandRow()

    expect(screen.queryByLabelText('Clear all filters')).toBeNull()
  })
})
