// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import type { ClientEvent, Flag } from '../lib/types'
import { emptyFilters } from '../lib/types'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { groupModeState } from '../lib/groupMode.svelte'
import { flagsState } from '../lib/flags.svelte'
import { fallState } from '../lib/fall.svelte'
import { MAX_RENDERED_ROWS } from '../lib/constants'
import { COLUMNS, PINNED_COLUMNS, columnState } from '../lib/columns.svelte'
// Vite's `?raw` import, the same device Topography.svelte.test.ts uses
// for its own CSS-token assertions -- not a Node fs read, so this stays
// type-checkable under the browser-only app tsconfig. Needed below
// because vitest.config.ts leaves `test.css` at its default `false`:
// jsdom never applies this component's stylesheet, so a
// getComputedStyle() assertion on position/overflow would pass no
// matter what the rule actually said. Reading the source text is the
// only way to prove the sticky head's supporting CSS, not just assume it.
import componentSource from './LiveTable.svelte?raw'

// jsdom (unlike a real browser) has no window.matchMedia -- LiveTable
// pulls in lib/viewport.svelte.ts, whose ViewportState singleton calls
// it at module-load time. Polyfilled here, before the dynamic import
// below, rather than at the top of a static `import LiveTable from
// './LiveTable.svelte'`: static imports are hoisted and evaluate before
// any of this file's own top-level code runs, so a polyfill after a
// static import would already be too late.
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

// Same gap as matchMedia above -- jsdom has no ResizeObserver, and
// LiveTable's own column-resize-handle measurement observes gridEl with
// one. A no-op stub is enough: this test isn't exercising column resize.
if (typeof ResizeObserver === 'undefined') {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}

const { default: LiveTable } = await import('./LiveTable.svelte')

function makeEvent(raw: string, overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: Math.random(),
    time: '2026-08-08T12:00:00Z',
    deviceId: 'router1',
    sourceIp: '203.0.113.10',
    action: 'accept',
    ruleLabel: 'test-rule',
    chain: 'input',
    raw,
    receivedAt: Date.now(),
    ...overrides,
  }
}

beforeEach(() => {
  appState.autoscroll = true
  appState.paused = false
  appState.events = []
  appState.filters = emptyFilters()
  // Now module-level rather than component-local (see appState.frozenPool),
  // so it survives unmount -- and would leak between tests without this.
  appState.frozenPool = null
  // Same reasoning -- a test that sets this and fails before its own
  // cleanup would otherwise leak a false "fetch failed" empty-state
  // message into whichever test runs next (issue #373).
  appState.fetchFailed = false
  // Settled by default -- the "still loading" ghost-rows branch (#549)
  // only matters to the tests that specifically exercise it below, and
  // leaving this at its module default (false) would have it silently
  // apply to every other empty-buffer case in this file instead.
  appState.initialLoadDone = true
  appState.devices = []
  authState.role = ''
  // Reset centrally, not at the end of whichever test set it. A test body
  // that fails never reaches its own cleanup line, and the next test then
  // renders in grouped mode and fails for a reason that has nothing to do
  // with what it is checking -- one real failure reported as two.
  groupModeState.enabled = false
  // flagsState is a module-level singleton -- would otherwise leak a
  // flag from one test's fixture into the next's unflagged-row assertion.
  flagsState.list = []
  // Same for fallState, which the foot line's dark-boundary fact reads
  // (#691). Seeded with one boundary so LiveTable's own mount-time
  // "fetch the rule tables if nobody has" never fires in jsdom -- the
  // reactive read is what these tests are about, not the fetch.
  fallState.boundaries = [
    {
      key: 'forward|lan|wan',
      chain: 'forward',
      inInterface: 'lan',
      outInterface: 'wan',
      srcAddressList: 'lan',
      label: 'lan → wan',
      coverage: 'observed',
      epithet: '',
    },
  ]
  // columnState is a module-level singleton, shared with
  // columns.svelte.test.ts and FilterBar.svelte.test.ts -- reset to the
  // shipped default (#729: all fifteen visible) so a toggle from one test
  // can't leak into the next, the same hygiene groupModeState/flagsState
  // above already get.
  columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
})

