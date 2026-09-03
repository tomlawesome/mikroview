// One-off dev tool: builds mikroview, boots it against the demo config with
// synthetic traffic, and checks the responsive/mobile layout in one shot --
// toolbar control overflow, live-table horizontal scroll + sticky time
// column, and dashboard card overflow -- across mobile/desktop and
// light/dark. Screenshots go to outDir; a PASS/FAIL summary prints to
// stdout so most runs don't need the images opened at all.
//
// Usage: node mobile-check.mjs [outDir]
import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'
import { spawn, execSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const REPO_ROOT = path.resolve(HERE, '..', '..')
const HTTP_PORT = 18083
const SYSLOG_PORT = 11514
const URL = `http://127.0.0.1:${HTTP_PORT}`
const outDir = path.resolve(process.argv[2] ?? path.join(HERE, 'mobile-check-out'))
mkdirSync(outDir, { recursive: true })

const results = []
function check(name, pass, detail) {
  results.push({ name, pass, detail })
  console.log(`${pass ? 'PASS' : 'FAIL'}  ${name}${detail ? `  (${detail})` : ''}`)
}

console.log('building frontend + backend...')
execSync('make build', { cwd: REPO_ROOT, stdio: 'inherit' })

console.log('starting mikroview against demo config...')
const server = spawn(path.join(REPO_ROOT, 'mikroview'), [], {
  cwd: REPO_ROOT,
  env: {
    ...process.env,
    MIKROVIEW_CONFIG: path.join(HERE, 'demo-config.yaml'),
    MIKROVIEW_LISTEN_HTTP: `:${HTTP_PORT}`,
    MIKROVIEW_LISTEN_SYSLOG_UDP: `:${SYSLOG_PORT}`,
    MIKROVIEW_LISTEN_SYSLOG_TCP: `:${SYSLOG_PORT}`,
  },
  stdio: ['ignore', 'pipe', 'pipe'],
})
let serverLog = ''
server.stdout.on('data', (d) => (serverLog += d))
server.stderr.on('data', (d) => (serverLog += d))

async function cleanup() {
  server.kill()
}
process.on('exit', cleanup)

try {
  for (let i = 0; i < 30; i++) {
    try {
      // URL is http://127.0.0.1:<port>, the instance this script just
      // started itself. The rule is about a browser app reaching a
      // remote host in the clear; nothing here leaves the loopback.
      // nosemgrep: typescript.react.security.react-insecure-request.react-insecure-request
      await fetch(`${URL}/api/healthz`)
      break
    } catch {
      await new Promise((r) => setTimeout(r, 200))
    }
  }

  console.log('seeding synthetic traffic...')
  execSync(`python3 gen_traffic.py ${SYSLOG_PORT}`, { cwd: HERE, stdio: 'inherit' })
  await new Promise((r) => setTimeout(r, 500))

  const browser = await chromium.launch()

  for (const scheme of ['dark', 'light']) {
    // Mobile pass: layout + interaction checks.
    {
      const context = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: scheme })
      const page = await context.newPage()
      await page.goto(URL, { waitUntil: 'networkidle' })
      await page.waitForSelector('.row', { timeout: 8000 }).catch(() => {})
      await page.waitForTimeout(500)
      await page.screenshot({ path: `${outDir}/mobile-${scheme}-live.png` })

      const table = await page.evaluate(() => {
        const el = document.querySelector('.body')
        return el ? { scrollWidth: el.scrollWidth, clientWidth: el.clientWidth } : null
      })
      check(`[${scheme}] live table scrolls horizontally on mobile`, !!table && table.scrollWidth > table.clientWidth, JSON.stringify(table))

      const controlsOverflow = await page.evaluate(() => {
        const el = document.querySelector('.toolbar')
        return el ? el.scrollWidth <= el.clientWidth + 1 : null
      })
      check(`[${scheme}] toolbar controls don't overflow off-screen`, controlsOverflow === true, `toolbar.scrollWidth<=clientWidth: ${controlsOverflow}`)

      await page.evaluate(() => {
        document.querySelector('.body').scrollLeft = 250
      })
      await page.waitForTimeout(150)
      await page.screenshot({ path: `${outDir}/mobile-${scheme}-scrolled.png` })

      await page.getByTitle('Event charts and traffic breakdowns').click().catch(() => {})
      await page.waitForTimeout(400)
      await page.screenshot({ path: `${outDir}/mobile-${scheme}-dashboard.png` })

      const dash = await page.evaluate(() => {
        const el = document.querySelector('.dashboard')
        return el ? { scrollWidth: el.scrollWidth, clientWidth: el.clientWidth } : null
      })
      check(`[${scheme}] dashboard has no stray horizontal overflow`, !!dash && dash.scrollWidth <= dash.clientWidth + 1, JSON.stringify(dash))

      await context.close()
    }

    // Desktop pass: regression check only.
    {
      const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: scheme })
      const page = await context.newPage()
      await page.goto(URL, { waitUntil: 'networkidle' })
      await page.waitForSelector('.row', { timeout: 8000 }).catch(() => {})
      await page.waitForTimeout(500)
      await page.screenshot({ path: `${outDir}/desktop-${scheme}-live.png` })
      await context.close()
    }
  }

  await browser.close()
} catch (err) {
  console.error('mobile-check crashed:', err)
  console.error('--- server log ---\n' + serverLog)
  process.exitCode = 1
} finally {
  await cleanup()
}

const failed = results.filter((r) => !r.pass)
console.log(`\n${results.length - failed.length}/${results.length} checks passed. Screenshots: ${outDir}`)
if (failed.length) process.exitCode = 1
