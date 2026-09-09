// SPDX-License-Identifier: AGPL-3.0-only

import { formatDayMonth, formatHM } from './format'
import type {
  DecommissionOffer,
  DecommissionReceipt,
  DecommissionSighting,
  DecommissionWatch,
  GhostState,
} from './types'

// The decommission offer and the ghost segment (#460), design round 55.
//
// Everything in this file is a pure function of the API's own data, and
// it exists so the flat map and the city say the same words about the
// same watch. Round 55's rule: "the offer, the receipt, the state names
// and inks, the cards and the watchlist row are one thing on both
// surfaces; only the mark's geometry differs." The geometry lives in
// each surface; the sentences live here.

// The ghost's ink is its watch state -- grey before the watch starts,
// watch-purple while it holds, alarm-red once it is broken. These are the
// screen's existing inks, not new ones: --fg-dim is what a dark boundary
// is already drawn in, and --marked is this screen's own watcher ink, the
// one the host rings, the aggregate bar and the dials all use. A ghost
// therefore reads like any other watched thing, which is the point.
export const GHOST_INK: Record<GhostState, string> = {
  none: 'var(--fg-dim)',
  holding: 'var(--marked)',
  broken: 'var(--alarm)',
}

// STRAGGLER_ALARM_MS is how long a straggler holds the ghost red.
//
// Round 55 draws both ends of this and no rule between them: at 22:41 a
// straggler turns the ghost alarm-red, and at 03:52 -- same watch, three
// stragglers behind it, four hours of quiet since -- the ghost is back in
// watch purple. So the alarm is the arrival, not the history. An hour is
// the boundary because it is the one the ghost already shows: the tally
// counts whole hours, so a ghost that cannot yet claim an hour of quiet
// is saying "0 h of 6 h", and that is exactly the moment the alarm is
// still true. Nothing is invented to hold the two scenes apart.
const STRAGGLER_ALARM_MS = 3_600_000

// ghostStateOf answers what the ghost is painted in.
//
// It reads the watch's own record rather than the server's State string,
// because the server's four states and the map's three inks are not the
// same question. 'draining' means a straggler was seen at some point and
// the clock restarted, which stays true for the rest of the watch's life;
// the map's alarm is about now.
//
// Two things make a ghost red. A straggler just arrived -- the range was
// declared dead and something contradicted the declaration. Or nothing
// logs the range at all, in which case the watch cannot claim quiet and
// says so rather than reporting silence it never had the means to hear
// (the same honesty #546's broken ring exists for). Which of the two it
// is shows in the tally line, never in the colour.
//
// An unanswered offer has no watch at all: that is 'none', the grey the
// segment is drawn in between leaving the router and being answered.
export function ghostStateOf(watch: DecommissionWatch | null | undefined, nowMs: number): GhostState {
  if (!watch || watch.state === 'retired') return 'none'
  if (!watch.covered) return 'broken'
  if (watch.trafficCount > 0 && watch.lastTrafficAt && nowMs - Date.parse(watch.lastTrafficAt) < STRAGGLER_ALARM_MS) {
    return 'broken'
  }
  return 'holding'
}

// hoursOfWindow renders "4 h of 6 h": how long the range has been quiet
// against how long it must stay quiet. Both halves are rounded down to
// whole hours, which is what the design draws and is also the honest
// direction to round -- claiming the fifth hour before it is finished
// would overstate how close the watch is to retiring.
export function hoursOfWindow(watch: DecommissionWatch, nowMs: number): string {
  const windowH = Math.floor(watch.cleanWindow / 3_600_000_000_000)
  const since = watch.lastTrafficAt ?? watch.createdAt
  const quietH = Math.max(0, Math.floor((nowMs - Date.parse(since)) / 3_600_000))
  return `${Math.min(quietH, windowH)} h of ${windowH} h`
}

// retiresAt is when the watch retires if nothing else arrives -- the
// "retires by itself at 04:11" the ghost's card promises.
export function retiresAt(watch: DecommissionWatch): string {
  const since = watch.lastTrafficAt ?? watch.createdAt
  return formatHM(new Date(Date.parse(since) + watch.cleanWindow / 1_000_000).toISOString())
}

// ghostTally is the flat map's lane line -- the short form, shortened at
// the owner's request in round 55's second batch so it fits the lane.
export function ghostTally(
  state: GhostState,
  watch: DecommissionWatch | null,
  offer: DecommissionOffer | null,
  nowMs: number,
): string {
  if (state === 'none') {
    const at = formatHM(offer?.departedAt ?? watch?.createdAt ?? new Date(nowMs).toISOString())
    const n = offer?.lastKnown?.length ?? 0
    return n > 0 ? `gone at ${at} · ${n} host${n === 1 ? '' : 's'} were here` : `gone at ${at}`
  }
  if (!watch) return ''
  const at = formatHM(watch.createdAt)
  if (state === 'holding') return `retired ${at} · holding · ${hoursOfWindow(watch, nowMs)}`
  // Broken has two causes and the design draws only one of them, so the
  // straggler wording is used where there is a straggler and the other
  // cause says what it actually is rather than borrowing a line count it
  // does not have.
  if (watch.trafficCount > 0) {
    return `retired ${at} · BROKEN · ${watch.trafficCount} line${watch.trafficCount === 1 ? '' : 's'}`
  }
  return `retired ${at} · BROKEN · nothing logs this range`
}

