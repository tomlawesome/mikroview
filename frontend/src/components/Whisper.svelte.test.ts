// SPDX-License-Identifier: AGPL-3.0-only
//
// #717: the fence pill is gone -- click seeks, drag fences, and the
// curve itself is the one keyboard control (arrows move, Enter marks an
// edge, Escape clears). This covers the three gestures directly, plus
// the shape LiveTable actually reads off whisperState.fenceRange.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// jsdom (unlike a real browser) has no window.matchMedia, and Whisper
// now pulls in lib/viewport.svelte.ts -- the hand's `group` pill is
// absent at phone width -- whose ViewportState singleton calls it at
// module-load time. vi.hoisted, not a plain `if` further down: static
// imports are hoisted and evaluate before any of this file's own
// top-level code, so a polyfill written below them is already too late
// (LiveTable.svelte.test.ts solves the same problem by importing the
// component dynamically instead). `matches: false` means desktop, which
// is where the hand shows all three of its toggles.
vi.hoisted(() => {
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
})
import { render, screen } from '@testing-library/svelte'
import { fireEvent } from '@testing-library/dom'
import { flushSync } from 'svelte'
import Whisper from './Whisper.svelte'
import { appState } from '../lib/state.svelte'
import { whisperState } from '../lib/whisper.svelte'
import { groupModeState } from '../lib/groupMode.svelte'
import { COLUMNS, PINNED_COLUMNS, columnState } from '../lib/columns.svelte'
import { emptyFilters, type ClientEvent, type Stats, type TimeBucket } from '../lib/types'

// #1197: opens the columns ▸ picker the same way an operator would, by
// clicking the toggle -- moved here from FilterBar.svelte.test.ts's own
// former helper of the same name, along with the trigger itself.
async function openColumns() {
  await fireEvent.click(screen.getByRole('button', { name: 'Choose which columns the stream shows' }))
  flushSync()
}


// jsdom's own getBoundingClientRect returns all zeros; the drag/click
// math needs real pixel geometry to turn a clientX back into a bucket
// index. A 1000px-wide rect maps 1:1 onto the curve's own 0-1000
// viewBox, so clientX 0/250/500/750/1000 line up exactly with the five
// one-minute buckets the fixture below builds.
Element.prototype.getBoundingClientRect = () =>
  ({
    x: 0,
    y: 0,
    width: 1000,
    height: 30,
    top: 0,
    left: 0,
    right: 1000,
    bottom: 30,
    toJSON: () => {},
  }) as DOMRect

const MIN = 60_000
const BASE = new Date('2026-01-01T00:00:00.000Z').getTime()
const N = 5

function bucket(i: number): TimeBucket {
  return { time: new Date(BASE + i * MIN).toISOString(), byAction: { accept: 1 } }
}

function stats(): Stats {
  return {
    total: N,
    byAction: { accept: N },
    topRules: [],
    timeSeries: Array.from({ length: N }, (_, i) => bucket(i)),
    eventsPerSecond: 1,
    capacity: 1000,
    count: N,
    windowSeconds: 900,
    oldestHeld: null,
    connectedClients: 0,
  }
}

function bucketMs(i: number): number {
  return BASE + i * MIN
}

beforeEach(() => {
  whisperState.fenceRange = null
  whisperState.seekMs = null
  appState.autoscroll = true
  appState.events = []
  appState.stats = stats()
})

function renderSvg() {
  const { container } = render(Whisper)
  const svg = container.querySelector('.wsvg')
  if (!svg) throw new Error('curve not found')
  return svg
}

describe('drag', () => {
  it('sets the fence to the range dragged', async () => {
    const svg = renderSvg()
    await fireEvent.pointerDown(svg, { pointerId: 1, button: 0, clientX: 0, clientY: 0 })
    await fireEvent.pointerMove(svg, { pointerId: 1, clientX: 750, clientY: 0 })
    await fireEvent.pointerUp(svg, { pointerId: 1, clientX: 750, clientY: 0 })

    expect(whisperState.fenceRange).toEqual({ start: bucketMs(0), end: bucketMs(3) + MIN })
    expect(whisperState.seekMs).toBeNull()
  })
})