describe('LiveTable autoscroll-off freezing (issue #232)', () => {
  it('keeps showing the same rows while autoscroll is off, even as new events arrive', async () => {
    const e1 = makeEvent('event-one')
    const { container, rerender } = render(LiveTable, { props: { events: [e1] } })

    expect(container.querySelector('[title="event-one"]')).toBeTruthy()

    appState.autoscroll = false

    const e2 = makeEvent('event-two')
    await rerender({ events: [e1, e2] })

    // The view is frozen at the moment autoscroll turned off -- a new
    // event arriving afterward must not appear or otherwise disturb
    // what's rendered, matching #232's report that the live view kept
    // moving on its own with autoscroll off.
    expect(container.querySelector('[title="event-one"]')).toBeTruthy()
    expect(container.querySelector('[title="event-two"]')).toBeNull()
  })

  it('resumes showing the live set once autoscroll is turned back on', async () => {
    const e1 = makeEvent('event-one')
    const { container, rerender } = render(LiveTable, { props: { events: [e1] } })

    appState.autoscroll = false
    const e2 = makeEvent('event-two')
    await rerender({ events: [e1, e2] })
    expect(container.querySelector('[title="event-two"]')).toBeNull()

    appState.autoscroll = true
    await rerender({ events: [e1, e2] })

    expect(container.querySelector('[title="event-two"]')).toBeTruthy()
  })

  it('does not freeze anything while autoscroll stays on', async () => {
    const e1 = makeEvent('event-one')
    const { container, rerender } = render(LiveTable, { props: { events: [e1] } })

    const e2 = makeEvent('event-two')
    await rerender({ events: [e1, e2] })

    expect(container.querySelector('[title="event-two"]')).toBeTruthy()
  })

  it('holds the rendered window past MAX_RENDERED_ROWS, not just below it -- the actual reported symptom', () => {
    // #232's report only manifests once the sliding window itself starts
    // evicting rows from the top (MAX_RENDERED_ROWS = 800): below that
    // threshold every event is rendered regardless of autoscroll, so a
    // 2-event fixture (as the tests above use) can't distinguish "frozen"
    // from "nothing to evict yet". Drives the global feed (no `events`
    // prop) since that's the code path #232 actually reported against.
    const initial = Array.from({ length: MAX_RENDERED_ROWS + 10 }, (_, i) => makeEvent(`initial-${i}`))
    appState.events = initial

    const { container } = render(LiveTable)
    flushSync()

    const oldestVisibleBeforeFreeze = container.querySelector('[title="initial-10"]')
    const evictedBeforeFreeze = container.querySelector('[title="initial-9"]')
    expect(oldestVisibleBeforeFreeze).toBeTruthy()
    expect(evictedBeforeFreeze).toBeNull()

    appState.autoscroll = false
    flushSync()

    // Push well past the window with autoscroll off -- if the view
    // weren't frozen, every one of these would evict another row from
    // the top exactly as #232 described.
    const overflow = Array.from({ length: 50 }, (_, i) => makeEvent(`overflow-${i}`))
    appState.events = [...appState.events, ...overflow]
    flushSync()

    expect(container.querySelector('[title="initial-10"]')).toBeTruthy()
    expect(container.querySelector('[title="overflow-0"]')).toBeNull()
    expect(container.querySelector('[title="overflow-49"]')).toBeNull()
  })

  it('re-derives the frozen snapshot when the filter set changes, within what was already frozen', () => {
    const matching = makeEvent('matches-filter', { action: 'accept' })
    const nonMatching = makeEvent('excluded-by-filter', { action: 'drop' })
    appState.events = [matching, nonMatching]

    const { container } = render(LiveTable)
    flushSync()

    expect(container.querySelector('[title="matches-filter"]')).toBeTruthy()
    expect(container.querySelector('[title="excluded-by-filter"]')).toBeTruthy()

    appState.autoscroll = false
    flushSync()

    // Changing the filter while frozen must still narrow the visible
    // rows -- freezing is about new *arrivals* not disturbing the view,
    // not about the user's own filter change going unreflected.
    appState.filters = { ...emptyFilters(), action: 'accept' }
    flushSync()

    expect(container.querySelector('[title="matches-filter"]')).toBeTruthy()
    expect(container.querySelector('[title="excluded-by-filter"]')).toBeNull()

    // An event arriving *after* the freeze began must still never appear,
    // even though the filter has since changed -- the frozen pool itself
    // was captured once, at freeze time, and never grows. Clearing the
    // filter again re-widens only within that same frozen pool.
    const arrivedAfterFreeze = makeEvent('arrived-after-freeze', { action: 'accept' })
    appState.events = [...appState.events, arrivedAfterFreeze]
    appState.filters = emptyFilters()
    flushSync()

    expect(container.querySelector('[title="matches-filter"]')).toBeTruthy()
    expect(container.querySelector('[title="excluded-by-filter"]')).toBeTruthy()
    expect(container.querySelector('[title="arrived-after-freeze"]')).toBeNull()
  })

  it('does not freeze a caller-supplied table (honorAutoscroll=false)', async () => {
    const e1 = makeEvent('event-one')
    const { container, rerender } = render(LiveTable, {
      props: { events: [e1], honorAutoscroll: false },
    })

    appState.autoscroll = false
    const e2 = makeEvent('event-two')
    await rerender({ events: [e1, e2], honorAutoscroll: false })

    // The global Autoscroll toggle must not freeze a table that has no
    // Autoscroll control of its own.
    expect(container.querySelector('[title="event-two"]')).toBeTruthy()
  })
})

describe('Group mode drawer consistency (issue #381)', () => {
  // Two events that share a group key but differ in rule label, so a rule
  // filter can narrow the group to one member while its drawer is open.
  function pair() {
    const shared = {
      srcIp: '198.51.100.7',
      dstIp: '203.0.113.9',
      dstPort: 22,
      protocol: 'TCP',
      action: 'drop' as const,
    }
    return [
      makeEvent('member-a', { id: 1, ruleLabel: 'test-rule', ...shared }),
      makeEvent('member-b', { id: 2, ruleLabel: 'other', ...shared }),
    ]
  }

  it('drops the drawer when a filter narrows an open group to one member', () => {
    groupModeState.enabled = true
    appState.events = pair()

    const { container } = render(LiveTable)
    flushSync()

    // Expand the group: head plus its two members.
    const toggle = container.querySelector<HTMLElement>('[aria-expanded]')
    expect(toggle).toBeTruthy()
    toggle!.click()
    flushSync()
    expect(container.querySelectorAll('.row').length).toBe(3)

    // Narrow to one member. The group's count falls to 1, so the toggle
    // goes away -- and before #381 the drawer did not, leaving the single
    // remaining event rendered twice, once as a child of itself, with no
    // control left to collapse it.
    appState.filters = { ...emptyFilters(), rule: 'other' }
    flushSync()

    const rows = container.querySelectorAll('.row')
    expect(rows.length).toBe(1)
    expect(container.querySelectorAll('.row.member').length).toBe(0)
  })
})

describe('Clear releases the freeze snapshot (issue #381)', () => {
  it('empties the table when Clear is pressed with autoscroll off', async () => {
    appState.events = [makeEvent('e-one', { id: 1 }), makeEvent('e-two', { id: 2 })]

    const { container } = render(LiveTable)
    flushSync()
    expect(container.querySelectorAll('.row').length).toBe(2)

    // Freeze, per #232.
    appState.autoscroll = false
    flushSync()
    expect(container.querySelectorAll('.row').length).toBe(2)

    // Clear used to empty the buffer and leave the screen untouched: the
    // frozen pool still held both rows, with nothing on screen to explain
    // why and no way out but toggling autoscroll back on.
    appState.clearBuffer()
    flushSync()

    expect(appState.events.length).toBe(0)
    expect(appState.frozenPool).toEqual([])
    expect(container.querySelectorAll('.row').length).toBe(0)
  })
})

