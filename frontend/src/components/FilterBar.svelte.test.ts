// SPDX-License-Identifier: AGPL-3.0-only
//
// The stream's filter to round 30's ratified shape (#697, #700/#691):
// "one filter, two hands". The box (`.fbox`) is ALWAYS on screen, carries
// the typed grammar as chips, and says so when empty instead of
// vanishing -- so there is no second "Filters ▸" control left to exist.
//
// Owner correction, 2026-08-31: the "bar ▸"/"◂ bar" toggle that used to
// be welded to the box's left edge is gone -- "remove the bar button
// entirely, and the filter bar instead folds out of the search box as a
// drawer... clicking in the box opens it, clicking away from the box
// closes it." The box itself is now the disclosure for round 8's thin
// strip -- device · action · chain · proto · source ⇄ destination
// (scope + country) · port · interface · rule -- as dim micro-labels
// over hairline-underlined values, no boxes, no placeholder prose, with
// `× clear` and `fold ▸` at its end. The span pills (15 m/1 h/24 h/14 d,
// #703) and the "holding N" reach words ride the filter line's own right
// end (moved here from SceneBar.svelte.test.ts under the same issue).

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import type { FirewallEvent } from '../lib/types'
import { COLUMNS, PINNED_COLUMNS, columnState } from '../lib/columns.svelte'

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

// The box div carries no role -- it holds the chips' own remove buttons,
// and a screen reader flattens the contents of anything with
// role="button". The hint inside it is the real control -- a text
// input, #734, so it can actually be typed into -- and carries the
// expanded state, so that is what these tests drive.
function getBox() {
  return screen.getByRole('textbox', { name: /type a term/ })
}

// Opens the desktop strip the same way an owner-specified click does --
// there is no button any more, so this clicks the box itself.
async function expandRow() {
  await fireEvent.click(getBox())
  flushSync()
}

// Minimal event fixture, mirroring lib/state.svelte.test.ts's own evt() --
// only the fields applyFilters' rule branch reads.
function evt(overrides: Partial<FirewallEvent> = {}): FirewallEvent {
  return {
    id: 1,
    time: '2026-01-01T00:00:00Z',
    deviceId: 'core',
    sourceIp: '10.0.0.1',
    action: 'accept',
    ruleLabel: 'lan-wan',
    chain: 'forward',
    raw: 'A|lan-wan|forward: ...',
    ...overrides,
  }
}

beforeEach(() => {
  appState.filters = emptyFilters()
  appState.devices = []
  appState.events = []
  appState.stats = null
  retentionState.set(null)
})

