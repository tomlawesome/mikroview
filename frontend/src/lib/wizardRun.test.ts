// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  addrProblem,
  arrivedAll,
  chipsFor,
  footSpec,
  freshAnswers,
  NO_EVIDENCE,
  railRows,
  reachedIdx,
  rowState,
  stageOf,
  stripFor,
  type Evidence,
  type RunAnswers,
} from './wizardRun'

// The gating and the receipts, as the ratified prototype's wizard.js
// has them (#1381). A row that unlocks early, or a receipt in the wrong
// ink, is exactly the kind of claim this wizard exists not to make.

function answered(over: Partial<RunAnswers> = {}): RunAnswers {
  return { ...freshAnswers(), name: 'rb5009', addr: '192.168.13.1', push: true, backup: true, ...over }
}

function ev(over: Partial<Evidence> = {}): Evidence {
  return { ...NO_EVIDENCE, ...over }
}

describe('the router address', () => {
  it('is a dotted quad and nothing else', () => {
    expect(addrProblem('')).toBe('')
    expect(addrProblem('192.168.13.1')).toBe('')
    expect(addrProblem('192.168.13.1:514')).toMatch(/No port/)
    expect(addrProblem('192.168.13.0/24')).toMatch(/No prefix length/)
    expect(addrProblem('router.lan')).toMatch(/A name will not do/)
    expect(addrProblem('192.168.13')).toMatch(/Four numbers/)
    expect(addrProblem('192.168.13.999')).toMatch(/Four numbers/)
    // The server's rule (netip.ParseAddr) refuses leading zeros and
    // accepts an IPv6 literal; the client accepts nothing the server
    // would refuse (#1380).
    expect(addrProblem('192.168.013.1')).toMatch(/Four numbers/)
    expect(addrProblem('fd00:13::1')).toBe('')
    expect(addrProblem('2001:db8:0:0:0:0:0:1')).toBe('')
    expect(addrProblem('fd00:13::1:')).toMatch(/No port/)
    expect(addrProblem('fd00::13::1')).toMatch(/No port/)
  })
})

describe('the stage and the reach', () => {
  it('starts on the router with nothing else reached', () => {
    const s = freshAnswers()
    expect(stageOf(s)).toBe('router')
    expect(reachedIdx(s)).toBe(0)
  })

  it('reaches the password once the four are answered and Next is taken', () => {
    const s = answered({ q: 4 })
    expect(stageOf(s)).toBe('pass')
    expect(reachedIdx(s)).toBe(1)
  })

  it("reads the router's turn as the paste step", () => {
    expect(stageOf(answered({ stage: 'watch' }))).toBe('paste')
    expect(reachedIdx(answered({ stage: 'watch' }))).toBe(2)
    expect(reachedIdx(answered({ stage: 'tune' }))).toBe(3)
    expect(stageOf(answered({ stage: 'done' }))).toBe('stand')
    expect(reachedIdx(answered({ stage: 'done' }))).toBe(4)
  })
})

describe('the rail', () => {
  it('locks every step ahead of the furthest reached', () => {
    const rows = railRows(freshAnswers(), ev())
    expect(rows.map((r) => r.title)).toEqual([
      'The router',
      'Mint the token',
      'Paste once',
      'Tag firewall rules',
      'Where setup stands',
    ])
    expect(rows.map((r) => r.locked)).toEqual([false, true, true, true, true])
    expect(rows.map((r) => r.can)).toEqual([false, false, false, false, false])
    expect(rows[0].current).toBe(true)
    expect(rows[4].n).toBe('✓')
  })

  it('lets an earlier completed step be clicked while the run is still yours to change', () => {
    const rows = railRows(answered({ q: 4 }), ev())
    expect(rows[0].can).toBe(true)
    expect(rows[1].current).toBe(true)
    expect(rows[1].can).toBe(false)
    const onPaste = railRows(answered({ stage: 'paste' }), ev({ tokenUntil: '14:17' }))
    expect(onPaste[0].can).toBe(true)
    expect(onPaste[1].can).toBe(true)
  })

  it('goes back to nothing once the router is answering', () => {
    for (const stage of ['watch', 'tune', 'done'] as const) {
      const rows = railRows(answered({ stage, copied: true }), ev({ cert: '2026-09-27T14:02:58Z' }))
      expect(rows.every((r) => !r.can)).toBe(true)
    }
  })
})

