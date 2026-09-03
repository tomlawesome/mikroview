// SPDX-License-Identifier: AGPL-3.0-only
//
// A footbridge's state, and only its state (#866): never guessed. The
// ingest side (#874) reports each tunnel interface's own state as
// 'up' | 'down' | 'unknown' -- 'unknown' meaning the wireguard-peer (or
// ppp-active) kind has never been pushed for that device at all, so
// up/down cannot be computed. A tunnel this build only knows about from
// its own events -- never named in either pushed table -- carries no
// API state whatsoever, passed here as `null`; it reads exactly like
// 'unknown' (no state, "state not pushed"), because from the operator's
// chair the two are the same fact: nothing pushed says whether this
// tunnel is up.
//
// 'quiet' is not part of that API vocabulary at all -- it is mikroview's
// own reading, layered on top: a tunnel the API calls up, but that carries
// no events in the window, is lit but empty rather than claiming traffic
// it never saw.
export type ApiTunnelState = 'up' | 'down' | 'unknown'

export type BridgeState = 'up' | 'down' | 'quiet' | 'unknown'

/**
 * bridgeStateFor is the one place a footbridge's paint decision is made.
 * Pure: no clock, no store, just the two facts that decide it.
 */
export function bridgeStateFor(apiState: ApiTunnelState | null, eventsInWindow: number): BridgeState {
  if (apiState === null || apiState === 'unknown') return 'unknown'
  if (apiState === 'down') return 'down'
  return eventsInWindow > 0 ? 'up' : 'quiet'
}

/** The label a footbridge's chip carries, per the ratified wording. */
export function bridgeStateLabel(state: BridgeState): string {
  switch (state) {
    case 'up':
      return 'UP'
    case 'down':
      return 'DOWN'
    case 'quiet':
      return 'QUIET'
    case 'unknown':
      return 'state not pushed'
  }
}
