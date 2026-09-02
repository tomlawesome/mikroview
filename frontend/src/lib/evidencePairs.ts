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

// pairsTruncationLabel is the one line that closes the flag drawer's
// evidence list: "12 of 340 pairs", or "12 of at least 340 pairs" when
// the total is itself a floor. Only meaningful when pairsTruncated(...)
// is true -- a caller checks that first, so that nothing is cut means
// no line at all, rather than a line saying so.
//
// Wording ruled on #750 (group B item 3, 2026-09-02): the disclosure
// goes where the metrics register's ledger foot goes, in the register's
// own quiet idiom, which spells the qualifier out. That supersedes
// #654's terser "50 of 200+" -- the same fact, said in the register's
// words rather than in a glyph.
//
// The floor case exists because pairsTotal is itself bounded by
// internal/engine's maxEvidencePairsTracked (a resource-safety ceiling,
// independent of the smaller display cap Pairs() applies -- see that
// constant's own doc comment for why an exact count was traded away
// past a point). Once pairsTotalIsFloor is true, pairsTotal is a lower
// bound, not the real count, and a flat "12 of 340" would look exactly
// as precise as a genuine one while lying about it -- so the floor case
// must read visibly differently, not just share the same template with
// a bigger number.
export function pairsTruncationLabel(pairsShown: number, pairsTotal: number, pairsTotalIsFloor: boolean | undefined): string {
  return `${pairsShown} of ${pairsTotalIsFloor ? 'at least ' : ''}${pairsTotal} pairs`
}