describe('Group expansion does not outlive its group (issue #381 item 3)', () => {
  function pairFor(ip: string) {
    const shared = { srcIp: ip, dstIp: '203.0.113.9', dstPort: 22, protocol: 'TCP', action: 'drop' as const }
    return [
      makeEvent(`${ip}-a`, { id: Math.random(), ruleLabel: 'test-rule', ...shared }),
      makeEvent(`${ip}-b`, { id: Math.random(), ruleLabel: 'test-rule', ...shared }),
    ]
  }

  it('renders a recurring group collapsed after its events left the window', () => {
    groupModeState.enabled = true
    appState.events = pairFor('198.51.100.7')

    const { container } = render(LiveTable)
    flushSync()

    // Expand it.
    container.querySelector<HTMLElement>('[aria-expanded]')!.click()
    flushSync()
    expect(container.querySelectorAll('.row').length).toBe(3)

    // Its traffic leaves the buffer entirely -- the shape of events
    // aging out or a filter excluding them.
    appState.events = pairFor('203.0.113.77')
    flushSync()

    // The old connection recurs. Before the prune, its stale open state
    // survived the absence and the group rendered pre-expanded --
    // an expansion the operator performed against events that no longer
    // exist, silently reapplied to different ones.
    appState.events = pairFor('198.51.100.7')
    flushSync()

    const toggle = container.querySelector('[aria-expanded]')
    expect(toggle?.getAttribute('aria-expanded')).toBe('false')
    expect(container.querySelectorAll('.row.member').length).toBe(0)
  })
})
describe('EventRow token interaction (#439)', () => {
  const HOST_IP = '203.0.113.77'
  const HOST_NAME = 'nas.example.internal'
  const RAW_RULE = 'raw-rule-label-42'
  const FRIENDLY_RULE = 'Block known-bad scanners'

  // The label/raw gap (b) needs a row where a friendly name is actively
  // resolved over the raw value -- otherwise "copies the raw value"
  // can't be told apart from "copies whatever text is shown".
  function renderLabelledRow() {
    const e = makeEvent('token-row', {
      srcIp: HOST_IP,
      srcHostName: HOST_NAME,
      ruleLabel: RAW_RULE,
      ruleName: FRIENDLY_RULE,
    })
    const utils = render(LiveTable, { props: { events: [e] } })
    flushSync()
    return utils
  }

  function selectTextIn(el: Element) {
    const range = document.createRange()
    range.selectNodeContents(el)
    const sel = window.getSelection()
    sel?.removeAllRanges()
    sel?.addRange(range)
  }

  function clearSelection() {
    window.getSelection()?.removeAllRanges()
  }

  // (a) row text is selectable.
  //
  // There's no `user-select: none` anywhere in app.css to assert
  // against removing (investigation found none -- the actual cause was
  // every token being a <button>, and no browser makes button content
  // selectable regardless of CSS). So the real, checkable condition is
  // the element swap this issue's fix makes: a plain element with
  // role="button"/tabindex, not a <button>.
  it('renders row tokens as plain selectable elements, not <button>s', () => {
    const { container } = renderLabelledRow()
    const addrToken = container.querySelector('.addr-btn')
    expect(addrToken).toBeTruthy()
    expect(addrToken?.tagName).not.toBe('BUTTON')
    expect(addrToken?.getAttribute('role')).toBe('button')
    expect(addrToken?.getAttribute('tabindex')).toBe('0')
  })

  // (b) copy glyph present on tokens, writes the RAW value.
  it('shows a copy control per token that writes the RAW value behind a resolved label', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    })

    const { container } = renderLabelledRow()

    expect(container.querySelectorAll('.copy-btn').length).toBeGreaterThan(0)

    // The source-address cell displays the resolved hostname, not the
    // IP -- exactly the gap #439 exists to bridge -- but its copy
    // control must still write the IP.
    const addrCell = container.querySelector('.cell.addr')
    expect(addrCell?.textContent).toContain(HOST_NAME)
    expect(addrCell?.textContent).not.toContain(HOST_IP)

    const addrCopyBtn = addrCell?.querySelector('.copy-btn') as HTMLButtonElement
    expect(addrCopyBtn).toBeTruthy()
    await fireEvent.click(addrCopyBtn)
    expect(writeText).toHaveBeenCalledWith(HOST_IP)

    // Same story for the rule cell: the friendly name is what's shown,
    // the raw rule label is what gets copied.
    const ruleCell = container.querySelector('.cell.rule')
    expect(ruleCell?.textContent).toContain(FRIENDLY_RULE)
    const ruleCopyBtn = ruleCell?.querySelector('.copy-btn') as HTMLButtonElement
    expect(ruleCopyBtn).toBeTruthy()
    await fireEvent.click(ruleCopyBtn)
    expect(writeText).toHaveBeenCalledWith(RAW_RULE)
  })

  // (c) click-with-selection does not filter; click-without-selection does.
  it('suppresses the click-to-filter action once a drag has left a selection behind, but not a plain click', async () => {
    const { container } = renderLabelledRow()
    const addrToken = container.querySelector('.addr-btn') as HTMLElement
    expect(addrToken).toBeTruthy()

    // A drag-to-select: press, select some of the row's own text,
    // release. The release must not be read as a click -- the
    // selection it left behind is what the gesture actually did.
    selectTextIn(addrToken)
    await fireEvent.mouseDown(addrToken)
    await fireEvent.mouseUp(addrToken)
    expect(appState.filters.srcQuery).toBe('')

    // A plain click -- no selection left behind -- still filters,
    // unchanged from before this issue.
    clearSelection()
    await fireEvent.mouseDown(addrToken)
    await fireEvent.mouseUp(addrToken)
    expect(appState.filters.srcQuery).toBe(HOST_IP)
  })

  it('keeps keyboard activation working for filter cells (Enter), now that they are not <button>s', async () => {
    const { container } = renderLabelledRow()
    const addrToken = container.querySelector('.addr-btn') as HTMLElement
    await fireEvent.keyDown(addrToken, { key: 'Enter' })
    expect(appState.filters.srcQuery).toBe(HOST_IP)
  })
})

// Row order itself (as opposed to scroll position, which jsdom has no real
// layout for -- see live-newest-first.mjs for the pixel-level proof) is
// exactly what jsdom CAN pin: the `{#each}` iterates a plain array, and
// array order doesn't depend on layout at all. Deliberately not asserting
// anything about scrollTop/scrollHeight here -- jsdom reports 0 for both
// regardless of what the component does, which would make a "scrolled to
// the top" assertion pass whether or not the code actually did that.
describe('LiveTable newest-at-top ordering (issue #363)', () => {
  function rowTitles(container: HTMLElement): (string | null)[] {
    return Array.from(container.querySelectorAll('.grid .row')).map((el) => el.getAttribute('title'))
  }

  it('renders the newest event first and the oldest last', () => {
    const e1 = makeEvent('event-one')
    const e2 = makeEvent('event-two')
    const e3 = makeEvent('event-three')
    // Arrival order is oldest first, matching how appState.events/the
    // frozen pool actually accumulate (push at the end) -- the component
    // must invert this for display, not receive it pre-inverted.
    const { container } = render(LiveTable, { props: { events: [e1, e2, e3] } })
    flushSync()

    expect(rowTitles(container)).toEqual(['event-three', 'event-two', 'event-one'])
  })

  it('puts the most recently *started* group at the top, and a repeat hit does not move an older group down there instead', () => {
    groupModeState.enabled = true

    const connA = { srcIp: '10.0.0.1', dstIp: '10.0.0.2', dstPort: 80, protocol: 'tcp', action: 'accept' as const }
    const connB = { srcIp: '10.0.0.3', dstIp: '10.0.0.4', dstPort: 22, protocol: 'tcp', action: 'accept' as const }

    const a1 = makeEvent('A-first', connA)
    const b1 = makeEvent('B-first', connB)
    // Arrives last, but it's a repeat of connection A, which arrived
    // first -- groupEvents anchors a group on its first arrival (see
    // grouping.ts), so this must grow group A's count in place rather
    // than pulling group A back to the top ahead of group B.
    const a2 = makeEvent('A-second', connA)

    const { container } = render(LiveTable, { props: { events: [a1, b1, a2] } })
    flushSync()

    // Group B started more recently than group A (b1 arrived after a1),
    // so newest-at-top puts B's row above A's -- even though A was the
    // one that was just hit again.
    expect(rowTitles(container)).toEqual(['B-first', 'A-first'])

    const countCell = container.querySelector('[title="A-first"] .count')
    expect(countCell?.textContent?.trim()).toBe('2')
  })
})

