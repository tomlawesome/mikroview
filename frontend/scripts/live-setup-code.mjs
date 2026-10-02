// SPDX-License-Identifier: AGPL-3.0-only
//
// #1415: the first admin is created only with the one-time setup code
// the server prints in its log. This drives the create-account screen
// (docs/design/screens/setup-code/DESIGN.md) against a real server with
// an empty accounts store, reading the code out of that server's own
// log the way an operator does. What the unit tests cannot show:
//
//  1. That the code the screen asks for is the one the server logged --
//     the log line, the field and the server's check agree end to end.
//  2. That a wrong code is refused by the real server with the screen's
//     own copy, and the typed code stays in the field.
//  3. That the right code works typed the way people type it -- here
//     without dashes and in the other case -- and is dead once an
//     account exists.
//
// Run by scripts/live-setup-code.sh, standalone, on its own instance
// started with MV_FIRST_RUN=1 -- not by scripts/run-scenarios.sh, whose
// shared instance already holds its admin (and so shows no setup code at
// all). run-scenarios.sh excludes this file by name.
//
// Never prints the code: a check message says what was compared, not
// the value.

import fs from 'node:fs'
import path from 'node:path'
import { launchBrowser, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
const SERVER_LOG = path.join(process.env.MV_DIR || '', 'server.log')

// The screen's copy, from the design's "States and copy" table.
const EMPTY_LINE = "Enter the setup code from MikroView's log."
const WRONG_LINE = "that setup code didn't match -- the current one is in MikroView's log; restart MikroView for a new one"

/** The newest setup code in the server's log, or '' if it printed none. */
function setupCodeFromLog() {
  let text = ''
  try {
    text = fs.readFileSync(SERVER_LOG, 'utf8')
  } catch {
    return ''
  }
  const found = [...text.matchAll(/create the first admin with setup code ([A-Za-z0-9-]+)/g)]
  return found.length ? found[found.length - 1][1] : ''
}

async function sessionState() {
  const res = await fetch(`${URL_BASE}/api/auth/session`, { cache: 'no-store' })
  return res.json()
}

// --- The server's side: an empty store, and one code in its log --------

const code = setupCodeFromLog()
check(/^[A-Za-z0-9]{4}(-[A-Za-z0-9]{4}){3}$/.test(code), 'the empty instance logged a setup code shaped xxxx-xxxx-xxxx-xxxx')
check((await sessionState()).setupRequired === true, 'the instance starts with no account (setupRequired)')

// --- The screen ----------------------------------------------------------

const browser = await launchBrowser()
const page = await browser.newPage({ ignoreHTTPSErrors: true })
// Uncaught exceptions only: the wrong-code attempt below is a real 401,
// which the browser itself reports on the console as a failed resource.
const pageErrors = []
page.on('pageerror', (e) => pageErrors.push(String(e)))

await page.goto(URL_BASE, { waitUntil: 'networkidle' })
await page.getByRole('button', { name: 'Enter' }).click()
await page.getByRole('heading', { name: 'Create the admin account' }).waitFor({ timeout: 10000 })

const fields = await page.evaluate(() => [...document.querySelectorAll('form input')].map((i) => i.placeholder))
check(
  JSON.stringify(fields) === JSON.stringify(['setup code', 'account', 'password', 'confirm password']),
  `the setup code is the first field, above account (got ${JSON.stringify(fields)})`,
)

const note = page.locator('.subtitle-note', { hasText: "It's in MikroView's log from when it last started" })
check(await note.isVisible(), 'the note saying where the code is shows above the field')
// The words the note quotes, read off the note itself, have to be on
// the line the server logged -- or the operator greps for nothing.
const quoted = ((await note.textContent()) ?? '').replace(/\s+/g, ' ').match(/the line that says “([^”]+)”/)?.[1] ?? ''
check(
  quoted !== '' && fs.readFileSync(SERVER_LOG, 'utf8').includes(`${quoted} ${code}`),
  `the note quotes the words of the line the server actually logs ("${quoted}")`,
)

const codeField = page.getByRole('textbox', { name: 'setup code' })
check(
  (await codeField.getAttribute('autocomplete')) === 'off' && (await codeField.getAttribute('inputmode')) === null,
  'the field asks for no autofill and no numeric keyboard -- the code has letters in it',
)

// Empty: refused on the screen, before anything reaches the server.
await page.click('button[type="submit"]')
check(
  await page
    .locator('.error', { hasText: EMPTY_LINE })
    .waitFor({ timeout: 5000 })
    .then(() => true)
    .catch(() => false),
  'an empty setup code is refused first, with the empty line',
)

// Wrong: the real server refuses it, and the screen shows its words.
const wrong = code.toLowerCase() === 'zzzz-zzzz-zzzz-zzzz' ? 'yyyy-yyyy-yyyy-yyyy' : 'zzzz-zzzz-zzzz-zzzz'
await codeField.fill(wrong)
await page.getByRole('textbox', { name: 'account' }).fill(USER)
await page.getByLabel('password', { exact: true }).fill(PASS)
await page.getByLabel('confirm password').fill(PASS)
await page.click('button[type="submit"]')
const wrongError = page.locator('.error', { hasText: "that setup code didn't match" })
await wrongError.waitFor({ timeout: 10000 }).catch(() => {})
check((await wrongError.textContent())?.trim() === WRONG_LINE, "a wrong code shows the server's refusal, the design's copy")
check((await codeField.inputValue()) === wrong, 'the typed code stays in the field so a typo can be fixed')
check((await sessionState()).setupRequired === true, 'a wrong code created no account')

// Right: typed without dashes and in the other case, as the screen
// promises nothing about either.
const typed = (code === code.toLowerCase() ? code.toUpperCase() : code.toLowerCase()).replace(/-/g, '')
await codeField.fill(typed)
await page.click('button[type="submit"]')
const created = await page
  .getByRole('heading', { name: 'Admin account created' })
  .waitFor({ timeout: 15000 })
  .then(() => true)
  .catch(() => false)
check(created, 'the logged code, without dashes and in the other case, creates the admin')
check((await sessionState()).setupRequired === false, 'the server now holds an account')

// Dead once an account exists: the same code cannot make a second one.
const again = await fetch(`${URL_BASE}/api/auth/register`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
  body: JSON.stringify({ username: `${USER}-second`, password: PASS, setupCode: code }),
})
check(again.status === 409, `the same code cannot create a second account once one exists (got ${again.status})`)

// Continue opens the app as the new admin; the create screen is gone.
await page.getByRole('button', { name: 'Continue' }).click()
await page
  .getByRole('heading', { name: 'Create the admin account' })
  .waitFor({ state: 'detached', timeout: 10000 })
  .catch(() => {})
const signedIn = await (await page.request.get(`${URL_BASE}/api/auth/session`)).json()
check(signedIn.authenticated === true, 'Continue leaves the browser signed in as the admin it just created')
check(
  (await page.getByRole('heading', { name: 'Create the admin account' }).count()) === 0,
  'the create-account screen does not come back',
)

check(pageErrors.length === 0, `no uncaught page errors (got ${JSON.stringify(pageErrors)})`)

await browser.close()
done()
