// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import type { ClientEvent } from '../lib/types'
import { appState } from '../lib/state.svelte'

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

function makeEvent(raw: string): ClientEvent {
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
  }
}

beforeEach(() => {
  appState.autoscroll = true
  appState.paused = false
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
})
