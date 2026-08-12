// SPDX-License-Identifier: AGPL-3.0-only
//
// Every in-document anchor link in the docs has to resolve to a heading
// in the same file.
//
// This project's documentation is unusually detailed, which raises
// rather than lowers the cost of a wrong line: readers trust it. A link
// that goes nowhere is the cheapest kind of wrong to introduce -- a
// heading gets reworded and nothing complains -- and the cheapest to
// catch. #268 finding 20 turned one up that had been broken for a
// while: an "Entities" link written before "port" was added to that
// heading.
//
// Deliberately only in-document (#anchor) links. An external URL needs a
// network request to check, which would make CI depend on other
// people's uptime. The slug rule below is GitHub's own.

import { readFileSync } from 'node:fs'
import { globSync } from 'node:fs'

const files = globSync(['README.md', 'CONTRIBUTING.md', 'SECURITY.md', 'AGENTS.md', 'CHANGELOG.md', 'docs/**/*.md'])

function slug(heading) {
  return heading
    .trim()
    .toLowerCase()
    .replace(/[`*_[\]()]/g, '')
    .replace(/[^\w\s-]/g, '')
    .replace(/\s+/g, '-')
    .replace(/^-+|-+$/g, '')
}

let broken = 0
for (const file of files) {
  const text = readFileSync(file, 'utf8')
  const headings = new Set()
  for (const line of text.split('\n')) {
    const m = /^#{1,6}\s+(.*)$/.exec(line)
    if (m) headings.add(slug(m[1]))
  }
  for (const m of text.matchAll(/\]\(#([a-zA-Z0-9\-_]+)\)/g)) {
    if (!headings.has(m[1].toLowerCase())) {
      console.error(`${file}: link to #${m[1]} has no matching heading`)
      broken++
    }
  }
}

if (broken > 0) {
  console.error(`\n${broken} broken in-document link(s).`)
  process.exit(1)
}
console.log(`Checked ${files.length} file(s): every in-document link resolves.`)