describe('the receipts, in the ink of what they record', () => {
  it('reads the router row as chosen, then enrolled', () => {
    expect(rowState(freshAnswers(), ev(), 'router')).toEqual({ cls: '', receipt: 'name, address, push, backup', ink: '' })
    const chosen = rowState(answered({ backup: false }), ev(), 'router')
    expect(chosen.cls).toBe('chosen')
    expect(chosen.receipt).toBe('rb5009 · 192.168.13.1 · push yes · backup not now')
    const done = rowState(answered(), ev({ enrol: '2026-09-27T14:03:04Z', from: '192.168.13.1' }), 'router')
    expect(done.cls).toBe('done')
    expect(done.ink).toBe('logs')
    expect(done.receipt).toMatch(/^rb5009 · enrolled 192\.168\.13\.1 · /)
  })

  it('reads the token row in decision blue until the certificate lands', () => {
    expect(rowState(answered(), ev(), 'pass').receipt).toBe('your password, once')
    const minted = rowState(answered(), ev({ tokenUntil: '14:17' }), 'pass')
    expect(minted).toEqual({ cls: 'chosen', receipt: 'token good until 14:17', ink: 'token' })
    expect(rowState(answered(), ev({ tokenUntil: '14:17', cert: 'x' }), 'pass').cls).toBe('done')
  })

  it('reads the paste row from what has arrived', () => {
    expect(rowState(answered(), ev(), 'paste').receipt).toBe('one block, into the terminal')
    expect(rowState(answered({ copied: true }), ev(), 'paste').receipt).toBe('copied · waiting for the certificate')
    const cert = rowState(answered({ copied: true }), ev({ cert: 'x' }), 'paste')
    expect(cert).toEqual({ cls: 'chosen', receipt: 'certificate fetched · waiting', ink: 'cert' })
    const all = rowState(answered({ push: false, backup: false }), ev({ cert: 'x', enrol: 'y', lines: 69 }), 'paste')
    expect(all).toEqual({ cls: 'done', receipt: 'everything arrived · 69 lines', ink: 'logs' })
  })

  it('reads the rules row as needing the push, proposed, tagged, or left dark', () => {
    expect(rowState(answered({ push: false }), ev(), 'tune').receipt).toBe('needs the push')
    expect(rowState(answered(), ev(), 'tune').receipt).toBe('proposed from the push')
    expect(rowState(answered({ tuneSkipped: true }), ev(), 'tune')).toEqual({ cls: 'chosen', receipt: 'left dark', ink: 'off' })
    const tagged = rowState(answered({ chosenCount: 4, tunedAt: '14:03:57' }), ev({ tagged: true }), 'tune')
    expect(tagged).toEqual({ cls: 'done', receipt: '4 rules tagged · 14:03:57', ink: 'rules' })
  })
})

describe('everything chosen has arrived', () => {
  it('waits for the push and the backup only where they were said yes to', () => {
    const base = ev({ cert: 'c', enrol: 'e' })
    expect(arrivedAll(answered(), base)).toBe(false)
    expect(arrivedAll(answered({ push: false, backup: false }), base)).toBe(true)
    expect(arrivedAll(answered(), ev({ cert: 'c', enrol: 'e', push: 'p', backup: 'b' }))).toBe(true)
    expect(arrivedAll(answered({ push: false, backup: false }), ev({ enrol: 'e' }))).toBe(false)
  })
})

