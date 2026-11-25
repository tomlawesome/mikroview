// SPDX-License-Identifier: AGPL-3.0-only
//
// Issue #1344: the setup wizard's rendering of internal/routeros.Upgrades
// -- one block per catalogue entry, naming every connected router (and
// the operator's own pick) whose version is at or past that entry's
// From, and nothing when none carries the ID.

import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import UpgradeWarnings from './UpgradeWarnings.svelte'
import type { RouterosUpgrade, RouterosWarningRouter, SetupCommandsResponse } from '../lib/types'

const UPGRADE: RouterosUpgrade = {
  id: 'cert-store-7.24.3',
  from: '7.24.3',
  steps: ['syslog', 'push', 'backup', 'droplist'],
  heading: 'RouterOS 7.24.3 stopped trusting one public root certificate, GoDaddy Class 2.',
  body: 'Check the chain your certificate uses.',
}

function router(over: Partial<RouterosWarningRouter> = {}): RouterosWarningRouter {
  return { id: 'core', name: 'core', routerosVersion: '7.24.4', standing: 'reviewed', upgrades: [UPGRADE.id], ...over }
}

function commands(over: Partial<SetupCommandsResponse> = {}): SetupCommandsResponse {
  return {
    routeros: { minimum: '7.18', newest: '7.24.4', rows: [], upgrades: [UPGRADE] },
    picked: null,
    routers: [],
    steps: {
      caTrust: { commands: '', note: '' },
      syslog: { commands: '', note: '' },
      ruleTagging: { commands: '', note: '' },
      push: { commands: '', note: '' },
      schedule: { commands: '', note: '' },
      backup: { commands: '', note: '' },
      backupSchedule: { commands: '', note: '' },
    },
    ...over,
  }
}

function text(el: Element | null) {
  return el?.textContent?.replace(/\s+/g, ' ').trim()
}

describe('UpgradeWarnings', () => {
  it('names every router at or past From, in order, and excludes one below it', () => {
    const { container } = render(UpgradeWarnings, {
      props: {
        commands: commands({
          routers: [
            router({ id: 'core', name: 'core', routerosVersion: '7.24.4' }),
            router({ id: 'edge-1', name: 'edge-1', routerosVersion: '7.25.1' }),
            router({ id: 'edge-2', name: 'edge-2', routerosVersion: '7.24.2', upgrades: [] }),
          ],
        }),
      },
    })
    const notes = container.querySelectorAll('.note.upgrade[data-upgrade="cert-store-7.24.3"]')
    expect(notes.length).toBe(1)
    expect(text(notes[0].querySelector('strong'))).toBe('core (7.24.4) and edge-1 (7.25.1) run RouterOS 7.24.3 or later.')
    expect(text(notes[0])).not.toContain('edge-2')
  })

  it('uses "runs" for exactly one router', () => {
    const { container } = render(UpgradeWarnings, { props: { commands: commands({ routers: [router()] }) } })
    expect(text(container.querySelector('.note.upgrade strong'))).toBe('core (7.24.4) runs RouterOS 7.24.3 or later.')
  })

  it('names the picked version last when picked and no routers', () => {
    const { container } = render(UpgradeWarnings, {
      props: {
        commands: commands({
          routers: [],
          picked: { version: '7.24.3', standing: 'reviewed', dialect: 'a', upgrades: [UPGRADE.id] },
        }),
      },
    })
    expect(text(container.querySelector('.note.upgrade strong'))).toBe('Your picked version (7.24.3) runs RouterOS 7.24.3 or later.')
  })

  it('renders no block when the catalogue is present but no router or pick carries the ID', () => {
    const { container } = render(UpgradeWarnings, {
      props: {
        commands: commands({
          routers: [router({ upgrades: [] })],
          picked: { version: '7.24.2', standing: 'reviewed', dialect: 'a', upgrades: [] },
        }),
      },
    })
    expect(container.querySelector('.note.upgrade')).toBeNull()
  })

  it('reads the Affects sentence with every step label named', () => {
    const { container } = render(UpgradeWarnings, { props: { commands: commands({ routers: [router()] }) } })
    expect(text(container.querySelector('.note.upgrade'))).toContain(
      'Affects Send logs, Push router state, Back up the router and the drop list scheduler in Settings ▸ Engine room.',
    )
  })

  it('renders an unknown step key as itself', () => {
    const { container } = render(UpgradeWarnings, {
      props: {
        commands: commands({
          routeros: { minimum: '7.18', newest: '7.24.4', rows: [], upgrades: [{ ...UPGRADE, steps: ['made-up-key'] }] },
          routers: [router()],
        }),
      },
    })
    expect(text(container.querySelector('.note.upgrade'))).toContain('Affects made-up-key.')
  })
})
