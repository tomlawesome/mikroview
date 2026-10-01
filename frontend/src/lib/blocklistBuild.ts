// SPDX-License-Identifier: AGPL-3.0-only

// The blocklist builder's page model (#1360, BUILD.md part 7), ported
// from round 2's builder.html and tail.html: pure functions of what the
// server said (GET /api/blocklist/builder) and what the operator has
// clicked, so the cards, the rail, the ledger and the copy row stay
// testable without a browser -- the wizardRun.ts shape.
//
// Nothing here holds RouterOS syntax: every command line comes from the
// server (internal/routeros/blocklist.go). Nothing here talks to a
// router either; MikroView never connects to one (AGENTS.md).

import type {
  BlocklistBuilder,
  BlocklistCatalogueEntry,
  BlocklistChoice,
  BlocklistCommands,
  BlocklistLedgerEntry,
  BlocklistPart,
  BlocklistRefresh,
} from './api'

// One card's state on the page: its Yes / Not now, and its four choices.
export interface CardChoice {
  on: boolean
  direction: BlocklistChoice['direction']
  ipv6: boolean
  log: boolean
  refresh: BlocklistRefresh
}

export type Choices = Record<string, CardChoice>

// A list's ink, the custom property its card, part heads and rows wear
// (round 2 README, "Gates": one ink per list). Round 2 drew three; the
// four lists the owner admitted afterwards (1b+1c) have none drawn yet,
// so they wear --ink-list, a neutral, until a design gives them one.
const DRAWN_INKS: Record<string, string> = {
  spamhaus: 'var(--ink-spamhaus)',
  et: 'var(--ink-et)',
  cins: 'var(--ink-cins)',
  push: 'var(--ink-push)',
  own: 'var(--ink-own)',
}

export function inkFor(key: string): string {
  return DRAWN_INKS[key] ?? 'var(--ink-list)'
}

// The rail's and the ledger's name for a list: the drawn "Emerging
// Threats compromised" for ET, the card's own title for every other.
const RAIL_NAMES: Record<string, string> = { et: 'Emerging Threats compromised' }

export function railName(e: BlocklistCatalogueEntry): string {
  return RAIL_NAMES[e.key] ?? e.name
}

// Marked text: **bold** and *emphasis*, the catalogue's and the hints'
// way of carrying the drawn <b> and <em> without markup.
export interface Seg {
  text: string
  b?: boolean
  em?: boolean
}

export function marked(s: string): Seg[] {
  const out: Seg[] = []
  const re = /\*\*([^*]+)\*\*|\*([^*]+)\*/g
  let last = 0
  for (let m = re.exec(s); m; m = re.exec(s)) {
    if (m.index > last) out.push({ text: s.slice(last, m.index) })
    if (m[1] !== undefined) out.push({ text: m[1], b: true })
    else out.push({ text: m[2], em: true })
    last = m.index + m[0].length
  }
  if (last < s.length) out.push({ text: s.slice(last) })
  return out
}

// The hint under each choice. Round 2 drew Spamhaus's and ET's; BUILD.md's
// "Catalogue copy and defaults" (2026-10-01) gives the other five theirs.
const LOG_HINT = 'Each drop reaches MikroView as a line naming the list; a home WAN sees a few an hour.'
const INBOUND_HINT = 'Inbound is enough — these are attackers and scanners; nothing on your LAN talks to them on purpose.'
const NO_CADENCE = 'The source states no cadence; daily is plenty, and a failed fetch leaves yesterday’s list standing.'

export interface Hints {
  block: string
  ipv6: string
  log: string
  refresh: string
}

export function hintsFor(key: string, schedulerDays: boolean): Hints {
  switch (key) {
    case 'spamhaus':
      return {
        block: 'Inbound is enough — nothing on your LAN talks to hijacked space on purpose.',
        ipv6: 'DROPv6, 91 ranges, the same terms. Harmless on an IPv4-only router: nothing matches.',
        log: LOG_HINT,
        refresh: 'Spamhaus asks for at least an hour apart; daily is plenty for a list that barely moves.',
      }
    case 'et':
      return {
        block: '**Both ways** here: a LAN device talking *to* one of these is the finding.',
        ipv6: '',
        log: 'Keep it on here: an outbound drop is the line you want to see.',
        refresh: schedulerDays
          ? 'The list is rebuilt once a day on weekdays, so **weekdays** fetches when there is something new.'
          : 'What Emerging Threats recommends; the list is rebuilt about once a day.',
      }
    case 'cins':
      return {
        block: INBOUND_HINT,
        ipv6: '',
        log: LOG_HINT,
        refresh: 'Changes hourly, but each load is 15,000 adds; 6 h keeps up without doing that every hour.',
      }
    case 'blde':
      return {
        block: INBOUND_HINT,
        ipv6: '',
        log: LOG_HINT,
        refresh: 'The source refreshes every half hour; 6 h is current enough for a 48-hour window.',
      }
    case 'dshield':
      return {
        block:
          'Inbound is enough — these are attackers and scanners; nothing on your LAN talks to them on purpose, and blocking *to* a whole /24 would catch its innocent hosts.',
        ipv6: '',
        log: LOG_HINT,
        refresh: 'A rolling three-day window; daily keeps up with it.',
      }
    default:
      return { block: INBOUND_HINT, ipv6: '', log: LOG_HINT, refresh: NO_CADENCE }
  }
}

