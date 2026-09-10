// SPDX-License-Identifier: AGPL-3.0-only
//
// The declare form's footer, on both surfaces (#1031).
//
// DESIGN.md "Cards" ratifies one interaction, the same on both
// surfaces, and lists the declare form's own order: reason (required),
// `both directions ☑`, `Declare`, and who. The two surfaces had drifted
// -- "both directions" on its own line above the buttons on the 2D map,
// tucked in beside them in the city -- and a reader crossing the slider
// saw the footer rearrange itself under a card that is 288px wide on
// both sides.
//
// The card is not a shared component, so the footer is written twice
// on purpose and this is what stops the two copies parting again: the
// rendered structure is compared element for element, and the two
// stylesheets' own `.form` rules are compared property for property.
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { mockupEstate } from '../lib/city/fixture'
import { layoutGround } from '../lib/city/layout'
import { appState } from '../lib/state.svelte'
import { authState } from '../lib/auth.svelte'
import { zonesState } from '../lib/zones.svelte'
import { policyState } from '../lib/policy.svelte'
import { coverageState } from '../lib/coverage.svelte'
import { baselineState } from '../lib/baseline.svelte'
import { EMPTY_OFF_BASELINE } from '../lib/baseline'
import { flagsState } from '../lib/flags.svelte'
import { hostsState } from '../lib/hosts.svelte'
import { watchlistState } from '../lib/watchlist.svelte'
import { topologyNavState } from '../lib/topologyNav.svelte'
import { altitudeStopState } from '../lib/altitudeStop.svelte'
import { wizardState } from '../lib/wizard.svelte'
import { emptyFilters, type ClientEvent } from '../lib/types'
import City from './City.svelte'
import Topography from './Topography.svelte'
// Vite's own `?raw` import, as Topography.svelte.test.ts already uses:
// the stylesheet halves are read as source text rather than through
// `node:fs`, which the browser-only app tsconfig has no types for.
import citySource from './City.svelte?raw'
import topographySource from './Topography.svelte?raw'

// Both components build a wide SVG into jsdom, and this file mounts one
// of each. The same 20000ms both their own suites set, for the reason
// City.svelte.test.ts records at length: the cost is jsdom's.
vi.setConfig({ testTimeout: 20000 })

const ground = layoutGround(mockupEstate())

function event(overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: 1,
    time: '2026-08-08T12:00:00Z',
    deviceId: 'router1',
    sourceIp: '192.168.1.50',
    action: 'accept',
    ruleLabel: 'test-rule',
    chain: 'forward',
    raw: '',
    receivedAt: Date.now(),
    ...overrides,
  }
}

beforeEach(() => {
  appState.devices = []
  appState.events = []
  appState.filters = emptyFilters()
  appState.view = 'topography'
  authState.role = 'admin'
  authState.username = 'tom'
  zonesState.pushed = []
  policyState.edges = []
  policyState.byDevice = {}
  policyState.anyPushed = false
  coverageState.declarations = []
  coverageState.error = null
  hostsState.hosts = []
  hostsState.error = null
  baselineState.off = EMPTY_OFF_BASELINE
  baselineState.error = null
  flagsState.list = []
  watchlistState.entries = []
  watchlistState.coverage = {}
  topologyNavState.pendingFlagId = null
  topologyNavState.pendingWatchId = null
  topologyNavState.pendingDescend = null
  wizardState.open = false
  altitudeStopState.stop = 'city'
  localStorage.removeItem('mikroview:topography-altitude')
})

afterEach(() => {
  vi.restoreAllMocks()
  authState.role = ''
  authState.username = ''
})

/**
 * A rendered subtree as the shape a reader sees: tag, classes, the
 * attributes that decide the layout, and each element's own words, in
 * document order and nesting.
 *
 * Svelte's own scoping classes are dropped -- they are a different hash
 * per component by design and say nothing about the footer -- and so
 * are `id`/`for`, which are unique per mount on purpose.
 */
function shape(el: Element, depth = 0): string[] {
  const classes = [...el.classList].filter((c) => !c.startsWith('svelte-')).sort()
  const attrs = ['type', 'placeholder', 'disabled']
    .filter((a) => el.hasAttribute(a))
    .map((a) => `${a}=${el.getAttribute(a) === '' ? a : el.getAttribute(a)}`)
  const own = [...el.childNodes]
    .filter((n) => n.nodeType === 3)
    .map((n) => (n.textContent ?? '').replace(/\s+/g, ' ').trim())
    .filter(Boolean)
    .join(' ')
  const line =
    '  '.repeat(depth) +
    el.tagName.toLowerCase() +
    (classes.length > 0 ? '.' + classes.join('.') : '') +
    (attrs.length > 0 ? ' [' + attrs.join(' ') + ']' : '') +
    (own.length > 0 ? ` "${own}"` : '')
  return [line, ...[...el.children].flatMap((c) => shape(c, depth + 1))]
}

