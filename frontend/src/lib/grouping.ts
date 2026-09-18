// SPDX-License-Identifier: AGPL-3.0-only

// Group mode for the live view (#341): collapse repeats of the same
// connection into one row carrying a count, so a host retrying the same
// thing four hundred times costs one line instead of four hundred.
//
// The grouping key is deliberately strict. The owner's constraint:
//
//   "the source - dest - port combo is the golden group. Any two of the
//    three tells you little, you need all three."
//
// So all three are part of identity, and nothing is ever merged across
// them. What this collapses is *exact repeats* -- the same host, hitting
// the same address, on the same port, the same way. Nothing is
// summarised away and nothing has to be inferred from the row: it says
// exactly what a single row said, plus how many times it happened.
//
// Rule and chain are deliberately NOT part of the key. Again the owner:
//
//   "the rule is actually less important here, because that's just how
//    it's being handled, what's happening is important."
//
// So two events differing only in which rule matched are still one thing
// happening, and the row reports the rules it saw rather than splitting.
// That case is worth surfacing rather than hiding -- the same traffic
// matching two different rules usually means a rule-ordering surprise.

import type { FirewallEvent, Flag } from './types'

// EventGroup is one collapsed row. `events` holds the members in arrival
// order, oldest first, and is what the drawer renders.
export interface EventGroup {
  key: string
  // The first event to arrive with this key, which is the one the row
  // renders. Anchoring on the first (rather than the latest) is what
  // keeps a group where it appeared instead of jumping down the screen
  // every time it is hit again -- the count climbs in place.
  head: FirewallEvent
  events: FirewallEvent[]
  count: number
  // Every distinct rule label seen in this group. Length > 1 means the
  // same traffic matched more than one rule.
  rules: string[]
}

// groupKeyOf builds the identity of a connection: who, to where, on what
// port, over what protocol, and what happened to it. Any field being
// absent is part of the key too -- an event with no destination port is
// not "the same as" one with port 22.
export function groupKeyOf(e: FirewallEvent): string {
  return [e.srcIp ?? '', e.dstIp ?? '', e.dstPort ?? '', e.protocol ?? '', e.action ?? ''].join('\u0000')
}

// groupEvents collapses events into groups, preserving the arrival order
// of each group's first member.
//
// Deliberately a plain function over the array the view already holds,
// rather than an incremental structure maintained on the ingest path:
// the buffer is bounded (MAX_CLIENT_EVENTS), this runs at render, and a
// pass over it costs far less than the re-filter that already happens
// there. Simpler to reason about, and impossible to leave stale.
export function groupEvents(events: readonly FirewallEvent[]): EventGroup[] {
  const byKey = new Map<string, EventGroup>()
  for (const e of events) {
    const key = groupKeyOf(e)
    const existing = byKey.get(key)
    if (existing) {
      existing.events.push(e)
      existing.count++
      if (e.ruleLabel && !existing.rules.includes(e.ruleLabel)) existing.rules.push(e.ruleLabel)
      continue
    }
    byKey.set(key, {
      key,
      head: e,
      events: [e],
      count: 1,
      rules: e.ruleLabel ? [e.ruleLabel] : [],
    })
  }
  return [...byKey.values()]
}

// maxDrawerEvents bounds what an opened group renders. The owner's
// figure: past about twenty lines a drawer is not something you read, it
// is something you scroll -- and bulk belongs in an extract, not in the
// DOM. It also keeps a group of four thousand from rendering four
// thousand rows because someone clicked it, which would blow the
// deliberate MAX_RENDERED_ROWS budget in one go.
export const maxDrawerEvents = 20

// drawerEvents returns what an open group shows: its most recent members,
// newest first, capped. Newest first because the question being asked of
// an open group is "what is it doing", not "how did it start".
export function drawerEvents(group: EventGroup): FirewallEvent[] {
  return group.events.slice(-maxDrawerEvents).reverse()
}

// hiddenInDrawer is how many members the drawer is not showing -- shown
// to the operator rather than left to be inferred from the count, so a
// drawer listing twenty of four hundred never reads as the whole story.
export function hiddenInDrawer(group: EventGroup): number {
  return Math.max(0, group.count - maxDrawerEvents)
}

// flaggedSources builds the set of source addresses that currently carry
// an active flag, for the row marker.
//
// Keyed on the flag's target address because that is the only honest
// link available: a flag records what it was raised *about*, not which
// events evidenced it (see #341 -- that gap is the largest unbuilt piece
// behind the log-extract idea). So the marker means "this source has an
// active flag against it", not "this event caused that flag", and the
// UI must say the former.
export function flaggedSources(flags: readonly { target: string; cleared: boolean }[]): Set<string> {
  const out = new Set<string>()
  for (const f of flags) {
    if (f.cleared) continue
    // Targets can carry a suffix ("1.2.3.4 -> port 22"); the address is
    // the part before it. Mirrors extractSourceIp in flags.svelte.ts.
    const addr = f.target.replace(/ -> port \d+$/, '')
    if (addr) out.add(addr)
  }
  return out
}

// flagsBySource (#1269) is flaggedSources' own one-pass walk, kept
// alongside it rather than replacing it: the marker only ever needed
// membership, but EventRow's ⚑ mark needs the actual open flag(s) a
// source carries -- which flag to open, and how many to say "N open
// flags" about. Before this, EventRow built that itself by filtering
// the full flagsState.list once per flagged row it drew, so naming a
// screenful of rows from the same handful of sources cost rows x flags
// instead of the one pass this does. LiveTable builds this once and
// hands each row its own slice, the same shape flaggedSources already
// gets right for the boolean case.
export function flagsBySource(flags: readonly Flag[]): Map<string, Flag[]> {
  const out = new Map<string, Flag[]>()
  for (const f of flags) {
    if (f.cleared) continue
    const addr = f.target.replace(/ -> port \d+$/, '')
    if (!addr) continue
    const existing = out.get(addr)
    if (existing) existing.push(f)
    else out.set(addr, [f])
  }
  return out
}
