// SPDX-License-Identifier: AGPL-3.0-only
//
// The account menu's rows, per tier (#657).
//
// This file exists because of the defect it now pins. The menu carried
// its own hardcoded copy of the operate rows, with a binary `admin` flag
// that predated #653's three tiers, and nothing tested it. So when #657
// gated Settings away from a viewer, the rail and the small-screen bar
// both obeyed and this menu did not: a viewer was still offered Settings
// -- the headline example in #657's own ruling -- and a user was still
// denied Entities. Reading navGroups.ts in a test could never have caught
// it, because the menu was not reading navGroups.ts.
//
// The live check found it. These tests are the cheap guard underneath.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

// jsdom has no matchMedia, which lib/viewport.svelte.ts reads at module
// load. Same vi.hoisted shim SetupWizard.svelte.test.ts installs, and
// for the same reason: it has to be in place before the component's
// import chain runs.
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
})

import AccountMenu from './AccountMenu.svelte'
import { authState } from '../lib/auth.svelte'

async function openMenuAs(role: 'admin' | 'user' | 'viewer') {
  authState.state = 'authenticated'
  authState.role = role
  authState.username = `test-${role}`
  render(AccountMenu)
  await fireEvent.click(screen.getByRole('button', { name: /test-/ }))
}

function rowLabels(): string[] {
  return screen
    .getAllByRole('menuitem')
    .map((el) => el.textContent?.trim() ?? '')
    .filter(Boolean)
}

describe('AccountMenu operate rows follow navGroups (#657)', () => {
  beforeEach(() => {
    authState.hasLocalPassword = false
    authState.ssoAvailable = false
  })

  it('offers a viewer Fleet and nothing that exists to change things', async () => {
    await openMenuAs('viewer')
    const rows = rowLabels()

    expect(rows).toContain('Fleet')
    // The four a viewer must never be offered here. Settings is the one
    // that regressed: the menu kept showing it after #657 hid it
    // everywhere else.
    expect(rows).not.toContain('Settings')
    expect(rows).not.toContain('Entities')
    expect(rows).not.toContain('Audit log')
    expect(rows).not.toContain('Run setup…')
  })

  it('offers a user Settings and Entities, but not the admin-only rows', async () => {
    await openMenuAs('user')
    const rows = rowLabels()

    expect(rows).toContain('Settings')
    expect(rows).toContain('Fleet')
    // Entities is the other half of the drift: the rail gave it to a
    // user under #653 and this menu still required admin.
    expect(rows).toContain('Entities')
    expect(rows).not.toContain('Audit log')
    expect(rows).not.toContain('Run setup…')
  })

  it('offers an admin every operate row', async () => {
    await openMenuAs('admin')
    const rows = rowLabels()

    for (const label of ['Settings', 'Fleet', 'Entities', 'Audit log', 'Run setup…']) {
      expect(rows).toContain(label)
    }
  })

  it('keeps the operate rows in their established order for every tier', async () => {
    // The menu's selection is its own, but its order is fixed: a row
    // must not move about depending on who is signed in.
    await openMenuAs('admin')
    const rows = rowLabels()
    const operate = rows.filter((r) => ['Settings', 'Fleet', 'Entities', 'Audit log', 'Run setup…'].includes(r))
    expect(operate).toEqual(['Settings', 'Fleet', 'Entities', 'Audit log', 'Run setup…'])
  })
})
