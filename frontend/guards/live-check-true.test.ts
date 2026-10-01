// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'

// check(cond, label) (scripts/live-browser.mjs) only aborts nothing on
// failure -- it logs and carries on, so a live-check scenario's honesty
// rests entirely on `cond` reflecting something that actually happened.
// `check(true, …)` throws that away: it passes regardless of what the
// preceding step did.
//
// That is sound only when the line above it already could not have
// reached the check without proving the thing it names -- a Playwright
// wait that throws on timeout (waitFor, waitForSelector, waitForFunction,
// goTo), or a skip-marker inside a branch whose real check(...) already
// recorded the failure. It is unsound when the line above is one of the
// scripts' own non-throwing helpers (waitForCondition, waitForArrival)
// whose result was never looked at -- issue #1403 found two of exactly
// that shape in live-enrolment.mjs.
//
// Rather than re-deriving that judgement call automatically -- it needs
// reading the helper the line above calls, which this guard cannot do --
// every `check(true` is required to carry a `safe:` comment immediately
// above it (or on the same line) explaining why. Adding a new one without
// that reasoning written down fails the build instead of failing silently
// in a live run nobody is watching closely enough to notice.

function scriptFiles(dir: string): string[] {
  return readdirSync(dir)
    .filter((name) => name.endsWith('.mjs'))
    .map((name) => join(dir, name))
}

const CHECK_TRUE = /\bcheck\(\s*true\b/

describe('live-check scripts: check(true, …) must be explained', () => {
  it('every check(true, …) call carries a `safe:` comment above or on its own line', () => {
    const offences: string[] = []
    for (const path of scriptFiles(join(import.meta.dirname, '..', 'scripts'))) {
      const lines = readFileSync(path, 'utf8').split('\n')
      lines.forEach((line, i) => {
        // A `safe:` comment can itself mention `check(true` in prose (to
        // explain what it is guarding); only a real call -- not a
        // comment line -- needs justifying.
        if (line.trim().startsWith('//')) return
        if (!CHECK_TRUE.test(line)) return

        const sameLineSafe = line.includes('safe:')
        let prevSafe = false
        for (let j = i - 1; j >= 0; j--) {
          const prev = lines[j].trim()
          if (prev === '') continue
          prevSafe = prev.includes('safe:')
          break
        }
        if (!sameLineSafe && !prevSafe) {
          offences.push(
            `${path}:${i + 1} -- check(true, …) with no \`safe:\` comment above it explaining why the ` +
              `preceding step could not have reached this line without the thing it names being true`,
          )
        }
      })
    }
    expect(
      offences,
      'Either the line above genuinely throws on failure (say so: "// safe: waitForSelector above throws ' +
        'on timeout") or it does not, in which case check its actual result instead of writing check(true, …).',
    ).toEqual([])
  })
})
