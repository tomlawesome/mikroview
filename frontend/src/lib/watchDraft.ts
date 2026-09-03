// SPDX-License-Identifier: AGPL-3.0-only
//
// #641's "watch for this": the Resolved undo line offers a watcher, and
// the Watchlist's own entry form opens prefilled from the flag rather
// than making the operator retype what mikroview already recorded.
//
// The point of the watcher, in the issue's words: it is a tripwire finer
// than the detector. The detector brings a resolved flag back only when
// the host re-crosses its threshold; the watch fires on the first line
// that reappears, which is when you want to hear the block did not hold.
//
// This module is the mapping only -- pure, so a unit test can drive it
// without rendering either page. Flags.svelte hands the result to the
// shared handoff in topologyNav.svelte.ts and Watchlist.svelte opens its
// draft from it.

import type { PendingWatchDraft } from './topologyNav.svelte'
import { extractSourceIp } from './flags.svelte'
import { pairsTruncated } from './evidencePairs'
import type { Flag, HostPort } from './types'

// watchDraftForFlag builds the prefilled draft for f, or null where the
// flag gives nothing honest to prefill: no evidence pairs to watch, or a
// target that names no host (a rule label, a bare port, "global" -- see
// extractSourceIp).
//
// Identity is MAC-preferred, IP-fallback, the same rule every other
// identity in this app follows (matchlog.Identity): a MAC-bound watch
// follows the device through a DHCP lease change, an IP-bound one stops
// matching it. Which one this is gets said out loud in the provenance
// line rather than left for the operator to infer from the field.
export function watchDraftForFlag(f: Flag): PendingWatchDraft | null {
  const pairs = f.evidence?.pairs ?? []
  if (pairs.length === 0) return null

  const mac = f.evidence?.srcMac
  const who = mac || extractSourceIp(f.target)
  if (!who) return null

  return {
    who,
    toward: towardFor(pairs),
    mode: 'expect',
    provenance: provenanceFor(f, pairs, Boolean(mac)),
    returnTo: 'flags',
  }
}

// towardFor renders the pairs into the draft's one compound "toward"
// field ("nas · :445, :139" -- a host before the separator, its ports
// after it).
//
// A watchlist entry scopes one destination (watchlist.Entry.DestIP), and
// the pairs behind an outbound_anomaly or internal_recon flag routinely
// name several -- reaching many destinations is what those detectors
// measure. Naming only the first would quietly drop the rest, and a
// tripwire that misses most of what it was drafted from is worse than
// none. So where the pairs share one destination it is named, and where
// they do not the ports are watched toward any destination: broader than
// the pairs, never narrower, and the provenance line says which of the
// two this is before the operator saves it.
export function towardFor(pairs: HostPort[]): string {
  const hosts = [...new Set(pairs.map((p) => p.host))]
  const ports = [...new Set(pairs.map((p) => p.port))].sort((a, b) => a - b)
  const portList = ports.map((p) => `:${p}`).join(', ')
  if (hosts.length === 1) return `${hosts[0]} · ${portList}`
  return `· ${portList}`
}

// provenanceFor is the sentence stated wherever the pairs are shown
// (#641): where they came from, how many of how many, and whether the
// identity is MAC- or IP-bound.
//
// "6 of at least 14 pairs" rather than a flat "6 of 14" once
// pairsTotalIsFloor is set: past internal/engine's
// maxEvidencePairsTracked the total stops being exact, and a precise-
// looking number that is not precise is the overclaim #654 added the
// flag for.
export function provenanceFor(f: Flag, pairs: HostPort[], macBound: boolean): string {
  const total = f.evidence?.pairsTotal
  const floor = f.evidence?.pairsTotalIsFloor
  let counted: string
  if (pairsTruncated(pairs, total)) {
    counted = `${pairs.length} of ${floor ? 'at least ' : ''}${total} pairs`
  } else {
    counted = `${pairs.length} ${pairs.length === 1 ? 'pair' : 'pairs'}`
  }

  const identity = macBound
    ? 'MAC-bound, so it follows this device through a lease change'
    : 'IP-bound, so it stops matching if this device gets a new address'

  const hosts = new Set(pairs.map((p) => p.host))
  const spread =
    hosts.size > 1
      ? ` Those pairs name ${hosts.size} destinations and one watch holds one, so this watches those ports toward any destination.`
      : ''

  return `From the last firing window, ${counted} — ${identity}.${spread}`
}