describe('click below the drag threshold', () => {
  it('seeks instead of fencing', async () => {
    const svg = renderSvg()
    await fireEvent.pointerDown(svg, { pointerId: 2, button: 0, clientX: 500, clientY: 0 })
    await fireEvent.pointerMove(svg, { pointerId: 2, clientX: 502, clientY: 0 })
    await fireEvent.pointerUp(svg, { pointerId: 2, clientX: 502, clientY: 0 })

    expect(whisperState.seekMs).toBe(bucketMs(2))
    expect(whisperState.fenceRange).toBeNull()
    expect(appState.autoscroll).toBe(false)
  })

  it('a later click clears a fence already drawn', async () => {
    whisperState.setFenceRange(bucketMs(0), bucketMs(1) + MIN)
    const svg = renderSvg()
    await fireEvent.pointerDown(svg, { pointerId: 3, button: 0, clientX: 1000, clientY: 0 })
    await fireEvent.pointerUp(svg, { pointerId: 3, clientX: 1000, clientY: 0 })

    expect(whisperState.fenceRange).toBeNull()
    expect(whisperState.seekMs).toBe(bucketMs(4))
  })
})

describe('keyboard', () => {
  it('arrows move the cursor, Enter marks each edge, and the closed fence lands in the shape LiveTable consumes', async () => {
    const svg = renderSvg()
    await fireEvent.keyDown(svg, { key: 'End' })
    await fireEvent.keyDown(svg, { key: 'Enter' })
    await fireEvent.keyDown(svg, { key: 'Home' })
    await fireEvent.keyDown(svg, { key: 'Enter' })

    expect(whisperState.fenceRange).toEqual({ start: bucketMs(0), end: bucketMs(4) + MIN })
    expect(typeof whisperState.fenceRange!.start).toBe('number')
    expect(typeof whisperState.fenceRange!.end).toBe('number')
  })

  it('Escape clears a closed fence', async () => {
    const svg = renderSvg()
    await fireEvent.keyDown(svg, { key: 'End' })
    await fireEvent.keyDown(svg, { key: 'Enter' })
    await fireEvent.keyDown(svg, { key: 'Home' })
    await fireEvent.keyDown(svg, { key: 'Enter' })
    expect(whisperState.fenceRange).not.toBeNull()

    await fireEvent.keyDown(svg, { key: 'Escape' })
    expect(whisperState.fenceRange).toBeNull()
  })

  it('Escape before the second Enter cancels the pending edge instead of clearing an existing fence', async () => {
    whisperState.setFenceRange(bucketMs(0), bucketMs(1) + MIN)
    const svg = renderSvg()
    await fireEvent.keyDown(svg, { key: 'End' })
    await fireEvent.keyDown(svg, { key: 'Enter' })

    await fireEvent.keyDown(svg, { key: 'Escape' })
    expect(whisperState.fenceRange).toEqual({ start: bucketMs(0), end: bucketMs(1) + MIN })
  })
})

