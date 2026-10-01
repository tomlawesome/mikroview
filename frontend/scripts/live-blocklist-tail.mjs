// SPDX-License-Identifier: AGPL-3.0-only
//
// #1360: the wizard's first-run tail against a real running mikroview.
// A router that has done everything the wizard asks -- fetched the
// certificate, enrolled over syslog, pushed on RouterOS 7.24.4 -- and
// Run setup… lands on Where setup stands with the tail offered: a sixth
// ledger row with Set it up, and Block known-bad addresses beside
// Finish, which stays the primary. The offer opens the builder in the
// wizard's frame; Not now records the tail's decision (record 8) through
// the real POST /api/setup/mark and sets the row aside.
//
// Its own scenario rather than a beat in live-setup-wizard.mjs: that one
// stops at the rail's gating and never reaches the ledger, which is
// where the tail is offered.
//
// The shared instance keeps its setup ledger across resets, so record 8
// stays marked after the first run. The status read is narrowed to this
// scenario's router, and step-8 marks are left out of it, so a re-run on
// the same instance still sees a first run; the mark itself is checked
// on the wire.

import http from 'http'
import https from 'https'
import { check, done, enrolDevice, goTo, pushFrom, session } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const DEVICE = 'bl-tail'
const ADDR = '127.0.0.62'

const onlyMine = async (route) => {
  const res = await route.fetch()
  const body = await res.json()
  if ((body.devices ?? []).some((d) => d.device === DEVICE)) {
    body.sources = (body.sources ?? []).filter((s) => s.source === ADDR)
    body.devices = body.devices.filter((d) => d.device === DEVICE)
  }
  body.marks = (body.marks ?? []).filter((m) => m.step !== 8)
  body.witnesses = (body.witnesses ?? []).filter((w) => w.step !== 8)
  await route.fulfill({ response: res, body: JSON.stringify(body) })
}
const myDevice = async (route) => {
  const res = await route.fetch()
  const body = await res.json()
  const list = Array.isArray(body) ? body : body.devices
  const mine = (list ?? []).filter((d) => d.id === DEVICE)
  const out = mine.length ? mine : list
  await route.fulfill({ response: res, body: JSON.stringify(Array.isArray(body) ? out : { ...body, devices: out }) })
}

const { page, consoleErrors } = await session({
  mocksApi: true,
  routes: [
    ['**/api/setup/status', onlyMine],
    ['**/api/devices', myDevice],
  ],
})

// The router's own side: enrol over syslog, fetch the certificate from
// its address, push.
await enrolDevice(page.request, URL_BASE, DEVICE, ADDR)
const caStatus = await new Promise((resolve, reject) => {
  const url = new URL(`${URL_BASE}/ca.crt`)
  const mod = url.protocol === 'https:' ? https : http
  mod
    .get({ hostname: url.hostname, port: url.port, path: url.pathname, localAddress: ADDR, rejectUnauthorized: false }, (res) => {
      res.resume()
      resolve(res.statusCode)
    })
    .on('error', reject)
})
check(caStatus === 200, `${DEVICE} fetches the certificate from ${ADDR} (${caStatus})`)
const tokRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-blocklist-tail', kind: 'ingest', device: DEVICE },
})
const token = (await tokRes.json()).value
const pushed = await pushFrom(URL_BASE, ADDR, token, { kind: 'filter-rule', page: 1, pages: 1, routerosVersion: '7.24.4 (stable)', wizardVersion: 6, records: [] })
check(pushed === 200, `${DEVICE} pushes on RouterOS 7.24.4 (${pushed})`)

await goTo(page, 'Run setup…')
const wizard = page.locator('.page.wiz')
await wizard.waitFor({ state: 'visible' })
const setItUp = wizard.getByRole('button', { name: 'Set it up' })
await setItUp.waitFor({ timeout: 20000 })
check(true, 'Where setup stands offers the tail: a sixth ledger row with Set it up')
const railTitles = await wizard.locator('.rail .step-title').allTextContents()
check(railTitles[5] === 'Block known-bad addresses', `the rail gains the sixth row (${JSON.stringify(railTitles)})`)
const footButtons = await wizard.locator('.foot button').evaluateAll((bs) => bs.map((b) => [b.textContent.trim(), b.classList.contains('primary')]))
check(
  JSON.stringify(footButtons) === JSON.stringify([['Add another router', false], ['Block known-bad addresses', false], ['Finish', true]]),
  `Block known-bad addresses sits beside Finish, and Finish stays the primary (${JSON.stringify(footButtons)})`,
)

await wizard.locator('.foot button', { hasText: 'Block known-bad addresses' }).click()
await wizard.locator('.body.wide h3', { hasText: `Block known-bad addresses on ${DEVICE}.` }).waitFor({ timeout: 10000 })
await page.waitForFunction(() => /^Copy — 2 lists · 8 parts$/.test(document.querySelector('.wiz .foot button.primary')?.textContent?.trim() ?? ''), null, { timeout: 15000 })
check(true, 'the stage is the builder in the wizard’s frame, its Copy counting the block')

const marked = page.waitForResponse((r) => r.url().endsWith('/api/setup/mark') && r.request().method() === 'POST')
await wizard.locator('.foot button', { hasText: /^Not now$/ }).click()
const markRes = await marked
const markBody = await markRes.json().catch(() => ({}))
check(markRes.status() < 300 && markBody.step === 8 && markBody.outcome === 'skipped', `Not now records record 8 as skipped (${markRes.status()} ${JSON.stringify(markBody)})`)
await wizard.locator('.ledger .r', { hasText: 'not now · Settings ▸ drop list' }).waitFor({ timeout: 10000 })
check(true, 'the row reads not now · Settings ▸ drop list')
check((await setItUp.count()) === 0, 'Set it up is gone once the tail is set aside')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
