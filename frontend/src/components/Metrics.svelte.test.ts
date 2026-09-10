// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, beforeEach, vi } from 'vitest'
import { flushSync } from 'svelte'
import { render, screen } from '@testing-library/svelte'
import { fireEvent } from '@testing-library/dom'

// appState/flagsState reach the network only through their own refresh
// methods, which nothing here calls -- the page reads whatever is
// already in the stores. Metrics.svelte itself does make one request of
// its own (#644 round 21's top-port/top-talker poll), stubbed here so
// rendering it under jsdom never reaches for the network.
vi.mock('../lib/api', () => ({
  fetchStatsTops: vi.fn(async () => []),
}))

import { appState } from '../lib/state.svelte'
import { flagsState } from '../lib/flags.svelte'
import { metricsPref } from '../lib/metrics.svelte'
import { formatHM } from '../lib/format'
import type { Stats } from '../lib/types'
import Metrics from './Metrics.svelte'

// jsdom implements no ResizeObserver, and `bind:clientWidth` -- how the
// drawn views measure their own box, so the SVG can be sized in real CSS
// pixels -- is compiled to one. A no-op stub is all this needs: jsdom
// reports every box as zero-sized regardless, so the drum draws at its
// own minimum width here.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal('ResizeObserver', ResizeObserverStub)

function minute(n: number): string {
  return new Date(Date.UTC(2026, 7, 24, 13, n, 0)).toISOString()
}

const stats: Stats = {
  oldestHeld: null,
  total: 3,
  byAction: { accept: 1231, drop: 109, reject: 2 },
  topRules: [{ rule: 'fwd-drop', count: 109 }],
  timeSeries: [
    { time: minute(0), byAction: { accept: 400, drop: 9 } },
    { time: minute(1), byAction: { accept: 410, drop: 88, reject: 2 } },
    { time: minute(2), byAction: { accept: 421, drop: 12 } },
  ],
  eventsPerSecond: 7.4,
  capacity: 100000,
  count: 3,
  windowSeconds: 3600,
  connectedClients: 1,
}

