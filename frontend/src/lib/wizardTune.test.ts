// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import type { RouterFilterRule } from './api'
import { logPrefixSlug } from './fall.svelte'
import { matcherFor, prefixFor, proposeRules, quoteScript, slugFor, tagBlock, taggedInPush, undoBlock } from './wizardTune'

// The proposal and the block, as the ratified record has them (#1384):
// rules that log nothing on a boundary nobody watches, each named by
// its boundary, and a `set` line per ticked rule that MikroView can
// decode afterwards. A rule proposed on a watched boundary, or a prefix
// MikroView could not read back, is the kind of claim this step must
// not make.

function rule(over: Partial<RouterFilterRule> & { ordinal: number }): RouterFilterRule {
  return {
    comment: '',
    chain: 'forward',
    action: 'drop',
    srcAddressList: '',
    logPrefix: '',
    log: false,
    ...over,
  }
}

// The prototype's data story, in the shape the push sends: five rules
// dark, three already logging, one established/related accept.
const TABLE: RouterFilterRule[] = [
  rule({ ordinal: 0, chain: 'input', action: 'accept', comment: 'ssh from mgmt', inInterface: 'bridge', log: true, logPrefix: 'A|in-ssh|' }),
  rule({ ordinal: 1, chain: 'forward', action: 'accept', comment: 'established, related', connectionState: ['established', 'related'], log: true, logPrefix: 'A|est-rel|' }),
  rule({ ordinal: 2, chain: 'input', action: 'accept', comment: 'icmp', log: true, logPrefix: 'A|icmp|' }),
  rule({ ordinal: 3, chain: 'forward', action: 'drop', comment: 'drop everything else', inInterface: 'bridge', outInterface: 'ether1' }),
  rule({ ordinal: 4, chain: 'input', action: 'drop', comment: 'drop from WAN', inInterface: 'ether1' }),
  rule({ ordinal: 5, chain: 'forward', action: 'accept', comment: 'port-forward 443 → .40', inInterface: 'ether1', outInterface: 'bridge' }),
  rule({ ordinal: 6, chain: 'forward', action: 'drop', comment: 'guest → LAN', inInterface: 'bridge-guest', outInterface: 'bridge' }),
  rule({ ordinal: 7, chain: 'input', action: 'drop', comment: 'wg0 → input', inInterface: 'wg0' }),
]

describe('the proposal', () => {
  it('lists the rules that log nothing on a dark boundary, with the boundary as why', () => {
    const p = proposeRules(TABLE)
    expect(p.total).toBe(8)
    expect(p.alreadyLog).toBe(3)
    expect(p.rules.map((r) => r.ordinal)).toEqual([3, 4, 5, 6, 7])
    expect(p.rules.map((r) => r.why)).toEqual(['bridge → ether1', 'ether1 · input', 'ether1 → bridge', 'bridge-guest → bridge', 'wg0 · input'])
    expect(p.rules.map((r) => r.prefix)).toEqual(['D|drop-everyth|', 'D|drop-from-wa|', 'A|port-forward|', 'D|guest-lan|', 'D|wg0-input|'])
    for (const r of p.rules) expect(r.prefix.length).toBeLessThanOrEqual(15)
  })

  it('leaves a rule on a watched boundary alone -- an accept as much as a drop', () => {
    // An input rule on the bridge already logs (in-ssh), so the second
    // bridge input rule sits on a watched boundary and is not proposed.
    const p = proposeRules([...TABLE, rule({ ordinal: 8, chain: 'input', action: 'drop', comment: 'bridge drop', inInterface: 'bridge' })])
    expect(p.rules.map((r) => r.ordinal)).toEqual([3, 4, 5, 6, 7])
  })

  it('leaves disabled rules and every-packet rules out', () => {
    const p = proposeRules([
      rule({ ordinal: 0, comment: 'off', disabled: true, inInterface: 'ether1' }),
      rule({ ordinal: 1, action: 'accept', comment: 'fast', connectionState: ['established'], inInterface: 'ether2' }),
      rule({ ordinal: 2, comment: 'dark', inInterface: 'ether3' }),
    ])
    expect(p.rules.map((r) => r.ordinal)).toEqual([2])
    expect(p.total).toBe(3)
  })

  it('is empty when nothing was pushed', () => {
    expect(proposeRules([])).toEqual({ total: 0, alreadyLog: 0, rules: [] })
  })
})

