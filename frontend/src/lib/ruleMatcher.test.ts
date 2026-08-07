// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import { matchingIds, RuleMatcher, type MatchCandidate } from './ruleMatcher'

const candidates: MatchCandidate[] = [
  { id: 1, ruleLabel: 'drop-wan-in', raw: 'in:ether1 out:(none) proto TCP' },
  { id: 2, ruleLabel: 'accept-lan', raw: 'in:bridge out:ether1 proto UDP' },
]

describe('matchingIds', () => {
  it('matches the rule label', () => {
    expect(matchingIds('^drop', candidates)).toEqual([1])
  })

  // The raw line is matched too. Losing this was the reason the simple
  // "match a bounded set of labels" design was rejected.
  it('matches the raw log line, not just the label', () => {
    expect(matchingIds('proto UDP', candidates)).toEqual([2])
  })

  it('is case-insensitive, as the synchronous version was', () => {
    expect(matchingIds('DROP-WAN', candidates)).toEqual([1])
  })

  it('throws on an invalid pattern rather than matching nothing', () => {
    expect(() => matchingIds('(unclosed', candidates)).toThrow()
  })
})

/** A Worker stand-in; jsdom has none. */
function fakeWorker(behaviour: (msg: any, reply: (data: any) => void) => void) {
  const w = {
    onmessage: null as ((e: { data: unknown }) => void) | null,
    onerror: null as ((e: unknown) => void) | null,
    terminated: false,
    postMessage(msg: any) {
      behaviour(msg, (data) => this.onmessage?.({ data }))
    },
    terminate() {
      this.terminated = true
    },
  }
  return w
}

describe('RuleMatcher', () => {
  it('returns the ids the worker reports', async () => {
    const w = fakeWorker((msg, reply) => reply({ id: msg.id, ids: [1, 2] }))
    const m = new RuleMatcher(() => w)
    await expect(m.run('x', candidates)).resolves.toEqual({ status: 'ok', ids: [1, 2] })
  })

  it('reports an invalid pattern', async () => {
    const w = fakeWorker((msg, reply) => reply({ id: msg.id, invalid: true }))
    const m = new RuleMatcher(() => w)
    await expect(m.run('(', candidates)).resolves.toEqual({ status: 'invalid' })
  })

  // The guarantee. A pattern that never returns must not leave anything
  // running, and must not leave the caller waiting forever.
  it('kills a worker that overruns and reports too-slow', async () => {
    vi.useFakeTimers()
    const w = fakeWorker(() => {
      /* never replies -- a catastrophic pattern */
    })
    const m = new RuleMatcher(() => w)
    const p = m.run('(a+)+$', candidates, 50)
    await vi.advanceTimersByTimeAsync(60)
    await expect(p).resolves.toEqual({ status: 'too-slow' })
    expect(w.terminated).toBe(true)
    vi.useRealTimers()
  })

  // The user types, so patterns supersede each other constantly. A reply
  // for an abandoned pattern must not resolve the current request, or the
  // view shows results for something the user already changed.
  it('ignores a reply belonging to a superseded request', async () => {
    let replyToFirst: ((data: any) => void) | undefined
    const w = fakeWorker((msg, reply) => {
      if (msg.id === 1) replyToFirst = reply
      else reply({ id: msg.id, ids: [2] })
    })
    const m = new RuleMatcher(() => w)
    const first = m.run('a', candidates)
    const second = m.run('b', candidates)
    // The stale reply arrives late, addressed to request 1.
    replyToFirst!({ id: 1, ids: [999] })
    await expect(second).resolves.toEqual({ status: 'ok', ids: [2] })
    void first
  })

  it('builds a fresh worker after one was killed', async () => {
    vi.useFakeTimers()
    let built = 0
    const m = new RuleMatcher(() => {
      built++
      return built === 1
        ? fakeWorker(() => {})
        : fakeWorker((msg, reply) => reply({ id: msg.id, ids: [7] }))
    })
    const dead = m.run('bad', candidates, 10)
    await vi.advanceTimersByTimeAsync(20)
    await expect(dead).resolves.toEqual({ status: 'too-slow' })
    vi.useRealTimers()

    await expect(m.run('good', candidates)).resolves.toEqual({ status: 'ok', ids: [7] })
    expect(built).toBe(2)
  })
})
