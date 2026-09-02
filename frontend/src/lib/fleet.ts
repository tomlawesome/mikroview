// SPDX-License-Identifier: AGPL-3.0-only
//
// The fleet's sort/status logic (issue #98), pulled into its own small
// module by #647 so it has exactly one home: Fleet.svelte (still the
// dedicated page the phone-width bottom bar reaches) and Entities.svelte
// (whose "routers" section now leads the merged page, #634 round 23 --
// "fleet looks a bit lost", folded in rather than left as its own,
// sparser card) both read from here instead of keeping their own copies
// that could drift apart. The deck-facing surface says "Entities", never
// "fleet" (round 23's verdict) -- this module is the "internal
// code/state" the record allows to keep the name.
import { prose } from './setupsteps'
import type { ClientEvent, Device } from './types'

export const RECENT_WINDOW_MS = 5 * 60 * 1000

export const STATUS_LABEL: Record<Device['status'], string> = {
  live: 'Live',
  stale: 'Stale',
  never_seen: 'Never seen',
}

// Configured devices first (an auto-discovered source is secondary
// information, not something you set out to monitor), then by status
// severity (stale/never-seen surfaced above live -- the whole point of
// a fleet view is spotting the ones that need a look), then
// alphabetically so the order is otherwise stable.
export function sortedDevices(devices: readonly Device[]): Device[] {
  return [...devices].sort((a, b) => {
    if (a.configured !== b.configured) return a.configured ? -1 : 1
    const severity: Record<Device['status'], number> = { stale: 0, never_seen: 1, live: 2 }
    if (severity[a.status] !== severity[b.status]) return severity[a.status] - severity[b.status]
    return a.name.localeCompare(b.name)
  })
}

// A rough per-device rate, client-side, from the live event buffer --
// complements the lifetime eventCount GET /api/devices already reports,
// without needing a new backend endpoint.
export function recentCount(events: readonly ClientEvent[], deviceId: string, nowMs: number): number {
  const cutoff = nowMs - RECENT_WINDOW_MS
  let n = 0
  for (const e of events) {
    if (e.deviceId === deviceId && e.receivedAt >= cutoff) n++
  }
  return n
}

// The router card's status vocabulary (#675's fstate, hoisted here for
// #657/#706 so Entities' router row and the viewer's Fleet card
// literally share it rather than keeping copies that could drift): a
// mark plus a written label, never colour alone (#616's honesty rule),
// and "quiet" rather than an alarm word -- a silent router is a fact to
// read, not a fault to shout.
export interface DeviceStateMark {
  mark: string
  cls: 'ok' | 'quiet'
  text: string
}

export function deviceState(d: Device, nowMs: number): DeviceStateMark {
  if (d.status === 'live') return { mark: '●', cls: 'ok', text: 'LIVE' }
  if (d.status === 'never_seen') return { mark: '◌', cls: 'quiet', text: STATUS_LABEL.never_seen.toUpperCase() }
  const days = Math.floor((nowMs - new Date(d.lastSeen).getTime()) / 86_400_000)
  return { mark: '◌', cls: 'quiet', text: `QUIET${days >= 1 ? ` · ${days} d` : ''}` }
}

// Fixed to one decimal rather than lib/format's formatEps (whole number
// at >=1 events/s, one decimal below it): two router cards showing "1"
// and "1.0" side by side read as inconsistent even though each is
// individually correct under formatEps's own rule (#718). Hoisted with
// deviceState above, for the same no-drift reason.
export function ratePerSecond(events: readonly ClientEvent[], deviceId: string, nowMs: number): string {
  return (recentCount(events, deviceId, nowMs) / (RECENT_WINDOW_MS / 1000)).toFixed(1)
}

// multihomedEcho (#442) is the one sentence a configured-silent card
// carries when the server has paired it with undeclared addresses that
// are streaming -- the fleet already shows the pair, so this only points
// an operator who never reopens the wizard back at step 2, where the
// router console is open and the command is printed with their values.
// Null for every other card: the notice clears itself once the declared
// device sends its first log, and nothing else here diagnoses another
// device's silence.
export function multihomedEcho(d: Device): string | null {
  const arriving = d.multihomedCandidates ?? []
  if (!d.configured || arriving.length === 0) return null
  const declared = d.sourceIp || d.id
  return (
    `Declared as ${declared}, nothing arrived. If ${prose(arriving, 'or')} below is the same router ` +
    `on another of its addresses, Run setup… step 2 shows the one-line fix.`
  )
}
