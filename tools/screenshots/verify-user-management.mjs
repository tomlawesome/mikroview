// Drives the admin Users panel end-to-end in a real browser (issue #133).
//
// Kept as a script rather than a one-off paste: this covers the parts a
// typecheck and a unit test can't reach -- that the menu entry exists and
// opens the panel, that a created account actually appears in the list
// without a reload, that the admin row offers no delete control at all,
// and that deleting a user removes its row.
//
// Usage: node verify-user-management.mjs <baseUrl> <outDir>
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'

const base = process.argv[2] ?? 'http://127.0.0.1:18099'
const outDir = process.argv[3] ?? './user-management-out'
mkdirSync(outDir, { recursive: true })

const ADMIN = { username: 'alice', password: 'password123' }
const NEW_USER = { username: 'carol', password: 'password456' }

const failures = []
function check(name, ok, detail = '') {
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? ` -- ${detail}` : ''}`)
  if (!ok) failures.push(name)
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
page.on('console', (m) => {
  if (m.type() === 'error') console.log(`  [browser error] ${m.text()}`)
})

await page.goto(base, { waitUntil: 'networkidle' })

// First run: the setup form creates the admin.
await page.getByLabel(/username/i).first().fill(ADMIN.username)
await page.getByLabel('Password', { exact: true }).fill(ADMIN.password)
await page.getByLabel(/confirm password/i).fill(ADMIN.password)
await page.getByRole('button', { name: /create/i }).first().click()
await page.waitForTimeout(2000)
await page.screenshot({ path: `${outDir}/01-after-setup.png` })

// The menu entry replaces the old "Add user".
await page.getByRole('button', { name: /menu/i }).first().click()
await page.waitForTimeout(300)
const usersEntry = page.getByRole('button', { name: /^users$/i })
check('menu offers a "Users" entry', (await usersEntry.count()) > 0)
await usersEntry.first().click()
await page.waitForTimeout(500)

const dialog = page.getByRole('dialog', { name: /users/i })
check('Users panel opens', await dialog.isVisible())
await page.screenshot({ path: `${outDir}/02-users-panel.png` })

// The admin must be listed, and must not be deletable from here.
const adminRowText = await dialog.textContent()
check('the admin account is listed', adminRowText.includes(ADMIN.username))
check('the admin row says it cannot be removed', adminRowText.includes("can't be removed"))
check(
  'no Delete control is rendered for a lone admin',
  (await dialog.getByRole('button', { name: /^delete$/i }).count()) === 0,
)

// Add a user and confirm it lands in the list without a reload.
await dialog.getByPlaceholder('Username').fill(NEW_USER.username)
await dialog.getByPlaceholder('Password').fill(NEW_USER.password)
await dialog.getByRole('button', { name: /^add$/i }).click()
await page.waitForTimeout(800)
const afterAdd = await dialog.textContent()
check('the new account appears without a reload', afterAdd.includes(NEW_USER.username))
check(
  'the new account is deletable',
  (await dialog.getByRole('button', { name: /^delete$/i }).count()) === 1,
)
await page.screenshot({ path: `${outDir}/03-user-added.png` })

// Delete it. The confirm() dialog has to be accepted.
page.once('dialog', (d) => {
  console.log(`  [confirm] ${d.message().split('\n')[0]}`)
  d.accept()
})
await dialog.getByRole('button', { name: /^delete$/i }).click()
await page.waitForTimeout(800)
const afterDelete = await dialog.textContent()
check('the deleted account is gone from the list', !afterDelete.includes(NEW_USER.username))
check('the admin is still listed', afterDelete.includes(ADMIN.username))
await page.screenshot({ path: `${outDir}/04-user-deleted.png` })

await browser.close()

console.log(failures.length === 0 ? '\nAll checks passed.' : `\n${failures.length} FAILED: ${failures.join(', ')}`)
process.exit(failures.length === 0 ? 0 : 1)
