// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import type { ClientEvent } from '../lib/types'
import { emptyFilters } from '../lib/types'
import { appState } from '../lib/state.svelte'
import { groupModeState } from '../lib/groupMode.svelte'
import { MAX_RENDERED_ROWS } from '../lib/constants'

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
  // Reset centrally, not at the end of whichever test set it. A test body
  // that fails never reaches its own cleanup line, and the next test then
  // renders in grouped mode and fails for a reason that has nothing to do
  // with what it is checking -- one real failure reported as two.
  groupModeState.enabled = false
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
    expect(appState.filters.ip).toBe('')

    // A plain click -- no selection left behind -- still filters,
    // unchanged from before this issue.
    clearSelection()
    await fireEvent.mouseDown(addrToken)
    await fireEvent.mouseUp(addrToken)
    expect(appState.filters.ip).toBe(HOST_IP)
  })

  it('keeps keyboard activation working for filter cells (Enter), now that they are not <button>s', async () => {
    const { container } = renderLabelledRow()
    const addrToken = container.querySelector('.addr-btn') as HTMLElement
    await fireEvent.keyDown(addrToken, { key: 'Enter' })
    expect(appState.filters.ip).toBe(HOST_IP)
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
