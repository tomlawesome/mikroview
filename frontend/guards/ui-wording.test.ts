// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

// #1541 (UI wording, from the #1438 consistency review): source guards for
// the six wording decisions C6-C11. Same shape as injection-sinks.test.ts:
// wording is a property of the shipped strings, so the cheapest honest check
// is to read the client source. Comments are stripped first, so a comment
// that quotes the old wording (as history) does not trip a guard.

const SRC = join(import.meta.dirname, '..', 'src')

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) sourceFiles(path, acc)
    else if (/\.(svelte|ts)$/.test(path) && !/\.test\.ts$/.test(path)) acc.push(path)
  }
  return acc
}

// Source with block comments, HTML comments and whole-line // comments
// blanked out (line count preserved so hits keep their line numbers).
function code(path: string): string[] {
  const blank = (m: string) => m.replace(/[^\n]/g, ' ')
  return readFileSync(path, 'utf8')
    .replace(/\/\*[\s\S]*?\*\//g, blank)
    .replace(/<!--[\s\S]*?-->/g, blank)
    .split('\n')
    .map((l) => (/^\s*\/\//.test(l) ? '' : l))
}

function hits(files: string[], re: RegExp): string[] {
  const out: string[] = []
  for (const f of files) {
    code(f).forEach((l, i) => {
      if (re.test(l)) out.push(`${relative(SRC, f)}:${i + 1}: ${l.trim()}`)
    })
  }
  return out
}

const all = sourceFiles(SRC)
const at = (...rel: string[]) => rel.map((r) => join(SRC, r))

describe('C6 in-page controls and busy labels are lowercase', () => {
  it('Watchlist busy labels are lowercase (no Saving… / Forgetting…)', () => {
    expect(hits(at('components/Watchlist.svelte'), /'(Saving|Forgetting|Loading)…'/)).toEqual([])
  })
  it('EngineRoomWatchers Try/Save buttons read lowercase beside save', () => {
    expect(hits(at('components/EngineRoomWatchers.svelte'), /'Try'|'Save'|^\s*(Try|Save)\s*$/)).toEqual([])
  })
  it('ExpectationsLedger forget button is lowercase', () => {
    expect(hits(at('components/ExpectationsLedger.svelte'), /'Forget'/)).toEqual([])
  })
  it('Topography compose close button is lowercase like its cancel siblings', () => {
    expect(hits(at('components/Topography.svelte'), /class="composer-close"[^>]*>Close</)).toEqual([])
  })
  it('filter-row placeholders use one form across Flags, Watchlist, AuditLog', () => {
    const forms = new Set<string>()
    for (const f of at('components/Flags.svelte', 'components/Watchlist.svelte', 'components/AuditLog.svelte')) {
      for (const l of code(f)) for (const m of l.matchAll(/placeholder="(filter[^"]*)"/g)) forms.add(m[1])
    }
    expect([...forms]).toHaveLength(1)
  })
  it('overlays keep sentence case (Cancel stays) -- the register split', () => {
    expect(hits(at('components/ChangePasswordOverlay.svelte'), />Cancel</).length).toBeGreaterThan(0)
  })
})

describe('C7 client validation is sentence case with a period; "Enter your password"', () => {
  it('no "Type your password" anywhere', () => {
    expect(hits(all, /Type your password/)).toEqual([])
  })
  it('EngineRoom and wizard use "Enter your password"', () => {
    expect(hits(at('components/EngineRoom.svelte'), /Enter your password/).length).toBe(3)
    expect(hits(at('lib/wizard.svelte.ts'), /Enter your password/).length).toBeGreaterThan(0)
  })
  it('RouterBackups formError strings start capitalised and end with a period', () => {
    const bad: string[] = []
    for (const l of code(join(SRC, 'components/RouterBackups.svelte'))) {
      const m = l.match(/formError = (["'`])(.*)\1\s*$/)
      if (m && !/^[A-Z$].*\.$/.test(m[2])) bad.push(m[2])
    }
    expect(bad).toEqual([])
  })
})

describe('C8 em dash in client-authored strings; server -- passed through', () => {
  // A quoted string, or element text, containing an ASCII " -- ".
  const asciiDash = /(["'`])[^"'`]* -- [^"'`]*\1|>[^<>]* -- [^<>]*</
  it('detectorCopy.ts uses em dashes', () => {
    expect(hits(at('lib/detectorCopy.ts'), asciiDash)).toEqual([])
  })
  it('the other client strings listed in #1541 use em dashes', () => {
    expect(
      hits(at('components/ConfigUpgrade.svelte', 'components/EngineRoom.svelte', 'components/City.svelte',
        'lib/passkeys.svelte.ts', 'lib/auth.svelte.ts'), asciiDash),
    ).toEqual([])
  })
})

describe('C9 "ingest token" for the router credential', () => {
  const old = /ingest key|enrolment token|mint a key|mint (the|a) token|API token/i
  it('no old name for the credential in client strings', () => {
    expect(hits(all, old)).toEqual([])
  })
  it('EngineRoom says "ingest token"', () => {
    expect(hits(at('components/EngineRoom.svelte'), /ingest token/).length).toBeGreaterThan(0)
  })
})

describe('C10 "watch" and "detector", never "watcher" in user-facing text', () => {
  it('no "watcher(s)" in strings, aria-labels or markup text', () => {
    expect(hits(all, /(["'`>][^"'`<]*|\{[^}]*\}\s*)\bwatchers?\b(?!-)/i)).toEqual([])
  })
})

describe('C11 "router" in prose, not "RouterOS device"', () => {
  it('Fleet and LiveTable first-run lines say router', () => {
    expect(hits(at('components/Fleet.svelte', 'components/LiveTable.svelte'), /RouterOS devices?/)).toEqual([])
  })
})
