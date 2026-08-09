// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import type { ClientEvent } from '../lib/types'
import { emptyFilters } from '../lib/types'
import { appState } from '../lib/state.svelte'
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

  it('does not freeze a caller-supplied table (honorAutoscroll=false), e.g. ControlPorts', async () => {
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
