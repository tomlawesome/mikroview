// SPDX-License-Identifier: AGPL-3.0-only
//
// #1382: the full-screen setup wizard's first two steps -- The router
// (one form: name, address, push Yes / No, backup Yes / No) and Mint
// the token -- against a real running mikroview, as
// docs/design/screens/wizard/DESIGN.md draws them.
//
// The unit tests cover the address rule, the Yes / No controls and the
// calls the mint makes. What they cannot show is that a token minted
// through the real form is one the real server then honours: that the
// enrol line fed from the address the operator typed enrols the router
// they named, and that the rail's receipts read the server's answer
// back. So every assertion here goes through a real browser against a
// real server.
//
// live-env.sh's harness declares one router (live-router, 127.0.0.1)
// and has been feeding it since before the page loaded, so the wizard's
// own door -- Run setup… -- correctly reopens on the router's turn, the
// ledger as it stands (DESIGN.md, "The model"), and never on the form.
// A first router's happy path therefore cannot be walked on this
// harness without a second instance. As the wizard's own source-split
// scenario does for its own unreachable shape, this drives the real bundled
// components against the real server's actual answers with only the
// shape under test overridden: /api/setup/status with its sources not
// yet heard from, so the run starts at The router. Everything that
// follows -- the record, the mint, the enrol line, the receipts -- is
// the real server's doing; /api/devices is never touched.

import { session, check, done, goTo, adminPassword, feedRawFrom } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
// A loopback address of its own, so the enrol line arrives from the
// address the form was given and nowhere else (#1291: the window opens
// for that address alone), and a name no sibling scenario declares.
const ROUTER_NAME = 'rb-wizard-1382'
const ROUTER_ADDR = '127.0.0.82'

// Registered before sign-in (session()'s `routes`): the run is placed
// from the status read at sign-in, so a mock added afterwards never
// reaches it.
const { page, consoleErrors } = await session({
  mocksApi: true,
  routes: [
    [
      '**/api/setup/status',
      async (route) => {
        const res = await route.fetch()
        const body = await res.json()
        body.sources = (body.sources ?? []).map(({ syslogFirstSeenAt, ...s }) => s)
        await route.fulfill({ response: res, body: JSON.stringify(body) })
      },
    ],
  ],
})

const text = async (sel) => ((await page.locator(sel).textContent()) ?? '').replace(/\s+/g, ' ').trim()
const row = (n) => `.wiz .rail li:nth-child(${n}) .step-row`

// --- The way in: the full screen, on The router ------------------------------
await goTo(page, 'Run setup…')
const wizard = page.locator('.page.wiz')
await wizard.waitFor({ state: 'visible' })
check((await page.locator('.setup-wizard').count()) === 0, 'the wizard is a full screen, not the retired modal')
check((await page.locator(row(1)).getAttribute('aria-current')) === 'step', 'the run opens on The router')
check((await text(`${row(1)} .step-receipt`)) === 'name, address, push, backup', 'its receipt names the four')
for (const n of [2, 3, 4, 5]) {
  check(
    (await page.locator(row(n)).getAttribute('aria-disabled')) === 'true' &&
      (await page.locator(row(n)).getAttribute('title')) === 'After the step before it',
    `row ${n} is locked, titled with the reason`,
  )
}
check((await text('.wiz .body h3')) === 'Your first router.', 'the lead is the record’s')
check(/MikroView never connects to it — the router sends\./.test(await text('.wiz .body .hint')), 'and keeps the promise')
check(await page.locator('#f-name').evaluate((el) => el === document.activeElement), 'the name field has focus')

const next = page.locator('.wiz .foot button.primary:text-is("Next")')
check(await next.isDisabled(), 'Next waits for the four')
check((await text('.wiz .foot .fhint')) === 'All four, then Next', 'the footer says so')

// --- The address, checked as you type (#1380) ---------------------------------
await page.fill('#f-name', ROUTER_NAME)
const addr = page.locator('#f-addr')
const problem = page.locator('.wiz .form .problem')
const cases = [
  ['192.168.1', 'Four numbers, 0–255, separated by dots.'],
  ['192.168.1.1:514', 'No port here — just the address. The port is MikroView’s side.'],
  ['rb5009.lan', 'A name will not do: the enrolment window binds to an address.'],
  ['10.0.0.0/24', 'No prefix length — the router’s own address, not its network.'],
]
for (const [typed, line] of cases) {
  await addr.fill(typed)
  await problem.filter({ hasText: line }).waitFor({ state: 'visible', timeout: 5000 })
  check((await addr.getAttribute('aria-invalid')) === 'true', `${typed}: the field is marked invalid`)
  check(await next.isDisabled(), `${typed}: Next stays disabled`)
}
check((await problem.getAttribute('aria-live')) === 'polite', 'the problem line is aria-live')
await addr.fill(ROUTER_ADDR)
await page.waitForFunction(() => document.querySelector('.wiz .form .problem')?.textContent === '')
check((await addr.getAttribute('aria-invalid')) === 'false', 'a bare address clears the line')