// Issue #373: a failed refetch (or initial load) used to leave `events`
// exactly as it was, with nothing recording that the fetch itself failed --
// so an empty (or stale-and-non-matching) buffer rendered the same "No
// events match the current filters" message a real, confirmed-empty query
// would. appState.fetchFailed (set by loadInitial()/refetchWithFilters() on
// rejection -- see state.svelte.ts) is what LiveTable now checks first, so a
// failure reads as a failure instead of a false negative.
describe('LiveTable distinguishes a failed fetch from a confirmed empty result (issue #373)', () => {
  it('shows a failure message, not "No events match", when the last fetch failed and the buffer is empty', () => {
    appState.fetchFailed = true
    appState.events = []

    const { container } = render(LiveTable, {})
    flushSync()

    const empty = container.querySelector('.empty')
    expect(empty).not.toBeNull()
    expect(empty?.textContent).not.toContain('No events match the current filters.')
    expect(empty?.textContent?.toLowerCase()).toMatch(/could not load|failed|error/)
  })

  it('still shows the genuine "no matches" message once the buffer is real and the fetch is not flagged as failed', () => {
    appState.fetchFailed = false
    appState.events = [makeEvent('present')]
    appState.filters = { ...emptyFilters(), rule: 'no-such-rule-anywhere' }

    const { container } = render(LiveTable, {})
    flushSync()

    const empty = container.querySelector('.empty')
    expect(empty?.textContent).toContain('No events match the current filters.')
  })
})

// #549's chrome Loading state ("shell plus ghost rows -- never a spinner
// page") and its first-run empty state ("point the operator at Admin ▸
// Run setup…"). Both hang off the same empty buffer that #373's tests
// above cover -- these add the two readings #373 predates: "still
// loading" and "confirmed empty because no device has ever sent
// anything".
describe('LiveTable Loading and first-run empty states (#549)', () => {
  it('shows ghost rows, not text, while the initial fetch has not settled yet', () => {
    appState.fetchFailed = false
    appState.events = []
    appState.initialLoadDone = false

    const { container } = render(LiveTable, {})
    flushSync()

    expect(container.querySelector('.ghost-rows')).not.toBeNull()
    expect(container.querySelector('.empty')).toBeNull()
  })

  it('does not show ghost rows once the fetch has settled, even with an empty result', () => {
    appState.fetchFailed = false
    appState.events = []
    appState.initialLoadDone = true

    const { container } = render(LiveTable, {})
    flushSync()

    expect(container.querySelector('.ghost-rows')).toBeNull()
    expect(container.querySelector('.empty')).not.toBeNull()
  })

  it('points an admin at Admin ▸ Run setup… once settled with no devices ever seen', () => {
    appState.fetchFailed = false
    appState.events = []
    appState.initialLoadDone = true
    appState.devices = []
    authState.role = 'admin'

    const { container } = render(LiveTable, {})
    flushSync()

    expect(container.querySelector('.empty')?.textContent).toMatch(/Admin ▸ Run setup…/)
  })

  it('tells a viewer to ask an administrator instead, rather than naming a control they cannot reach', () => {
    appState.fetchFailed = false
    appState.events = []
    appState.initialLoadDone = true
    appState.devices = []
    authState.role = 'user'

    const { container } = render(LiveTable, {})
    flushSync()

    const text = container.querySelector('.empty')?.textContent ?? ''
    expect(text).not.toMatch(/Run setup…/)
    expect(text.toLowerCase()).toMatch(/administrator/)
  })

  it('reads as an ordinary quiet buffer, not first run, once at least one device exists', () => {
    appState.fetchFailed = false
    appState.events = []
    appState.initialLoadDone = true
    appState.devices = [
      { id: 'r1', name: 'router1', configured: true, status: 'live', lastSeen: null, sourceIp: '10.0.0.1', eventCount: 0 },
    ] as unknown as (typeof appState)['devices']

    const { container } = render(LiveTable, {})
    flushSync()

    expect(container.querySelector('.empty')?.textContent).toContain('Waiting for events…')
  })
})

