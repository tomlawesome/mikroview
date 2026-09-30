// SPDX-License-Identifier: AGPL-3.0-only
//
// The "seen serving" lens's own arithmetic and wording (#1320), kept out
// of Topography.svelte for the same reason portFilter.ts is: what the
// pill reads and what a host's badge says can be read and tested
// without a DOM.
//
// The one rule everything here serves: this is traffic, never a scan.
// A host lights up because the window watched it answer, and nothing
// here ever claims a port is "open" -- a quiet listener is invisible,
// and a host busy last week but closed now still shows.
import type { ServedPort } from './api'

/**
 * servingTally is the count both surfaces read from: the lane card's
 * own `7 of 12 hosts answer`, and the pill's `7 of 41 hosts answer`
 * before it gets its own `in the window` tail appended.
 *
 * Counted over the hosts the surface actually knows about, the same
 * #1056 rule zoneTally follows: an address seen answering that no lane
 * draws is not counted anywhere. Null where the surface knows of no
 * host at all -- "0 of 0" is not a finding.
 */
export function servingTally(on: number, total: number): string | null {
  if (total === 0) return null
  return `${on} of ${total} host${total === 1 ? '' : 's'} answer`
}

/**
 * hostBadge is the served-port half of a lit dot's own title:
 * `445, 139, 22/tcp · +3 more` for `192.168.1.23 · seen serving
 * 445, 139, 22/tcp · +3 more`. Busiest first, as the answer already
 * sorts them.
 *
 * A single trailing `/proto` where every port shares one, because that
 * is the ordinary shape (a host answering several ports the same way)
 * and repeating the protocol on every entry would say nothing extra.
 * Ports on different protocols each keep their own.
 */
export function hostBadge(ports: ServedPort[], more: number): string {
  if (ports.length === 0) return more > 0 ? `+${more} more` : ''
  const protos = new Set(ports.map((p) => p.proto))
  const list =
    protos.size <= 1
      ? ports.map((p) => String(p.port)).join(', ') + (ports[0].proto ? `/${ports[0].proto}` : '')
      : ports.map((p) => `${p.port}/${p.proto}`).join(', ')
  return more > 0 ? `${list} · +${more} more` : list
}
