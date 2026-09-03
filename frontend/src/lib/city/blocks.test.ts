import { describe, expect, it } from 'vitest'
import { symbolFor } from './blocks'

describe('city blocks', () => {
  it('stamps every kind as a solid block with a void backing first', () => {
    for (const kind of ['router', 'router-ant', 'host', 'post'] as const) {
      const s = symbolFor(kind)
      expect(s.top).toBeGreaterThan(0)
      expect(s.paths).toHaveLength(6)
      expect(s.paths.slice(0, 3).every((p) => p.fill === 'void')).toBe(true)
      expect(s.paths.slice(3).every((p) => p.fill === 'body')).toBe(true)
      for (const p of s.paths) expect(p.d).toMatch(/^M.*Z$/)
    }
  })

  it('stands a post taller than a host', () => {
    expect(symbolFor('post').top).toBeGreaterThan(symbolFor('host').top)
  })
})