// --- Push and backup, a plain Yes / No each -------------------------------------
const groups = page.locator('.wiz .form .seg[role="radiogroup"]')
check((await groups.count()) === 2, 'push and backup are two Yes / No pairs')
check(
  (await groups.nth(0).locator('button').allTextContents()).join('|') === 'Yes|No',
  'the buttons carry no interval -- the label’s own line says what each does',
)
check(await next.isDisabled(), 'Next still waits for push and backup')
await groups.nth(0).locator('button:text-is("Yes")').click()
await groups.nth(1).locator('button:text-is("No")').click()
check((await groups.nth(0).locator('button:text-is("Yes")').getAttribute('aria-checked')) === 'true', 'push: Yes is on')
check(
  await groups.nth(1).locator('button:text-is("No")').evaluate((el) => el.classList.contains('on') && el.classList.contains('no')),
  'backup: No is on, in its own dress',
)
await next.waitFor({ state: 'visible' })
await page.waitForFunction(() => !document.querySelector('.wiz .foot button.primary')?.disabled)
check(!(await next.isDisabled()), 'all four answered: Next is free')
check((await text('.wiz .foot .fhint')) === '', 'and the hint is gone')
const chosen = await text(`${row(1)} .step-receipt`)
check(
  chosen === `${ROUTER_NAME} · ${ROUTER_ADDR} · push yes · backup not now`,
  `the rail carries the four as the receipt (${chosen})`,
)
check(await page.locator(row(1)).evaluate((el) => el.classList.contains('chosen')), 'in the chosen dress')
check((await text('.wiz .bar .chips .att.dec')) === ROUTER_NAME, 'the bar wears the router’s name as a chip')

// --- Mint the token ---------------------------------------------------------------
await next.click()
await page.locator(`${row(2)}[aria-current="step"]`).waitFor({ state: 'visible' })
check((await text('.wiz .body h3')) === `Your password, to mint ${ROUTER_NAME}’s token.`, 'the lead names the router')
check(
  (await text('.wiz .body .hint')) ===
    `Minting opens the log port for ${ROUTER_ADDR}, for 15 minutes, so it asks for your password at that moment. The token goes at the end of the block.`,
  'and says why it asks',
)
check((await page.locator(row(1)).isDisabled()) === false, 'The router is a real button again: the run is still yours to change')
const back = page.locator('.wiz .foot button:text-is("Back")')
const mint = page.locator('.wiz .foot button.primary:text-is("Mint the token")')
check((await back.count()) === 1 && (await mint.count()) === 1, 'the footer offers Back and Mint the token')
check(await mint.isDisabled(), 'Mint waits for the password')
check((await page.locator('.wiz .body pre').count()) === 0, 'nothing of the block is shown before the token exists')

const pass = page.locator('.wiz .body input[type="password"]')
check(await pass.evaluate((el) => el === document.activeElement), 'the password field has focus')
await pass.fill(adminPassword)
check(!(await mint.isDisabled()), 'a password frees Mint')

const [minted] = await Promise.all([
  page.waitForResponse((r) => /\/api\/devices\/[^/]+\/enrolment$/.test(r.url()) && r.request().method() === 'POST'),
  mint.click(),
])
check(minted.status() === 201, `the mint is the modal’s own call, POST /api/devices/{id}/enrolment (${minted.status()})`)
check(minted.url().includes(`/api/devices/${ROUTER_NAME}/enrolment`), 'for the router the form named')
const mintBody = await minted.request().postDataJSON()
check(mintBody.expectedAddress === ROUTER_ADDR, `bound to the address the form was given (${mintBody.expectedAddress})`)
check(typeof mintBody.password === 'string' && mintBody.password.length > 0, 'with the password, re-proved at that moment')
const { token } = await minted.json()
check(typeof token === 'string' && token.length > 0, 'the server answers with a token')

const created = await page.request.get(`${URL_BASE}/api/devices`).then((r) => r.json())
const rec = (created.devices ?? []).find((d) => d.id === ROUTER_NAME)
check(!!rec, `the record was made on the server first (${JSON.stringify((created.devices ?? []).map((d) => d.id))})`)
check(!!rec?.enrolment?.pending, 'and holds the pending enrolment')

// The run moves on; the receipt reads the token's life back.
await page.locator(`${row(3)}[aria-current="step"]`).waitFor({ state: 'visible' })
const tokenReceipt = await text(`${row(2)} .step-receipt`)
check(/^token good until \d\d:\d\d$/.test(tokenReceipt), `the Mint row carries the token’s life (${tokenReceipt})`)
check(await page.locator(row(2)).evaluate((el) => el.classList.contains('chosen')), 'in decision blue until the certificate lands')
check((await pass.count()) === 0, 'the password field is gone with the step')

// --- The enrol line, from the address the form was given --------------------------
// What the router would do once the block is pasted: the enrol line
// arrives over syslog from the router's own address.
feedRawFrom(ROUTER_ADDR, `<14>Jan  1 00:00:00 ${ROUTER_NAME} mikroview-enrol ${token}`)
await page
  .locator(`${row(1)} .step-receipt`, { hasText: `${ROUTER_NAME} · enrolled ${ROUTER_ADDR} · ` })
  .waitFor({ state: 'visible', timeout: 20000 })
check(await page.locator(row(1)).evaluate((el) => el.classList.contains('done')), 'The router reads as done on evidence')
const enrolled = await text(`${row(1)} .step-receipt`)
check(
  new RegExp(`^${ROUTER_NAME} · enrolled ${ROUTER_ADDR.replace(/\./g, '\\.')} · \\d\\d:\\d\\d:\\d\\d$`).test(enrolled),
  `the receipt reads name · enrolled address · hh:mm:ss (${enrolled})`,
)
check(/^logs · \d\d:\d\d:\d\d$/.test(await text('.wiz .bar .chips .att.logs')), 'the bar wears the logs chip in its ink')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
