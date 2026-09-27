// SPDX-License-Identifier: AGPL-3.0-only
//
// #1352: the "country and network owner" group in Settings, and the two
// data credits in About, driven for real.
//
// What this proves that a component test over a mocked API cannot:
//
//  - A key typed into the group really reaches the server and comes
//    back as "key set", with the field emptied -- and the server's own
//    GET never hands the key back (the write-only rule on #1352).
//  - Remove is two clicks against the real endpoint, and the server
//    then reports no key.
//  - About carries the IPinfo and MaxMind credits with working links,
//    and never names DB-IP.
//
// What it does not depend on: the internet. The token is fake, so the
// download it sets off fails (or never starts on a gate with no route
// out); the group's rows are asserted, never a download succeeding or a
// source switching. The server's `source` is read back as-is.

import { session, check, done, goTo, openAccountMenu, responsive } from './live-browser.mjs'

const GEO = '#engineroom-geo'
const IPINFO = `${GEO} [data-source="ipinfo"]`
const FAKE_TOKEN = 'live-geo-sources-not-a-real-token'

const { page, consoleErrors } = await session()

/** The server's own answer -- what the group restates. */
async function geo(p) {
  const res = await p.request.get(new URL('/api/settings/geo', p.url()).toString())
  check(res.ok(), `GET /api/settings/geo answers an admin -- status ${res.status()}`)
  return { text: await res.text(), body: res.ok() ? await res.json() : null }
}

const flat = async (locator) => ((await locator.textContent()) ?? '').replace(/\s+/g, ' ').trim()

// --- the group is there, beside disk ---------------------------------------

await goTo(page, 'Settings')
await page.waitForSelector(GEO, { timeout: 15000 })
check(
  (await page.locator(`${GEO} > h3`).textContent())?.trim() === 'country and network owner',
  'Settings has a "country and network owner" group',
)
check(
  await page.$eval(GEO, (el) => el.previousElementSibling?.id === 'diskg'),
  'the group sits directly after the disk group',
)
const topLine = await flat(page.locator(`${GEO} [data-testid="geo-in-use"]`))
check(
  /^Flags from (DB-IP Lite|IPinfo Lite|MaxMind GeoLite2)$/.test(topLine) || topLine === 'No flag source loaded yet',
  `the top line says which source is in use -- got "${topLine}"`,
)
for (const source of ['dbip', 'ipinfo', 'maxmind']) {
  check((await page.locator(`${GEO} [data-source="${source}"]`).count()) === 1, `the group has a ${source} row`)
}

const before = await geo(page)
check(before.body?.sources?.ipinfo?.keySet === false, 'the instance starts with no IPinfo key')

// --- set a fake IPinfo token -------------------------------------------------

const field = page.locator(`${IPINFO} input[aria-label="IPinfo token"]`)
await field.fill(FAKE_TOKEN)
await page.click(`${IPINFO} button:has-text("set")`)
await page.locator(`${IPINFO} input`).waitFor({ state: 'detached', timeout: 15000 })

const setRow = await flat(page.locator(`${IPINFO} .orow`))
check(/key set · by /.test(setRow), `the IPinfo row reads "key set · by …" -- got "${setRow}"`)
check(!setRow.includes(FAKE_TOKEN), 'the row does not show the key')
check(!(await page.content()).includes(FAKE_TOKEN), 'the key is nowhere on the page, the field emptied with it')

const afterSet = await geo(page)
check(afterSet.body?.sources?.ipinfo?.keySet === true, 'the server reports an IPinfo key set')
check(!afterSet.text.includes(FAKE_TOKEN), 'the server never hands the key back')

// --- remove it: two clicks ---------------------------------------------------

const remove = page.locator(`${IPINFO} button.revoke`)
await remove.click()
check((await flat(remove)).startsWith('confirm'), 'the first click on remove only arms it')
check((await geo(page)).body?.sources?.ipinfo?.keySet === true, 'one click removes nothing on the server')
await remove.click()
await page.locator(`${IPINFO} input[aria-label="IPinfo token"]`).waitFor({ state: 'visible', timeout: 15000 })
check(!/key set/.test(await flat(page.locator(`${IPINFO} .orow`))), 'the second click removes it: the row offers the field again')
check((await geo(page)).body?.sources?.ipinfo?.keySet === false, 'the server reports no IPinfo key')

// --- About: the IPinfo and MaxMind credits ----------------------------------

await openAccountMenu(page)
await page.click('.account .menu button.row:has-text("About & licence")')
const about = page.locator('[role="dialog"][aria-label="About MikroView"]')
await about.waitFor({ state: 'visible', timeout: 5000 })
check(
  (await about.locator('a[href="https://ipinfo.io"]').count()) === 1,
  'About links IPinfo',
)
check(
  (await flat(about.locator('[data-testid="about-credit-ipinfo"]'))) === 'Country and network data from IPinfo Lite.',
  'About credits IPinfo Lite in the settled words',
)
check(
  (await about.locator('a[href="https://www.maxmind.com"]').count()) === 1,
  'About links MaxMind',
)
check(
  (await flat(about.locator('[data-testid="about-credit-maxmind"]'))).startsWith(
    'This product includes GeoLite data created by MaxMind',
  ),
  'About carries the MaxMind credit in its licence words',
)
check(!/DB-IP/i.test((await about.textContent()) ?? ''), 'About never names DB-IP')
await page.keyboard.press('Escape')
await about.waitFor({ state: 'detached', timeout: 5000 })

check(await responsive(page), 'the main thread is still answering')
check(consoleErrors.length === 0, `no console errors -- got ${JSON.stringify(consoleErrors)}`)

done()
