// SPDX-License-Identifier: AGPL-3.0-only
//
// The on-disk history's switch (#910, round 42's `#set` disk group,
// docs/design/concepts/round-42/disk.html). The memory group reads as a
// bar for what is held and a track for what is allowed; the disk group
// is the same two things one storey down, and every change that would
// delete something is a proposal until a link that names the deletion
// is taken.
//
// The arithmetic and the sentences live here rather than in
// DiskControl.svelte so the wording can be asserted without standing up
// a browser -- memory.ts's own reasoning. Bytes throughout; days are
// whole days.

import type { HistoryHeld, HistorySettings, PersistenceInfo } from './types'
import { TRACK_X0, TRACK_X1, formatHours, formatSize } from './memory'

export const DAYS_MIN = 1
export const DAYS_MAX = 365
const TRACK_W = TRACK_X1 - TRACK_X0
const MIB = 1024 * 1024

/**
 * dayX places a day figure along the track: round 42's doubling scale
 * from 1 d to 365 d, x = 8 + 500 * log2(v) / log2(365) (build.py's tx).
 */
export function dayX(days: number): number {
  const d = clampDays(days)
  return TRACK_X0 + (TRACK_W * Math.log2(d)) / Math.log2(DAYS_MAX)
}

/** daysAtX is the inverse: the whole day a track position proposes. */
export function daysAtX(x: number): number {
  const fraction = Math.min(1, Math.max(0, (x - TRACK_X0) / TRACK_W))
  return clampDays(Math.round(Math.pow(DAYS_MAX, fraction)))
}

export function clampDays(days: number): number {
  if (!Number.isFinite(days)) return DAYS_MIN
  return Math.min(DAYS_MAX, Math.max(DAYS_MIN, Math.round(days)))
}

/** stepDays is one arrow key: one day, so every figure is reachable. */
export function stepDays(days: number, direction: 1 | -1): number {
  return clampDays(days + direction)
}

/** pageStepDays is one Page Up or Page Down: a doubling on a doubling scale. */
export function pageStepDays(days: number, direction: 1 | -1): number {
  return clampDays(direction > 0 ? days * 2 : days / 2)
}

/** The marks under the rail: every doubling from 2 d to 256 d, as drawn. */
export const DAY_TICKS = [2, 4, 8, 16, 32, 64, 128, 256]

/**
 * capDays is where a byte cap runs out at today's rate -- the dashed
 * mark on the track -- or null when there is no rate to reckon from.
 * Cut down rather than rounded: 1 GiB at 30 MiB a day is ~34 days, not
 * 35, because the 35th would not fit.
 */
export function capDays(maxBytes: number, bytesPerDay: number): number | null {
  if (!(bytesPerDay > 0) || !(maxBytes > 0)) return null
  return Math.max(1, Math.floor(maxBytes / bytesPerDay))
}

/** bytesForDays is what a number of days needs at today's rate. */
export function bytesForDays(days: number, bytesPerDay: number): number {
  return Math.max(0, days) * Math.max(0, bytesPerDay)
}

/** mibOf is a byte cap as the whole MiB the inline field edits. */
export function mibOf(bytes: number): number {
  return Math.max(1, Math.round(bytes / MIB))
}

/** bytesOfMib is the inline field's figure back in bytes, or null when it is not a number of MiB. */
export function bytesOfMib(text: string): number | null {
  const n = Number(text.trim())
  if (!Number.isFinite(n) || n < 1 || n % 1 !== 0) return null
  return n * MIB
}

// --- dates -----------------------------------------------------------------
//
// The server names days as YYYY-MM-DD, the date of the file. They are
// handled as calendar days rather than instants so a day never shifts
// with the reader's timezone: "7 Aug" on disk is 7 Aug wherever it is
// read from.

function parseDay(ymd: string): { y: number; m: number; d: number } | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(ymd)
  if (!m) return null
  return { y: Number(m[1]), m: Number(m[2]), d: Number(m[3]) }
}

/** addDays moves a YYYY-MM-DD by whole days; an unparseable day is returned as is. */
export function addDays(ymd: string, n: number): string {
  const p = parseDay(ymd)
  if (!p) return ymd
  const t = Date.UTC(p.y, p.m - 1, p.d + n)
  return new Date(t).toISOString().slice(0, 10)
}

/**
 * dayLabel writes a day the way round 42 writes it, "7 Aug": a bare day
 * and short month in the reader's locale, format.ts's formatDayMonth
 * convention. `locale` is for tests; the app passes nothing.
 */
