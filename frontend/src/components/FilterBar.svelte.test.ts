// SPDX-License-Identifier: AGPL-3.0-only
//
// The stream's filter to round 30's ratified shape (#697, #700/#691):
// "one filter, two hands". The box (`.fbox`) is ALWAYS on screen, carries
// the typed grammar as chips, and says so when empty instead of
// vanishing -- so there is no second "Filters ▸" control left to exist.
// `bar ▸`/`◂ bar`, welded to the box's own left edge, unfurls round 8's
// thin strip -- device · action · chain · proto · source ⇄ destination
// (scope + country) · port · interface · rule -- as dim micro-labels
// over hairline-underlined values, no boxes, no placeholder prose, with
// `× clear` and `fold ▸` at its end. The span pills (15 m/1 h/24 h/14 d,
// #703) and the "holding N" reach words ride the filter line's own right
// end (moved here from SceneBar.svelte.test.ts under the same issue).

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
const { retentionState } = await import('../lib/retention.svelte')

async function expandRow() {
  await fireEvent.click(screen.getByRole('button', { name: /bar/ }))
  flushSync()
}

beforeEach(() => {
  appState.filters = emptyFilters()
  appState.devices = []
  appState.events = []
  appState.stats = null
  retentionState.set(null)
})

describe('FilterBar, the filter line (#697, ratified round 30)', () => {
  it('is always on screen, folded by default, welded to a "bar ▸" toggle', () => {
    render(FilterBar)
    expect(document.querySelector('.fbox')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'bar ▸' })).toBeTruthy()
    expect(screen.queryByLabelText('Device')).toBeNull()
  })

  it('does not draw the old "Filters ▸" trigger beside the box -- retired, the box is the one way in (#697)', () => {
    render(FilterBar)
    expect(screen.queryByText('Filters ▸')).toBeNull()
  })

  it('says so when no term is set, rather than vanishing', () => {
    render(FilterBar)
    expect(screen.getByText('no filter — every line, as it arrived. type a term, or click a value in a row')).toBeTruthy()
  })

  it('shows every active term as a chip, with an invitation to add more', () => {
    appState.filters = { ...emptyFilters(), action: 'drop', port: '445' }
    render(FilterBar)
    const box = document.querySelector('.fbox')
    expect(box?.textContent?.replace(/\s+/g, ' ').trim()).toBe('action:drop⌫port:445⌫ type a term, or click a value in a row')
    expect(screen.getByText('type a term, or click a value in a row')).toBeTruthy()
  })

  it('drops one term at a time from its own chip, leaving the rest of the filter alone', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop', port: '445' }
    render(FilterBar)
    await fireEvent.click(screen.getByLabelText('Remove the action filter'))
    flushSync()
    expect(appState.filters.action).toBe('')
    expect(appState.filters.port).toBe('445')
  })

  it('drops a compound source chip by clearing every field it summarises', async () => {
    appState.filters = { ...emptyFilters(), srcQuery: 'cam-porch', srcScope: 'internal' }
    render(FilterBar)
    await fireEvent.click(screen.getByLabelText('Remove the source filter'))
    flushSync()
    expect(appState.filters.srcQuery).toBe('')
    expect(appState.filters.srcScope).toBe('')
    expect(appState.filters.srcCountry).toBe('')
  })

  it('toggles the handle\'s own text and unfurls the named-field strip', async () => {
    render(FilterBar)
    const open = screen.getByRole('button', { name: 'bar ▸' })
    expect(open.getAttribute('aria-expanded')).toBe('false')

    await fireEvent.click(open)
    flushSync()
    expect(screen.getByRole('button', { name: '◂ bar' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '◂ bar' }).getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByLabelText('Device')).toBeTruthy()
  })

  // #703: the control is only honest if a span the buffer cannot cover is
  // visibly not on offer. These pin that, and that choosing one sets the
  // same display window the mobile drawer sets -- moved here from
  // SceneBar.svelte.test.ts, whose own bar no longer draws this (#697).
  describe('the span control, moved from the bar', () => {
    function statsHolding(oldestHeld: string | null) {
      appState.stats = {
        total: 0,
        byAction: {},
        topRules: [],
        timeSeries: [],
        eventsPerSecond: 34,
        capacity: 100000,
        count: 10,
        windowSeconds: 3600,
        oldestHeld,
        connectedClients: 1,
      }
    }

    it('offers every span the buffer reaches back far enough to answer', () => {
      statsHolding(new Date(appState.now - 2 * 86400 * 1000).toISOString())
      render(FilterBar)
      flushSync()

      for (const label of ['15 m', '1 h', '24 h']) {
        expect(screen.getByRole('button', { name: label }).hasAttribute('disabled')).toBe(false)
      }
    })

    it('withholds a fortnight from a buffer holding nine hours, and says what it holds', () => {
      statsHolding(new Date(appState.now - 9 * 3600 * 1000).toISOString())
      render(FilterBar)
      flushSync()

      expect(screen.getByRole('button', { name: '1 h' }).hasAttribute('disabled')).toBe(false)
      expect(screen.getByRole('button', { name: '24 h' }).hasAttribute('disabled')).toBe(true)
      expect(screen.getByRole('button', { name: '14 d' }).hasAttribute('disabled')).toBe(true)
      expect(screen.getByText('holding 9 h')).toBeTruthy()
    })

    it('offers only the shortest span while the buffer holds nothing', () => {
      statsHolding(null)
      render(FilterBar)
      flushSync()

      expect(screen.getByRole('button', { name: '15 m' }).hasAttribute('disabled')).toBe(false)
      for (const label of ['1 h', '24 h', '14 d']) {
        expect(screen.getByRole('button', { name: label }).hasAttribute('disabled')).toBe(true)
      }
      expect(screen.getByText('nothing held yet')).toBeTruthy()
    })

    it('sets the display window when a span is chosen', async () => {
      statsHolding(new Date(appState.now - 2 * 3600 * 1000).toISOString())
      render(FilterBar)
      flushSync()

      await fireEvent.click(screen.getByRole('button', { name: '1 h' }))
      expect(retentionState.maxAgeSeconds).toBe(3600)
      expect(screen.getByRole('button', { name: '1 h' }).getAttribute('aria-pressed')).toBe('true')
    })
  })
})

describe('FilterBar, expanded desktop row (#683/#697, ratified round 30)', () => {
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

  // Round 30 upgrades round 29's bare "×"/"▸" to "× clear"/"fold ▸"
  // (the-whole.html's own `.fb-clear`/`.fb-fold` text).
  it('clears with "× clear" and folds back with "fold ▸"', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(FilterBar)
    await expandRow()

    expect(screen.getByLabelText('Clear all filters').textContent?.trim()).toBe('× clear')
    expect(screen.getByLabelText('Fold filters back into the box').textContent?.trim()).toBe('fold ▸')
  })

  it('has no clear control when nothing is filtered', async () => {
    render(FilterBar)
    await expandRow()

    expect(screen.queryByLabelText('Clear all filters')).toBeNull()
  })

  it('folds the strip back into the box, leaving the toggle reading "bar ▸" again', async () => {
    render(FilterBar)
    await expandRow()
    await fireEvent.click(screen.getByLabelText('Fold filters back into the box'))
    flushSync()

    expect(screen.getByRole('button', { name: 'bar ▸' })).toBeTruthy()
    expect(screen.queryByLabelText('Device')).toBeNull()
  })
})
