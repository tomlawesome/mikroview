// SPDX-License-Identifier: AGPL-3.0-only
//
// The five groups and their pages, in the ratified order
// (docs/design/screens/navigation/DESIGN.md's table). Pulled out of
// NavRail.svelte (#544/#545/#546/#548) so #550's small-screen bottom bar
// and half-sheet can share the exact same geography, reserved-slot rule,
// badge marker and #490 admin-gating rather than keeping a second copy
// that could drift from the rail's.
import type { View } from './state.svelte'
import type { IconName } from '../components/RailIcon.svelte'

export type NavItem = {
  label: string
  // Every row is a view the app already renders -- "Run setup…" reads as
  // an action in the record, but its interim mechanics (#548) are the
  // same view switch as any page.
  view: View
  admin?: boolean
  title: string
  icon: IconName
  // The record's "one count on the rail": Flags carries it and nothing
  // else does. A marker rather than a number so the count stays derived
  // from the store at render time instead of being copied into this
  // table, which is built once at module load.
  badge?: boolean
  // The broken ring (#546): a marker for the same reason badge is one.
  // Watchlist is the only row that carries it today -- coverage is an
  // expectation-only question -- but nothing here assumes that, so a
  // second surface can pick it up later without touching this table.
  ring?: boolean
}

export type NavGroup = { name: string; items: NavItem[] }

// Fixed order, per the record. The reserved-slot rule lives here rather
// than in the DOM: Map (v0.5.0) and Lookback (unbuilt) are deliberately
// absent, not stubbed or disabled.
//
// Interim, per #544's body: the Live group carries Stream alone until the
// fall ships, and Stream is the landing.
export const navGroups: NavGroup[] = [
  {
    name: 'Live',
    items: [{ label: 'Stream', view: 'live', icon: 'stream', title: 'The live event stream' }],
  },
  {
    name: 'Investigate',
    items: [
      { label: 'Metrics', view: 'metrics', icon: 'metrics', title: 'Event charts and traffic breakdowns' },
      { label: 'Audit log', view: 'audit', admin: true, icon: 'audit', title: 'Who changed what, and when' },
    ],
  },
  {
    name: 'Detect',
    items: [
      {
        label: 'Flags',
        view: 'flags',
        icon: 'flags',
        badge: true,
        title: 'Behavioral flags: port scans, activity spikes, critical-port attempts, and volume spikes',
      },
      // Detectors moved wholesale into the engine room's watchers
      // station (#490) -- see EngineRoomWatchers.svelte. No rail row of
      // its own any more.
    ],
  },
  {
    name: 'Expect',
    items: [
      {
        label: 'Watchlist',
        view: 'watchlist',
        admin: true,
        icon: 'watchlist',
        ring: true,
        title: 'Hosts and ports you expect to see',
      },
    ],
  },
  {
    name: 'Admin',
    // Fleet and Entities are pages (#548). "Run setup…" stays an action,
    // not a page: interim, per #548's body, it opens the existing wizard
    // page (view: 'setup') until #487's modal replaces the target.
    //
    // Users and Tokens retired wholesale into the engine room (#490) --
    // see EngineRoomDoors.svelte -- along with Detectors (see the Detect
    // group's own comment above). The engine room itself carries no
    // `admin: true`: it is deliberately viewer-readable (the design
    // record's authz-matrix clause widens the tokens/definitions/setup-
    // status GETs it reads to any signed-in user), with per-control
    // verbs gated inside the page instead. GET /api/auth/users is the
    // one exception -- the owner overrode the record's original "widen
    // users too" clause mid-build, so the engine room's own "who may
    // look in" door stays admin-only *within* an otherwise
    // viewer-readable page (see EngineRoomDoors.svelte).
    //
    // Entities keeps `admin: true` -- the backend still 403s its GET
    // route for a non-admin, so rendering it for a viewer would be a
    // page that loads and immediately fails, not a read-only one.
    items: [
      {
        label: 'The engine room',
        view: 'engineroom',
        icon: 'engineroom',
        title: "Mikroview's own signal path, live, with every setting on the station it governs",
      },
      {
        label: 'Fleet',
        view: 'fleet',
        icon: 'fleet',
        title: 'Every known RouterOS device: live/stale/never-seen status, last-seen, and event counts',
      },
      { label: 'Entities', view: 'entities', admin: true, icon: 'entities', title: 'Named hosts, ports and services' },
      { label: 'Run setup…', view: 'setup', admin: true, icon: 'setup', title: 'Re-run the setup wizard' },
    ],
  },
]

// #490's grammar: admin-only rows are absent for viewers, never disabled.
// A group whose every item is admin-only disappears with them rather
// than rendering an empty heading. Shared by both nav surfaces so a
// viewer sees the same geography on a phone as on a desk.
export function visibleGroups(isAdmin: boolean): NavGroup[] {
  return navGroups
    .map((g) => ({ ...g, items: g.items.filter((i) => !i.admin || isAdmin) }))
    .filter((g) => g.items.length > 0)
}
