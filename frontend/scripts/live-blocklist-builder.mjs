// SPDX-License-Identifier: AGPL-3.0-only
//
// #1360: the blocklist builder against a real running mikroview. A
// router's push arrives through the real ingest endpoint -- RouterOS
// 7.24.4, Spamhaus DROP already loaded and its rule firing -- and the
// page is opened from Settings ▸ drop list, as an operator opens it:
// the version line is the pushed one, switching Emerging Threats to Not
// now takes its three parts out of the block, Copy puts the block on the
// clipboard, and the ledger reads what the router holds from its push.
//
// No RouterOS router is booted: what a router sends is the payload
// below, the shape a real CHR produced (internal/ingest's
// TestDecodeRealRawRulePush and TestDecodeRealAddressListCountPush). The
// block itself is proven on real CHRs by scripts/live-blocklist-chr.sh.

import { check, done, enrolDevice, goTo, grantClipboard, pushFrom, session } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const DEVICE = 'bl-rb5009'
const ADDR = '127.0.0.61'

const { page } = await session()

await enrolDevice(page.request, URL_BASE, DEVICE, ADDR)
const tokRes = await page.request.post(`${URL_BASE}/api/tokens`, {
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  data: { name: 'live-blocklist', kind: 'ingest', device: DEVICE },
})
check(tokRes.status() === 201, `an ingest token is minted for ${DEVICE} (${tokRes.status()})`)
const token = (await tokRes.json()).value

const page1 = (kind, records) => ({ kind, page: 1, pages: 1, routerosVersion: '7.24.4 (stable)', wizardVersion: 6, records })
for (const [kind, records] of [
  ['filter-rule', []],
  ['raw-rule', [
    { family: 'ip', ordinal: 0, comment: 'mikroview blocklist: spamhaus (from)', chain: 'prerouting', action: 'drop', srcAddressList: 'mv-bl-spamhaus', dstAddressList: null, logPrefix: 'D|bl-spamhaus|', log: true, disabled: false, packets: 412, bytes: 24720 },
    { family: 'ipv6', ordinal: 0, comment: 'mikroview blocklist: spamhaus (from)', chain: 'prerouting', action: 'drop', srcAddressList: 'mv-bl-spamhaus6', dstAddressList: null, logPrefix: 'D|bl-spamhaus|', log: true, disabled: false, packets: 0, bytes: 0 },
  ]],
  ['address-list-count', [
    { list: 'mv-bl-spamhaus', family: 'ip', count: 1692, loadedAt: '2026-10-01 04:17:02' },
    { list: 'mv-bl-spamhaus6', family: 'ipv6', count: 91, loadedAt: '2026-10-01 04:17:05' },
    { list: 'mv-bl-et', family: 'ip', count: 0, loadedAt: '' },
  ]],
]) {
  const status = await pushFrom(URL_BASE, ADDR, token, page1(kind, records))
  check(status === 200, `the router's ${kind} page is accepted (${status})`)
}

await goTo(page, 'Settings')
const door = page.getByRole('button', { name: 'Block known-bad addresses…' })
await door.waitFor({ timeout: 10000 })
await door.click()
const builder = page.locator('.bl-page')
await builder.waitFor({ timeout: 10000 })

// The door opens the group's first router; this scenario shares its
// instance, so pick this one where there are several.
const picker = builder.locator('select[aria-label="Router"]')
if (await picker.count()) await picker.selectOption(DEVICE)
await builder.locator('.vline').filter({ hasText: 'written for RouterOS 7.24.4' }).waitFor({ timeout: 10000 })
check(true, 'the version line is the one the router pushed: RouterOS 7.24.4')

const copy = builder.locator('.copyrow button.primary')
await page.waitForFunction(() => document.querySelector('.bl-page .copyrow button.primary')?.textContent === 'Copy — 2 lists · 8 parts', null, { timeout: 10000 })
check(true, 'the two default lists make eight parts: the push, three each, run now')

await builder.getByRole('region', { name: 'Emerging Threats compromised IPs' }).getByRole('button', { name: 'Not now' }).click()
await page.waitForFunction(() => document.querySelector('.bl-page .copyrow button.primary')?.textContent === 'Copy — 1 list · 5 parts', null, { timeout: 10000 })
check(true, 'Not now on Emerging Threats takes its three parts out of the block')
const block = await builder.locator('pre.script').textContent()
check(!block.includes('mv-bl-et'), 'the block no longer names mv-bl-et')

await grantClipboard(page)
await copy.click()
const clip = await page.evaluate(() => navigator.clipboard.readText())
check(clip.includes('/system script run mv-bl-spamhaus') && clip.includes('log-prefix="D|bl-spamhaus|"'), 'Copy puts the block on the clipboard')
check(clip.includes('/system script add name=mv-push'), 'the copied block re-sets the push with this page’s token')

const spamhaus = builder.locator('.ledger .row').first()
const receipt = await spamhaus.textContent()
check(receipt.includes('1,692 held (+ 91 IPv6)') && receipt.includes('refreshed 04:17'), `the Spamhaus row reads held … refreshed (${receipt.trim()})`)

await builder.getByRole('button', { name: 'Back to the drop list' }).click()
await builder.waitFor({ state: 'detached', timeout: 5000 })
check(true, 'Back to the drop list closes the page')

done()