// ghostNote is the city plaque's second line -- the same facts as the
// lane's tally in the plaque's own longer voice.
export function ghostNote(
  state: GhostState,
  watch: DecommissionWatch | null,
  offer: DecommissionOffer | null,
  nowMs: number,
): string {
  if (state === 'none') {
    return `gone from the router · ${formatHM(offer?.departedAt ?? watch?.createdAt ?? new Date(nowMs).toISOString())}`
  }
  if (!watch) return ''
  const at = formatHM(watch.createdAt)
  if (state === 'holding') return `retired ${at} · watch holding · quiet ${hoursOfWindow(watch, nowMs)}`
  if (watch.trafficCount > 0) {
    return `retired ${at} · WATCH BROKEN · ${watch.trafficCount} line${watch.trafficCount === 1 ? '' : 's'} since`
  }
  return `retired ${at} · WATCH BROKEN · nothing logs this range`
}

// boroughLabel counts a ghost separately from the live districts it sits
// among -- "4 DISTRICTS · 1 GHOST". A ghost is still a place on the map,
// so it is inside the borough ring; it is not a district any more, so it
// is not inside the district count.
export function boroughLabel(name: string, districts: number, ghosts: number): string {
  const base = `${name} · ${districts} ${districts === 1 ? 'DISTRICT' : 'DISTRICTS'}`
  if (ghosts <= 0) return base
  return `${base} · ${ghosts} GHOST${ghosts === 1 ? '' : 'S'}`
}

// receiptSentence is the offer's only argument: what a watch on this
// range would have caught over the corpus that was actually available.
//
// The span is never dropped. A count with no period invites the reader to
// supply a flattering one, which is the exact dishonesty the server-side
// view refuses to allow, and the sentence degrades in the same direction:
// where the replay retained no addresses it says the count and the span
// and stops, rather than naming a device it cannot name.
export function receiptSentence(receipt: DecommissionReceipt): string {
  const span = spanWords(receipt)
  const n = receipt.emissionCount
  if (n === 0) return `in ${span} a watch here would have caught nothing`
  const head = `in ${span} a watch here would have caught ${n} line${n === 1 ? '' : 's'}`
  const addrs = receipt.addresses ?? []
  if (addrs.length === 0) return head
  if (addrs.length === 1) {
    const a = addrs[0]
    return `${head} — all from ${a.address}${a.name ? `, last named ${a.name}` : ''}`
  }
  const named = addrs.slice(0, 3).map((a) => a.address)
  return `${head} — from ${named.join(', ')}${addrs.length > named.length ? ' and others' : ''}`
}

// spanWords turns the replay window into the phrase the receipt opens
// with. Whole hours where it has them, because "the last 3 h" is what the
// operator can hold in their head; minutes on a young instance, where
// saying "the last 0 h" would read as a bug.
export function spanWords(receipt: DecommissionReceipt): string {
  const ms = Date.parse(receipt.end) - Date.parse(receipt.start)
  if (!Number.isFinite(ms) || ms <= 0) return 'the ring so far'
  const hours = Math.floor(ms / 3_600_000)
  if (hours >= 1) return `the last ${hours} h`
  return `the last ${Math.max(1, Math.round(ms / 60_000))} min`
}

// offerHeadline and offerSummary are the offer card's first two lines.
export function offerHeadline(offer: DecommissionOffer): string {
  return `${offer.name || offer.interface || offer.cidr} has left the router`
}

export function offerSummary(offer: DecommissionOffer): string {
  const at = formatHM(offer.departedAt)
  const leases = offer.lastKnown?.length ?? 0
  const what = leases > 0 ? `the address and ${leases} lease${leases === 1 ? '' : 's'} are gone` : 'the address is gone'
  return `the ${at} push no longer carries it — ${what}`
}

// offerQuestion is the offer itself, in the words #460 was written in.
export function offerQuestion(offer: DecommissionOffer): string {
  return `Watch the dead range for stragglers? Anything to or from ${offer.cidr} from now on is a violation, named with the host that last had the address.`
}

// watchlistRowText is the watchlist's own row, shown inside the ghost's
// card and the straggler's so the operator can see exactly what stays
// behind when the ghost leaves the map.
export function watchlistRowText(watch: DecommissionWatch, nowMs: number): string {
  const state = ghostStateOf(watch, nowMs)
  if (state === 'broken') {
    return watch.trafficCount > 0
      ? `broken · ${watch.trafficCount} line${watch.trafficCount === 1 ? '' : 's'}`
      : 'broken · nothing logs this range'
  }
  return `holding · ${hoursOfWindow(watch, nowMs)}`
}