// #644's squared columns: TIME · ACTION · SOURCE · address · DESTINATION ·
// address · proto · port · RULE. The name columns show the resolved host
// name where one exists and the bare address (dim, country code beside it)
// where not; the address columns then show the raw IP only where the name
// column is showing a name. #644 moved device, chain, interfaces, src
// port, NAT and MAC into the detail sheet and off the row entirely; #717
// (owner ruling, 2026-08-31) restores all six as columns of their own
// (see columns.svelte.ts), so this describe block now covers fifteen
// columns, not nine -- the nine above are unchanged, and the restored
// six get their own describe block below.
describe('LiveTable squared columns (#644)', () => {
  it('shows a bare external source dim in the name column, an em dash in its address column', () => {
    const e = makeEvent('bare-source', {
      srcIp: '185.220.101.34',
      srcCountry: 'DE',
      dstIp: '10.0.40.5',
      dstHostName: 'nas',
    })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const nameCells = container.querySelectorAll('.cell.addr')
    const ipCells = container.querySelectorAll('.cell.ip')

    // Unnamed source: the address IS the name column's content (marked
    // bare so it renders dim), the country code rides beside it, and the
    // address column repeats nothing.
    expect(nameCells[0]?.textContent).toContain('185.220.101.34')
    expect(nameCells[0]?.textContent).toContain('DE')
    expect(nameCells[0]?.querySelector('.addr-btn')?.classList.contains('bare')).toBe(true)
    expect(ipCells[0]?.textContent?.trim()).toBe('—')

    // Named destination: the other way round.
    expect(nameCells[1]?.textContent).toContain('nas')
    expect(nameCells[1]?.textContent).not.toContain('10.0.40.5')
    expect(nameCells[1]?.querySelector('.addr-btn')?.classList.contains('bare')).toBe(false)
    expect(ipCells[1]?.textContent?.trim()).toBe('10.0.40.5')
  })

  // #685: the demo this issue was filed against has nothing named, so
  // every row shows the unnamed-fallback path the test above already
  // covers -- that data gap is #687's, not this one's. What #685 must
  // not do is touch the pairing logic itself, so this pins the *named*
  // side on both columns at once: a named host reads bright with its
  // address dim and right-aligned beside it, on source and destination
  // together, the way a real deployment with resolved names would
  // actually render.
  it('shows a named source and a named destination bright, each with its raw address dim beside it', () => {
    const e = makeEvent('named-pair', {
      srcIp: '10.0.10.2',
      srcHostName: 'tom-desktop',
      dstIp: '10.0.40.3',
      dstHostName: 'pihole',
    })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const nameCells = container.querySelectorAll('.cell.addr')
    const ipCells = container.querySelectorAll('.cell.ip')

    expect(nameCells[0]?.textContent).toContain('tom-desktop')
    expect(nameCells[0]?.textContent).not.toContain('10.0.10.2')
    expect(nameCells[0]?.querySelector('.addr-btn')?.classList.contains('bare')).toBe(false)
    expect(ipCells[0]?.textContent?.trim()).toBe('10.0.10.2')

    expect(nameCells[1]?.textContent).toContain('pihole')
    expect(nameCells[1]?.textContent).not.toContain('10.0.40.3')
    expect(nameCells[1]?.querySelector('.addr-btn')?.classList.contains('bare')).toBe(false)
    expect(ipCells[1]?.textContent?.trim()).toBe('10.0.40.3')
  })

  it('renders the destination port as the bare number, keeping any friendly name to the tooltip', () => {
    const e = makeEvent('port-row', { dstIp: '10.0.40.5', dstPort: 445, dstPortName: 'smb' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    // #717 restored Src port beside Source's own facts, ahead of
    // Destination -- both it and the destination Port column share the
    // .cell.port class (same font/layout treatment), so `:not(.srcport)`
    // is what actually picks out this row's *destination* port cell.
    const portCell = container.querySelector('.cell.port:not(.srcport)')
    expect(portCell?.textContent?.trim()).toBe('445')
    expect(portCell?.querySelector('.port-btn')?.getAttribute('title')).toContain('smb')
  })

  it('renders the timestamp with milliseconds', () => {
    const e = makeEvent('ms-row', { time: '2026-08-08T12:00:00.482Z' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelector('.cell.time')?.textContent).toMatch(/\.482/)
  })

  // #717 restored device/chain/src-port/MAC as columns too (see the
  // describe block below), but the sheet stays the row's one full-detail
  // surface regardless -- this pins that opening it still works and
  // still carries these fields, independent of whatever the row's own
  // columns show.
  it('opens the detail sheet from the row, still carrying device/chain/src-port/MAC/interfaces', async () => {
    const e = makeEvent('sheet-row', {
      srcIp: '10.0.20.11',
      srcPort: 49812,
      srcMac: 'AA:BB:CC:DD:EE:FF',
      dstIp: '10.0.40.5',
      dstPort: 445,
      protocol: 'tcp',
      inInterface: 'bridge-iot',
      outInterface: 'bridge1',
    })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelector('[role="dialog"]')).toBeNull()

    const timeBtn = container.querySelector('.time-btn') as HTMLElement
    expect(timeBtn).toBeTruthy()
    await fireEvent.mouseDown(timeBtn)
    await fireEvent.mouseUp(timeBtn)

    const sheet = container.querySelector('[role="dialog"]')
    expect(sheet).toBeTruthy()
    // makeEvent's default chain is 'input'; the device row resolves the
    // id itself since no device list is loaded in these tests.
    expect(sheet?.textContent).toContain('Chain')
    expect(sheet?.textContent).toContain('input')
    expect(sheet?.textContent).toContain('49812')
    expect(sheet?.textContent).toContain('AA:BB:CC:DD:EE:FF')
    expect(sheet?.textContent).toContain('bridge-iot')
    expect(sheet?.textContent).toContain('bridge1')
  })

  // NAT still reads on the action badge (#644) -- a natted event reads
  // exactly like an accept/drop one, just with its own badge colour --
  // *and*, since #717, also gets its own column with the translated
  // address (see the describe block below for that column's own
  // coverage). This only pins the badge half of that pair.
  it('shows NAT as the action badge', () => {
    const e = makeEvent('nat-row', {
      action: 'natted',
      chain: 'srcnat',
      srcIp: '10.0.10.2',
      natIp: '203.0.113.7',
      natPort: 51512,
    })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelector('.cell.action .badge-natted')).toBeTruthy()
  })

  // #644's own text: "Per-cell ⓘ buttons are removed entirely." That is
  // IpInvestigateButton (titled "Investigate {ip}") and
  // PortInvestigateButton (titled "What is port {port}?") specifically --
  // not RouterRuleButton, which keeps its rule-cell lookup trigger
  // untouched (see EventRow.svelte's own comment on natFilterKey). A
  // public source IP and a port with a commonPorts entry (443) are
  // exactly the two conditions that used to grow one of the retired
  // buttons.
  it('carries no per-cell ⓘ investigate button for the source IP or the port', () => {
    const e = makeEvent('no-investigate-row', {
      srcIp: '203.0.113.50',
      dstIp: '198.51.100.9',
      dstPort: 443,
    })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const titles = Array.from(container.querySelectorAll('[title]')).map((el) => el.getAttribute('title') ?? '')
    expect(titles.some((t) => t.startsWith('Investigate '))).toBe(false)
    expect(titles.some((t) => /^What is port \d+\?$/.test(t))).toBe(false)
  })
})

// #717 (owner ruling, 2026-08-31): the six columns #644 folded into
// EventDetailSheet -- device, chain, src port, MAC, interfaces, NAT --
// come back as columns of their own, threaded in beside the fact each
// belongs with (see columns.svelte.ts's own comment for where and why).
// The existing nine keep the coverage above unchanged; this covers only
// the six restored ones -- that each renders, reads off the same field
// the sheet already uses, and shows the table's em dash when the fact
// is absent.
describe('LiveTable restored columns (#717)', () => {
  it('shows the resolved device name in its own column, filtering to the device id on click', async () => {
    const e = makeEvent('device-row', { deviceId: 'router1' })
    appState.devices = [
      { id: 'router1', name: 'office-router', configured: true, status: 'live', lastSeen: null, sourceIp: '10.0.0.1', eventCount: 1 },
    ] as unknown as (typeof appState)['devices']

    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const deviceCell = container.querySelector('.cell.device')
    expect(deviceCell?.textContent).toContain('office-router')

    const deviceBtn = deviceCell?.querySelector('.device-btn') as HTMLElement
    await fireEvent.mouseDown(deviceBtn)
    await fireEvent.mouseUp(deviceBtn)
    expect(appState.filters.device).toBe('router1')
  })

  it('falls back to the device id when no device list has resolved a name for it', () => {
    const e = makeEvent('device-fallback-row', { deviceId: 'router9' })
    appState.devices = []

    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelector('.cell.device')?.textContent).toContain('router9')
  })

  it('shows the chain in its own column, filtering to it on click, and an em dash when absent', async () => {
    const e = makeEvent('chain-row', { chain: 'srcnat' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const chainCell = container.querySelector('.cell.chain') as HTMLElement
    expect(chainCell?.textContent?.trim()).toBe('srcnat')
    await fireEvent.mouseDown(chainCell)
    await fireEvent.mouseUp(chainCell)
    expect(appState.filters.chain).toBe('srcnat')

    const noChain = makeEvent('no-chain-row', { chain: '' })
    const { container: container2 } = render(LiveTable, { props: { events: [noChain] } })
    flushSync()
    expect(container2.querySelector('.cell.chain')?.textContent?.trim()).toBe('—')
  })

  it("shows the source's own port beside its address, distinct from the destination Port column", () => {
    const e = makeEvent('srcport-row', { srcIp: '10.0.10.2', srcPort: 51234, dstIp: '10.0.40.5', dstPort: 443 })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const srcPortCell = container.querySelector('.cell.port.srcport')
    expect(srcPortCell?.textContent?.trim()).toBe('51234')
    const dstPortCell = container.querySelector('.cell.port:not(.srcport)')
    expect(dstPortCell?.textContent?.trim()).toBe('443')
  })

  it('shows an em dash in the Src port column when the event has no source port', () => {
    const e = makeEvent('no-srcport-row', { srcIp: '10.0.10.2' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelector('.cell.port.srcport')?.textContent?.trim()).toBe('—')
  })

  it("shows the event's own srcMac in the MAC column, plain (not click-to-filter), and an em dash when absent", () => {
    const e = makeEvent('mac-row', { srcMac: 'AA:BB:CC:DD:EE:FF' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const macCell = container.querySelector('.cell.mac')
    expect(macCell?.textContent?.trim()).toBe('AA:BB:CC:DD:EE:FF')
    expect(macCell?.getAttribute('role')).not.toBe('button')

    const noMac = makeEvent('no-mac-row', {})
    const { container: container2 } = render(LiveTable, { props: { events: [noMac] } })
    flushSync()
    expect(container2.querySelector('.cell.mac')?.textContent?.trim()).toBe('—')
  })

  it('shows both interfaces joined by an arrow, and an em dash when neither is set', () => {
    const e = makeEvent('iface-row', { inInterface: 'bridge-iot', outInterface: 'bridge1' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const ifaceCell = container.querySelector('.cell.iface')
    expect(ifaceCell?.textContent).toContain('bridge-iot')
    expect(ifaceCell?.textContent).toContain('bridge1')

    const noIface = makeEvent('no-iface-row', {})
    const { container: container2 } = render(LiveTable, { props: { events: [noIface] } })
    flushSync()
    expect(container2.querySelector('.cell.iface')?.textContent?.trim()).toBe('—')
  })

  it('shows the translated address in its own NAT column, reading from natIp/natPort like the detail sheet does', () => {
    const e = makeEvent('nat-col-row', {
      action: 'natted',
      chain: 'srcnat',
      srcIp: '10.0.10.2',
      natIp: '203.0.113.7',
      natPort: 51512,
    })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const natCell = container.querySelector('.cell.nat')
    expect(natCell?.textContent).toContain('203.0.113.7:51512')
    expect(natCell?.classList.contains('has-value')).toBe(true)
  })

  it('shows an em dash in the NAT column when the event was not translated', () => {
    const e = makeEvent('no-nat-row', {})
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const natCell = container.querySelector('.cell.nat')
    expect(natCell?.textContent?.trim()).toBe('—')
    expect(natCell?.classList.contains('has-value')).toBe(false)
  })
})

// #685, superseded by #691's round-30 audit: a row on a flagged pathway
// carries *both* the full-row wash (the-whole.html's `tr.hl`) and a ⚑
// mark after the time (its `.rmk`). Round 29 drew only the wash, so #685
// took the shipped mark out; round 30 draws both, and the mark annotates
// the wash rather than replacing it. This pins the pair, and that the
// mark follows the time rather than preceding it -- ahead of the figures
// it breaks the left edge the tabular numerals line up on.
describe('Flagged pathway row wash and mark (#685, #691)', () => {
  function activeFlag(target: string): Flag {
    return {
      id: 'f1',
      type: 'port_scan',
      target,
      detail: '',
      count: 1,
      firstSeen: '2026-01-01T00:00:00Z',
      lastSeen: '2026-01-01T00:00:00Z',
      cleared: false,
    }
  }

  it('marks a row whose source carries an active flag, and leaves an ordinary row unmarked', () => {
    const flaggedSourceEvent = makeEvent('flagged-row', { srcIp: '203.0.113.9' })
    const ordinaryEvent = makeEvent('ordinary-row', { srcIp: '198.51.100.20' })
    flagsState.list = [activeFlag('203.0.113.9')]

    const { container } = render(LiveTable, { props: { events: [flaggedSourceEvent, ordinaryEvent] } })
    flushSync()

    const flaggedRow = container.querySelector('[title="flagged-row"]')
    const ordinaryRow = container.querySelector('[title="ordinary-row"]')
    expect(flaggedRow?.classList.contains('flagged')).toBe(true)
    expect(ordinaryRow?.classList.contains('flagged')).toBe(false)

    // The mark rides with the wash, and only on the flagged row.
    const mark = flaggedRow?.querySelector('.rmk')
    expect(mark).not.toBeNull()
    expect(mark?.textContent).toBe('\u2691')
    expect(ordinaryRow?.querySelector('.rmk')).toBeNull()

    // ...and it follows the time, rather than pushing the figures right.
    const timeCell = flaggedRow?.querySelector('.cell.time')
    expect(timeCell?.textContent?.trimStart().startsWith('\u2691')).toBe(false)
    expect(timeCell?.textContent?.trimEnd().endsWith('\u2691')).toBe(true)
  })

  it('does not mark a row whose flag has been cleared', () => {
    const e = makeEvent('cleared-flag-row', { srcIp: '203.0.113.9' })
    flagsState.list = [{ ...activeFlag('203.0.113.9'), cleared: true }]

    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelector('[title="cleared-flag-row"]')?.classList.contains('flagged')).toBe(false)
  })
})

// The foot line (#691, round 30's .foot-legend): three computed facts
// on the stream's own footing. Unmounted outright behind
// FOOT_LEGEND_ENABLED per the owner's 2026-08-31 #717 ruling ("I hate
// it, remove it") -- these pin that it never draws, even with facts to
// show, matching RESIZE_HANDLES_ENABLED's own pattern. The fact
// computation itself (footLineFacts) keeps its own coverage in
// footLine.test.ts; what matters here is only that this component does
// not render it.
describe('the foot line', () => {
  const darkBoundary = {
    key: 'forward|guest|wan',
    chain: 'forward',
    inInterface: 'guest',
    outInterface: 'wan',
    srcAddressList: 'guest',
    label: 'guest → wan',
    coverage: 'dark' as const,
    epithet: '',
  }

  function repeatedDrops(): Flag {
    return {
      id: 'rd1',
      type: 'repeated_drops',
      target: '10.0.20.11 -> port 445',
      detail: '',
      count: 14,
      firstSeen: new Date(Date.now() - 10 * 60_000).toISOString(),
      lastSeen: new Date(Date.now() - 60_000).toISOString(),
      cleared: false,
      evidence: { hosts: ['10.0.40.5'] },
    }
  }

  it('does not render at all when none of the three facts has data', () => {
    const { container } = render(LiveTable, { props: { events: [makeEvent('row')] } })
    flushSync()

    expect(container.querySelector('.foot-legend')).toBeNull()
  })

  it('stays unmounted even when all three facts have data (#717)', () => {
    fallState.boundaries = [darkBoundary]
    flagsState.list = [repeatedDrops()]
    appState.events = [
      makeEvent('drop-row', {
        action: 'drop',
        srcIp: '10.0.20.11',
        srcHostName: 'cam-porch',
        dstIp: '10.0.40.5',
        dstHostName: 'nas',
        dstPort: 445,
      }),
    ]

    const { container } = render(LiveTable, { props: { events: appState.events } })
    flushSync()

    expect(container.querySelector('.foot-legend')).toBeNull()
  })

  it('stays unmounted with a cleared flag too', () => {
    flagsState.list = [{ ...repeatedDrops(), cleared: true }]
    appState.events = [makeEvent('drop-row', { srcIp: '10.0.20.11', dstPort: 445, action: 'drop' })]

    const { container } = render(LiveTable, { props: { events: appState.events } })
    flushSync()

    expect(container.querySelector('.foot-legend')).toBeNull()
  })
})

describe('the stream is the scene, not a boxed widget (#733)', () => {
  it('drops the card look from .table-wrap: no border, no radius, no separate panel tint', () => {
    const rule = componentSource.match(/\.table-wrap\s*\{([^}]*)\}/)
    expect(rule).toBeTruthy()
    const decls = rule![1]
    expect(decls).not.toMatch(/border(?!-box):/)
    expect(decls).not.toMatch(/border-radius/)
    expect(decls).not.toMatch(/--bg-elevated/)
    expect(decls).toMatch(/background:\s*var\(--bg\)/)
  })

  it('leaves .body -- not .table-wrap -- as the only overflow ancestor between the head and its scroll range', () => {
    // position: sticky tracks the nearest ancestor whose own overflow is
    // not visible. That has to stay .body: it's the real, intentional
    // scroll container for the sideways scroll of the table's 1622px of
    // fixed columns (#729), and it's also the container the sticky
    // header cells actually stick against. Giving .table-wrap (an
    // ancestor of .body) any overflow other than visible again would add
    // a second, pointless clip outside the real scrollport -- the same
    // way the metrics head lost its stick earlier this round (see
    // MetricsTable.svelte's own .table-wrap comment).
    const wrapRule = componentSource.match(/\.table-wrap\s*\{([^}]*)\}/)
    const bodyRule = componentSource.match(/\n\s*\.body\s*\{([^}]*)\}/)
    expect(wrapRule).toBeTruthy()
    expect(bodyRule).toBeTruthy()
    expect(wrapRule![1]).not.toMatch(/overflow\s*:/)
    expect(bodyRule![1]).toMatch(/overflow:\s*auto/)
  })

  it('keeps the header cell sticky and opaque, against the scene\'s own ground rather than a panel tint', () => {
    const headerRule = componentSource.match(/\.header-cell\s*\{([^}]*)\}/)
    expect(headerRule).toBeTruthy()
    const decls = headerRule![1]
    expect(decls).toMatch(/position:\s*sticky/)
    expect(decls).toMatch(/top:\s*0/)
    expect(decls).toMatch(/background:\s*var\(--bg\)/)
    expect(decls).not.toMatch(/--bg-elevated/)
  })
})

// #729: the column chooser (built in FilterBar.svelte, state in
// columns.svelte.ts) narrows which of the fifteen columns this table
// actually renders. Default stays all fifteen (the file's own beforeEach
// resets columnState.visible before every test here) -- these cover what
// happens once a reader has turned some off: the header and the body
// must never disagree about which columns are showing, since both read
// off one shared CSS Grid template (`.row` is `display: contents`, so a
// mismatch would not just look wrong on one row -- every following cell
// in the grid would shift by a track).
describe('LiveTable column chooser rendering (#729)', () => {
  it('renders one header cell per visible column when the reader has hidden some', () => {
    columnState.toggleColumn('device')
    columnState.toggleColumn('mac')
    columnState.toggleColumn('nat')

    const { container } = render(LiveTable, { props: { events: [] } })
    flushSync()

    const headerLabels = Array.from(container.querySelectorAll('.header-cell .label-text')).map((el) => el.textContent)
    expect(container.querySelectorAll('.header-cell').length).toBe(COLUMNS.length - 3)
    expect(headerLabels).not.toContain('Device')
  })

  it('renders exactly as many body cells as header cells -- the head and the body can never disagree', () => {
    columnState.toggleColumn('device')
    columnState.toggleColumn('mac')
    columnState.toggleColumn('nat')

    const e = makeEvent('agree-row', {
      deviceId: 'router1',
      chain: 'forward',
      srcIp: '10.0.0.1',
      srcPort: 51000,
      srcMac: 'AA:BB:CC:DD:EE:FF',
      dstIp: '10.0.0.2',
      dstPort: 443,
      protocol: 'tcp',
      inInterface: 'lan',
      outInterface: 'wan',
      natIp: '203.0.113.5',
      natPort: 51512,
      ruleLabel: 'lan-wan',
    })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    const headerCount = container.querySelectorAll('.header-cell').length
    const row = container.querySelector('.row') as HTMLElement
    const bodyCellCount = row.querySelectorAll(':scope > .cell').length

    expect(headerCount).toBe(COLUMNS.length - 3)
    expect(bodyCellCount).toBe(headerCount)
    // The three turned off above are genuinely absent, not just hidden --
    // no .cell.device/.cell.mac/.cell.nat node exists in this row at all.
    expect(row.querySelector(':scope > .cell.device')).toBeNull()
    expect(row.querySelector(':scope > .cell.mac')).toBeNull()
    expect(row.querySelector(':scope > .cell.nat')).toBeNull()
  })

  it('keeps the grid template in step with the header when a subset is hidden', () => {
    columnState.toggleColumn('device')
    columnState.toggleColumn('chain')

    const { container } = render(LiveTable, { props: { events: [] } })
    flushSync()

    // A flexible column's own token ("minmax(140px, 1fr)") contains a
    // space, so counting tracks via a naive split(' ') would overcount it
    // by one -- this counts CSS tracks instead (a bare px length, or a
    // whole minmax(...) call).
    const grid = container.querySelector('.grid') as HTMLElement
    const trackCount = (grid.style.gridTemplateColumns.match(/\d+px|minmax\([^)]*\)/g) ?? []).length
    const headerCount = container.querySelectorAll('.header-cell').length

    expect(trackCount).toBe(headerCount)
    expect(trackCount).toBe(COLUMNS.length - 2)
    // Rule stays the sole flexible track regardless of which fixed
    // columns are on or off (#685) -- the sideways scroll and the fixed
    // widths both still have to work with an arbitrary subset.
    expect(grid.style.gridTemplateColumns).toMatch(/minmax\(140px,\s*1fr\)/)
  })

  it('leaves a usable table -- just Time and Rule -- when every optional column is turned off', () => {
    for (const col of COLUMNS) {
      if (!PINNED_COLUMNS.has(col.key)) columnState.toggleColumn(col.key)
    }

    const e = makeEvent('minimal-row', { ruleLabel: 'lan-wan' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelectorAll('.header-cell').length).toBe(PINNED_COLUMNS.size)
    const row = container.querySelector('.row') as HTMLElement
    expect(row.querySelectorAll(':scope > .cell').length).toBe(PINNED_COLUMNS.size)
    expect(row.querySelector(':scope > .cell.time')).toBeTruthy()
    expect(row.querySelector(':scope > .cell.rule')).toBeTruthy()
  })

  // EventRow.svelte's default (all-visible) path renders these thirteen
  // optional cells as plain, unconditional markup; the moment a reader
  // hides any one column, every *remaining* optional cell switches to
  // rendering from the {#snippet} copies of that same markup instead (see
  // that file's own comment on why the raw path is duplicated rather than
  // always going through snippets -- a snippet costs measurably more per
  // invocation than inlined markup at 810 rows, enough on its own to push
  // #728's already-borderline render-cost test over vitest's timeout).
  // columnState.allVisible only ever reads real visibility, so there is no
  // way to force the snippet path while every column is genuinely visible
  // -- this instead compares each optional column's own rendered cell
  // between a row with everything visible (raw path) and a row with some
  // *other* column hidden (snippet path, this column still visible). If
  // either copy of a cell's markup drifts from the other, this fails.
  describe('the two render paths stay identical (#729)', () => {
    function fullEvent(): ClientEvent {
      return makeEvent('compare-row', {
        deviceId: 'router1',
        chain: 'forward',
        srcIp: '10.0.0.1',
        srcHostName: 'workstation',
        srcCountry: 'DE',
        srcPort: 51000,
        srcMac: 'AA:BB:CC:DD:EE:FF',
        dstIp: '10.0.0.2',
        dstHostName: 'server',
        dstCountry: 'US',
        dstPort: 443,
        protocol: 'tcp',
        inInterface: 'lan',
        outInterface: 'wan',
        natIp: '203.0.113.5',
        natPort: 51512,
        ruleLabel: 'lan-wan',
      })
    }

    // Renders one fresh row (a brand-new LiveTable instance, own
    // container) and returns it. Each optional cell has a selector below;
    // .cell.addr/.cell.ip are shared between Source and Destination, so
    // those two read by position (0 = source, 1 = destination) rather
    // than by class alone.
    function renderRow(): HTMLElement {
      const { container } = render(LiveTable, { props: { events: [fullEvent()] } })
      flushSync()
      return container.querySelector('.row') as HTMLElement
    }

    // Serialises a cell by walking the DOM rather than reading its
    // serialised markup: the Go guard in injection_sinks_test.go forbids
    // those two property names anywhere under frontend/src, and a test
    // file is not worth an exception in docs/decisions/injection-audit.md.
    // Tag, sorted attributes, the same for every descendant, and the text
    // -- enough to catch the drift this test exists for.
    function describeCell(el: Element | null | undefined): string {
      if (!el) return ''
      const attrs = (e: Element) =>
        Array.from(e.attributes)
          .map((a) => `${a.name}=${a.value}`)
          .sort()
          .join(' ')
      const kids = Array.from(el.querySelectorAll('*'))
        .map((c) => `${c.tagName}[${attrs(c)}]`)
        .join(',')
      return `${el.tagName}[${attrs(el)}] ${kids} :: ${el.textContent}`
    }

    function captureCells(row: HTMLElement): Record<string, string> {
      const addrCells = row.querySelectorAll('.cell.addr')
      const ipCells = row.querySelectorAll('.cell.ip')
      return {
        device: describeCell(row.querySelector('.cell.device')),
        action: describeCell(row.querySelector('.cell.action')),
        chain: describeCell(row.querySelector('.cell.chain')),
        source: describeCell(addrCells[0]),
        srcAddr: describeCell(ipCells[0]),
        srcPort: describeCell(row.querySelector('.cell.port.srcport')),
        mac: describeCell(row.querySelector('.cell.mac')),
        destination: describeCell(addrCells[1]),
        dstAddr: describeCell(ipCells[1]),
        proto: describeCell(row.querySelector('.cell.proto')),
        iface: describeCell(row.querySelector('.cell.iface')),
        port: describeCell(row.querySelector('.cell.port:not(.srcport)')),
        nat: describeCell(row.querySelector('.cell.nat')),
      }
    }

    it("keeps every optional column's markup identical whether or not another column happens to be hidden", () => {
      const raw = captureCells(renderRow())

      // Hiding 'nat' forces columnState.allVisible false (the snippet
      // path) without disturbing any other column's position -- nat sits
      // last and shares no CSS class with anything else, so this reads
      // every other optional column's snippet-path rendering unchanged.
      columnState.toggleColumn('nat')
      const gatedByNat = captureCells(renderRow())
      columnState.toggleColumn('nat')

      // nat itself needs a *different* column hidden to reach the snippet
      // path while nat stays visible -- device, hidden here, shares no
      // class with .cell.nat and sits well before the addr/ip pairs, so
      // it disturbs nothing this test reads.
      columnState.toggleColumn('device')
      const gatedByDevice = captureCells(renderRow())
      columnState.toggleColumn('device')

      const comparedViaGatedByNat = [
        'device',
        'action',
        'chain',
        'source',
        'srcAddr',
        'srcPort',
        'mac',
        'destination',
        'dstAddr',
        'proto',
        'iface',
        'port',
      ]
      for (const key of comparedViaGatedByNat) {
        expect(raw[key], `raw path produced no markup for ${key}`).not.toBe('')
        expect(gatedByNat[key], `${key} identical between the raw and snippet paths`).toBe(raw[key])
      }

      expect(raw.nat).not.toBe('')
      expect(gatedByDevice.nat).toBe(raw.nat)
    })
  })

  it('still shows every column, in order, when nothing has been turned off (the shipped default)', () => {
    const e = makeEvent('default-row', { ruleLabel: 'lan-wan' })
    const { container } = render(LiveTable, { props: { events: [e] } })
    flushSync()

    expect(container.querySelectorAll('.header-cell').length).toBe(COLUMNS.length)
    const row = container.querySelector('.row') as HTMLElement
    expect(row.querySelectorAll(':scope > .cell').length).toBe(COLUMNS.length)
  })
})
