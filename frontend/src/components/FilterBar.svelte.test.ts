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
const { geoipState } = await import('../lib/geoip.svelte')
const { seenValuesState } = await import('../lib/seenValues.svelte')
const { presetState } = await import('../lib/presets.svelte')

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
    // `saved ▾` rides the box's right end from round 37 on ("saved
    // filters are the box's business") -- part of the box's own content,
    // hence part of what this reads back.
    expect(box?.textContent?.replace(/\s+/g, ' ').trim()).toBe('action:drop⌫port:445⌫ saved ▾')
    expect(screen.getByPlaceholderText('type a term, or click a value in a row')).toBeTruthy()
  })

  it('carries saved filters at the box\'s own right end, not beside it (round 37)', () => {
    render(FilterBar)
    const trigger = document.querySelector('.fbox .fsaved')
    expect(trigger?.textContent?.trim()).toBe('saved ▾')
  })

  it('opening saved filters does not also unfold the strip the box discloses', async () => {
    render(FilterBar)
    await fireEvent.click(document.querySelector('.fbox .fsaved') as HTMLElement)
    flushSync()
    expect(document.querySelector('.fpmenu')).toBeTruthy()
    // The box's own "click inside opens the strip" handler must not have
    // fired: reaching for a saved filter is not reaching for the fields.
    expect(document.querySelector('.bar.thin')).toBeNull()
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

  it('the strip stays open when its own "clear" button empties the last active filter (#963)', async () => {
    // A click that removes its own target from the DOM as part of its
    // handler (resetFilters() makes hasActiveFilters false, which is
    // what tf-clear itself is gated on) used to read as a click outside
    // the strip once e.target was detached -- onWindowClick's own
    // barEl.contains(e.target) check went false for a click that never
    // left the strip, and folded it as a side effect of clearing.
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(FilterBar)
    const box = getBox()
    await expandRow()
    expect(screen.getByLabelText('Device')).toBeTruthy()

    await fireEvent.click(screen.getByLabelText('Clear all filters'))
    flushSync()
    expect(appState.hasActiveFilters).toBe(false)
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

    // #729's "Columns" field is not one of these any more (#710), and
    // #1197 moved its desktop "columns ▸" toggle off this strip entirely
    // -- it lives on Whisper.svelte's own hand now (Whisper.svelte.test.ts
    // covers it), so there is no trace of it left to assert here.
    // Direct children of the strip: #1191 captions the three controls
    // inside each address group with the same micro-label, and those are
    // that group's business, not the strip's field order (they have
    // their own test at the foot of this file).
    const labels = Array.from(document.querySelectorAll('.bar.thin > .fb-field > .fb-label')).map(
      (el) => el.textContent,
    )
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

// #1191: SOURCE and DESTINATION each carried three unlabelled controls,
// and the middle one was a bare underline with nothing saying what it
// took. The aria-labels had said scope/name-IP-or-CIDR/country all
// along; the owner's ruling makes them visible and keeps the text
// field's hint on desktop as well as on phones.
describe('FilterBar, the source and destination captions (#1191)', () => {
  it('captions the three controls in each address group', async () => {
    render(FilterBar)
    await expandRow()

    const groups = Array.from(document.querySelectorAll('.addr-group'))
    expect(groups.length).toBe(2)
    for (const group of groups) {
      const captions = Array.from(group.querySelectorAll('.fb-label')).map((el) => el.textContent?.trim())
      expect(captions).toEqual(['Scope', 'Name, IP or CIDR', 'Country'])
    }
  })

  it('keeps the address query hint on screen at desktop width', async () => {
    render(FilterBar)
    await expandRow()

    expect(screen.getByLabelText('Source — name, IP or CIDR').getAttribute('placeholder')).toBe('name, IP or CIDR')
    expect(screen.getByLabelText('Destination — name, IP or CIDR').getAttribute('placeholder')).toBe(
      'name, IP or CIDR',
    )
  })
})

// #1198: a bare empty country select was indistinguishable from "no
// public traffic yet" -- this disabled row says why instead. Both tests
// set geoipState.enabled directly rather than mocking fetchHealthz: the
// singleton's ensureLoaded() only ever runs its fetch once per module
// lifetime (see lib/geoip.svelte.ts's own loaded guard), so a real
// mount's onMount call is a same-value no-op here and never overwrites
// what the test sets.
describe("FilterBar, the country select's no-GeoIP row (#1198)", () => {
  afterEach(() => {
    geoipState.enabled = null
  })

  it('shows a disabled explainer row on both country selects with no database configured', async () => {
    geoipState.enabled = false
    render(FilterBar)
    await expandRow()

    for (const label of ['Source country', 'Destination country']) {
      const select = screen.getByLabelText(label) as HTMLSelectElement
      const opt = Array.from(select.options).find((o) => o.textContent === 'no GeoIP database — see docs ▸')
      expect(opt).toBeTruthy()
      expect(opt?.disabled).toBe(true)
    }
  })

  it('omits the row once a database is configured', async () => {
    geoipState.enabled = true
    render(FilterBar)
    await expandRow()

    const select = screen.getByLabelText('Source country') as HTMLSelectElement
    expect(Array.from(select.options).some((o) => o.textContent?.includes('no GeoIP database'))).toBe(false)
  })
})

// #1226: Proto and Interface were the only two controls in the strip
// with no list behind them -- free-text boxes an operator typed `tcp`
// into and hoped. They are now pickers over what this instance has
// actually seen, which is a store and an endpoint away (internal/seen,
// GET /api/seen-values), read here through seenValuesState.
//
// The load-bearing test is the last one. A picker that refused an unseen
// value would be worse than the box it replaced: a filter you cannot set
// up until the traffic arrives cannot be used to watch for traffic that
// has not arrived, which is most of what an operator wants one for. So
// the list is a suggestion, never a closed set -- a <datalist> combo,
// the same idiom the watchers station's scope boxes already use, not a
// <select>.
//
// mount's ensureLoaded() call cannot reach a server here and is caught,
// so what these tests put on the state is what the strip draws.
describe('FilterBar, the Proto and Interface pickers (#1226)', () => {
  afterEach(() => {
    seenValuesState.reset()
  })

  function optionsOf(id: string): string[] {
    const list = document.getElementById(id)
    expect(list?.tagName.toLowerCase()).toBe('datalist')
    return Array.from(list?.querySelectorAll('option') ?? []).map((o) => (o as HTMLOptionElement).value)
  }

  it('offers the protocols this instance has actually seen, not a hardcoded set', async () => {
    seenValuesState.proto = ['udp', 'tcp', 'gre']
    render(FilterBar)
    await expandRow()

    const input = screen.getByLabelText('Protocol') as HTMLInputElement
    expect(input.getAttribute('list')).toBe('fb-seen-protos')
    expect(optionsOf('fb-seen-protos')).toEqual(['udp', 'tcp', 'gre'])
  })

  it('offers one interface list for both directions, because there is one interface filter', async () => {
    seenValuesState.interfaces = ['ether1', 'bridge-lan', 'wg0']
    render(FilterBar)
    await expandRow()

    const input = screen.getByLabelText('Interface') as HTMLInputElement
    expect(input.getAttribute('list')).toBe('fb-seen-interfaces')
    expect(optionsOf('fb-seen-interfaces')).toEqual(['ether1', 'bridge-lan', 'wg0'])
  })

  it('leaves both boxes usable on a fresh instance that has seen nothing yet', async () => {
    render(FilterBar)
    await expandRow()

    expect(optionsOf('fb-seen-protos')).toEqual([])
    expect(optionsOf('fb-seen-interfaces')).toEqual([])

    const proto = screen.getByLabelText('Protocol') as HTMLInputElement
    expect(proto.disabled).toBe(false)
    expect(proto.tagName.toLowerCase()).toBe('input')
  })

  it('still accepts a value that has never been seen -- typing is not restricted to the list', async () => {
    seenValuesState.proto = ['tcp', 'udp']
    seenValuesState.interfaces = ['ether1']
    render(FilterBar)
    await expandRow()

    // Neither value is in either list: this is the operator setting a
    // filter up before the traffic they are waiting for has arrived.
    await fireEvent.input(screen.getByLabelText('Protocol'), { target: { value: 'sctp' } })
    await fireEvent.input(screen.getByLabelText('Interface'), { target: { value: 'ether9' } })
    flushSync()

    expect(appState.filters.protocol).toBe('sctp')
    expect(appState.filters.interface).toBe('ether9')
  })
})

// #1246 (round 57, ratified): the box's third face. A field menu on
// focus, a value menu under the field picked, and a committed token that
// is the chip face two already drew -- same markup, same aria-label, same
// appState.filters. Nothing here is a second filter store, so every
// assertion below reads the filter back off appState.
//
// The strip's own named-field controls are untouched by all of this, and
// so is the mobile drawer: this face is the wide-screen box only.
describe('FilterBar, the token bar (#1246)', () => {
  function menu() {
    return document.querySelector('.token-menu')
  }

  function menuNames(): string[] {
    return Array.from(document.querySelectorAll('.token-menu .tm-name')).map((n) => n.textContent?.trim() ?? '')
  }

  function pick(label: string) {
    const item = Array.from(document.querySelectorAll('.token-menu .tm-item')).find(
      (el) => el.querySelector('.tm-name')?.textContent?.trim() === label,
    )
    expect(item).toBeTruthy()
    return fireEvent.click(item as HTMLElement)
  }

  // The menu opens on focus, which a click into the box also causes.
  async function focusBox() {
    await fireEvent.focus(getBox())
    flushSync()
  }

  // Once a token commits the menu gets out of the way, with the caret
  // still in the box -- so a second token starts from a click, the
  // pointer's own way back in.
  async function clickBox() {
    await fireEvent.click(getBox())
    flushSync()
  }

  afterEach(() => {
    seenValuesState.reset()
    for (const p of [...presetState.presets]) presetState.remove(p.name)
  })

  it('opens the field menu on focus, listing the eight token fields', async () => {
    render(FilterBar)
    expect(menu()).toBeNull()

    await focusBox()
    expect(menu()).toBeTruthy()
    expect(menuNames()).toEqual(['device', 'action', 'chain', 'proto', 'interface', 'port', 'source', 'destination'])
  })

  it('shortens the action hint to a count, and leaves the full list to the value menu (verdict 2)', async () => {
    render(FilterBar)
    await focusBox()

    const action = Array.from(document.querySelectorAll('.token-menu .tm-item')).find(
      (el) => el.querySelector('.tm-name')?.textContent?.trim() === 'action',
    )
    expect(action?.querySelector('.tm-hint')?.textContent?.trim()).toBe('7 values')

    await pick('action')
    expect(menuNames()).toEqual(['Accept', 'Drop', 'Reject', 'Log', 'Marked (mangle)', 'Natted (NAT)', 'Unknown'])
  })

  it('shows a pending token and the field\'s own values once a field is picked', async () => {
    appState.devices = [
      { id: 'dev-cam', name: 'cam-porch' },
      { id: 'dev-nas', name: 'nas' },
    ] as unknown as (typeof appState)['devices']
    render(FilterBar)
    await focusBox()
    await pick('device')

    // Pending, not committed: the box must not claim a filter that is not
    // yet active.
    expect(document.querySelector('.chip.pending')?.textContent?.replace(/\s+/g, '')).toBe('device:')
    expect(appState.filters.device).toBe('')
    expect(menuNames()).toEqual(['cam-porch', 'nas'])
  })

  it('commits a value as the chip face two already draws, writing the same appState.filters', async () => {
    appState.devices = [{ id: 'dev-cam', name: 'cam-porch' }] as unknown as (typeof appState)['devices']
    render(FilterBar)
    await focusBox()
    await pick('device')
    await pick('cam-porch')
    flushSync()

    expect(appState.filters.device).toBe('dev-cam')
    expect(document.querySelector('.chip.pending')).toBeNull()
    const chip = screen.getByLabelText('Remove the device filter')
    expect(chip.tagName).toBe('BUTTON')
    expect(chip.parentElement?.textContent?.replace(/\s+/g, '')).toBe('device:cam-porch⌫')
  })

  it('removes a token from its own chip, the way the strip already does', async () => {
    appState.devices = [{ id: 'dev-cam', name: 'cam-porch' }] as unknown as (typeof appState)['devices']
    appState.filters = { ...emptyFilters(), device: 'dev-cam', action: 'drop' }
    render(FilterBar)

    await fireEvent.click(screen.getByLabelText('Remove the device filter'))
    flushSync()
    expect(appState.filters.device).toBe('')
    expect(appState.filters.action).toBe('drop')
  })

  it('offers proto and interface only what this instance has really seen (verdict 1)', async () => {
    seenValuesState.proto = ['tcp', 'udp']
    render(FilterBar)
    await focusBox()
    await pick('proto')

    expect(menuNames()).toEqual(['tcp', 'udp'])
    await pick('udp')
    flushSync()
    expect(appState.filters.protocol).toBe('udp')
  })

  it('takes a port as typed text, committed on Enter', async () => {
    render(FilterBar)
    await focusBox()
    await pick('port')

    const box = getBox()
    await fireEvent.input(box, { target: { value: '8291' } })
    flushSync()
    // A pending value is not the rule search: free text is untouched
    // while a field is waiting for its value.
    expect(appState.filters.rule).toBe('')

    await fireEvent.keyDown(box, { key: 'Enter' })
    flushSync()
    expect(appState.filters.port).toBe('8291')
    expect(document.querySelector('.chip.pending')).toBeNull()
  })

  it('commits a side to ONE composite token, however many of its parts are picked', async () => {
    appState.events = [
      evt({ id: 1, sourceIp: '185.220.101.34', srcIp: '185.220.101.34', srcCountry: 'DE' }),
    ] as unknown as (typeof appState)['events']
    render(FilterBar)
    await focusBox()
    await pick('source')

    expect(menuNames()).toEqual(['internal', 'external', '🇩🇪 DE'])
    await pick('external')
    flushSync()
    expect(appState.filters.srcScope).toBe('external')

    // A second part of the same side lands in the same chip, not a
    // second one -- sideValue() joins what is set.
    await clickBox()
    await pick('source')
    await pick('🇩🇪 DE')
    flushSync()
    expect(appState.filters.srcCountry).toBe('DE')
    expect(document.querySelectorAll('.chip').length).toBe(1)
    expect(screen.getByLabelText('Remove the source filter').parentElement?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
      'source:external · DE⌫',
    )
  })

  it('takes a typed address for a side on Enter, into the same composite token', async () => {
    render(FilterBar)
    await focusBox()
    await pick('destination')

    const box = getBox()
    await fireEvent.input(box, { target: { value: '10.0.40.5' } })
    await fireEvent.keyDown(box, { key: 'Enter' })
    flushSync()
    expect(appState.filters.dstQuery).toBe('10.0.40.5')
    expect(appState.filters.rule).toBe('')
  })

  it('never swallows plain typing -- it lands in free text, beside the tokens', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(FilterBar)
    await focusBox()

    const box = getBox()
    await fireEvent.input(box, { target: { value: 'iot-to-lan' } })
    flushSync()
    expect(appState.filters.rule).toBe('iot-to-lan')
    expect(appState.filters.action).toBe('drop')
  })

  it('enters the menu on ArrowDown with the first item focused, and picks it on Enter', async () => {
    render(FilterBar)
    await focusBox()
    const box = getBox()

    await fireEvent.keyDown(box, { key: 'ArrowDown' })
    flushSync()
    const focused = document.querySelector('.token-menu .tm-item.focused')
    expect(focused?.querySelector('.tm-name')?.textContent?.trim()).toBe('device')
    expect(focused?.getAttribute('aria-selected')).toBe('true')

    await fireEvent.keyDown(box, { key: 'Enter' })
    flushSync()
    expect(document.querySelector('.chip.pending')?.textContent?.replace(/\s+/g, '')).toBe('device:')
  })

  it('closes the menu on Escape', async () => {
    render(FilterBar)
    await focusBox()
    expect(menu()).toBeTruthy()

    await fireEvent.keyDown(window, { key: 'Escape' })
    flushSync()
    expect(menu()).toBeNull()
  })

  it('deletes the last token on Backspace in an empty free-text box', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop', chain: 'input' }
    render(FilterBar)
    await focusBox()

    await fireEvent.keyDown(getBox(), { key: 'Backspace' })
    flushSync()
    expect(appState.filters.chain).toBe('')
    expect(appState.filters.action).toBe('drop')
  })

  it('leaves the free-text term alone -- Backspace takes the last token, never the rule search', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop', rule: 'iot-to-lan' }
    render(FilterBar)
    // The box is not empty: it holds the rule search, so Backspace is
    // editing that character-by-character and reaches no token at all.
    await fireEvent.keyDown(getBox(), { key: 'Backspace' })
    flushSync()
    expect(appState.filters.rule).toBe('iot-to-lan')
    expect(appState.filters.action).toBe('drop')
  })

  it('never reaches past a half-typed value to the last token (verdict 3)', async () => {
    appState.filters = { ...emptyFilters(), action: 'drop' }
    render(FilterBar)
    await focusBox()
    await pick('port')

    const box = getBox()
    await fireEvent.input(box, { target: { value: '829' } })
    await fireEvent.keyDown(box, { key: 'Backspace' })
    flushSync()
    // The character is the browser's to delete; the token stands.
    expect(appState.filters.action).toBe('drop')
    expect(document.querySelector('.chip.pending')).toBeTruthy()
  })

  it('applies a saved filter as individually removable tokens, not one opaque named one', async () => {
    presetState.save('WAN scans', { ...emptyFilters(), action: 'drop', chain: 'input', srcCountry: 'DE' })
    render(FilterBar)

    await fireEvent.click(document.querySelector('.fbox .fsaved') as HTMLElement)
    flushSync()
    await fireEvent.click(document.querySelector('.fpname') as HTMLElement)
    flushSync()

    expect(screen.getByLabelText('Remove the action filter')).toBeTruthy()
    expect(screen.getByLabelText('Remove the chain filter')).toBeTruthy()
    expect(screen.getByLabelText('Remove the source filter')).toBeTruthy()
    expect(screen.queryByText('WAN scans', { selector: '.chip' })).toBeNull()

    await fireEvent.click(screen.getByLabelText('Remove the chain filter'))
    flushSync()
    expect(appState.filters.chain).toBe('')
    expect(appState.filters.action).toBe('drop')
  })
})
