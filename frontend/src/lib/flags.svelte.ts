// SPDX-License-Identifier: AGPL-3.0-only

import { clearFlag, clearFlagPermanent, fetchFlags } from './api'
import type { Flag, FlagTimeBucket } from './types'

const IPV4_RE = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/

function isIpAddress(value: string): boolean {
  const m = value.match(IPV4_RE)
  if (m) return m.slice(1).every((octet) => Number(octet) <= 255)
  // Loose IPv6 check -- this only needs to decide "does this look like a
  // single IP" for grouping purposes, not fully validate address syntax
  // (unlike isPublicIp in format.ts, which is deliberately IPv4-only for
  // its own narrower purpose).
  return value.includes(':') && /^[0-9a-fA-F:]+$/.test(value)
}

// Extracts the leading source IP from a flag's `target` for campaign
// grouping (issue #106), or null if the target isn't a single-source-IP
// shape to correlate on. `target` varies by detector (see
// internal/flags.Flag's doc comment and internal/detect/*.go):
//   - bare source IP for most per-host detectors (port_scan,
//     activity_spike, critical_port, outbound_anomaly, internal_recon,
//     low_slow_scan)
//   - "<ip> -> port <N>" for repeated_drops -- the port suffix is
//     stripped here for grouping purposes only; the flag's own
//     detail/display still shows the full composite target
//   - "port <N>" for distributed_brute_force -- no single source IP
//   - a rule label for rule_spike -- no single source IP
//   - "global" for global_spike -- no single actor at all
// The latter three (and anything else that isn't IP-shaped after
// stripping the repeated_drops suffix) return null so they're excluded
// from grouping rather than mis-grouped under a bogus shared key.
export function extractSourceIp(target: string): string | null {
  const withoutDropsSuffix = target.replace(/ -> port \d+$/, '')
  return isIpAddress(withoutDropsSuffix) ? withoutDropsSuffix : null
}

// Behavioral flags (port scans, activity spikes, critical-port attempts,
// global volume spikes -- see internal/detect) raised server-side and
// reviewed/cleared by a human here. Kept as its own small module rather
// than folded into appState, matching how theme/colorway/retention/
// presets each get their own state module in this codebase.
class FlagsState {
  list = $state<Flag[]>([])
  // Last hour of newly-raised-episode counts by type at 1-minute
  // resolution (see internal/flags.Store.TimeSeries), for FlagsChart --
  // fetched alongside list in the same GET /api/flags response.
  timeSeries = $state<FlagTimeBucket[]>([])

  activeCount = $derived(this.list.filter((f) => !f.cleared).length)

  // Groups *active* flags by normalized source IP (see extractSourceIp)
  // so "one actor, several signals" -- a port scan, then a critical-port
  // hit, then a reputation-triggered flag, all from the same host --
  // reads as one correlated unit in Flags.svelte instead of N unrelated
  // entries (issue #106). Cleared flags are left out: they've already
  // been reviewed, so there's nothing left to correlate them toward.
  // Only IPs with more than one active flag are kept -- a group of one
  // is just a normal flag, not a campaign worth calling out.
  groupedBySource = $derived.by(() => {
    const groups = new Map<string, Flag[]>()
    for (const f of this.list) {
      if (f.cleared) continue
      const ip = extractSourceIp(f.target)
      if (!ip) continue
      const existing = groups.get(ip)
      if (existing) existing.push(f)
      else groups.set(ip, [f])
    }
    for (const [ip, flags] of groups) {
      if (flags.length < 2) groups.delete(ip)
    }
    return groups
  })

  async refresh() {
    const res = await fetchFlags()
    this.list = res.flags
    this.timeSeries = res.timeSeries
  }

  // Updates the flag locally the instant the user clicks Clear, rather
  // than waiting on a network round-trip before anything visible
  // happens (the old code awaited clearFlag then a second full refresh()
  // -- two serial round-trips per click, whose completion queued behind
  // whatever else the main thread was doing under a live-traffic flood).
  // App.svelte's existing 5s poll reconciles this against the server
  // regardless, so there's no correctness gap from skipping the extra
  // refetch here -- only a failed clearFlag call needs an explicit
  // revert, since otherwise the flag would sit incorrectly "cleared"
  // until that poll ran.
  async clear(id: string) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.cleared) return

    const wasCleared = flag.cleared
    const previousClearedAt = flag.clearedAt
    flag.cleared = true
    flag.clearedAt = new Date().toISOString()

    try {
      await clearFlag(id)
    } catch (err) {
      flag.cleared = wasCleared
      flag.clearedAt = previousClearedAt
      throw err
    }
  }

  // "Clear and never flag this again" -- same optimistic-update
  // reasoning as clear() above (instant feedback, revert on failure),
  // plus the permanent exclusion this adds server-side. There's no
  // client-visible "excluded" flag state to revert beyond the clear
  // itself; a caller that also renders an exclusions list (see
  // exclusions.svelte.ts) is expected to refresh() that separately.
  async clearPermanent(id: string) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.cleared) return

    const wasCleared = flag.cleared
    const previousClearedAt = flag.clearedAt
    flag.cleared = true
    flag.clearedAt = new Date().toISOString()

    try {
      await clearFlagPermanent(id)
    } catch (err) {
      flag.cleared = wasCleared
      flag.clearedAt = previousClearedAt
      throw err
    }
  }
}

export const flagsState = new FlagsState()