// Rounds 36-38 gave the stream's verbs a home on the whisper's own line
// (`following · pause · group`, then `wipe` and `csv ↓`). Round 30 had
// retired the toolbar that carried them and drawn no replacement, which
// is how Autoscroll became a one-way trapdoor -- so these cover both the
// controls themselves and the two facts the hand creates on the stat
// line ("held at…", "wiped…"), which are unrecoverable from the buffer.
describe("the stream's hand (rounds 36-38)", () => {
  function event(overrides: Partial<ClientEvent> = {}): ClientEvent {
    return {
      id: Math.random(),
      time: new Date(bucketMs(4)).toISOString(),
      deviceId: 'router1',
      sourceIp: '203.0.113.10',
      action: 'accept',
      ruleLabel: 'test-rule',
      chain: 'input',
      raw: 'test',
      receivedAt: bucketMs(4),
      ...overrides,
    }
  }

  // The pills are updated in place rather than recreated, so holding a
  // reference to each one across a state change is safe and keeps every
  // assertion below pointed at the same element the operator clicked.
  function renderHand() {
    const { container } = render(Whisper)
    const handBtns = container.querySelectorAll<HTMLButtonElement>('.hand .hand-btn')
    const wpills = container.querySelectorAll<HTMLButtonElement>('.wpill')
    const curve = container.querySelector('.wsvg')
    if (!curve) throw new Error('curve not found')
    return {
      container,
      curve,
      handBtns,
      follow: handBtns[0],
      pause: handBtns[1],
      group: handBtns[2],
      wipe: wpills[0],
      csv: wpills[1],
      stat: () => container.querySelector('.wstat')?.textContent ?? '',
    }
  }

  // A plain click on the curve, below the drag threshold -- the gesture
  // that seeks, which is the one thing that turns following off by
  // itself.
  async function clickCurve(curve: Element, clientX: number, pointerId: number) {
    await fireEvent.pointerDown(curve, { pointerId, button: 0, clientX, clientY: 0 })
    await fireEvent.pointerUp(curve, { pointerId, clientX, clientY: 0 })
  }

  // Every one of these is a module-level singleton, so a pause, a wipe or
  // a group toggle left on by one test is still on in the next one.
  beforeEach(() => {
    appState.paused = false
    appState.pausedAt = null
    appState.wipedAt = null
    appState.pendingCount = 0
    appState.filters = emptyFilters()
    groupModeState.enabled = false
  })

  it('carries the three verbs in the drawn order, with wipe and csv after them', () => {
    const { handBtns, wipe, csv } = renderHand()

    expect([...handBtns].map((b) => b.textContent?.trim())).toEqual(['following', 'pause', 'group'])
    expect(wipe.textContent?.trim()).toBe('wipe')
    expect(csv.textContent?.trim()).toBe('csv ↓')
  })

  it('starts following, and says so as a word rather than only as a colour', () => {
    const { follow } = renderHand()

    expect(appState.autoscroll).toBe(true)
    expect(follow.classList.contains('on')).toBe(true)
    expect(follow.getAttribute('aria-pressed')).toBe('true')
    expect(follow.textContent?.trim()).toBe('following')
  })

  it('a seek turns following off and the pill changes its word to `follow`', async () => {
    const { curve, follow } = renderHand()

    await clickCurve(curve, 500, 11)

    expect(appState.autoscroll).toBe(false)
    expect(follow.classList.contains('on')).toBe(false)
    expect(follow.getAttribute('aria-pressed')).toBe('false')
    // The word, not just the class: a pill that only changed colour would
    // leave a screen reader and a monochrome eye with no way to tell that
    // the control now offers the way back rather than the way out.
    expect(follow.textContent?.trim()).toBe('follow')
  })

  it('taking `follow` after a seek follows again and clears the cursor and the window (#749)', async () => {
    const { curve, follow } = renderHand()

    // Seek, then fence: both lenses can be set at once (a drag does not
    // clear a seeked minute), and re-following has to clear both.
    await clickCurve(curve, 500, 12)
    await fireEvent.pointerDown(curve, { pointerId: 13, button: 0, clientX: 0, clientY: 0 })
    await fireEvent.pointerMove(curve, { pointerId: 13, clientX: 750, clientY: 0 })
    await fireEvent.pointerUp(curve, { pointerId: 13, clientX: 750, clientY: 0 })
    expect(appState.autoscroll).toBe(false)
    expect(whisperState.seekMs).toBe(bucketMs(2))
    expect(whisperState.fenceRange).not.toBeNull()

    await fireEvent.click(follow)
    flushSync()

    // #749 itself: with the toolbar retired, seeking was the only writer
    // of autoscroll left in the app and it only ever wrote `false`, so a
    // single click on the curve held the stream for the rest of the
    // session and nothing short of a reload got it moving again.
    expect(appState.autoscroll).toBe(true)
    // The cursor and the window are the *reason* it stopped, so following
    // again clears them rather than chasing new lines through a window
    // drawn over old ones.
    expect(whisperState.seekMs).toBeNull()
    expect(whisperState.fenceRange).toBeNull()
    expect(follow.textContent?.trim()).toBe('following')
  })

  it('turning following off by hand leaves a drawn fence exactly as drawn', async () => {
    // Set directly because the gesture that draws a fence also turns
    // following off, and this is the other state the pill has to handle:
    // still following, with a window already drawn.
    whisperState.fenceRange = { start: bucketMs(1), end: bucketMs(3) }
    const { follow } = renderHand()

    await fireEvent.click(follow)
    flushSync()

    expect(appState.autoscroll).toBe(false)
    // "Stop moving", not "forget where I was looking" -- clearing here
    // would throw away the range the operator drew to read.
    expect(whisperState.fenceRange).toEqual({ start: bucketMs(1), end: bucketMs(3) })
  })

  it('`pause` holds the lines and the stat line says when the hold was taken and how many wait', async () => {
    appState.pendingCount = 212
    const { pause, stat } = renderHand()

    await fireEvent.click(pause)
    flushSync()

    expect(appState.paused).toBe(true)
    expect(pause.textContent?.trim()).toBe('paused')
    expect(pause.getAttribute('aria-pressed')).toBe('true')
    // Neither fact survives in the buffer, so the hold is only legible if
    // the interface states it: the moment it was taken, and the backlog.
    expect(stat()).toMatch(/held at \d{2}:\d{2}:\d{2}/)
    expect(stat()).toContain('arrived since, waiting')
    expect(stat()).toContain('212')

    await fireEvent.click(pause)
    flushSync()
    expect(appState.paused).toBe(false)
    expect(pause.textContent?.trim()).toBe('pause')
  })

  it('`group` toggles the fold-repeats mode and reports it as a pressed state', async () => {
    const { group } = renderHand()

    expect(group.getAttribute('aria-pressed')).toBe('false')

    await fireEvent.click(group)
    flushSync()

    expect(groupModeState.enabled).toBe(true)
    expect(group.getAttribute('aria-pressed')).toBe('true')
    expect(group.classList.contains('on')).toBe(true)
  })

  it('`wipe` empties this screen, and the stat line says so only while the lines are still gone', async () => {
    appState.events = [event()]
    const { wipe, stat } = renderHand()
    expect(stat()).not.toContain('wiped')

    await fireEvent.click(wipe)
    flushSync()

    expect(appState.events).toEqual([])
    // Only this screen's copy went; the server's ring is untouched, which
    // is the half the operator cannot see and so the half worth stating.
    expect(stat()).toMatch(/wiped \d{2}:\d{2}:\d{2}/)

    appState.events = [event()]
    flushSync()
    // "wiped at" stops being true of what you are looking at the moment
    // lines are back, so the clause goes rather than lingering all session.
    expect(stat()).not.toContain('wiped')
  })

  it('`csv ↓` is disabled with nothing held, and otherwise says what it would give before giving it', () => {
    const { csv } = renderHand()

    // Disabled rather than hidden: the way to an export never silently
    // stops existing.
    expect(csv.disabled).toBe(true)

    appState.events = [event(), event()]
    flushSync()

    expect(csv.disabled).toBe(false)
    // The count is off the same filtered set the table is drawn from, so
    // the figure on the control and the rows in the file are one thing.
    expect(csv.title).toContain('2 rows, every column')
  })

  // Round 36 (#1005): "ring holds 41 m" replaced the old buffer-%
  // reading with how far back the held buffer's own oldest line goes --
  // the same set csv ↓'s title counts above.
  it('says how far back the held buffer reaches, after top port', () => {
    appState.events = [event({ id: 1, receivedAt: BASE }), event({ id: 2, receivedAt: BASE + 41 * MIN })]
    const { stat } = renderHand()

    expect(stat()).toContain('ring holds 41 m')
  })

  it('says nothing of the reach with an empty buffer, rather than "ring holds 0 m"', () => {
    appState.events = []
    const { stat } = renderHand()

    expect(stat()).not.toContain('ring holds')
  })
})