// The words a refresh choice is drawn with.
export function refreshLabel(r: BlocklistRefresh): string {
  return r === '6h' ? '6 h' : r
}

// held reports whether the router holds any of a list.
export function held(row: BlocklistLedgerEntry | undefined): boolean {
  return !!row && row.state === 'held'
}

// defaultChoices is what the page opens with: each card at its
// catalogue defaults, Yes where the catalogue says so or the router
// already holds the list.
export function defaultChoices(b: BlocklistBuilder): Choices {
  const out: Choices = {}
  for (const e of b.catalogue) {
    const row = b.lists.find((l) => l.key === e.key)
    out[e.key] = {
      on: e.default || held(row),
      direction: e.defaultDirection,
      ipv6: e.ipv6,
      log: true,
      refresh: e.refreshDefault,
    }
  }
  return out
}

// requestLists is the choices as the commands request carries them: the
// lists switched on, in catalogue order.
export function requestLists(b: BlocklistBuilder, c: Choices): BlocklistChoice[] {
  return b.catalogue
    .filter((e) => c[e.key]?.on)
    .map((e) => {
      const x = c[e.key]
      return { key: e.key, direction: x.direction, ipv6: e.ipv6 && x.ipv6, log: x.log, refresh: x.refresh }
    })
}

export function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

// partsSummary is the Copy button's label: "Copy — 2 lists · 8 parts".
export function partsSummary(lists: number, parts: number): string {
  return `Copy — ${plural(lists, 'list', 'lists')} · ${plural(parts, 'part', 'parts')}`
}

// ranges writes ordinals as the drawn note does: "5–7", "1, 5–7".
export function ranges(ns: number[]): string {
  const out: string[] = []
  let i = 0
  while (i < ns.length) {
    let j = i
    while (j + 1 < ns.length && ns[j + 1] === ns[j] + 1) j++
    out.push(j === i ? String(ns[i]) : `${ns[i]}–${ns[j]}`)
    i = j + 1
  }
  return out.join(', ')
}

// newPartsNote is the copy row's note: which parts are new on the router
// and which a re-paste leaves as they are. A list's parts are new until
// the router holds it; the push's part is new until the router pushes
// the two kinds it adds. The closing run-now line is neither, unless
// every part is new.
export function newPartsNote(b: BlocklistBuilder, parts: BlocklistPart[], device: string): string {
  if (parts.length === 0) return ''
  const fresh: number[] = []
  const same: number[] = []
  for (const p of parts) {
    if (!p.ink) continue
    const isNew = p.ink === 'push' ? !b.pushCurrent : !held(b.lists.find((l) => l.key === p.ink))
    ;(isNew ? fresh : same).push(p.ordinal)
  }
  if (same.length === 0) return `All ${parts.length} parts are new on ${device}.`
  if (fresh.length === 0) return `Nothing here is new; re-pasting ${ranges(same)} changes nothing.`
  return `${fresh.length === 1 ? 'Part' : 'Parts'} ${ranges(fresh)} ${fresh.length === 1 ? 'is' : 'are'} new; re-pasting ${ranges(same)} changes nothing.`
}

// hm is the time a router's own creation-time string shows ("2026-10-01
// 04:17:02" or "oct/01/2026 04:17:02" -> "04:17").
export function hm(routerTime: string | undefined): string {
  if (!routerTime) return ''
  const t = routerTime.trim().split(/\s+/).pop() ?? ''
  return t.slice(0, 5)
}

// hmLocal is an instant as this browser's wall clock ("14:32").
export function hmLocal(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit' })
}

export function num(n: number): string {
  return n.toLocaleString('en')
}

// flagsFact is the ledger's last fact about MikroView's own flags from a
// list (BUILD.md, Decisions: only where MikroView flags from it and the
// flag names its feed).
export function flagsFact(e: BlocklistCatalogueEntry, row: BlocklistLedgerEntry): string {
  if (!e.flaggedByMikroView) return 'MikroView does not flag from it'
  if (row.flags24h === null) return 'flags: not tracked per list'
  return `flags from this list: ${row.flags24h} in 24 h (dropped first)`
}

export type RowState = 'done' | 'chosen' | 'off'

export interface RailRow {
  key: string
  title: string
  n: string
  state: RowState
  receipt: string
  ink: string
}

