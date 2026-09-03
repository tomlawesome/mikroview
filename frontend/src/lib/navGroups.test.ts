// SPDX-License-Identifier: AGPL-3.0-only
//
// The ratified surface matrix (#657) as a test. navGroups.ts is the one
// table both nav surfaces read, so what a tier can reach is decided here
// and nowhere else -- but nothing asserted it until this file, and the
// gating flags had drifted once already (#653 added `edit` beside
// `admin` without any test distinguishing the two).
//
// These assert the whole visible set per tier, not the presence of one
// row. A test that only checks "Settings is hidden from a viewer" still
// passes if a later change opens Entities to them by accident, which is
// the failure this table is most likely to have.
import { describe, it, expect } from 'vitest'
import { visibleGroups } from './navGroups'

function labels(isAdmin: boolean, canEdit: boolean = isAdmin): string[] {
  return visibleGroups(isAdmin, canEdit).flatMap((g) => g.items.map((i) => i.label))
}

function groupNames(isAdmin: boolean, canEdit: boolean = isAdmin): string[] {
  return visibleGroups(isAdmin, canEdit).map((g) => g.name)
}

describe('navGroups tier gating (#657)', () => {
  it('a viewer sees the log and the devices that feed it, and nothing that exists to change things', () => {
    // The owner's test is not "may they read this" but "does it help
    // them interrogate the log". Fleet stays because a stale router is
    // why the log looks wrong; Entities goes because the names already
    // ride on the event rows; Watchlist goes because its broken ring is
    // a prompt to author something a viewer cannot author.
    expect(labels(false, false)).toEqual(['The fall', 'Topography', 'Stream', 'Metrics', 'Flags', 'Fleet'])
  })

  it('a user adds the pages they can actually act on', () => {
    // Settings, Watchlist, Entities and Tune logging are the edit tier.
    // The doors inside Settings are gated separately, within the page.
    expect(labels(false, true)).toEqual([
      'The fall',
      'Topography',
      'Stream',
      'Tune logging',
      'Metrics',
      'Flags',
      'Watchlist',
      'Settings',
      'Fleet',
      'Entities',
    ])
  })

  it('an admin adds the audit log and re-running setup, and nothing else', () => {
    expect(labels(true)).toEqual([
      'The fall',
      'Topography',
      'Stream',
      'Tune logging',
      'Metrics',
      'Audit log',
      'Flags',
      'Watchlist',
      'Settings',
      'Fleet',
      'Entities',
      'Run setup…',
    ])
  })

  it('drops a group whose every row is gated away rather than rendering an empty heading', () => {
    // #490's grammar. Investigate survives for a viewer only because
    // Metrics is open -- Audit log alone would take the heading with it.
    expect(groupNames(false, false)).toEqual(['Live', 'Investigate', 'Detect', 'Admin'])
  })

  it('never shows a viewer a row it would only 403 on', () => {
    // The fault #653 described: a row a tier can reach whose read it
    // cannot make, so the page loads and immediately fails. Every row
    // visible to a viewer must be reachable without the edit tier.
    for (const group of visibleGroups(false, false)) {
      for (const item of group.items) {
        expect(item.admin ?? false).toBe(false)
        expect(item.edit ?? false).toBe(false)
      }
    }
  })
})