export function dayLabel(ymd: string, locale?: string): string {
  const p = parseDay(ymd)
  if (!p) return ymd
  return new Date(p.y, p.m - 1, p.d).toLocaleDateString(locale, { day: 'numeric', month: 'short' })
}

function nDays(n: number): string {
  return n === 1 ? '1 day' : `${n} days`
}

// --- the row on the right ----------------------------------------------------

/**
 * heldRow is the "on disk" row: "27 days · since 7 Aug · 812 MiB —
 * filling", "— full" when the cap is what decides, and nothing after the
 * size when the days allowed are all on disk. Reads what is held, never
 * the proposal: dragging changes what is allowed, not what is there.
 */
export function heldRow(s: HistorySettings, locale?: string): string | null {
  if (!s.held) return null
  const parts = `${nDays(s.held.days)} · since ${dayLabel(s.held.oldest, locale)} · ${formatSize(s.held.bytes)}`
  return parts + heldSuffix(s)
}

export function heldSuffix(s: HistorySettings): string {
  if (!s.held) return ''
  if (s.capped) return ' — full'
  if (s.held.days < s.days) return ' — filling'
  return ''
}

/** capMark is the dashed mark's own line: "~34 d — where 1 GiB runs out at today's rate". */
export function capMark(maxBytes: number, bytesPerDay: number): { days: number; label: string } | null {
  const days = capDays(maxBytes, bytesPerDay)
  if (days === null || days > DAYS_MAX) return null
  return { days, label: `~${days} d — where ${formatSize(maxBytes)} runs out at today's rate` }
}

// --- proposals ---------------------------------------------------------------

/**
 * Round 42's states: the section classes disk.html switches on `#set`,
 * plus round 43's `dfail` -- the settings GET did not answer, so the
 * group is one row saying so rather than absent.
 */
export type DiskPhase = 'rest' | 'dshrink' | 'dgrow' | 'dcap' | 'doff' | 'dcapped' | 'dstopped' | 'dnokey' | 'dfail'

/** Which of those a proposal puts the group in. */
export type ProposalKind = 'dshrink' | 'dgrow' | 'dcap' | 'doff'

export interface DiskProposal {
  kind: ProposalKind
  /** What would be sent: the whole switch, since one call carries all three. */
  enabled: boolean
  days: number
  maxBytes: number
  /** The consequence sentence, without the links after it. */
  sentence: string
  /** The link that takes it: "delete 13 days", or "apply" when nothing would go. */
  applyLabel: string
  /** The link that does not: "keep all 27", "keep 30 days", "keep 1 GiB", "keep them". */
  keepLabel: string
  /**
   * How many held days the bar dims, oldest first, or null when nothing
   * on disk would go. Equal to held.days when all of it would.
   */
  cut: number | null
  /** The oldest day the proposal keeps, YYYY-MM-DD, or null when it keeps none. */
  newOldest: string | null
  /** The bar's own line beside the cut: "20 Aug — the oldest that 14 days would keep". */
  cutLabel: string | null
}

export interface ProposalOptions {
  locale?: string
}

/**
 * Where the oldest surviving day falls when `kept` of the held days
 * survive: the newest day and the kept-1 before it. An estimate by
 * calendar arithmetic, which is what "at today's rate" already admits.
 */
function survivors(held: HistoryHeld, kept: number, what: string, locale?: string) {
  const deletes = held.days - kept
  if (kept <= 0) {
    return { cut: held.days, newOldest: null, deletes, cutLabel: `all ${held.days} days would let go` }
  }
  const newOldest = addDays(held.newest, -(kept - 1))
  return {
    cut: deletes,
    newOldest,
    deletes,
    cutLabel: `${dayLabel(newOldest, locale)} — the oldest that ${what} would keep`,
  }
}

function letsGo(deletes: number, newOldest: string | null, locale?: string): string {
  if (newOldest === null) return `all ${nDays(deletes)} let go`
  return deletes === 1
    ? `the day before ${dayLabel(newOldest, locale)} lets go`
    : `the ${deletes} days before ${dayLabel(newOldest, locale)} let go`
}

/**
 * proposeDays is the track's sentence. Fewer days:
 *
 *   "14 days holds ~420 MiB at today's rate — the 13 days before 20 Aug
 *    let go — delete 13 days · keep all 27"
 *
 * More days:
 *
 *   "90 days would need ~2.7 GiB at today's rate; the 1 GiB cap would
 *    hold ~34 of them — apply · keep 30 days"
 *
 * Null when the figure is the one in effect. Without a rate the "at
 * today's rate" clauses are left off rather than invented.
 */