// stragglerTitle is the straggler card's own heading: the pair, in the
// direction the line ran.
export function stragglerTitle(s: DecommissionSighting, peerName?: string): string {
  return s.peer ? `${s.address} → ${peerName || s.peer}` : s.address
}

// stragglerHeadline is the finding in one line -- what makes a legal,
// accepted packet a violation is only that the range was declared dead.
export function stragglerHeadline(watch: DecommissionWatch, s: DecommissionSighting): string {
  const port = s.port ? `${s.port}/${(s.protocol || 'tcp').toLowerCase()}` : (s.protocol || '').toLowerCase()
  const n = watch.trafficCount
  const parts = ['a retired range talking']
  if (port) parts.push(port)
  parts.push(`${n} line${n === 1 ? '' : 's'} since ${formatHM(watch.createdAt)}`)
  parts.push(`last ${formatHM(s.at)}`)
  return parts.join(' · ')
}

// stragglerProvenance is the enrichment line: what the address was called,
// where the router saw it arrive, and which rule judged it. Every clause
// is dropped where no push supplied it -- the absence rule, so the line
// shortens rather than guessing.
export function stragglerProvenance(watch: DecommissionWatch, s: DecommissionSighting): string {
  const parts: string[] = []
  const known = watch.lastKnown?.find((k) => k.address === s.address)
  if (known?.name) {
    const until = known.seenAt ? ` until ${formatDayMonth(known.seenAt)}` : ''
    const src = known.source ? ` (${known.source}, ${watch.device})` : ''
    parts.push(`was ${known.name}${until}${src}`)
  }
  if (s.interface) parts.push(`arrived on ${s.interface}`)
  if (s.rule) parts.push(`${s.action === 'drop' || s.action === 'reject' ? 'stopped by' : 'accepted by'} ${s.rule}`)
  return parts.join(' · ')
}

// stragglerReading is the sentence that says what this most likely is.
//
// Drawn in round 55 as "a device with a static address the move did not
// touch", and it is only said where the router once named the address:
// a range mikroview never had a lease for could as easily be a forgotten
// rule steering packets at a dead range, and asserting a device would be
// a guess.
export function stragglerReading(watch: DecommissionWatch, s: DecommissionSighting): string {
  const known = watch.lastKnown?.find((k) => k.address === s.address)
  if (!known) return ''
  return 'a device with a static address the move did not touch'
}

// stragglerCallout is the two-line chip drawn on the map beside the
// straggler's line, on both surfaces.
export function stragglerCallout(
  watch: DecommissionWatch,
  s: DecommissionSighting,
  peerName?: string,
): { head: string; detail: string } {
  const port = s.port ? `${(s.protocol || 'tcp').toLowerCase()}/${s.port}` : ''
  const head = `STRAGGLER · ${stragglerTitle(s, peerName)}${port ? ` · ${port}` : ''}`
  const known = watch.lastKnown?.find((k) => k.address === s.address)
  const bits: string[] = []
  if (known?.name) bits.push(`was ‘${known.name}’${known.seenAt ? ` until ${formatDayMonth(known.seenAt)}` : ''}`)
  if (s.interface) bits.push(`in: ${s.interface}`)
  bits.push(`${watch.trafficCount}×`)
  return { head, detail: bits.join(' · ') }
}

// retiredNote is the one line the map leaves behind when a ghost retires
// by itself -- round 55's "retirement is silent", which is why it is a
// note under the map and not a dialogue.
export function retiredNote(watch: DecommissionWatch): string {
  const windowH = Math.floor(watch.cleanWindow / 3_600_000_000_000)
  const at = watch.retiredAt ? formatHM(watch.retiredAt) : ''
  return `${watch.name || watch.interface} · ${watch.cidr} retired at ${at} — quiet for ${windowH} h — the ghost has left the map and the watch is closed`
}

// undoable reports whether the retirement can still be taken back. The
// server owns the rule and sends the deadline; this only reads it, so the
// hour cannot come to mean two different things.
export function undoable(watch: DecommissionWatch, nowMs: number): boolean {
  if (!watch.undoableUntil) return false
  return nowMs < Date.parse(watch.undoableUntil)
}

// forceRemoveContract is the whole of what pressing force-remove does,
// said before it is pressed. The #385 pattern is a heavy warning plus a
// recorded override, and this is the warning half.
export function forceRemoveContract(watch: DecommissionWatch, nowMs: number): string {
  const left = hoursOfWindow(watch, nowMs)
  return `The ghost leaves the map now. The watch goes on in the watchlist — ${left} of quiet still to go, or longer if a straggler resets its clock — and can only be forgotten from there.`
}
