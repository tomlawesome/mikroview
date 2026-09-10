// SPDX-License-Identifier: AGPL-3.0-only
//
// Living hosts, as the city draws them (round 49, #1016).
//
// The map's hosts used to come only from the live event buffer
// (zones.svelte.ts), which makes presence a property of the buffer
// rather than of the network: a host that stops talking scrolls out and
// vanishes. The server's register (internal/hosts, lib/hosts.svelte.ts)
// is the record that outlives the buffer, and this module is where the
// two are put together into the one list a district's plate is built
// from.
//
// It is deliberately plain data and pure functions, the same bargain the
// rest of lib/city keeps: input.ts and layout.ts never import a store,
// so the ground plan stays testable without mounting anything. The one
// piece that does need the store -- calling `presenceOf` against the
// clock -- happens in City.svelte, which hands the answers in here.
//
// The rule this draws (DESIGN.md "Living hosts"): quiet is 24 hours of
// nothing, a quiet host is never removed, and neither quiet nor quiet on
// purpose claims more than "not heard".
import { extractSourceIp } from '../flags.svelte'
import type { HostPresence } from '../hosts.svelte'
import type { Flag, WatchlistEntry } from '../types'

export type { HostPresence }

/**
 * One host on the map: whatever the event buffer and the register
 * between them know about it.
 *
 * `presence` is never 'dismissed' by the time a CityHost reaches the
 * layout -- mergeZoneHosts drops those, because dismissed means "take
 * this off my map". Everything else stays drawn.
 */
export interface CityHost {
  /** The register's key, `"<iface>|<ip>"`. Empty for a host only the
   * event buffer has seen: it has no record yet, so it cannot be
   * marked, and the card says so rather than offering a write that
   * would 404. */
  key: string
  ip: string
  label: string
  presence: HostPresence
  /** ISO stamps from the register; null for a buffer-only host, whose
   * first and last sighting the buffer does not keep. */
  lastSeen: string | null
  firstSeen: string | null
  events: number
  /** The reason recorded with a `quiet on purpose` mark, and who said
   * it when. Null unless the register carries a mark. */
  reason: string | null
  markedBy: string | null
  markedAt: string | null
  /** Open, un-cleared flags whose target names this address (#981,
   * round 46). A mark exists only while there is something behind it,
   * so this is the whole of what makes a building red -- there is no
   * toggle, on either surface (owner, 2026-09-08). */
  flags: number
  /** Watchlist entries with this address at either end. */
  watch: number
  /** One of those open flags is an activity spike -- the one flag kind
   * that is happening *now*, so its rim breathes and nothing else
   * does. Never true with `flags` at zero: the pulse is drawn inside
   * the flagged branch, so a pulsing mark is always a flagged mark. */
  spike: boolean
}

/** What one address is marked with, before it is put on a host. */
export interface HostMarks {
  flags: number
  watch: number
  spike: boolean
}

/** Nothing is behind this address: no mark is drawn at all. */
export const NO_MARKS: HostMarks = { flags: 0, watch: 0, spike: false }

/** The one flag type whose mark pulses (round 46, #981): activity is
 * the only flag kind that is happening now, so motion fits it and
 * nothing else. */
export const SPIKE_FLAG_TYPE = 'activity_spike'

/**
 * Every marked address, from the two lists that carry the marks.
 *
 * The readings are Topography.svelte's own, which the 2D map already
 * draws its halo and ring from: a flag counts against the source
 * address its target names (`extractSourceIp` -- a detector whose
 * target is a port, a rule label or `global` names no host and counts
 * against none), and a watchlist entry counts once against each end it
 * states. One entry naming the same address at both ends is still one
 * watcher, not two.
 */
export function hostMarksFrom(flags: readonly Flag[], entries: readonly WatchlistEntry[]): Map<string, HostMarks> {
  const out = new Map<string, HostMarks>()
  const at = (ip: string): HostMarks => {
    let m = out.get(ip)
    if (!m) out.set(ip, (m = { flags: 0, watch: 0, spike: false }))
    return m
  }
  for (const f of flags) {
    if (f.cleared) continue
    const ip = extractSourceIp(f.target)
    if (!ip) continue
    const m = at(ip)
    m.flags += 1
    if (f.type === SPIKE_FLAG_TYPE) m.spike = true
  }
  for (const e of entries) {
    const ends = new Set<string>()
    for (const ip of [e.source?.ip, e.destIp]) if (ip) ends.add(ip)
    for (const ip of ends) at(ip).watch += 1
  }
  return out
}