describe('the proposal: only prefixes MikroView reads back', () => {
  it('writes only prefixes MikroView reads back, and leaves out a rule no code names', () => {
    const table = [
      rule({ ordinal: 0, chain: 'forward', action: 'jump', comment: 'to wan-in', inInterface: 'ether1', outInterface: 'bridge' }),
      rule({ ordinal: 1, chain: 'input', action: 'return', comment: 'back', inInterface: 'ether1' }),
      rule({ ordinal: 2, chain: 'input', action: 'add-src-to-address-list', comment: 'scanners', inInterface: 'ether2' }),
      rule({ ordinal: 3, chain: 'forward', action: 'tarpit', comment: 'tarpit', inInterface: 'ether3', outInterface: 'bridge' }),
      rule({ ordinal: 4, chain: 'forward', action: 'drop', comment: 'guest', inInterface: 'bridge-guest', outInterface: 'bridge' }),
    ]
    const p = proposeRules(table)
    expect(p.total).toBe(5)
    expect(p.rules.map((r) => r.ordinal)).toEqual([0, 1, 2, 4])
    expect(p.rules.map((r) => r.prefix)).toEqual(['L|to-wan-in|', 'L|back|', 'L|scanners|', 'D|guest|'])
    for (const r of p.rules) expect(logPrefixSlug(r.prefix)).toBe(r.prefix.slice(2, -1))
    const block = tagBlock(table, p.rules)
    expect(block).toContain('/ip firewall filter set [find comment="back"] log=yes log-prefix="L|back|"')
    expect(block).not.toContain('tarpit')
  })
})

describe('taggedInPush (A2-F7)', () => {
  it('is the enabled rules logging under the convention prefix, and nothing else', () => {
    const rules = [
      rule({ ordinal: 0, log: true, logPrefix: 'A|in-ssh|' }),
      rule({ ordinal: 1, log: true, logPrefix: 'fw-drop' }),
      rule({ ordinal: 2, log: false, logPrefix: 'D|wan-in|' }),
      rule({ ordinal: 3, log: true, logPrefix: 'D|wan-in|', disabled: true }),
      rule({ ordinal: 4, log: true, logPrefix: '' }),
      rule({ ordinal: 5, log: true, logPrefix: 'D|drop|' }),
    ]
    expect(taggedInPush(rules).map((r) => r.ordinal)).toEqual([0, 5])
  })
})

describe('the prefix', () => {
  it('is the action initial, the slug, and both bars', () => {
    expect(prefixFor('drop', 'wan-in')).toBe('D|wan-in|')
    expect(prefixFor('accept', 'nat')).toBe('A|nat|')
    expect(prefixFor('reject', 'x')).toBe('R|x|')
    expect(prefixFor('log', 'x')).toBe('L|x|')
  })

  it('gives a rule that hands the packet on the L code, never a verdict it did not reach', () => {
    for (const action of ['return', 'add-src-to-address-list', 'add-dst-to-address-list', 'jump', 'passthrough', 'fasttrack-connection']) {
      expect(prefixFor(action, 'x'), action).toBe('L|x|')
    }
  })

  it('slugs the comment, falls back to the interfaces then the chain, and stays unique', () => {
    const taken = new Set<string>()
    expect(slugFor({ comment: 'Guest → LAN', chain: 'forward' }, taken)).toBe('guest-lan')
    expect(slugFor({ comment: '', chain: 'forward', inInterface: 'ether1', outInterface: 'bridge' }, taken)).toBe('ether1-bridg')
    expect(slugFor({ comment: '', chain: 'input' }, taken)).toBe('input')
    expect(slugFor({ comment: '!!!', chain: '' }, taken)).toBe('rule')
    taken.add('guest-lan')
    expect(slugFor({ comment: 'guest → lan', chain: 'forward' }, taken)).toBe('guest-lan-2')
    taken.add('guest-lan-2')
    expect(slugFor({ comment: 'guest → lan', chain: 'forward' }, taken)).toBe('guest-lan-3')
    // A long comment is cut so the suffix still fits within the slug.
    taken.add('drop-everyth')
    const long = slugFor({ comment: 'drop everything else', chain: 'forward' }, taken)
    expect(long).toBe('drop-every-2')
    expect(long.length).toBeLessThanOrEqual(12)
  })

  it('never reuses a slug a logging rule already wears', () => {
    const p = proposeRules([
      rule({ ordinal: 0, comment: 'wan in', inInterface: 'ether2', log: true, logPrefix: 'D|wan-in|' }),
      rule({ ordinal: 1, comment: 'wan in', inInterface: 'ether1' }),
    ])
    expect(p.rules.map((r) => r.prefix)).toEqual(['D|wan-in-2|'])
  })
})

