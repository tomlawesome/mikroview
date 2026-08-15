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
// file the dependency ships to be passed along as well; some of ours do.
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
import { readFileSync, writeFileSync, readdirSync, existsSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { createRequire } from 'node:module'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const REPO = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..')
const OUT = path.join(REPO, 'THIRD-PARTY-NOTICES.md')

const LICENCE_FILE = /^(LICEN[CS]E|COPYING|NOTICE)(\.(md|txt))?$/i

function goModules() {
  // -deps against the main package, not `go list -m all`: the latter
  // includes modules that are only needed to build tests or that got
  // pulled into the graph without a single line of their code reaching
  // the binary. We attribute what we actually ship.
  const out = execFileSync(
    'go',
    // -buildvcs=false: `go list` stamps like `go build` does, and stamping
    // cannot see a linked git worktree -- it fails there, or attributes a
    // foreign repository's commit (#357, golang/go#58218). Nothing reads
    // the stamp, and a licence inventory least of all.
    ['list', '-buildvcs=false', '-deps', '-f', '{{if .Module}}{{.Module.Path}}\t{{.Module.Version}}\t{{.Module.Dir}}{{end}}', '.'],
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

/**
 * Frontend packages are derived from the build output, not from
 * package.json.
 *
 * Reading package.json gets this wrong in both directions at once, which
 * is not a theoretical concern -- it was the first attempt at this file.
 * Listing every devDependency attributes Vite, Playwright, TypeScript and
 * Vitest, none of which put a byte in the artefact, so the notices claim
 * things that are not in it. And it *misses* workbox-core/-precaching/
 * -routing/-strategies, which ship as ~15KB of service worker but are
 * transitive dependencies of vite-plugin-pwa and therefore appear in no
 * package.json here at all. Over-inclusion looked like the safe error and
 * was actually wrong in the direction that matters.
 *
 * So: build with sourcemaps into a throwaway directory and read which
 * node_modules packages actually contributed source. That is a statement
 * about the shipped bytes rather than a guess about them, and it
 * self-corrects when the build config changes.
 */
function npmPackages() {
  const outDir = path.join(tmpdir(), 'mikroview-licence-build')
  rmSync(outDir, { recursive: true, force: true })
  execFileSync('npx', ['vite', 'build', '--sourcemap', '--outDir', outDir, '--emptyOutDir'], {
    cwd: path.join(REPO, 'frontend'),
    encoding: 'utf8',
    stdio: 'pipe',
  })

  // Every .map in the output, not just assets/ -- the service worker and
  // its workbox chunk are emitted at the output root, and an
  // assets/-only scan silently misses them.
  const names = new Set()
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, e.name)
      if (e.isDirectory()) walk(p)
      else if (e.name.endsWith('.map')) {
        const map = JSON.parse(readFileSync(p, 'utf8'))
        for (const src of map.sources ?? []) {
          const i = src.lastIndexOf('node_modules/')
          if (i < 0) continue
          const rest = src.slice(i + 'node_modules/'.length).split('/')
          names.add(rest[0].startsWith('@') ? `${rest[0]}/${rest[1]}` : rest[0])
        }
      }
    }
  }
  walk(outDir)
  rmSync(outDir, { recursive: true, force: true })

  const out = []
  for (const name of [...names].sort()) {
    // require.resolve rather than a fixed node_modules path: a package
    // may be hoisted to the top level or nested under whichever
    // dependency pulled it in, and which one is an npm implementation
    // detail that changes between installs.
    let dir
    try {
      dir = path.dirname(
        createRequire(path.join(REPO, 'frontend', 'package.json')).resolve(`${name}/package.json`),
      )
    } catch {
      throw new Error(
        `${name} contributes code to the shipped bundle but could not be resolved. ` +
          `Its licence cannot be attributed, and shipping it without attribution is an infringement.`,
      )
    }
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
  lines.push('Derived from the build output: these are the packages whose source the')
  lines.push('bundler actually emitted into the shipped JavaScript and service worker.')
  lines.push('Build-time-only tooling (Vite, TypeScript, Playwright, Vitest) is')
  lines.push('deliberately absent -- none of it reaches the artefact.')
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
