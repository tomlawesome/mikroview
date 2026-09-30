// SPDX-License-Identifier: AGPL-3.0-only

// Step 4 of the setup wizard, Tag firewall rules (#1384): the rule list
// proposed from the push, and the block that tags what was ticked.
// Pure functions of the pushed filter table, so the proposal and the
// commands stay testable without a browser.
//
// The proposal is read from MikroView's own copy of the router's rule
// table (the push; never the router itself -- the AGENTS.md invariant).
// A rule is proposed when it logs nothing and sits on a boundary no
// rule logs on -- "a rule, but no line" -- with the boundary named in
// the fall's own words as why it matters. Coverage is about boundaries,
// not accepts versus denies (AGENTS.md, "Why MikroView suggests"), so
// an accept on a dark boundary is proposed exactly as a drop is.
//
// The commands mirror the server's own per-rule tagging render
// (internal/api/tunelogging.go's matcherFor and buildTuneLoggingCommands):
// a rule is addressed by its comment when that comment is unique in the
// table, by its position otherwise, and the prefix follows
// docs/routeros-setup.md step 3 -- <ACTION>|<slug>|, 15 characters at
// most, the slug from the operator's own comment so a live line can be
// matched back to its rule (lib/fall.svelte.ts, slugs).

import type { RouterFilterRule } from './api'
import { boundariesFromRules, boundaryKeyOf, logPrefixSlug } from './fall.svelte'

export interface TuneRule {
  // The push's own 0-based position, which is also what RouterOS means
  // by `numbers=N` for a table with no dynamic rules.
  ordinal: number
  chain: string
  action: string
  // The rule's own comment; '' when it has none.
  comment: string
  // Why it matters: the boundary this rule sits on, in the fall's words.
  why: string
  // The full log-prefix the block sets ("D|fwd-drop|").
  prefix: string
  boundaryKey: string
}

export interface TuneProposal {
  // Every rule the push read, disabled ones included.
  total: number
  // Rules that already log.
  alreadyLog: number
  rules: TuneRule[]
}

// docs/routeros-setup.md step 3: the whole prefix, trailing '|'
// included, stays within 15 characters (internal/routeros/commands.go's
// maxLogPrefixLen). One initial and two bars leave twelve for the slug.
const MAX_PREFIX = 15
const MAX_SLUG = MAX_PREFIX - 3

// quoteScript renders s the way the server's export.Quote does
// (internal/routeros.QuoteScriptString): '\' and '"' escaped, and '$'
// too, because RouterOS expands `$name` and `$[cmd]` inside any
// double-quoted string -- a comment is whoever edits rules on the
// router's text, and it lands in a line the admin pastes.
export function quoteScript(s: string): string {
  return '"' + s.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\$/g, '\\$') + '"'
}

// slugBase turns a rule's comment into the slug's raw material:
// lowercase, runs of anything but letters and digits as one hyphen,
// trimmed, cut to fit. '' when the comment gives nothing usable.
function slugBase(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, MAX_SLUG)
    .replace(/-+$/g, '')
}

// slugFor picks a rule's slug: its comment first (the operator's own
// words), else the interfaces the rule names, else its chain; then made
// unique against `taken` with a numeric suffix, shortened to keep the
// whole slug within MAX_SLUG. A slug shared by two rules names neither
// (lib/fall.svelte.ts), so uniqueness is the point, not a nicety.
export function slugFor(rule: Pick<RouterFilterRule, 'comment' | 'chain' | 'inInterface' | 'outInterface'>, taken: Set<string>): string {
  const base =
    slugBase(rule.comment) ||
    slugBase([rule.inInterface, rule.outInterface].filter(Boolean).join('-')) ||
    slugBase(rule.chain) ||
    'rule'
  if (!taken.has(base)) return base
  for (let n = 2; ; n++) {
    const suffix = `-${n}`
    const cut = base.slice(0, MAX_SLUG - suffix.length).replace(/-+$/g, '')
    const candidate = cut + suffix
    if (!taken.has(candidate)) return candidate
  }
}

// prefixFor is the convention's <ACTION>|<slug>|: the action's own
// first letter, upper-cased, as internal/routeros.LogPrefixForAction
// has it (A for accept, D for drop, R for reject, L for log).
export function prefixFor(action: string, slug: string): string {
  const initial = (action[0] ?? '?').toUpperCase()
  return `${initial}|${slug}|`
}

// matcherFor addresses a rule the way the server's own render does:
// `[find comment="…"]` when the comment is set and unique in the table,
// `[find numbers=N]` for every rule without one or sharing one.
export function matcherFor(rules: readonly Pick<RouterFilterRule, 'ordinal' | 'comment'>[], rule: Pick<RouterFilterRule, 'ordinal' | 'comment'>): string {
  if (rule.comment && rules.filter((r) => r.comment === rule.comment).length === 1) {
    return `[find comment=${quoteScript(rule.comment)}]`
  }
  return `[find numbers=${rule.ordinal}]`
}

// everyPacket: a rule whose connection-state names established or
// related logs a line per packet rather than per connection (#1230).
// The wizard's own bulk block sets exactly these to log=no, so they are
// not proposed here either.
function everyPacket(rule: RouterFilterRule): boolean {
  return (rule.connectionState ?? []).some((s) => s === 'established' || s === 'related')
}

// proposeRules is the step's lead and its list: how many rules the push
// read, how many already log, and the rules that log nothing on a
// boundary nobody watches -- each with its boundary and the prefix the
// block would give it.
export function proposeRules(rules: readonly RouterFilterRule[]): TuneProposal {
  const boundaries = boundariesFromRules([...rules], rules.length > 0)
  const byKey = new Map(boundaries.map((b) => [b.key, b]))
  const taken = new Set<string>()
  for (const r of rules) {
    const slug = logPrefixSlug(r.logPrefix)
    if (slug) taken.add(slug)
  }
  const out: TuneRule[] = []
  for (const r of rules) {
    if (r.disabled === true || r.log || everyPacket(r)) continue
    const key = boundaryKeyOf(r.chain, r.inInterface, r.outInterface)
    const b = byKey.get(key)
    if (!b || b.coverage !== 'dark') continue
    const slug = slugFor(r, taken)
    taken.add(slug)
    out.push({
      ordinal: r.ordinal,
      chain: r.chain,
      action: r.action,
      comment: r.comment,
      why: b.label,
      prefix: prefixFor(r.action, slug),
      boundaryKey: key,
    })
  }
  return { total: rules.length, alreadyLog: rules.filter((r) => r.log).length, rules: out }
}

// tagBlock is the second paste: one `set` line per ticked rule, in
// table order. `set` rather than `add`, so pasting it again sets the
// same thing again -- "Safe to paste again; it sets, never adds."
export function tagBlock(table: readonly Pick<RouterFilterRule, 'ordinal' | 'comment'>[], chosen: readonly TuneRule[]): string {
  return [...chosen]
    .sort((a, b) => a.ordinal - b.ordinal)
    .map((r) => `/ip firewall filter set ${matcherFor(table, r)} log=yes log-prefix=${quoteScript(r.prefix)}`)
    .join('\n')
}

// undoBlock is the ledger's Undo for tagged rules: the same rules,
// logging switched back off and the prefix cleared.
export function undoBlock(table: readonly Pick<RouterFilterRule, 'ordinal' | 'comment'>[], chosen: readonly TuneRule[]): string {
  return [...chosen]
    .sort((a, b) => a.ordinal - b.ordinal)
    .map((r) => `/ip firewall filter set ${matcherFor(table, r)} log=no log-prefix=""`)
    .join('\n')
}