describe('the block', () => {
  it('addresses a rule by its comment when unique, by its number otherwise, quoted as RouterOS needs', () => {
    const table = [
      { ordinal: 0, comment: 'same' },
      { ordinal: 1, comment: 'same' },
      { ordinal: 2, comment: '' },
      { ordinal: 3, comment: 'say "hi" $x \\ done' },
    ]
    expect(matcherFor(table, table[0])).toBe('[find numbers=0]')
    expect(matcherFor(table, table[2])).toBe('[find numbers=2]')
    expect(matcherFor(table, table[3])).toBe('[find comment="say \\"hi\\" \\$x \\\\ done"]')
    expect(quoteScript('plain')).toBe('"plain"')
  })

  it('sets, never adds, one line per ticked rule in table order', () => {
    const p = proposeRules(TABLE)
    const chosen = [p.rules[3], p.rules[0]]
    expect(tagBlock(TABLE, chosen)).toBe(
      [
        '/ip firewall filter set [find comment="drop everything else"] log=yes log-prefix="D|drop-everyth|"',
        '/ip firewall filter set [find comment="guest → LAN"] log=yes log-prefix="D|guest-lan|"',
      ].join('\n'),
    )
    expect(tagBlock(TABLE, [])).toBe('')
  })

  it('undoes the same rules by switching logging back off', () => {
    const p = proposeRules(TABLE)
    expect(undoBlock(TABLE, [p.rules[1]])).toBe('/ip firewall filter set [find comment="drop from WAN"] log=no log-prefix=""')
  })
})

// #1426 (A2-F12) break attempt: the action arrives from the router's own
// text, so it can be anything. Every action must either get a code that
// logPrefixSlug reads back to the slug, or be left out -- never a prefix
// built from an object property name ('constructor', 'toString',
// '__proto__' all resolve on a plain object lookup), an upper-case
// spelling, or a stray action nobody mapped; and a rule that already logs
// under its own prefix is never re-tagged.
describe('the proposal never writes a prefix it cannot read back (#1426 A2-F12)', () => {
  it('reads every proposed prefix back, whatever spelling the action has', () => {
    const odd = [
      'constructor', 'toString', 'hasOwnProperty', '__proto__', 'valueOf',
      'DROP', 'Accept', 'RETURN', 'fasttrack', 'tarpit', 'unknown-action', '', ' drop',
      'drop ', 'accept\n',
    ]
    const table = odd.map((action, i) =>
      rule({ ordinal: i, chain: 'forward', action, comment: `r${i}`, inInterface: `if${i}`, outInterface: 'bridge' }),
    )
    // a rule that already logs under its own convention prefix on a
    // boundary of its own, and one logging under a foreign prefix
    table.push(rule({ ordinal: odd.length, chain: 'input', action: 'return', comment: 'mine', inInterface: 'ether9', log: true, logPrefix: 'D|mine|' }))
    table.push(rule({ ordinal: odd.length + 1, chain: 'input', action: 'passthrough', comment: 'theirs', inInterface: 'ether8', log: true, logPrefix: 'vendor-x' }))
    const p = proposeRules(table)
    for (const r of p.rules) {
      expect(r.prefix, `action ${JSON.stringify(r.action)}`).toMatch(/^[ADRL]\|[a-z0-9-]+\|$/)
      expect(logPrefixSlug(r.prefix), `action ${JSON.stringify(r.action)}`).toBe(r.prefix.slice(2, -1))
      expect(r.prefix.length).toBeLessThanOrEqual(15)
    }
    // none of the logging rules is proposed again, and no odd action
    // other than the three verdicts is proposed at all
    expect(p.rules.map((r) => r.ordinal).filter((o) => o >= odd.length)).toEqual([])
    for (const r of p.rules) expect(['accept', 'drop', 'reject']).toContain(r.action)
    expect(tagBlock(table, p.rules)).not.toMatch(/function|native code|Object/)
  })
})
