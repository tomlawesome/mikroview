// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchSetupCommands: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
}))

import { wizardState } from './wizard.svelte'
import type { SetupStatus } from './types'

function status(over: Partial<SetupStatus> = {}): SetupStatus {
  return {
    instance: { tlsEnabled: true, hosts: ['localhost'], syslogPort: ':6514', syslogEnabled: true },
    sources: [],
    devices: [],
    pushKinds: [],
    marks: [],
    ...over,
  }
}

// wizardState is a module singleton and "auto-launch, once" is a
// module-lifetime fact, so these run in order against one instance --
// which is the honest way to test a once-only rule, and why they live in
// a file of their own rather than beside the ledger's arithmetic.
describe('auto-launch, once', () => {
  it('waits for an answer rather than treating "no ledger yet" as "nothing has arrived"', () => {
    wizardState.status = null
    wizardState.maybeAutoLaunch(false)
    expect(wizardState.open).toBe(false)
  })

  it('opens on first run: an admin, a painted shell, and no router sending', () => {
    wizardState.status = status()
    wizardState.maybeAutoLaunch(false)
    expect(wizardState.open).toBe(true)
    expect(wizardState.pane).toBe(1)
  })

  // An explicit close that undoes itself is worse than no close at all.
  // The once-ness is spent on the first answerable call, so closing
  // cannot re-arm it.
  it('does not reopen itself when the operator closes it', () => {
    wizardState.close()
    wizardState.maybeAutoLaunch(false)
    expect(wizardState.open).toBe(false)
  })
})

describe('relaunch is the same door', () => {
  it('reopens at the first step still waiting', () => {
    wizardState.status = status({
      sources: [{ source: '192.0.2.1', caFetchedAt: '2026-08-23T09:00:00Z' }],
    })
    wizardState.launch()
    expect(wizardState.open).toBe(true)
    // Step 1 has its evidence, so the first thing still waiting is 2.
    expect(wizardState.pane).toBe(2)
  })

  it('carries the ledger to the surfaces that have a silence to explain', () => {
    wizardState.status = status()
    expect(wizardState.silence).toBeNull()

    wizardState.status = status({
      marks: [{ step: 2, outcome: 'forced', actor: 'tom', at: '2026-08-23T09:00:00Z', note: 'nothing arrived' }],
    })
    expect(wizardState.silence).toContain('step 2')
    expect(wizardState.silence).toContain('forced past')
  })
})

describe('openLostRouter (#394)', () => {
  it('jumps to step 6 for the named router, not the first step still waiting', () => {
    wizardState.close()
    wizardState.openLostRouter('hap-ax2')
    expect(wizardState.open).toBe(true)
    expect(wizardState.pane).toBe(6)
    expect(wizardState.lostRouterDevice).toBe('hap-ax2')
  })

  it('is cleared by an ordinary close or relaunch, so it never leaks into the next visit', () => {
    wizardState.openLostRouter('hap-ax2')
    wizardState.close()
    expect(wizardState.lostRouterDevice).toBeNull()

    wizardState.status = status()
    wizardState.openLostRouter('hap-ax2')
    wizardState.launch()
    expect(wizardState.lostRouterDevice).toBeNull()
  })
})