export function proposeDays(s: HistorySettings, proposed: number, opts: ProposalOptions = {}): DiskProposal | null {
  const days = clampDays(proposed)
  if (days === s.days) return null
  const rate = s.bytesPerDay > 0
  const cap = formatSize(s.maxBytes)
  const base = { enabled: s.enabled, days, maxBytes: s.maxBytes }

  if (days > s.days) {
    let sentence: string
    if (rate) {
      const need = formatSize(bytesForDays(days, s.bytesPerDay))
      const fits = capDays(s.maxBytes, s.bytesPerDay) ?? days
      sentence =
        fits < days
          ? `${nDays(days)} would need ~${need} at today's rate; the ${cap} cap would hold ~${fits} of them`
          : `${nDays(days)} would need ~${need} at today's rate, within the ${cap} cap`
    } else {
      sentence = `${nDays(days)}, under the ${cap} cap; no rate yet to say how much of it that needs`
    }
    return {
      ...base,
      kind: 'dgrow',
      sentence,
      applyLabel: 'apply',
      keepLabel: `keep ${nDays(s.days)}`,
      cut: null,
      newOldest: null,
      cutLabel: null,
    }
  }

  const holds = rate
    ? `${nDays(days)} holds ~${formatSize(bytesForDays(days, s.bytesPerDay))} at today's rate`
    : nDays(days)
  if (!s.held || days >= s.held.days) {
    return {
      ...base,
      kind: 'dshrink',
      sentence: `${holds} — nothing on disk lets go`,
      applyLabel: 'apply',
      keepLabel: `keep ${nDays(s.days)}`,
      cut: null,
      newOldest: null,
      cutLabel: null,
    }
  }
  const sv = survivors(s.held, days, nDays(days), opts.locale)
  return {
    ...base,
    kind: 'dshrink',
    sentence: `${holds} — ${letsGo(sv.deletes, sv.newOldest, opts.locale)}`,
    applyLabel: `delete ${nDays(sv.deletes)}`,
    keepLabel: `keep all ${s.held.days}`,
    cut: sv.cut,
    newOldest: sv.newOldest,
    cutLabel: sv.cutLabel,
  }
}

/**
 * proposeCap is the inline field's sentence:
 *
 *   "512 MiB holds ~17 days at today's rate — the 10 days before 17 Aug
 *    let go — delete 10 days · keep 1 GiB"
 *
 * The bite is an estimate from today's rate, bounded by the days allowed
 * and by what is actually on disk: a cap above what is held deletes
 * nothing whatever the rate says, and one below it deletes at least a
 * day. Null when the figure is the one in effect.
 */
export function proposeCap(s: HistorySettings, maxBytes: number, opts: ProposalOptions = {}): DiskProposal | null {
  if (!(maxBytes > 0) || maxBytes === s.maxBytes) return null
  const rate = s.bytesPerDay > 0
  const size = formatSize(maxBytes)
  const base = { enabled: s.enabled, days: s.days, maxBytes, kind: 'dcap' as const }
  const keepLabel = `keep ${formatSize(s.maxBytes)}`
  const fits = capDays(maxBytes, s.bytesPerDay)
  const holds = fits === null ? size : `${size} holds ~${Math.min(fits, s.days)} days at today's rate`

  const nothing = !s.held || maxBytes >= s.held.bytes
  if (nothing) {
    return {
      ...base,
      sentence: `${holds} — nothing on disk lets go`,
      applyLabel: 'apply',
      keepLabel,
      cut: null,
      newOldest: null,
      cutLabel: null,
    }
  }
  const held = s.held as HistoryHeld
  if (!rate) {
    return {
      ...base,
      sentence: `${size} is less than the ${formatSize(held.bytes)} on disk — the oldest days let go until it fits`,
      applyLabel: 'apply',
      keepLabel,
      cut: null,
      newOldest: null,
      cutLabel: null,
    }
  }
  // At least one day goes: the cap is below what is held, however the
  // rate rounds.
  const kept = Math.min(fits as number, s.days, held.days - 1)
  const sv = survivors(held, kept, size, opts.locale)
  return {
    ...base,
    sentence: `${holds} — ${letsGo(sv.deletes, sv.newOldest, opts.locale)}`,
    applyLabel: `delete ${nDays(sv.deletes)}`,
    keepLabel,
    cut: sv.cut,
    newOldest: sv.newOldest,
    cutLabel: sv.cutLabel,
  }
}

/**
 * proposeOff is `turn off`'s sentence: "off deletes all 27 days on disk,
 * back to 7 Aug, and keeps nothing after — delete 27 days · keep them".
 * Null when nothing is held, since off then deletes nothing and is not
 * a proposal at all -- the same rule that makes turning on immediate.
 */