/** A live host the event buffer has seen and the register has not (yet). */
export function bufferHost(label: string, ip: string): CityHost {
  return {
    key: '',
    ip,
    label,
    presence: 'live',
    lastSeen: null,
    firstSeen: null,
    events: 0,
    reason: null,
    markedBy: null,
    markedAt: null,
    flags: 0,
    watch: 0,
    spike: false,
  }
}

/**
 * How long a host has been silent, in the shortest honest unit.
 *
 * The ratified wording is `quiet · 26 h` (DESIGN.md "Living hosts", and
 * the mockup's tv-lounge card), so hours are the unit until they stop
 * being readable. Under an hour is rounded up to `1 h` rather than
 * shown in minutes: the window is 24 hours, so nothing reaches this
 * function having been quiet for minutes, and a minutes figure would
 * invite reading the drawing as a live-ness meter, which it is not.
 */
export function quietFor(lastSeen: string | null, now: number): string {
  if (!lastSeen) return 'not heard'
  const t = Date.parse(lastSeen)
  if (Number.isNaN(t)) return 'not heard'
  const hours = (now - t) / 3_600_000
  if (hours < 1) return '1 h'
  if (hours < 72) return Math.round(hours) + ' h'
  return Math.round(hours / 24) + ' d'
}

/**
 * The one line a quiet building says about itself, under its name.
 * Live hosts say nothing extra -- being drawn normally is the whole
 * statement -- so this returns null for them and the label falls back
 * to the address.
 */
export function presenceNote(h: CityHost, now: number): string | null {
  if (h.presence === 'quiet') return 'quiet · ' + quietFor(h.lastSeen, now)
  if (h.presence === 'intended') return 'quiet on purpose'
  return null
}

/**
 * mergeZoneHosts puts one district's two sources together.
 *
 * The order is what the plate's slots are handed out in, and
 * layout.ts's MAX_BUILDINGS cap takes the first of them, so it decides
 * what is drawn when a district holds more hosts than the plate can
 * carry legibly. Live hosts come first, in the buffer's own busiest-
 * first order, and the register's quiet ones fill in behind -- so the
 * cap costs a silent machine its slot before it costs a talking one,
 * and the district's `+N` says how many did not fit either way.
 *
 * A dismissed host is dropped here and nowhere else: that is the one
 * state that means "off my map". It comes back by itself, because the
 * server clears the dismissal the moment the feed hears the host again
 * (internal/hosts.Register.Observe), so nothing in the browser has to
 * remember it.
 */
export function mergeZoneHosts(
  buffer: { label: string; ip: string }[],
  registered: CityHost[],
  marks: ReadonlyMap<string, HostMarks> = new Map(),
): CityHost[] {
  const byIp = new Map<string, CityHost>()
  for (const h of registered) {
    const had = byIp.get(h.ip)
    // One address can hold more than one register key (the same machine
    // seen on two interfaces). The kindest reading wins, for the same
    // reason a wall takes the worst of its gates and a road the best of
    // its directions: presence is a claim about silence, and one
    // interface still hearing the host is evidence it is not silent.
    if (!had || rank(h.presence) < rank(had.presence)) byIp.set(h.ip, h)
  }
  const out: CityHost[] = []
  const taken = new Set<string>()
  for (const b of buffer) {
    const reg = byIp.get(b.ip)
    if (reg) {
      taken.add(b.ip)
      // Dismissed wins over the buffer still listing the address. The
      // register is the record, and it is the server that takes a
      // dismissal back -- the moment the feed hears the host, Observe
      // clears the mark and the next read of the register brings the
      // building back. Un-dismissing it here instead would be the
      // browser second-guessing that rule, and the two would part on
      // the first change to either.
      if (reg.presence === 'dismissed') continue
      // The buffer's label is the one the rest of the map already shows,
      // so it wins where it says anything; the register's fills the gap
      // for a host nothing in the window named.
      out.push({ ...reg, label: b.label || reg.label || b.ip })
    } else {
      out.push(bufferHost(b.label || b.ip, b.ip))
    }
  }
  for (const [ip, h] of byIp) {
    if (taken.has(ip) || h.presence === 'dismissed') continue
    out.push({ ...h, label: h.label || ip })
  }
  // The marks go on last, over whatever either source carried: they are
  // a fact about the flag and watchlist ledgers, not about the register,
  // and applying them here means a buffer-only host wears them exactly
  // as a registered one does. A host nothing has marked keeps the zeros
  // it was built with -- and a building with zeros draws no mark at all.
  return out.map((h) => {
    const m = marks.get(h.ip)
    return m ? { ...h, flags: m.flags, watch: m.watch, spike: m.spike } : h
  })
}

const ORDER: Record<HostPresence, number> = { live: 0, quiet: 1, intended: 2, dismissed: 3 }
const rank = (p: HostPresence): number => ORDER[p]