/** The 2D map's declare form, open. */
function flatForm(): Element {
  zonesState.pushed = [{ address: '10.0.40.1/24', network: '10.0.40.0', interface: 'bridge4', comment: 'Guest' }]
  appState.events = [event({ inInterface: 'bridge4', srcIp: '10.0.40.9' }), event({ id: 2, inInterface: 'ether1', srcIp: '8.8.8.8' })]
  policyState.anyPushed = true
  policyState.edges = [
    { key: 'bridge4|ether1', from: 'bridge4', to: 'ether1', accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
    { key: 'ether1|bridge4', from: 'ether1', to: 'bridge4', accepted: false, refused: true, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged: false },
  ]
  const { container } = render(Topography)
  flushSync()
  container.querySelector('.cov-g')!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  flushSync()
  container.querySelector<HTMLButtonElement>('.card .pin')!.click()
  flushSync()
  const form = container.querySelector('.card .form')
  expect(form, 'the 2D map opened no declare form to compare').not.toBeNull()
  return form!
}

/** The city's declare form, open. */
async function cityForm(): Promise<Element> {
  const { container } = render(City, { props: { stop: 'district', ground } })
  const wall = [...container.querySelectorAll('[data-wall]')].find((w) => w.getAttribute('aria-label')?.includes('dark'))
  expect(wall, 'the city drew no dark wall to open').toBeTruthy()
  await fireEvent.pointerEnter(wall!)
  flushSync()
  await fireEvent.click(container.querySelector('.bcard .pin')!)
  flushSync()
  const form = container.querySelector('.bcard .form')
  expect(form, 'the city opened no declare form to compare').not.toBeNull()
  return form!
}

/**
 * A component's own rules for one selector prefix, with the prefix
 * stripped -- so the 2D map's `.card .form .go` and the city's
 * `.bcard .form .go` come out as the same line if they say the same
 * thing. Order is kept: two stylesheets that declare the same
 * properties in a different order are not the same stylesheet.
 */
function formRules(source: string, prefix: string): string {
  const out: string[] = []
  // Comments first: a `/* ... */` above a rule would otherwise be read
  // as part of the selector, and both files have one there saying to
  // keep these two blocks in step.
  const css = source.replace(/\/\*[\s\S]*?\*\//g, '')
  for (const m of css.matchAll(/^ {2}([^{}\n][^{}]*)\{([^{}]*)\}/gm)) {
    const selector = m[1].replace(/\s+/g, ' ').trim()
    if (selector !== prefix && !selector.startsWith(prefix + ' ')) continue
    out.push(`${selector.slice(prefix.length).trim() || '(itself)'} { ${m[2].replace(/\s+/g, ' ').trim()} }`)
  }
  return out.join('\n')
}

describe('the declare form’s footer, on both surfaces (#1031)', () => {
  it('renders the same structure on the 2D map and in the city', async () => {
    const flat = shape(flatForm())
    const city = shape(await cityForm())
    expect(city).toEqual(flat)
  })

  it('puts “both directions” on its own line, above the buttons, on both', async () => {
    // The layout chosen, and why it is this one rather than the city's
    // old row: DESIGN.md "Cards" already lists the order -- reason,
    // `both directions ☑`, `Declare`, who -- and the row does not fit.
    // The card is 288px wide, which is 262px of content; `Declare`,
    // `cancel`, the checkbox and the gaps between them take about 141
    // of it, leaving room for roughly 19 characters of 10.5px monospace
    // against the 24 in "as tom · both directions" -- and a longer
    // username only makes it worse.
    for (const form of [flatForm(), await cityForm()]) {
      const both = form.querySelector('label.both')!
      const btns = form.querySelector('.btns')!
      expect(both, 'no “both directions” line').not.toBeNull()
      expect(both.querySelector('input[type="checkbox"]')).not.toBeNull()
      expect(btns.contains(both)).toBe(false)
      expect(both.compareDocumentPosition(btns) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
      // Who signs it stays in the button row, where the drawing puts it.
      expect(btns.querySelector('.who')?.textContent?.trim()).toBe('as tom')
      expect([...btns.querySelectorAll('button')].map((b) => b.textContent?.trim())).toEqual(['Declare', 'cancel'])
    }
  })

  it('styles it from two rule sets that say the same thing', () => {
    // The markup being identical is only half of it: the city was
    // painting its Declare button `var(--raised)`, a token this app
    // defines nowhere, so the same footer would still not have looked
    // the same.
    const flat = formRules(topographySource, '.card .form')
    const city = formRules(citySource, '.bcard .form')
    expect(flat).not.toBe('') // the extraction found something to compare
    expect(city).toBe(flat)
  })
})
