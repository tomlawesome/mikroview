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