// railRows is the rail: the operator's own drop list first, then a row
// per list -- ✓ in its ink where the router holds it, its card number in
// decision blue where it is in the block, dashed "not now" otherwise.
export function railRows(b: BlocklistBuilder, c: Choices): RailRow[] {
  const below = b.standing !== 'ok'
  const own = b.ownDroplist
  const ownReceipt = own.held
    ? `${plural(own.held, 'address', 'addresses')}${own.fetchedAt ? ` · fetched ${hmLocal(own.fetchedAt)}` : ''} · from Settings`
    : 'none on this router · from Settings'
  const rows: RailRow[] = [
    { key: 'own', title: 'Your drop list', n: own.held ? '✓' : '–', state: own.held ? 'done' : 'off', receipt: ownReceipt, ink: inkFor('own') },
  ]
  b.catalogue.forEach((e, i) => {
    const row = b.lists.find((l) => l.key === e.key)
    let r: RailRow
    if (below) r = { key: e.key, title: railName(e), n: '–', state: 'off', receipt: 'after the upgrade', ink: inkFor(e.key) }
    else if (row && held(row)) {
      const fired = row.firedToday !== null ? ` · fired ${num(row.firedToday)} today` : ''
      r = { key: e.key, title: railName(e), n: '✓', state: 'done', receipt: `${num(row.count)} held · refreshed ${hm(row.loadedAt)}${fired}`, ink: inkFor(e.key) }
    } else if (c[e.key]?.on) {
      r = { key: e.key, title: railName(e), n: String(i + 1), state: 'chosen', receipt: 'in the block · not on the router yet', ink: inkFor(e.key) }
    } else r = { key: e.key, title: railName(e), n: '–', state: 'off', receipt: 'not now', ink: inkFor(e.key) }
    rows.push(r)
  })
  return rows
}

export interface LedgerView {
  key: string
  title: string
  n: string
  state: 'done' | 'wait' | 'skip'
  receipt: string
  undo: string
  ink: string
}

// ledgerView is "Where it stands": what the router holds of each list,
// from its own push, with Undo where it holds one.
export function ledgerView(b: BlocklistBuilder, c: Choices): LedgerView[] {
  return b.catalogue.map((e, i) => {
    const row = b.lists.find((l) => l.key === e.key)
    const base = { key: e.key, title: railName(e), ink: inkFor(e.key) }
    if (row && held(row)) {
      const v6 = row.count6 ? ` (+ ${num(row.count6)} IPv6)` : ''
      const fired = row.firedToday !== null ? ` · rule fired ${num(row.firedToday)} times today` : ''
      return {
        ...base,
        n: '✓',
        state: 'done' as const,
        receipt: `${num(row.count)} held${v6} · refreshed ${hm(row.loadedAt)}${fired} · ${flagsFact(e, row)}`,
        undo: row.undo,
      }
    }
    if (c[e.key]?.on) return { ...base, n: String(i + 1), state: 'wait' as const, receipt: 'in the block · not on the router yet', undo: '' }
    return { ...base, n: '–', state: 'skip' as const, receipt: 'not now', undo: '' }
  })
}

// waitingLists names the lists in the block the router does not hold
// yet, as the observation line does ("mv-bl-et").
export function waitingLists(b: BlocklistBuilder, c: Choices): string[] {
  return b.catalogue.filter((e) => c[e.key]?.on && !held(b.lists.find((l) => l.key === e.key))).map((e) => `mv-bl-${e.key}`)
}

// blockHead is the block's own line: "8 parts, in order — the push,
// Spamhaus DROP, Emerging Threats, run now".
export function blockHead(b: BlocklistBuilder, block: BlocklistCommands): string {
  const names: string[] = []
  for (const p of block.parts) {
    if (p.ink === 'push') names.push('the push')
    else if (!p.ink) names.push('run now')
    else {
      const e = b.catalogue.find((x) => x.key === p.ink)
      const short = e?.short ?? p.ink
      if (!names.includes(short)) names.push(short)
    }
  }
  return `${plural(block.parts.length, 'part', 'parts')}, in order — ${names.join(', ')}`
}

// versionLine is the drawn "written for RouterOS 7.24.4 — what rb5009
// reported at 14:32 · …" split for its bold.
export function versionLine(b: BlocklistBuilder): { version: string; rest: string } {
  return {
    version: `RouterOS ${b.routerosVersion}`,
    rest: ` — what ${b.deviceName} reported at ${hmLocal(b.reportedAt)} · the choices below are the ones this version offers · reviewed on ${b.reviewedVersion}`,
  }
}

export const FOOT_HINT = 'Into the router’s terminal, not a script · the router does the fetching; MikroView only watches what comes back'

// The rail's foot-note (builder.html).
export const RAIL_NOTE = 'Each list is fetched by the router, from its source. MikroView reads what the router pushes back — it never serves a list and never connects.'