export function proposeOff(s: HistorySettings, opts: ProposalOptions = {}): DiskProposal | null {
  if (!s.held || s.held.days <= 0) return null
  const n = s.held.days
  const back = dayLabel(s.held.oldest, opts.locale)
  return {
    kind: 'doff',
    enabled: false,
    days: s.days,
    maxBytes: s.maxBytes,
    sentence:
      n === 1
        ? `off deletes the one day on disk, ${back}, and keeps nothing after`
        : `off deletes all ${n} days on disk, back to ${back}, and keeps nothing after`,
    applyLabel: `delete ${nDays(n)}`,
    keepLabel: 'keep them',
    cut: n,
    newOldest: null,
    cutLabel: n === 1 ? 'the one day would let go' : `all ${n} days would let go`,
  }
}

/**
 * barLabel is the bar's line at rest: "7 Aug — the oldest day on disk",
 * or, when the cap is what decides, "9 Aug — the oldest the 768 MiB cap
 * keeps".
 */
export function barLabel(s: HistorySettings, locale?: string): string | null {
  if (!s.held) return null
  const day = dayLabel(s.held.oldest, locale)
  return s.capped ? `${day} — the oldest the ${formatSize(s.maxBytes)} cap keeps` : `${day} — the oldest day on disk`
}

/**
 * memoryHint is the stopped state's line under where the bar would be:
 * "nothing on disk — events live in memory only, ~9 h of them at today's
 * rate; on keeps those and every day after". The span is the ring's
 * real reach, oldestHeld to now, or left off when there is none.
 */
export function memoryHint(reachHours: number | null): string {
  const span = reachHours !== null && reachHours > 0 ? `, ~${formatHours(reachHours)} of them at today's rate` : ''
  return `nothing on disk — events live in memory only${span}; on keeps those and every day after`
}

/**
 * restartRow is the memory group's `on restart` row (round 43, #921):
 * the buffer alone, and what outlives it. The buffer always clears --
 * nothing refills the ring from disk, and the live scenes start empty
 * after a restart whatever the disk holds -- so every reading starts
 * the same way and the second clause is the disk group's state:
 *
 *   on:      "the buffer clears — the 27 days on disk stay; trying a
 *             watcher reads them"
 *   off:     "the buffer clears — nothing outlives it; days can be kept
 *             on disk below"
 *   no key:  "the buffer clears — nothing outlives it"
 *   unknown: "the buffer clears" (null: the GET refused or failed, or
 *             this caller may not ask)
 *
 * The reader is named because it is the only one: a watcher's try reads
 * disk then memory; the Fall and the stream read the buffer only.
 */
export function restartRow(s: HistorySettings | null): string {
  const clears = 'the buffer clears'
  if (!s) return clears
  if (!s.keyed) return `${clears} — nothing outlives it`
  if (!s.enabled) return `${clears} — nothing outlives it; days can be kept on disk below`
  const n = s.held?.days ?? 0
  if (n === 1) return `${clears} — the 1 day on disk stays; trying a watcher reads it`
  if (n > 1) return `${clears} — the ${n} days on disk stay; trying a watcher reads them`
  return `${clears} — what is on disk stays; trying a watcher reads it`
}

/**
 * stateRow is the disk group's `state` row: the other thing kept on
 * disk, beside the key. Which backend the state store (flags,
 * definitions, watchlist entries, entities, tokens, accounts) actually
 * uses, from GET /api/persistence; null for a caller it refuses.
 *
 * 'memory' (#853) is what a JSON deployment with no key mounted reports:
 * flags, definitions, watchlist and entities refuse to persist rather
 * than write unencrypted, so those don't survive a restart. Accounts and
 * tokens are the exception (#853 rule 6): they hold only one-way hashes,
 * so they keep persisting to a plain file with no key, exactly as before
 * #853.
 */
export function stateRow(info: PersistenceInfo | null): string | null {
  if (!info) return null
  if (info.backend === 'postgres') return 'Postgres — flags, definitions, watchlist, entities, tokens'
  if (info.backend === 'memory') {
    return 'memory only — no key configured, so flags, definitions, watchlist and entities do not survive a restart (accounts and tokens still do, as one-way hashes)'
  }
  return `encrypted file store · ${info.dir ?? '—'} — flags, definitions, watchlist, entities, tokens`
}

/** The link to the setup guide's own section on mounting a key. */
export const HOW_TO_MOUNT_URL =
  'https://github.com/tomlawesome/mikroview/blob/main/docs/configuration.md#on-disk-event-history-optional-off-by-default'
