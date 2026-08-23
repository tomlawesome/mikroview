// SPDX-License-Identifier: AGPL-3.0-only
//
// One-off dev tool, not a live-check scenario -- deliberately named
// outside the `live-*.mjs` glob so scripts/run-scenarios.sh (and
// `make live-check`) never picks it up: it produces images, not
// pass/fail checks, and doesn't fit the check()/done() contract the
// other scripts share.
//
// Regenerates docs/screenshots/live-view-{dark,light}.png against a real,
// running instance, so the README/docs screenshots match what actually
// ships rather than a stale pre-#363 capture (newest at the bottom).
//
// tools/screenshots/{capture.mjs,gen_traffic.py} exist for this same job
// but predate two changes that broke both: authentication is no longer
// optional (capture.mjs never logs in), and the plaintext syslog
// listener gen_traffic.py fed is gone (#189 -- TLS is the only ingest
// path now). Fixing that pair is a separate piece of work; this script
// reuses the same traffic mix and IP pools (so the result still looks
// like the deployment the original screenshot depicted) but drives it
// through scripts/live-env.sh's already-working TLS feed and does a real
// login, matching how every live-check scenario already talks to a real
// instance.
//
// Usage:
//   eval "$(scripts/live-env.sh up)"
//   cd frontend && node scripts/capture-live-view-screenshots.mjs
//   scripts/live-env.sh down   (from the repo root)

import { chromium } from 'playwright'
import { execFileSync } from 'child_process'
import { fileURLToPath } from 'url'
import path from 'path'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const URL_BASE = process.env.MV_URL
const USER = process.env.MV_USER
const PASS = process.env.MV_PASS
if (!URL_BASE || !USER || !PASS) {
  console.error('MV_URL/MV_USER/MV_PASS unset -- run: eval "$(scripts/live-env.sh up)"')
  process.exit(2)
}
const ENV_SCRIPT = path.join(REPO, 'scripts', 'live-env.sh')

function feedRaw(payload) {
  execFileSync(ENV_SCRIPT, ['raw', payload], { stdio: 'ignore', cwd: REPO })
}

// Same IP pools and weighted event mix as tools/screenshots/gen_traffic.py,
// so the regenerated screenshot still depicts the kind of traffic a
// deployment actually sees rather than something invented for this PR.
// Sent bare (no RFC3164 <PRI> timestamp/hostname prefix) -- matching
// scripts/live-env.sh's own `syslog`/`portscan` generators, which every
// other live-check scenario already proves the TLS listener accepts.
const LAN_IPS = ['192.168.88.10', '192.168.88.12', '192.168.88.14', '192.168.88.21', '192.168.88.33', '192.168.88.50', '192.168.88.77']
const PUBLIC_IPS = ['104.21.5.12', '172.217.16.14', '151.101.65.69', '13.107.42.14', '185.199.108.153', '1.1.1.1', '8.8.8.8', '142.250.187.14']
const SCANNER_IPS = ['45.148.10.23', '185.220.101.4', '89.248.165.31', '194.26.29.77', '196.251.71.9', '23.94.35.180', '91.240.118.22']
const WAN_IP = '203.0.113.9'

const pick = (arr) => arr[Math.floor(Math.random() * arr.length)]
const randInt = (a, b) => a + Math.floor(Math.random() * (b - a + 1))

function line(action, slug, chain, inIf, outIf, proto, src, sport, dst, dport, flags) {
  const prefix = action ? `${action}|${slug}|` : ''
  const parts = [outIf ? `in:${inIf} out:${outIf}` : `in:${inIf}`, 'connection-state:new']
  if (proto === 'ICMP') {
    parts.push(`proto ICMP (${flags})`, `${src}->${dst}`)
  } else {
    parts.push(flags ? `proto ${proto} (${flags})` : `proto ${proto}`, `${src}:${sport}->${dst}:${dport}`)
  }
  parts.push(`len ${randInt(52, 1420)}`)
  return `firewall,info ${prefix}${chain}: ${parts.join(', ')}`
}

// Weights: 30 accept-web, 10 accept-dns, 5 accept-mgmt, 15 drop-invalid,
// 20 drop-scan, 8 reject-blocked, 8 log-wan, 4 unknown -- same shape as
// gen_traffic.py's random_event().
function randomEvent() {
  const roll = Math.random() * 100
  if (roll < 30) {
    return line('A', 'lan-wan', 'forward', 'bridge-lan', 'ether1', 'TCP', pick(LAN_IPS), randInt(40000, 65000), pick(PUBLIC_IPS), Math.random() < 0.5 ? 443 : 80, 'SYN')
  }
  if (roll < 40) {
    return line('A', 'lan-dns', 'forward', 'bridge-lan', 'ether1', 'UDP', pick(LAN_IPS), randInt(40000, 65000), '1.1.1.1', 53)
  }
  if (roll < 45) {
    return line('A', 'mgmt-ssh', 'input', 'bridge-lan', null, 'TCP', pick(LAN_IPS), randInt(40000, 65000), '192.168.88.1', 22, 'SYN')
  }
  if (roll < 60) {
    return line('D', 'invalid', 'input', 'ether1', null, 'TCP', pick(SCANNER_IPS), randInt(1024, 65000), WAN_IP, pick([22, 23, 445, 3389, 8291, 5900]), 'RST,ACK')
  }
  if (roll < 80) {
    return line('D', 'input-def', 'input', 'ether1', null, 'TCP', pick(SCANNER_IPS), randInt(1024, 65000), WAN_IP, pick([22, 23, 445, 3389, 8080, 8291, 2323, 5900]), 'SYN')
  }
  if (roll < 88) {
    return line('R', 'no-torrent', 'forward', 'bridge-lan', 'ether1', 'TCP', pick(LAN_IPS), randInt(40000, 65000), pick(PUBLIC_IPS), pick([6881, 51413]), 'SYN')
  }
  if (roll < 96) {
    const proto = pick(['TCP', 'UDP', 'ICMP'])
    return line('L', 'wan-test', 'input', 'ether1', null, proto, pick([...SCANNER_IPS, ...PUBLIC_IPS]), randInt(1024, 65000), WAN_IP, pick([53, 443, 22, 8291]), proto === 'ICMP' ? 'type 8, code 0' : 'SYN')
  }
  return line(null, null, 'forward', 'bridge-lan', 'ether1', 'TCP', pick(LAN_IPS), randInt(40000, 65000), pick(PUBLIC_IPS), 443, 'SYN')
}

const COUNT = 80
feedRaw(Array.from({ length: COUNT }, randomEvent).join('\n'))
console.log(`fed ${COUNT} demo events`)

const outDir = path.join(REPO, 'docs', 'screenshots')
const browser = await chromium.launch()

for (const scheme of ['dark', 'light']) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 860 }, colorScheme: scheme })
  const page = await context.newPage()
  await page.goto(URL_BASE, { waitUntil: 'networkidle' })
  await page.fill('input[autocomplete="username"]', USER)
  await page.fill('input[autocomplete="current-password"]', PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('.row', { timeout: 15000 })
  await page.waitForTimeout(600)
  await page.screenshot({ path: path.join(outDir, `live-view-${scheme}.png`) })
  await context.close()
  console.log(`captured live-view-${scheme}.png`)
}

await browser.close()