describe('the footer', () => {
  it('holds Next until all four are answered', () => {
    const f = footSpec(freshAnswers(), ev())
    expect(f.hint).toBe('All four, then Next')
    expect(f.right).toEqual([{ label: 'Next', action: 'router-next', primary: true, disabled: true }])
    expect(footSpec(answered(), ev()).right[0].disabled).toBe(false)
    expect(footSpec(answered(), ev()).hint).toBe('')
  })

  it('offers Back and Mint the token on the password step', () => {
    const f = footSpec(answered({ q: 4 }), ev())
    expect(f.left?.label).toBe('Back')
    expect(f.right[0]).toMatchObject({ label: 'Mint the token', disabled: true })
    expect(footSpec(answered({ q: 4, pass: 'x' }), ev()).right[0].disabled).toBe(false)
  })

  it('disables Next until Copy, then until everything chosen has arrived', () => {
    const paste = footSpec(answered({ stage: 'paste' }), ev())
    expect(paste.left?.label).toBe('Back')
    expect(paste.hint).toBe('Next checks what has arrived')
    expect(paste.right[0].disabled).toBe(true)
    const watch = footSpec(answered({ stage: 'watch', copied: true }), ev({ cert: 'c' }))
    expect(watch.left).toBeNull()
    expect(watch.right[0]).toMatchObject({ label: 'Next', action: 'to-tune', disabled: true })
    const all = footSpec(answered({ stage: 'watch', copied: true }), ev({ cert: 'c', enrol: 'e', push: 'p', backup: 'b' }))
    expect(all.right[0].disabled).toBe(false)
    expect(all.hint).toBe('')
  })

  it('offers Skip this step and Next on the rules step, and the two ways out on the ledger', () => {
    const tune = footSpec(answered({ stage: 'tune' }), ev())
    expect(tune.right.map((b) => b.label)).toEqual(['Skip this step', 'Next'])
    expect(tune.right[1].disabled).toBe(true)
    expect(footSpec(answered({ stage: 'tune' }), ev({ tagged: true })).right[1].disabled).toBe(false)
    const stand = footSpec(answered({ stage: 'done' }), ev())
    expect(stand.left?.label).toBe('Add another router')
    expect(stand.right[0]).toMatchObject({ label: 'Finish', action: 'finish' })
  })
})

describe('the bar and the strip', () => {
  it('adds a chip as each proof arrives, in its own ink', () => {
    expect(chipsFor(freshAnswers(), ev())).toEqual([])
    expect(chipsFor(answered(), ev()).map((c) => c.kind)).toEqual(['dec'])
    const full = chipsFor(
      answered({ chosenCount: 4 }),
      ev({
        cert: '2026-09-27T14:02:58Z',
        enrol: '2026-09-27T14:03:04Z',
        push: 'p',
        backup: 'b',
        tagged: true,
        boundaries: [
          { lane: 'x', watched: true, dark: false },
          { lane: '', watched: false, dark: true },
        ],
      }),
    )
    expect(full.map((c) => c.kind)).toEqual(['dec', 'cert', 'logs', 'push', 'dark', 'backup', 'rules'])
    expect(full[4].text).toBe('1 dark')
    expect(full[6].text).toBe('4 rules')
    expect(chipsFor(answered(), ev({ refused: '192.168.254.1' }))[1]).toEqual({ kind: 'alarm', text: 'refused · 192.168.254.1' })
    expect(chipsFor(answered(), ev({ push: 'p', ahead: true, version: '7.24.4' })).map((c) => c.text)).toContain(
      'RouterOS 7.24.4 — ahead of review',
    )
  })

  it('wears one grey tick, then green, then one per boundary', () => {
    expect(stripFor(ev())).toEqual([{ lane: '', on: false, dark: false }])
    expect(stripFor(ev({ enrol: 'e' }))).toEqual([{ lane: 'var(--accept)', on: true, dark: false }])
    const named = stripFor(
      ev({
        enrol: 'e',
        push: 'p',
        boundaries: [
          { lane: '#3987e5', watched: true, dark: false },
          { lane: '', watched: false, dark: true },
        ],
      }),
    )
    expect(named).toEqual([
      { lane: '#3987e5', on: true, dark: false },
      { lane: '', on: false, dark: true },
    ])
  })
})