// #729/#1197: the column chooser used to ride FilterBar.svelte's own
// fold-out strip; the owner's ruling on #1197 moved the desktop trigger
// and its popover onto this hand instead, right after csv ↓ -- this hand
// is already control over what the table holds and shows, and choosing
// columns is one more of those. FilterBar.svelte keeps its own drawer
// for the mobile always-open list, but the checkbox list itself is
// ColumnToggles.svelte, shared between the two rather than duplicated
// (#1218 audit finding 13) -- FilterBar.svelte.test.ts covers that
// component mounted in FilterBar's own drawer context. columnState
// is a module-level singleton (shared with columns.svelte.test.ts,
// ColumnToggles.svelte.test.ts, FilterBar.svelte.test.ts and
// LiveTable.svelte.test.ts), so every test
// below restores it rather than leaking a toggle into whichever test
// runs next.
describe('the columns ▸ picker (#729/#1197)', () => {
  beforeEach(() => {
    columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
  })

  afterEach(() => {
    columnState.visible = Object.fromEntries(COLUMNS.map((c) => [c.key, true]))
  })

  it('draws the trigger as the last pill on the hand, right after csv ↓', () => {
    const { container } = render(Whisper)
    const wpills = Array.from(container.querySelectorAll<HTMLButtonElement>('.wpill'))

    expect(wpills.map((b) => b.textContent?.trim())).toEqual(['wipe', 'csv ↓', 'columns ▸'])
    const trigger = wpills[wpills.length - 1]
    expect(trigger.getAttribute('aria-haspopup')).toBe('true')
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    expect(trigger.title).toBe('Choose which columns the stream shows')
  })

  it('has exactly one columns ▸ trigger on the page', () => {
    render(Whisper)

    expect(screen.getAllByRole('button', { name: 'Choose which columns the stream shows' }).length).toBe(1)
  })

  it('offers a checkbox for every optional column, and none for the pinned two', async () => {
    render(Whisper)
    await openColumns()

    // Time and Rule are each a unique label in this list -- a plain
    // queryByRole miss proves no checkbox exists for either.
    for (const key of PINNED_COLUMNS) {
      const label = COLUMNS.find((c) => c.key === key)?.label as string
      expect(screen.queryByRole('checkbox', { name: `${label} column` })).toBeNull()
    }

    // 15 columns, 2 pinned -- 13 checkboxes total.
    expect(screen.getAllByRole('checkbox').length).toBe(COLUMNS.length - PINNED_COLUMNS.size)

    // Spot-check a couple of ordinary columns with unique labels.
    expect(screen.getByRole('checkbox', { name: 'Device column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Chain column' })).toBeTruthy()
  })

  // #710: "Address column" used to name two different checkboxes (source's
  // and destination's), which is exactly the kind of thing an accessible
  // name is supposed to rule out. Each one carries its own disambiguated
  // aria-label even though the two read identically on screen ("address"
  // under each of two headings) -- this is what makes a by-name lookup
  // for either possible at all.
  it('disambiguates the address/port/MAC checkboxes that repeat visually, by aria-label', async () => {
    render(Whisper)
    await openColumns()

    expect(screen.getByRole('checkbox', { name: 'Source address column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Destination address column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Source port column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Destination port column' })).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Source MAC column' })).toBeTruthy()
  })

  // The visible word is the bare column name, not "<Label> column" -- the
  // "column" suffix and the disambiguation both still exist, just in the
  // aria-label (checked above), not on screen where two "Address column"s
  // side by side is what read as clunky in the first place.
  it('draws the bare column name on screen, grouped under source/destination headings for the repeated ones', async () => {
    render(Whisper)
    await openColumns()

    const panel = document.querySelector('.col-panel') as HTMLElement
    const labelTexts = Array.from(panel.querySelectorAll('.col-toggle')).map((el) => el.textContent?.trim())
    expect(labelTexts).toContain('device')
    expect(labelTexts).toContain('NAT')
    // "address" appears twice on screen -- once per heading -- which is
    // exactly the point: the heading, not the checkbox's own text, is
    // what tells the two apart now.
    expect(labelTexts.filter((t) => t === 'address').length).toBe(2)

    const headings = Array.from(panel.querySelectorAll('.col-group-heading')).map((el) => el.textContent?.trim())
    expect(headings).toEqual(['source', 'destination'])
  })

  it('defaults every checkbox to checked -- the shipped default stays all fifteen columns', async () => {
    render(Whisper)
    await openColumns()

    expect(screen.getByRole('checkbox', { name: 'Device column' })).toHaveProperty('checked', true)
  })

  it('unchecking a column writes through to columnState, and is a reader preference -- not tied to any filter term', async () => {
    render(Whisper)
    await openColumns()

    const device = screen.getByRole('checkbox', { name: 'Device column' })
    await fireEvent.click(device)
    flushSync()

    expect(columnState.isColumnVisible('device')).toBe(false)
    expect(appState.hasActiveFilters).toBe(false)
  })

  // The toggle itself -- opens on click, closes again on a second click,
  // on Escape (returning focus to the toggle), and on a click elsewhere
  // on the page.
  it('opens and closes the panel by clicking the toggle again', async () => {
    render(Whisper)
    await openColumns()
    expect(screen.getByRole('checkbox', { name: 'Device column' })).toBeTruthy()

    await openColumns()
    expect(screen.queryByRole('checkbox')).toBeNull()
  })

  it('closes the panel on Escape, and returns focus to the trigger', async () => {
    render(Whisper)
    await openColumns()

    await fireEvent.keyDown(window, { key: 'Escape' })
    flushSync()

    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Choose which columns the stream shows' }))
  })

  it('closes the panel on a click elsewhere on the page', async () => {
    render(Whisper)
    await openColumns()

    await fireEvent.click(document.body)
    flushSync()

    expect(screen.queryByRole('checkbox')).toBeNull()
  })

  // #1197 item 3 of the original ruling: one click undoes a bad drag. The
  // test's own final reset() call is what leaves columnState.widths clean
  // for whichever test runs next -- there is no separate afterEach for
  // widths (only `visible` is restored above), so this has to put its own
  // mutation back.
  it('offers "reset widths" in the panel, disabled only once widths already match the default', async () => {
    render(Whisper)
    await openColumns()

    const resetBtn = screen.getByRole('button', { name: 'reset widths' })
    expect(columnState.isDefault).toBe(true)
    expect(resetBtn).toHaveProperty('disabled', true)

    columnState.setWidth(0, 999)
    flushSync()
    expect(resetBtn).toHaveProperty('disabled', false)

    await fireEvent.click(resetBtn)
    flushSync()

    expect(columnState.isDefault).toBe(true)
    expect(resetBtn).toHaveProperty('disabled', true)
  })
})
