// SPDX-License-Identifier: AGPL-3.0-only
//
// #856: the on-disk event history, exercised against a real running
// instance rather than a temp directory in a Go test.
//
// This scenario has no browser assertions, and deliberately so: the
// feature has no UI yet (#910 is the control, and its design is not
// settled). What it does have is a write path that runs on every
// ingested event, and the two things worth proving about it cannot be
// proved anywhere else:
//
//  - Events written by a real instance, through the real ingest path,
//    actually reach a file. A unit test drives retention.Store directly
//    and would still pass if main never called Append.
//  - What lands there is not readable. This is the whole reason the
//    feature is off without a key -- and the exact failure a unit test
//    over a mocked writer would miss, because it would be asserting
//    about bytes it made up rather than bytes the process wrote.
//
// scripts/live-env.sh turns the history on for the gate and puts the key
// beside the data directory, not inside it. That placement is the rule
// the feature exists to keep, so the harness models it rather than
// taking the shortcut.

import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { session, feedSyslog, feedRaw, check, responsive, done } from './live-browser.mjs'

// Addresses and a rule label this scenario alone feeds, so finding one
// in a retained file is unambiguous: nothing else in the suite sends
// them.
const SECRET_SRC = '203.0.113.86'
const SECRET_DST = '198.51.100.42'
const SECRET_RULE = 'mv856-history'

// Bulk first, so the day file exists and has something to compress, then
// the marked lines whose addresses the assertions below look for.
feedSyslog(40, 'history')
for (let i = 0; i < 5; i++) {
  feedRaw(
    `firewall,info H|${SECRET_RULE}| forward: in:bridge1 out:ether1, connection-state:new, ` +
      `proto TCP (SYN), ${SECRET_SRC}:5${i}100->${SECRET_DST}:443, len 60`,
  )
}

const { page, consoleErrors } = await session({ waitForEvents: 20 })

const dir = process.env.MV_DIR
check(Boolean(dir), `the harness exported MV_DIR -- got ${dir ?? 'nothing'}`)

const historyDir = join(dir, 'data', 'history')

// The writer batches, flushing every few seconds rather than per event,
// so the file is not there the instant an event is ingested. Poll rather
// than sleep once: on a loaded gate host a fixed wait is either flaky or
// wastefully long.
async function retainedFiles(timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    let names = []
    try {
      names = readdirSync(historyDir).filter((n) => n.startsWith('events-') && n.endsWith('.mvevt'))
    } catch {
      names = []
    }
    if (names.length > 0) return names
    if (Date.now() > deadline) return []
    await new Promise((r) => setTimeout(r, 500))
  }
}

const files = await retainedFiles()
check(files.length > 0, `the running instance retained a day file -- got ${files.length} in ${historyDir}`)

if (files.length > 0) {
  const raw = readFileSync(join(historyDir, files[0]))
  const text = raw.toString('latin1')

  check(raw.length > 0, `the retained file holds something -- ${raw.length} bytes`)
  check(
    text.startsWith('MVEVT'),
    `the retained file is one of ours, by its magic -- got ${JSON.stringify(text.slice(0, 5))}`,
  )

  // The point of the whole feature. Every one of these went into the
  // instance on this run; none may come back out of the file.
  for (const needle of [SECRET_SRC, SECRET_DST, SECRET_RULE, 'firewall']) {
    check(!text.includes(needle), `the retained file does not carry ${JSON.stringify(needle)} in the clear`)
  }
}

// The key is the operator's, mounted from outside the data directory --
// so it must not have been copied in beside what it protects.
const dataEntries = readdirSync(join(dir, 'data'))
check(
  !dataEntries.some((n) => n.endsWith('.key') && n.includes('history')),
  `the history key is not sitting inside the data directory -- entries: ${dataEntries.join(', ')}`,
)

// Retention runs on the ingest path, so the cheapest way to be sure it
// has not broken ingest is to ask the server what it has.
const statsRes = await page.request.get(new URL('/api/stats', page.url()).toString())
check(statsRes.ok(), `GET /api/stats responds with the history on -- status ${statsRes.status()}`)
const stats = await statsRes.json()
check(
  typeof stats.total === 'number' && stats.total > 0,
  `the instance is still ingesting with retention on -- total ${stats.total}`,
)

await responsive(page)
check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
