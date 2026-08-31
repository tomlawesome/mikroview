// SPDX-License-Identifier: AGPL-3.0-only

// #654: internal/flags.Evidence.pairs is a flat list of (host, port)
// combinations actually observed together -- the raw shape a backend
// EvidenceSet accumulates in. The owner's recorded decision for the
// evidence panel is "group by host, never a flat host:port list and
// never two independent lists that imply combinations that were never
// seen" (issue #654, design comment). This module is the pure piece of
// that decision -- Flags.svelte renders what groupPairsByHost/
// pairsTruncated compute, rather than doing the grouping/truncation
// logic inline where a component test would have to drive a whole
// render to exercise it.

import type { HostPort } from './types'

// One host with every distinct port seen paired with it, ports sorted
// ascending -- the panel's own display unit (#654).
export interface HostPortGroup {
  host: string
  ports: number[]
}

// groupPairsByHost turns Evidence.pairs into one entry per distinct
// host. Deliberately re-sorts rather than trusting the caller's order:
// internal/engine.EvidenceSet.Pairs already sorts by host then port
// (see that method's own doc comment), but nothing here should silently
// depend on the backend never changing that, and a unit test fixture
// naming pairs in an arbitrary order should not have to also get the
// sort right by hand.
export function groupPairsByHost(pairs: HostPort[]): HostPortGroup[] {
  const byHost = new Map<string, number[]>()
  for (const { host, port } of pairs) {
    const ports = byHost.get(host)
    if (ports) {
      if (!ports.includes(port)) ports.push(port)
    } else {
      byHost.set(host, [port])
    }
  }
  return Array.from(byHost, ([host, ports]) => ({ host, ports: ports.slice().sort((a, b) => a - b) })).sort((a, b) =>
    a.host.localeCompare(b.host),
  )
}

// pairsTruncated reports whether `pairs` is a partial sample of
// `pairsTotal` -- #654's "never silently truncate" requirement, so a
// caller can render "50 of 214 pairs" instead of a short list that reads
// as complete. routeToFlag (internal/engine/router.go) only ever sets
// pairsTotal when it exceeds len(pairs), so in practice this is a
// readability wrapper over that contract -- but it is written to hold
// even if a future caller stops relying on that (an equal or absent
// pairsTotal is never "truncated").
export function pairsTruncated(pairs: HostPort[] | undefined, pairsTotal: number | undefined): boolean {
  return typeof pairsTotal === 'number' && pairsTotal > (pairs?.length ?? 0)
}

// pairsTruncationLabel formats the "N of M" (or "N of M+") notice for a
// truncated pairs list. Only meaningful when pairsTruncated(...) is true
// -- a caller checks that first, the same way every call site below
// does, rather than this function re-deriving it.
//
// The "+" case is #654's owner-mandated correction: pairsTotal is itself
// bounded by internal/engine's maxEvidencePairsTracked (a resource-safety
// ceiling, independent of the smaller display cap Pairs() applies -- see
// that constant's own doc comment for why an exact count was traded away
// past a point). Once pairsTotalIsFloor is true, pairsTotal is a lower
// bound, not the real count, and a flat "50 of 200" would look exactly
// as precise as a genuine "50 of 214" while lying about it -- so the
// floor case must render visibly differently ("50 of 200+"), not just
// share the same template with a bigger number.
export function pairsTruncationLabel(pairsShown: number, pairsTotal: number, pairsTotalIsFloor: boolean | undefined): string {
  return `${pairsShown} of ${pairsTotal}${pairsTotalIsFloor ? '+' : ''}`
}
