// SPDX-License-Identifier: AGPL-3.0-only
//
// The city's ground model (#863): what layout.ts computes and
// City.svelte draws. Everything is in ground coordinates (u, v); nothing
// here knows about pixels or a camera. This is the one ground plan both
// views share (docs/design/screens/city/DESIGN.md): the zones stop
// draws it flat (#869), the city stops draw it in isometric.
import type { Coverage } from '../coverageRule'
import type { GhostState } from '../types'
import type { CityHost } from './presence'
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
  /** A rank within the district, busiest first. #986 dropped the plinth
   * that once stood this tall (#867's importance reading) -- City.svelte
   * draws every building flat now -- but layout.ts still computes it and
   * layout.test.ts/depth.test.ts still check it. */
  h: number
  /** The district this stands in; null for a router or a bridge head. */
  districtId: string | null
  /** The router whose territory this is. */
  routerId: string
  /** Sequence within the district, for the keyboard walk. */
  index: number
  /** What the host register and the event buffer between them know
   * about this building (round 49, #1016): its presence, when it was
   * last and first heard, and any mark on it. Absent on a router, a
   * bridge post or anything else that is not a host -- presence is a
   * statement about a machine the syslog feed hears, and the router is
   * the thing doing the hearing. */
  host?: CityHost
}

/** One direction across a boundary, for the gate's card: a wall has no
 * direction, so its card lists both (round 49, #1016). */
export interface GateDirection {
  /** `from → to`, in the interfaces' own names. */
  label: string
  /** The declaration key for this direction, `from|to` -- what the
   * declare API (#392) is called with. */
  edgeKey: string
  coverage: Coverage
  /** Accept rules standing on this direction; 0 when none does. */
  ruleCount: number
}

/** A break in a district's wall (#865): an accept rule crossing this
 * boundary, aimed at wherever its other side resolves to. One gate per
 * neighbour, both its directions on it (round 49). */
export interface DistrictGate {
  /** The boundary key -- fall.svelte.ts's boundaryKeyOf shape. */
  key: string
  /** The point on the plate's edge, and its outward normal --
   * roads.ts's gateToward, reused rather than a second geometry. */
  p: Pt
  n1: Pt
  /** The far side's name, for the accessible label -- the raw
   * interface or district name this gate's rule actually names, never
   * invented when nothing resolves it to a place. */
  toward: string
  /** An accept rule on this exact boundary logs: the gate's lamp. */
  lamp: boolean
  ruleCount: number
  /** The RouterOS number of the lowest-numbered accept rule that opened
   * this gate, and that rule's own comment. The card reads them as
   * `rule 4 · nas access` (owner, 2026-09-08 on #1016) -- numbered as
   * RouterOS numbers them, so "go look at rule 4" means what it says.
   * `ruleName` is '' when the rule carries no comment: a gate with no
   * name is shown with none, never one invented for it. `ruleOrdinal`
   * is -1 when no rule is known, which is the state a ground model
   * built without a rule table is in -- the card then says nothing
   * rather than printing `rule 0`. Never drawn on the gate itself:
   * nothing is written on the drawing. */
  ruleOrdinal: number
  ruleName: string
  /** The worse of this gate's two directions (dark worse than quiet
   * worse than logged): accent posts and one lamp when logged, grey
   * posts and no lamp otherwise, and the wall edge it stands in takes
   * the same reading. */
  coverage: Coverage
  /** Both directions, for the card. */
  directions: GateDirection[]
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
  /** The lane's three-way coverage reading, carried through from
   * CityZone (see its own note): logged, declared quiet, or dark. */
  coverage: Coverage
  /** Nothing logs on this boundary and nobody declared it quiet: plate
   * and buildings dim. `coverage === 'dark'`. */
  dark: boolean
  /** Every one of this district's boundaries is dark -- the only case
   * the plate itself goes grey and dashed (round 49, #1016). A district
   * with one dark boundary and one declared quiet is not this: its dark
   * wall edge says where the hole is, and the plate stays in its own
   * ink. Narrower than `dark`, which is the lane's own reading. */
  plateDark: boolean
  buildings: Building[]
  /** Hosts beyond the buildings drawn (the plate is bounded). */
  more: number
  /** Every gate this district's wall actually opens, from the pushed
   * rule tables -- [] when nothing has been pushed at all (see
   * rulesPushed) or when a table is pushed but names no accept rule on
   * any of this district's boundaries. */
  gates: DistrictGate[]
  /** Set only on a segment the router has stopped carrying (#460, round
   * 55), carried through from CityZone: the plate draws with no fill,
   * a dashed wall all round and its last-known hosts faded inside, all
   * in the watch's own ink, and no road runs from the router because
   * the router no longer has one. */
  ghost?: GhostState
  /** No router has ever pushed a rule table (distinct from `dark`,
   * which means a table WAS pushed and nothing on it logs): the wall
   * draws with no gates, and the plaque says why rather than leaving
   * silence that could be misread as "no rules exist" (#865's honesty
   * rule). */
  rulesPushed: boolean
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

/** One rule's share of everything a drop mark aggregates (#1002): how
 * many refused events on this pair carried that rule's label. `rule` is
 * null for the bucket of drops that carried no label at all -- said
 * plainly, never folded into a named rule's count. */
export interface CityRuleDrop {
  rule: string | null
  count: number
}

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
  /** The rule that refused this road, from the events' own rule label
   * -- only meaningful when stop is 'drop'. Absent means no refused
   * event on this pair carried a rule label: said plainly beside the
   * mark, never guessed (#865). */
  refusedBy?: string
  /** Every rule that refused a crossing on this pair, and how many it
   * caught, busiest first (#1002: the owner's ruling that the aggregate
   * drop mark aggregates every dropped item, broken down per rule with a
   * count rather than a flat list of events). Only meaningful when stop
   * is 'drop'; [] when nothing refused on this pair carried any events
   * at all. This is the data the mark's own click would open -- where
   * and how it opens is not settled here (#1002's own note), so nothing
   * in City.svelte reads this field yet. */
  dropBreakdown?: CityRuleDrop[]
  /** Fades along its length (the highway leaving town). */
  fade?: boolean
  /** A building's own street to its district's edge. */
  lane?: boolean
  /** Plain words for the road's accessible name. */
  label: string
}

/* Round 49 (#1016) removed the lens tabs and with them `CityLens`:
   coverage is always on and is the material, traffic is the picture,
   and the policy lens went in slice C. There is nothing left to switch. */

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
  /** What the deck is drawn in (round 49, #1016): accent with lamps when
   * a rule logs this boundary, white translucent and unlamped when the
   * operator declared it quiet on purpose, grey with dashed rails when
   * nothing logs. A different fact from `state`, which is whether the
   * tunnel is up -- the chip still carries that. */
  coverage: Coverage
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
