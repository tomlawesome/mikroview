// SPDX-License-Identifier: AGPL-3.0-only
//
// CI size budget for the shipped JS bundle (#462).
//
// README.md quotes the bundle's size, and that figure went 3-5x stale
// before anything caught it (#382) -- nothing measured the build, so
// nobody noticed it drift. This script measures the real build output on
// every CI run and fails when it grows past budget, so a bundle that has
// outgrown the README is caught at the PR that grew it rather than
// noticed later by someone reading the number and doubting it.
//
// Gated on gzip only, not raw -- see #462: gzip is what an operator's
// connection actually carries, and a single budget is one less number to
// keep in sync than two (a change that compresses unusually badly is
// still caught, just via the gzip figure rather than a second budget).
//
// BUDGET_BYTES is the gzip-compressed size, in bytes, of the built entry
// bundle (dist/assets/index-*.js), gzipped at level 9 -- the same method
// this script uses below. History: set to 92,000 on #462 (2026-08-21),
// ~15% headroom over the then-current bundle. Reset to 200,000 by the
// owner's decision on #482 (2026-08-23, docs/decisions/ui-framework.md):
// the gate is a tripwire against drift, not a design ceiling, and the
// v0.4.0 interface reshape builds against room rather than a number
// derived from the interface it replaces. Re-derived the original way on
// #910 (2026-09-03), the reshape having shipped as v0.4.0 on 2026-08-25:
// the bundle then measured 201,044 bytes gzipped (666,551 raw), and
// 201,044 + ~15% is 230,000.
//
// Raise this only alongside a stated reason in the commit that does so,
// and update README.md's "UI" bullet (the shipped-bundle figure) in the
// same change -- otherwise raising the budget just relocates the drift
// this check exists to close off. Do not "tidy" it down to match
// whatever the bundle happens to measure today; that would turn the
// very next legitimate feature PR into a spurious CI failure.
const BUDGET_BYTES = 230_000

import { readFileSync, existsSync, globSync } from 'node:fs'
import { gzipSync, constants as zlibConstants } from 'node:zlib'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const distDir = path.join(here, '..', 'dist')

// An absent build is a failure, not a skip. A check that quietly passes
// when its input is missing is the exact failure mode AGENTS.md warns
// about for CI gates (see its TypeScript-pin example) -- it would report
// green having measured nothing.
if (!existsSync(distDir)) {
  console.error(
    `${distDir} does not exist -- the frontend has not been built.\n` +
      'This check needs a real build to measure. Run `npm run build` ' +
      '(inside frontend/) before this script, or `make frontend` from ' +
      'the repo root.',
  )
  process.exit(1)
}

const candidates = globSync(path.join(distDir, 'assets', 'index-*.js'))
if (candidates.length !== 1) {
  console.error(
    `Expected exactly one entry bundle matching dist/assets/index-*.js, found ${candidates.length}` +
      (candidates.length ? `:\n${candidates.join('\n')}` : '.') +
      '\nThe build output shape changed -- update this script to find the ' +
      'right file before trusting its result.',
  )
  process.exit(1)
}

const bundlePath = candidates[0]
const raw = readFileSync(bundlePath)
const gzipBytes = gzipSync(raw, { level: zlibConstants.Z_BEST_COMPRESSION }).length

console.log(`${path.relative(process.cwd(), bundlePath)}: ${raw.length} bytes raw, ${gzipBytes} bytes gzipped`)

if (gzipBytes > BUDGET_BYTES) {
  console.error(
    `\nJS bundle budget exceeded: ${gzipBytes} bytes gzipped > ${BUDGET_BYTES} byte budget ` +
      `(${raw.length} bytes raw).\n\n` +
      'Either bring the bundle back under budget, or -- if the growth is ' +
      'deliberate -- raise BUDGET_BYTES at the top of this file with a ' +
      "stated reason, and update README.md's shipped-bundle figure (the " +
      '"UI" bullet under Features) to the new measurement in the same PR. ' +
      'A budget raised without updating the README just moves the drift ' +
      'this check exists to catch.',
  )
  process.exit(1)
}

console.log(`Within budget (${BUDGET_BYTES} bytes gzipped).`)
