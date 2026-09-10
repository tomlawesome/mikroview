// SPDX-License-Identifier: AGPL-3.0-only
//
// The device dossier card (#410).
//
// HostDossier.svelte.test.ts already proves the composition against a
// hand-built response: seven sections, the locally-administered line,
// the absent line, the note-only identity. What a jsdom test cannot
// show is any of this against a real assembly:
//
// - The card is reachable at all. It is mounted once at the app root
//   and opened from four other components; a wiring mistake there
//   renders a component test perfectly green while nothing on the built
//   page opens anything.
// - The response is the Go backend's own, not a fixture. The field
//   names in lib/types.ts are hand-copied from internal/dossier, and a
//   typo there is invisible to TypeScript -- it simply renders an
//   undefined section. Driving it against a live instance that has just
//   been fed traffic is the only place the two halves meet.
// - The honesty line and "Reads as" have to survive the real bundle and
//   this app's CSP, which is how #659 shipped (a static style attribute
//   Chromium tolerates and Firefox refuses).

import { session, feedSyslog, check, responsive, goTo, done, waitForStreamRows } from './live-browser.mjs'

const { page, consoleErrors } = await session()

// Its own traffic, so the host the card is opened for has events of its
// own to be assembled from (the instance is reset before every
// scenario, #1064).
feedSyslog(60, 'live-dossier')
await waitForStreamRows(page, 60)

// --- The Entities host row opens the card -------------------------------
await goTo(page, 'Entities')
const card = '.card[aria-hidden="false"]'
await page.waitForSelector(`${card} #eviews`, { timeout: 15000 })
await page.click(`${card} #eviews [data-v="hosts"]`)
await page.waitForSelector(`${card} #eviews [data-v="hosts"].on`, { timeout: 5000 })

const opener = page.locator(`${card} .etable tbody .dossier-btn`).first()
await opener.waitFor({ state: 'visible', timeout: 15000 })
const openerIp = ((await opener.textContent()) ?? '').trim()
check(openerIp.length > 0, `the hosts view offers a host to open a dossier for -- got "${openerIp}"`)
await opener.click()

const sheet = page.locator('[data-testid="host-dossier"]')
await sheet.waitFor({ state: 'visible', timeout: 15000 })
check(await sheet.isVisible(), 'clicking a host address on the Entities row opens the dossier')

// --- It assembles: the sections land, from the real backend -------------
// Waited for rather than read immediately: the fetch is in flight while
// the ghost rows show, and asserting on the loading state's DOM would
// be asserting the card never filled in.
await sheet.locator('h3:text-is("Reads as")').waitFor({ state: 'visible', timeout: 15000 })

const headings = await sheet.locator('h3').allTextContents()
check(
  headings.slice(0, 7).join(',') === 'Reads as,Seen,Names,MAC,Address,Traffic,Firewall',
  `the card opens with Reads as over the six field marks, in order -- got ${JSON.stringify(headings)}`,
)

// The identity section says something either way: a suggestion with its
// confidence in a word, or the backend's note as the whole section.
// Which one this host draws depends on what it happened to talk to, so
// the assertion is that the section is not empty -- never that a
// particular profile was named, which would be the card overclaiming in
// test form.
const readsAs = ((await sheet.locator('section:has(h3:text-is("Reads as"))').textContent()) ?? '')
  .replace(/\s+/g, ' ')
  .replace(/^Reads as/, '')
  .trim()
check(readsAs.length > 0, `the identity section says something -- got "${readsAs}"`)
check(!/\d+%/.test(readsAs), `confidence is never a percentage -- got "${readsAs}"`)

const sheetText = ((await sheet.textContent()) ?? '').replace(/\s+/g, ' ')
check(
  sheetText.includes("Nothing here was probed or looked up outside the router's own pushes and the OUI registry."),
  'the honesty line is on the card, verbatim',
)

// --- The naming action, for a tier that can edit ------------------------
// session() signs in as the instance's admin, so the footer action is
// the one the editing tier sees. A viewer's card omits it, which
// HostDossier.svelte.test.ts covers.
check(
  (await sheet.locator('button:text-is("Name this device")').count()) === 1,
  'the card ends in Name this device for the editing tier',
)

// --- One dialog, spoken as headings, and Esc closes it ------------------
check((await sheet.getAttribute('role')) === 'dialog', 'the card is one dialog')
check(
  (await sheet.locator('section[aria-labelledby]').count()) >= 6,
  'each block is its own labelled section, so the card is spoken as headings',
)

await page.keyboard.press('Escape')
await sheet.waitFor({ state: 'hidden', timeout: 5000 })
check((await page.locator('[data-testid="host-dossier"]').count()) === 0, 'Esc closes the card')

check(await responsive(page), 'main thread responsive')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)
done()
