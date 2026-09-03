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

// NavAction is a row that does something rather than going somewhere.
// The record calls "Run setup…" an action, not a page, and since #487 it
// is one: it opens the setup modal over whatever the operator is already
// looking at.
export type NavAction = 'run-setup'

export type NavItem = {
  label: string
  // Most rows are a view the app renders. A row carrying `action`
  // instead has no view of its own -- it is the action, and both nav
  // surfaces run it rather than switching page.
  view?: View
  action?: NavAction
  admin?: boolean
  // #653: a row the user tier may reach, but a viewer may not. Distinct
  // from `admin`, which is the owner-level set (accounts, tokens, the
  // audit trail, re-running setup). A row carrying neither is open to
  // every signed-in session.
  edit?: boolean
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
// #616 retires #544's interim (Stream as landing) wholesale: the fall is
// now the first Live row and the landing page (see state.svelte.ts's
// AppState.view default); Stream keeps its own row, second.
export const navGroups: NavGroup[] = [
  {
    name: 'Live',
    items: [
      { label: 'The fall', view: 'fall', icon: 'fall', title: 'The live receiver: a band per boundary, live spectrum, and time pouring down' },
      // The Map slot un-reserves (#627): topography exists now.
      { label: 'Topography', view: 'topography', icon: 'map', title: 'The map of the place: internet above, router at the waist, your lanes below' },
      { label: 'Stream', view: 'live', icon: 'stream', title: 'The live event stream' },
      // Tune logging (#435): upload your router's export, get it back
      // with logging switched on for every rule that crosses a dark
      // connection. Gated `edit` (the user tier and above), matching the
      // two endpoints it calls (`callerIsUser`, contract §3-4) -- not
      // admin-only, per the issue's decision 2. Grouped with Topography
      // rather than a group of its own: it exists because of what the
      // coverage lens shows there.
      {
        label: 'Tune logging',
        view: 'tune-logging',
        edit: true,
        icon: 'setup',
        title: "Upload your router's export and get it back with coverage-complete logging attached",
      },
    ],
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
        edit: true,
        icon: 'watchlist',
        ring: true,
        title: 'Hosts and ports you expect to see',
      },
    ],
  },
  {
    name: 'Admin',
    // Fleet and Entities are pages (#548). "Run setup…" is an action,
    // not a page, and now genuinely is one: #548's interim mechanics
    // (a view switch to the old wizard page) retired with #487, which
    // removed that page wholesale and made this row open the setup
    // modal instead.
    //
    // Users and Tokens retired wholesale into the engine room (#490) --
    // its people and keys groups (round 32/#767) -- along with Detectors
    // (see the Detect group's own comment above).
    //
    // #657 gave the engine room `edit: true`, retiring #490's
    // viewer-readable settings page. The test the owner ruled on is not
    // whether a viewer may read a surface but whether it helps them
    // interrogate the log: a page of settings they cannot change is a
    // wall of controls, and the one thing in it a viewer genuinely needs
    // -- why an empty stream is empty -- is rendered by the Stream view
    // itself (LiveTable.svelte's empty state, from the #487 ledger), not
    // here. GET /api/setup/status therefore stays viewer-readable while
    // this page does not.
    //
    // The doors went further and are admin-only now, so a `user` sees the
    // engine room without them. Issuing keys is a setup task rather than
    // using the product (owner, 2026-08-31), and GET /api/tokens narrowed
    // back to match -- see internal/api/tokens.go.
    //
    // Entities carries `edit: true` since #653: the backend widened its
    // GET route from admin to the user tier, so a user gets a page that
    // works, while a viewer would still get one that loads and
    // immediately fails -- which is why the row is not simply opened.
    items: [
      {
        label: 'Settings',
        view: 'engineroom',
        edit: true,
        icon: 'engineroom',
        title: "Mikroview's own signal path, live, with every setting on the station it governs",
      },
      {
        label: 'Fleet',
        view: 'fleet',
        icon: 'fleet',
        title: 'Every known RouterOS device: live/stale/never-seen status, last-seen, and event counts',
      },
      { label: 'Entities', view: 'entities', edit: true, icon: 'entities', title: 'Named hosts, ports and services' },
      { label: 'Run setup…', action: 'run-setup', admin: true, icon: 'setup', title: 'Re-run the setup wizard' },
    ],
  },
]

// #490's grammar: rows a caller cannot use are absent, never disabled.
// A group whose every item is gated away disappears with them rather
// than rendering an empty heading. Shared by both nav surfaces so a
// viewer sees the same geography on a phone as on a desk.
//
// #653 made the gate two-level. `admin` rows need the owner-level tier;
// `edit` rows need the user tier, which admin includes. Callers pass
// both flags rather than a role string so the tier ordering lives in one
// place (authState) instead of being re-derived per nav surface.
export function visibleGroups(isAdmin: boolean, canEdit: boolean = isAdmin): NavGroup[] {
  return navGroups
    .map((g) => ({
      ...g,
      items: g.items.filter((i) => (!i.admin || isAdmin) && (!i.edit || canEdit)),
    }))
    .filter((g) => g.items.length > 0)
}