describe('FilterBar, the filter line (#697, ratified round 30)', () => {
  it('is always on screen, folded by default, with no separate button to reach the drawer', () => {
    render(FilterBar)
    const box = getBox()
    expect(box).toBeTruthy()
    expect(box.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByRole('button', { name: /bar/i })).toBeNull()
    expect(screen.queryByLabelText('Device')).toBeNull()
  })

  it('does not draw the old "Filters ▸" trigger beside the box -- retired, the box is the one way in (#697)', () => {
    render(FilterBar)
    expect(screen.queryByText('Filters ▸')).toBeNull()
  })

  it('says so when no term is set, rather than vanishing', () => {
    render(FilterBar)
    expect(
      screen.getByPlaceholderText('no filter — every line, as it arrived. type a term, or click a value in a row'),
    ).toBeTruthy()
  })

  it('shows every active term as a chip, with an invitation to add more', () => {
    appState.filters = { ...emptyFilters(), action: 'drop', port: '445' }
    render(FilterBar)
    const box = document.querySelector('.fbox')
    expect(box?.textContent?.replace(/\s+/g, ' ').trim()).toBe('action:drop⌫port:445⌫')
    expect(screen.getByPlaceholderText('type a term, or click a value in a row')).toBeTruthy()
  })

  it('actually takes keystrokes -- typing in the box writes the same free-text filter the strip\'s Rule field does, and narrows the stream (#734)', async () => {
    appState.events = [evt({ id: 1, ruleLabel: 'wan-block-scan' }), evt({ id: 2, ruleLabel: 'lan-internal' })] as unknown as (typeof appState)['events']
    render(FilterBar)
    const box = getBox()
    expect(appState.filters.rule).toBe('')
    expect(appState.filteredEvents.map((e) => e.id)).toEqual([1, 2])

    await fireEvent.input(box, { target: { value: 'block' } })
    flushSync()

    expect(appState.filters.rule).toBe('block')
    expect((box as HTMLInputElement).value).toBe('block')
    expect(appState.filteredEvents.map((e) => e.id)).toEqual([1])
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

  it('opens the named-field strip on a click inside the box (owner, 2026-08-31)', async () => {
    render(FilterBar)
    const box = getBox()
    expect(box.getAttribute('aria-expanded')).toBe('false')

    await fireEvent.click(box)
    flushSync()
    expect(box.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByLabelText('Device')).toBeTruthy()
  })

  it('closes the strip on a click away from both the box and the strip', async () => {
    render(FilterBar)
    const box = getBox()
    await expandRow()
    expect(screen.getByLabelText('Device')).toBeTruthy()

    await fireEvent.click(document.body)
    flushSync()
    expect(box.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByLabelText('Device')).toBeNull()
  })

  it('does not close when the click lands inside the open strip itself', async () => {
    render(FilterBar)
    const box = getBox()
    await expandRow()

    await fireEvent.click(screen.getByLabelText('Device'))
    flushSync()
    expect(box.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByLabelText('Device')).toBeTruthy()
  })

  it('does not open (or close) when removing a chip -- that click stops at the chip', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(FilterBar)
    const box = getBox()
    expect(box.getAttribute('aria-expanded')).toBe('false')

    await fireEvent.click(screen.getByLabelText('Remove the action filter'))
    flushSync()
    expect(box.getAttribute('aria-expanded')).toBe('false')
  })

  it('gives the keyboard a real input inside the box, so keystrokes land as text (#734)', async () => {
    render(FilterBar)
    const box = getBox()
    // A native <input>, not the button it used to be -- so Space types a
    // literal space rather than activating a control.
    expect(box.tagName).toBe('INPUT')
    expect(box.getAttribute('type')).toBe('text')

    await fireEvent.click(box)
    flushSync()
    expect(box.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByLabelText('Device')).toBeTruthy()
  })

  it('still gives the keyboard a way into the strip without a mouse: Enter in the box opens it (#734)', async () => {
    render(FilterBar)
    const box = getBox()
    expect(box.getAttribute('aria-expanded')).toBe('false')

    await fireEvent.keyDown(box, { key: 'Enter' })
    flushSync()
    expect(box.getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByLabelText('Device')).toBeTruthy()
  })

  it('leaves each chip\'s own remove button reachable -- the box takes no role that would flatten them', () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(FilterBar)
    expect(screen.getByLabelText('Remove the action filter').tagName).toBe('BUTTON')
  })

  it('closes on Escape and returns focus to the box -- the keyboard equivalent of "click away"', async () => {
    render(FilterBar)
    const box = getBox()
    await expandRow()

    await fireEvent.keyDown(window, { key: 'Escape' })
    flushSync()
    expect(box.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByLabelText('Device')).toBeNull()
    expect(document.activeElement).toBe(box)
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

    // #729 (owner ruling, built on top of this ratified round-30 row):
    // "Columns" is the one field added since -- the chooser lives here,
    // with the rest of the strip's controls, so it is the one addition to
    // this otherwise-frozen list.
    const labels = Array.from(document.querySelectorAll('.fb-label')).map((el) => el.textContent)
    expect(labels).toEqual([
      'Device',
      'Action',
      'Chain',
      'Proto',
      'Source',
      'Destination',
      'Port',
      'Interface',
      'Rule',
      'Columns',
    ])
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

  it('folds the strip back into the box via "fold ▸", returning focus to the box', async () => {
    render(FilterBar)
    const box = getBox()
    await expandRow()
    await fireEvent.click(screen.getByLabelText('Fold filters back into the box'))
    flushSync()

    expect(box.getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByLabelText('Device')).toBeNull()
    expect(document.activeElement).toBe(box)
  })
})

// #729: the column chooser rides in the same fold-out strip as the rest
// of the filter fields -- no new bar, no new button beside the search
// box. columnState is a module-level singleton (shared with
// columns.svelte.test.ts and LiveTable.svelte.test.ts), so every test
// below restores it rather than leaking a toggle into whichever test
// runs next.
describe('FilterBar, the column chooser (#729)', () => {
  beforeEach(() => {
    columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
  })

  afterEach(() => {
    columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
  })

  it('offers a checkbox for every optional column, and none for the pinned two', async () => {
    render(FilterBar)
    await expandRow()

    // Time and Rule are each a unique label in this list -- a plain
    // queryByRole miss proves no checkbox exists for either. (Address and
    // Port repeat between source/destination and are checked separately
    // below via a count, since a name lookup on a repeated label throws.)
    for (const key of PINNED_COLUMNS) {
      const label = COLUMNS.find((c) => c.key === key)?.label as string
      expect(screen.queryByRole('checkbox', { name: `${label} column` })).toBeNull()
    }

    // 15 columns, 2 pinned -- 13 checkboxes total, regardless of how many
    // labels repeat.
    expect(screen.getAllByRole('checkbox').length).toBe(COLUMNS.length - PINNED_COLUMNS.size)

    // Spot-check a couple of ordinary columns with unique labels.
    expect(screen.getByRole('checkbox', { name: 'Device column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Chain column' })).toBeTruthy()
  })

  it('defaults every checkbox to checked -- the shipped default stays all fifteen columns', async () => {
    render(FilterBar)
    await expandRow()

    expect(screen.getByRole('checkbox', { name: 'Device column' })).toHaveProperty('checked', true)
  })

  it('unchecking a column writes through to columnState, and is a reader preference -- not tied to any filter term', async () => {
    render(FilterBar)
    await expandRow()

    const device = screen.getByRole('checkbox', { name: 'Device column' })
    await fireEvent.click(device)
    flushSync()

    expect(columnState.isColumnVisible('device')).toBe(false)
    expect(appState.hasActiveFilters).toBe(false)
  })
})
