// SPDX-License-Identifier: AGPL-3.0-only
import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// #1538 C18: two files declared a `NARROW_BREAKPOINT` with different
// widths (1300 and 1600). The name must name one thing: either one shared
// constant, or the column-set one renamed (COLUMN_NARROW_BREAKPOINT).
const read = (name: string) => readFileSync(fileURLToPath(new URL(name, import.meta.url)), 'utf8')
const declares = (src: string) => /\b(?:const|let|var)\s+NARROW_BREAKPOINT\b/.test(src)

describe('NARROW_BREAKPOINT', () => {
  it('is declared in at most one of viewport and columns', () => {
    const files = ['./viewport.svelte.ts', './columns.svelte.ts']
    const declaring = files.filter((f) => declares(read(f)))
    expect(declaring.length).toBeLessThanOrEqual(1)
  })
})
