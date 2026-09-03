// SPDX-License-Identifier: AGPL-3.0-only
//
// The city's ground model (#863): what layout.ts computes and
// City.svelte draws. Everything is in ground coordinates (u, v); nothing
// here knows about pixels or a camera. This is the one ground plan both
// views share (docs/design/screens/city/DESIGN.md): the zones stop
// draws it flat (#869), the city stops draw it in isometric.
import type { Pt } from './project'

/** What a building is, for the device library's stamp (#864). Until
 * that lands every kind draws as a plain block. */
export type BuildingKind = 'router' | 'router-ant' | 'host' | 'post'

export interface Building {
  id: string
  name: string
  /** Address, or what stands in for one on a post ('wan bridge'). */
  ip: string
  kind: BuildingKind
  /** Ground centre. */
  u: number
  v: number
  /** Footprint radius, in the diamond metric. */
  R: number
  /** Plinth height. #867 makes this the importance reading; here it is
   * a rank within the district so the skyline is not flat. */
  h: number
  /** The district this stands in; null for a router or a bridge head. */
  districtId: string | null
  /** The router whose territory this is. */
  routerId: string
  /** Sequence within the district, for the keyboard walk. */
  index: number
}

export interface District {
  id: string
  name: string
  cidr: string | null
  u: number
  v: number
  /** Plate radius, diamond metric. */
  r: number
  /** Which lane ink (Topography's LANE_INKS index) tints it. */
  ink: number
  routerId: string
  /** Nothing logs on this boundary: plate and buildings dim. */
  dark: boolean
  buildings: Building[]
  /** Hosts beyond the buildings drawn (the plate is bounded). */
  more: number
}

export interface Borough {
  routerId: string
  name: string
  districtIds: string[]
  /** Ground bounds of everything the router owns, for framing. */
  bounds: { u0: number; u1: number; v0: number; v1: number }
}

/** Road verdict, the mockup's own letters: accept, drop (dies at the
 * wall), unplanned (the alarm), quiet (unjudged ink). */
export type RoadKind = 'a' | 'd' | 'x' | 'q'

export interface Road {
  id: string
  /** Waypoints in ground space; the curve is Catmull-Rom through them. */
  pts: Pt[]
  /** Width in ground units (the mockup's w, scaled by S when drawn). */
  w: number
  k: RoadKind
  /** Entity ids the road is trimmed against at each end. */
  from: string | null
  to: string | null
  /** A dropped road ends at the wall with bollards and a mark. */
  stop?: 'drop'
  /** Fades along its length (the highway leaving town). */
  fade?: boolean
  /** A building's own street to its district's edge. */
  lane?: boolean
  /** Plain words for the road's accessible name. */
  label: string
}

/** A tunnel's peer, drawn as the far-bank hamlet (#866): a WireGuard
 * peer (by allowedAddress/comment) or a ppp-active session (by
 * name/address). */
export interface CityPeer {
  id: string
  name: string
  address: string
  kind: 'wg' | 'ppp'
}

export interface Bridge {
  id: string
  iface: string
  /** Wide road bridge (the WAN) or narrow footbridge (a tunnel). */
  kind: 'road' | 'foot'
  /** Town-bank head, far-bank head, deck centre, half-length. */
  t: Pt
  f: Pt
  mid: Pt
  half: number
  w: number
  /** The gate post standing at the bridge head, by building id. */
  post: string
  /**
   * The road bridge (the WAN) never reads down/quiet: 'up' means a
   * logging rule covers the boundary (lamped), 'unknown' means it does
   * not (unlit) -- see cityInputFrom's wanLogged. A footbridge's state
   * is tunnelState.ts's bridgeStateFor: 'up'/'down' from the API,
   * 'quiet' when up but nothing crossed in the window, 'unknown' when
   * the API has no state for it at all (never a guessed down).
   */
  state: 'up' | 'quiet' | 'down' | 'unknown'
  /** The far-bank hamlet: empty for the road bridge. */
  peers: CityPeer[]
}

export interface River {
  /** The town bank and the far bank, as Catmull-Rom waypoints. */
  bankN: Pt[]
  bankF: Pt[]
  width: number
}

export interface Ground {
  districts: District[]
  /** Routers and bridge-head posts: buildings that stand in no district. */
  nodes: Building[]
  boroughs: Borough[]
  roads: Road[]
  river: River | null
  bridges: Bridge[]
  /** Everything, for the minimap fit and the pan clamp. */
  bounds: { u0: number; u1: number; v0: number; v1: number }
}