// The record's three views, chosen in the page header and persisted as a
// per-user preference (#488, docs/design/screens/metrics/DESIGN.md).
// What is pinned here is what the record makes load-bearing: all three
// ship, the choice is written where a reload will find it, and the
// cursor's minute survives a switch between them.
describe('Metrics', () => {
  beforeEach(() => {
    appState.stats = stats
    flagsState.timeSeries = [{ time: minute(1), byType: { repeated_drops: 2 } }]
    metricsPref.view = 'seismograph'
    metricsPref.select(null)
    localStorage.clear()
  })

  // The three-view switcher moved to the scene bar (#700), where round
  // 30 rides it beside the wordmark. Its controls, its default and its
  // persistence are covered in SceneBar.svelte.test.ts; what stays this
  // page's business is that the chosen view is the one it renders, and
  // that the cursor survives the switch.

  it('keeps the cursor on the same minute across a view switch', async () => {
    render(Metrics)
    // The table is the one view whose minutes are named controls, so it
    // is where a click can select a minute without geometry. The view
    // is chosen through the store rather than a button since #700 moved
    // the switcher to the bar -- this page renders whatever it is set
    // to, which is the half being tested here.
    metricsPref.setView('table')
    flushSync()
    const rows = screen.getAllByRole('button', { name: /^\d\d:\d\d$/ })
    await fireEvent.click(rows[0])
    const selected = metricsPref.minute
    expect(selected).not.toBeNull()

    metricsPref.setView('seismograph')
    flushSync()
    expect(metricsPref.minute).toBe(selected)
    metricsPref.setView('register')
    flushSync()
    expect(metricsPref.minute).toBe(selected)
  })

  it('announces the minute and its figures when the cursor moves', async () => {
    render(Metrics)
    const surface = screen.getByRole('slider', { name: 'The minute under the cursor' })
    await fireEvent.keyDown(surface, { key: 'End' })
    expect(surface.getAttribute('aria-valuetext')).toContain('Accept 421')
    await fireEvent.keyDown(surface, { key: 'ArrowLeft' })
    expect(surface.getAttribute('aria-valuetext')).toContain('Drop 88')
    expect(surface.getAttribute('aria-valuetext')).toContain('Repeated drops on a port times 2')
  })

  it('moves ten minutes with shift, and reaches both ends of the hour', async () => {
    render(Metrics)
    const surface = screen.getByRole('slider', { name: 'The minute under the cursor' })
    await fireEvent.keyDown(surface, { key: 'End' })
    expect(surface.getAttribute('aria-valuenow')).toBe('2')
    await fireEvent.keyDown(surface, { key: 'ArrowLeft', shiftKey: true })
    expect(surface.getAttribute('aria-valuenow')).toBe('0')
    await fireEvent.keyDown(surface, { key: 'ArrowRight' })
    expect(surface.getAttribute('aria-valuenow')).toBe('1')
    await fireEvent.keyDown(surface, { key: 'Home' })
    expect(surface.getAttribute('aria-valuenow')).toBe('0')
  })

  // Rounds 36-37 (#803): "reading the minute under the cursor across every
  // series was already the hourline's job; it now reads every series in one
  // line under the cursor". The `.fact` spans that are direct children of
  // `.hourline` are the cursor's group -- the hour's rate facts live inside
  // `.rate`, which is why this does not just query `.hourline .fact`.
  function cursorFacts(container: HTMLElement): string[] {
    const hourline = container.querySelector('.hourline')!
    return [...hourline.children]
      .filter((el) => el.classList.contains('fact'))
      .map((el) => el.textContent!.replace(/\s+/g, ' ').trim())
  }

  it('reads every series under the cursor, not a ratio naming two of them', async () => {
    const { container } = render(Metrics)
    const surface = screen.getByRole('slider', { name: 'The minute under the cursor' })
    await fireEvent.keyDown(surface, { key: 'End' })
    await fireEvent.keyDown(surface, { key: 'ArrowLeft' })

    const facts = cursorFacts(container)
    expect(facts).toContain('410 Accept')
    expect(facts).toContain('88 Drop')
    expect(facts).toContain('2 Reject')
    // Every traffic series gets a fact, plus one for the episodes -- so a
    // series that was silent this minute still says so rather than being
    // folded into an "of N events" denominator.
    expect(facts.length).toBe(container.querySelectorAll('.hourline > .fact').length)
    expect(facts.some((f) => /refused of/.test(f))).toBe(false)
  })

  it('wears the refused ink on the refused series, and only on it', async () => {
    const { container } = render(Metrics)
    const surface = screen.getByRole('slider', { name: 'The minute under the cursor' })
    await fireEvent.keyDown(surface, { key: 'End' })
    await fireEvent.keyDown(surface, { key: 'ArrowLeft' })

    const hourline = container.querySelector('.hourline')!
    const refused = [...hourline.children]
      .filter((el) => el.classList.contains('fact') && el.classList.contains('ref'))
      .map((el) => el.textContent!.replace(/\s+/g, ' ').trim())
    expect(refused).toContain('88 Drop')
    expect(refused).toContain('2 Reject')
    expect(refused).not.toContain('410 Accept')
  })

  // The flag-episode names are what the removed cross-section panel used to
  // print. They ride the hourline now, so nothing that panel said is lost.
  it('names the flag types behind the episode count', async () => {
    const { container } = render(Metrics)
    const surface = screen.getByRole('slider', { name: 'The minute under the cursor' })
    await fireEvent.keyDown(surface, { key: 'End' })
    await fireEvent.keyDown(surface, { key: 'ArrowLeft' })

    const episodes = cursorFacts(container).find((f) => /flag episode/.test(f))
    expect(episodes).toBe('2 flag episodes — Repeated drops on a port ×2')
  })

  it('draws no cross-section panel beside the register, and prints no instruction', () => {
    const { container } = render(Metrics)
    metricsPref.setView('register')
    flushSync()
    expect(container.querySelector('.cross-section')).toBeNull()
    expect(container.textContent).not.toContain('Pick a minute')
  })

  it('clears the cursor on Escape rather than leaving a minute stuck under it', async () => {
    render(Metrics)
    const surface = screen.getByRole('slider', { name: 'The minute under the cursor' })
    await fireEvent.keyDown(surface, { key: 'End' })
    expect(metricsPref.minute).not.toBeNull()
    await fireEvent.keyDown(surface, { key: 'Escape' })
    expect(metricsPref.minute).toBeNull()
  })

  // ── The restart statement (#795, design round 41, scene #s4) ──────────
  //
  // What the hour is an account of, after a restart: the last fact in the
  // hourline's right-hand rate group. The wording and the sixty-minute
  // clearing boundary are pinned in lib/provenance.test.ts, which both
  // surfaces read; what is this page's own business, and pinned here, is
  // that the fact is rendered, that it is *last* in the rate group (a
  // fact about the other facts belongs after them), and that it is a
  // statement rather than a control.
  function statement(container: HTMLElement): HTMLElement | null {
    return container.querySelector('.hourline .rate .fact.stmt')
  }

  const minutesAgo = (n: number) => new Date(Date.now() - n * 60 * 1000).toISOString()

  it('names both times on the hourline after a warm restart', () => {
    const liveSince = minutesAgo(34)
    const restoredTo = minutesAgo(38)
    appState.stats = { ...stats, liveSince, restoredTo }
    const { container } = render(Metrics)

    const el = statement(container)
    expect(el?.textContent).toMatch(/^restored to \d\d:\d\d · live since \d\d:\d\d$/)
    expect(el?.textContent).toBe(`restored to ${formatHM(restoredTo)} · live since ${formatHM(liveSince)}`)
  })

  it('says nothing came before after a cold start', () => {
    const liveSince = minutesAgo(34)
    appState.stats = { ...stats, liveSince }
    const { container } = render(Metrics)

    expect(statement(container)?.textContent).toBe(`counting since ${formatHM(liveSince)} — nothing before`)
  })

  it('draws the statement last in the rate group, as a fact and not a control', () => {
    appState.stats = { ...stats, liveSince: minutesAgo(3), restoredTo: minutesAgo(7) }
    const { container } = render(Metrics)

    const rate = container.querySelector('.hourline .rate')!
    expect(rate.lastElementChild!.classList.contains('stmt')).toBe(true)
    // A statement, not a control: there is nowhere for it to lead.
    expect(statement(container)!.tagName.toLowerCase()).toBe('span')
    expect(rate.querySelector('button')).toBeNull()
    // Its own separator went with it, rather than leaving a stray dot.
    expect(rate.lastElementChild!.previousElementSibling!.classList.contains('sep')).toBe(true)
  })

  it('has cleared an hour after the process came up', () => {
    appState.stats = { ...stats, liveSince: minutesAgo(61), restoredTo: minutesAgo(65) }
    const { container } = render(Metrics)

    expect(statement(container)).toBeNull()
    expect(container.textContent).not.toContain('restored to')
  })

  it('says nothing at all when the server sends no liveSince', () => {
    // The base fixture is a pre-#795 stats payload.
    const { container } = render(Metrics)
    expect(statement(container)).toBeNull()
  })
})
