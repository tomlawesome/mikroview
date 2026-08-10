// SPDX-License-Identifier: AGPL-3.0-only
//
// Generates THIRD-PARTY-NOTICES.md: the copyright notices and licence
// texts of everything mikroview actually distributes inside its binary.
//
// Why this has to exist. mikroview ships a single statically-linked Go
// binary in a distroless image, with the frontend bundle embedded in it.
// That binary contains the compiled form of every Go module below and
// the bundled runtime of the frontend packages below -- and every one of
// their licences (MIT, BSD-3, ISC, Apache-2.0) requires the copyright
// notice and licence text to accompany a *binary* distribution, not just
// a source one. Apache-2.0 s4(d) goes further and requires any NOTICE
// file the dependency ships to be passed along as well; two of ours have
// one.
//
// Generated rather than hand-maintained, and checked in CI (see
// .github/workflows/ci.yml), because a hand-written notices file is
// wrong the first time somebody adds a dependency and nobody notices.
// Same reasoning as injection_sinks_test.go and internal/api's
// authzMatrix: a standing obligation is only real if something fails
// when it stops being met.
//
// Usage: node tools/licenses/generate-notices.mjs [--check]
//   (no args)  rewrite THIRD-PARTY-NOTICES.md
//   --check    exit non-zero if the committed file is out of date

import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync, readdirSync, existsSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const OUT = path.join(REPO, 'THIRD-PARTY-NOTICES.md')

const LICENCE_FILE = /^(LICEN[CS]E|COPYING|NOTICE)(\.(md|txt))?$/i

/**
 * Go module cache paths lower-case uppercase letters and prefix them
 * with "!" (so github.com/BurntSushi becomes github.com/!burnt!sushi).
 * None of today's dependencies need it, which is exactly why it would be
 * missed the day one does.
 */
function escapeModulePath(p) {
  return p.replace(/[A-Z]/g, (c) => '!' + c.toLowerCase())
}

function goModules() {
  // -deps against the main package, not `go list -m all`: the latter
  // includes modules that are only needed to build tests or that got
  // pulled into the graph without a single line of their code reaching
  // the binary. We attribute what we actually ship.
  const out = execFileSync(
    'go',
    ['list', '-deps', '-f', '{{if .Module}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}{{end}}', '.'],
    { cwd: REPO, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 },
  )
  const seen = new Map()
  for (const line of out.split('\n')) {
    if (!line.trim()) continue
    const [modPath, version, dir] = line.split('\t')
    if (!modPath || modPath === 'github.com/tomlawesome/mikroview') continue
    if (!seen.has(modPath)) seen.set(modPath, { name: modPath, version, dir })
  }
  return [...seen.values()].sort((a, b) => a.name.localeCompare(b.name))
}

function npmPackages() {
  // Everything in devDependencies, deliberately over-inclusive. Some of
  // these (vite, playwright) contribute nothing to the shipped bundle,
  // but Svelte's runtime and vite-plugin-pwa's service-worker glue
  // demonstrably do, and the boundary between "bundled" and "build-time
  // only" moves whenever the build config changes. Over-attributing
  // costs a few paragraphs; under-attributing is the infringement.
  const pkg = JSON.parse(readFileSync(path.join(REPO, 'frontend', 'package.json'), 'utf8'))
  const names = Object.keys({ ...(pkg.dependencies ?? {}), ...(pkg.devDependencies ?? {}) }).sort()
  const out = []
  for (const name of names) {
    const dir = path.join(REPO, 'frontend', 'node_modules', ...name.split('/'))
    if (!existsSync(dir)) continue
    const meta = JSON.parse(readFileSync(path.join(dir, 'package.json'), 'utf8'))
    out.push({ name, version: meta.version, dir, declared: meta.license ?? meta.licenses })
  }
  return out
}

function licenceTexts(dir) {
  if (!dir || !existsSync(dir)) return []
  return readdirSync(dir)
    .filter((f) => LICENCE_FILE.test(f))
    .sort()
    .map((f) => ({ file: f, text: readFileSync(path.join(dir, f), 'utf8').trimEnd() }))
}

function render() {
  const go = goModules()
  const npm = npmPackages()
  const lines = []

  lines.push('# Third-party notices')
  lines.push('')
  lines.push('MikroView is licensed under the GNU AGPL-3.0 (see [LICENSE](LICENSE)).')
  lines.push('This file is separate from that: it reproduces the copyright notices and')
  lines.push('licence texts of third-party software that MikroView **distributes** — the Go')
  lines.push('modules statically linked into the binary, and the frontend packages whose')
  lines.push('code is bundled into the embedded web assets.')
  lines.push('')
  lines.push('Their licences (MIT, BSD-3-Clause, ISC, Apache-2.0) all require these notices')
  lines.push('to accompany a binary distribution, not only a source one, which is why they')
  lines.push('travel with the container image and are reachable from the running app.')
  lines.push('')
  lines.push('**Do not edit this file by hand.** It is generated by')
  lines.push('`tools/licenses/generate-notices.mjs` and verified in CI.')
  lines.push('')
  lines.push('## Go modules')
  lines.push('')
  for (const m of go) {
    lines.push(`### ${m.name} ${m.version}`)
    lines.push('')
    const texts = licenceTexts(m.dir)
    if (texts.length === 0) {
      throw new Error(`no licence file found for Go module ${m.name} at ${m.dir}`)
    }
    for (const t of texts) {
      lines.push(`<!-- ${t.file} -->`)
      lines.push('')
      lines.push('```')
      lines.push(t.text)
      lines.push('```')
      lines.push('')
    }
  }
  lines.push('## Frontend packages')
  lines.push('')
  for (const p of npm) {
    lines.push(`### ${p.name} ${p.version}${p.declared ? ` — ${p.declared}` : ''}`)
    lines.push('')
    const texts = licenceTexts(p.dir)
    if (texts.length === 0) {
      // Some npm packages declare a licence in package.json without
      // shipping the text. Recorded honestly rather than silently
      // skipped, so the gap is visible instead of looking like coverage.
      lines.push(`_No licence file shipped in the package; declared as \`${p.declared ?? 'unknown'}\` in its package.json._`)
      lines.push('')
      continue
    }
    for (const t of texts) {
      lines.push(`<!-- ${t.file} -->`)
      lines.push('')
      lines.push('```')
      lines.push(t.text)
      lines.push('```')
      lines.push('')
    }
  }
  return lines.join('\n').replace(/\n{3,}/g, '\n\n').trimEnd() + '\n'
}

const rendered = render()
const check = process.argv.includes('--check')

if (check) {
  const current = existsSync(OUT) ? readFileSync(OUT, 'utf8') : ''
  if (current !== rendered) {
    console.error(
      'THIRD-PARTY-NOTICES.md is out of date.\n' +
        'A dependency was added, removed or upgraded without regenerating it.\n' +
        'Run: node tools/licenses/generate-notices.mjs\n\n' +
        'This is a licence-compliance requirement, not a formatting preference: MIT,\n' +
        'BSD, ISC and Apache-2.0 all require their notices to accompany the binary we ship.',
    )
    process.exit(1)
  }
  console.log('THIRD-PARTY-NOTICES.md is up to date.')
} else {
  writeFileSync(OUT, rendered)
  console.log(`wrote ${OUT}`)
}
