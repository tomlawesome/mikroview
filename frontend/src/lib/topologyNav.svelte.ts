// SPDX-License-Identifier: AGPL-3.0-only
//
// Cross-page "descend into this host" handoff (#678): the docket's
// ratified record says every named where is a link into the topography
// at its sensible level -- host-level when the target resolves to a
// known device, zone-level (the map itself) otherwise, never the
// stream. Topography.svelte's own reach (host-level zoom) is local
// component state with no route param to carry a target across the view
// switch (appState.view is a bare string), so this is the one shared
// slot a caller fills in before flipping to 'topography'; the scene
// reads and clears it on arrival.
export interface PendingDescend {
  zoneId: string
  host: string
  ip: string
}

class TopologyNavState {
  pendingDescend = $state<PendingDescend | null>(null)

  requestHost(zoneId: string, host: string, ip: string) {
    this.pendingDescend = { zoneId, host, ip }
  }
}

export const topologyNavState = new TopologyNavState()
