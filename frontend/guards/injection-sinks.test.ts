// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

// The frontend half of the guard in injection_sinks_test.go at the repo
// root. See docs/decisions/injection-audit.md.
//
// Lives in guards/ rather than src/ for a specific reason: it needs
// node:fs to read the source tree, and tsconfig.app.json deliberately
// withholds Node globals from app code (`types: ["svelte",
// "vite/client"]`). Adding "node" there to accommodate one test would
// let every component reach for fs -- worse than moving the test.
//
// mikroview renders event data that came off the wire from a router,
// and from anyone who can get that router to log a line. Svelte escapes
// interpolated text by default, which is the entire reason there is no
// XSS here -- so the exposure is not "did we escape this string" but
// "did someone reach for an API that opts out of escaping". That is a
// much smaller thing to watch, and a much more reliable one.

const FORBIDDEN: { pattern: RegExp; why: string }[] = [
  { pattern: /\{@html\b/, why: "Svelte's opt-out of escaping -- renders a string as live markup" },
  { pattern: /\.innerHTML\s*=/, why: 'assigns unescaped markup into the DOM' },
  { pattern: /\.outerHTML\s*=/, why: 'assigns unescaped markup into the DOM' },
  { pattern: /\bdangerouslySetInnerHTML\b/, why: 'assigns unescaped markup into the DOM' },
  { pattern: /\beval\s*\(/, why: 'executes a string as code' },
  { pattern: /new\s+Function\s*\(/, why: 'executes a string as code' },
  { pattern: /insertAdjacentHTML\s*\(/, why: 'assigns unescaped markup into the DOM' },
  { pattern: /document\.write\s*\(/, why: 'assigns unescaped markup into the DOM' },
]

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) {
      sourceFiles(path, acc)
      continue
    }
    if (/\.(svelte|ts)$/.test(path)) acc.push(path)
  }
  return acc
}

describe('frontend injection sinks', () => {
  it('has no API that opts out of escaping', () => {
    const offences: string[] = []
    for (const path of sourceFiles(join(import.meta.dirname, '..', 'src'))) {
      const src = readFileSync(path, 'utf8')
      src.split('\n').forEach((line, i) => {
        for (const { pattern, why } of FORBIDDEN) {
          if (pattern.test(line)) {
            offences.push(`${path}:${i + 1} -- ${pattern.source}: ${why}`)
          }
        }
      })
    }
    expect(
      offences,
      'If one of these is genuinely needed, the value going into it must be proven to ' +
        'not originate from event data, and docs/decisions/injection-audit.md updated.',
    ).toEqual([])
  })
})
