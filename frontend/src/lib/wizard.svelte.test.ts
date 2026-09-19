// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  fetchSetupStatus: vi.fn(),
  fetchSetupCommands: vi.fn(),
  mintEnrolment: vi.fn(),
  fetchDevices: vi.fn(),
  markSetupStep: vi.fn(),
  saveSetupAddress: vi.fn(),
  saveSetupBackupTransport: vi.fn(),
  rebindEnrolment: vi.fn(),
  fetchRefusedSenders: vi.fn(),
}))

import {
  fetchDevices,
  fetchRefusedSenders,
  fetchSetupCommands,
  fetchSetupStatus,
  mintEnrolment,
  rebindEnrolment,
  saveSetupAddress,
  saveSetupBackupTransport,
} from './api'
import { ROUTER_STEPS, SETUP_STEPS } from './setupsteps'
import { wizardState } from './wizard.svelte'
import type { SetupStatus } from './types'

function status(over: Partial<SetupStatus> = {}): SetupStatus {
  return {
    instance: {
      tlsEnabled: true,
      hosts: ['localhost'],
      syslogPort: ':6514',
      syslogEnabled: true,
      address: '',
      addressCandidates: [],
      backupTransport: 'sftp',
    },
    sources: [],
    devices: [],
    pushKinds: [],
    marks: [],
    witnesses: [],
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
    // address (#1213) is ordinarily set by refresh() before the modal
    // can ever open; this test drives wizardState.status directly, so
    // it sets the answer itself -- a host the fixture's tls.hosts covers,
    // so step 1's own certificate check does not itself read as blocked.
    wizardState.address = 'localhost'
    wizardState.status = status({
      sources: [{ source: '192.0.2.1', caFetchedAt: '2026-08-23T09:00:00Z' }],
    })
    wizardState.launch()
    expect(wizardState.open).toBe(true)
    // Step 1 has its evidence, so the first thing still unanswered is
    // step 2 -- naming, which asks for something even though it waits
    // for nothing (#1284).
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

// #1218 audit finding 11: saveSetupAddress/saveSetupBackupTransport only
// ever resolve to an error string for a refusal the server actually
// answered -- a dropped connection rejects instead (postJSON/putJSON's
// own fetch), and neither setter caught that. Left uncaught, the
// rejection propagated to a fire-and-forget onclick/onblur caller in
// SetupWizard.svelte with nothing there to catch it either, so
// addressSaveError/backupTransportError were never set: no message
// beside the field, which then just looked saved.
describe('a dropped connection surfaces the same way a refusal does', () => {
  it('saveAddress', async () => {
    wizardState.address = '192.0.2.1'
    wizardState.addressSaveError = null
    vi.mocked(saveSetupAddress).mockRejectedValue(new Error('network unreachable'))

    await wizardState.saveAddress()

    expect(wizardState.addressSaveError).toBe('network unreachable')
  })

  it('setBackupTransport, without silently applying the switch it never confirmed', async () => {
    wizardState.backupTransport = 'sftp'
    wizardState.backupTransportError = null
    vi.mocked(saveSetupBackupTransport).mockRejectedValue(new Error('network unreachable'))

    await wizardState.setBackupTransport('https')

    expect(wizardState.backupTransportError).toBe('network unreachable')
    expect(wizardState.backupTransport).toBe('sftp')
  })
})

// The server writes the enrol line, and only from a token the caller
// echoes back -- it stores nothing but a hash, so it cannot look the
// value up. A minted token that never reaches POST /api/setup/commands
// is therefore a block that configures logging and enrols nothing: the
// operator pastes it, the router logs, and every line is refused.
describe('the minted token reaches the block the operator pastes (#1281)', () => {
  it('re-renders the commands with the token as soon as it is minted', async () => {
    wizardState.status = status()
    wizardState.ledgerDevice = 'edge-1'
    wizardState.enrolment = null
    // #1291: minting now needs the router's own address, which the
    // enrolment window binds to, and the admin's password re-typed at
    // that moment.
    wizardState.enrolExpectedAddress = '192.0.2.50'
    wizardState.enrolPassword = 'password123'
    vi.mocked(mintEnrolment).mockResolvedValue({
      token: 'examplenotarealtoken',
      expiresAt: '2026-09-19T12:15:00Z',
    })
    vi.mocked(fetchSetupCommands).mockResolvedValue('unused')

    await wizardState.mintEnrolmentToken()

    expect(mintEnrolment).toHaveBeenCalledWith('edge-1', 'password123', '192.0.2.50')
    expect(fetchSetupCommands).toHaveBeenCalledWith(
      expect.objectContaining({ device: 'edge-1', enrolToken: 'examplenotarealtoken' }),
    )
  })

  // The password is held only long enough to make the call. Left
  // standing it would weaken "re-prove at the moment of minting" to
  // "once per modal", which is the thing #1291 exists to stop.
  it('clears the password whether the mint succeeded or failed', async () => {
    wizardState.status = status()
    wizardState.ledgerDevice = 'edge-1'
    wizardState.enrolExpectedAddress = '192.0.2.50'

    wizardState.enrolPassword = 'password123'
    vi.mocked(mintEnrolment).mockResolvedValue({
      token: 'examplenotarealtoken',
      expiresAt: '2026-09-19T12:15:00Z',
    })
    await wizardState.mintEnrolmentToken()
    expect(wizardState.enrolPassword).toBe('')

    wizardState.enrolPassword = 'wrong-password'
    vi.mocked(mintEnrolment).mockResolvedValue('password is incorrect')
    await wizardState.mintEnrolmentToken()
    expect(wizardState.enrolPassword).toBe('')
    expect(wizardState.enrolmentError).toBe('password is incorrect')
  })

  // Neither field is something the wizard can supply on the operator's
  // behalf, so it says which one is missing rather than calling the
  // endpoint and reporting whatever it answers.
  it('will not mint without an address or without a password', async () => {
    wizardState.status = status()
    wizardState.ledgerDevice = 'edge-1'
    vi.mocked(mintEnrolment).mockClear()

    wizardState.enrolExpectedAddress = ''
    wizardState.enrolPassword = 'password123'
    await wizardState.mintEnrolmentToken()
    expect(mintEnrolment).not.toHaveBeenCalled()
    expect(wizardState.enrolmentError).toContain('address')

    wizardState.enrolExpectedAddress = '192.0.2.50'
    wizardState.enrolPassword = ''
    await wizardState.mintEnrolmentToken()
    expect(mintEnrolment).not.toHaveBeenCalled()
    expect(wizardState.enrolmentError).toContain('password')
  })

  // A later re-render -- the operator corrects the address, or picks a
  // RouterOS version -- must not drop the line back out again.
  it('keeps sending it on a re-render the mint did not cause', async () => {
    vi.mocked(fetchSetupCommands).mockClear()
    wizardState.address = '192.0.2.10'

    await wizardState.refreshCommands({ device: 'edge-1' })

    expect(fetchSetupCommands).toHaveBeenCalledWith(
      expect.objectContaining({ enrolToken: 'examplenotarealtoken' }),
    )
  })
})

// 2026-09-18 audit, stage 4 finding 14: refresh() already polls the
// device list, which carries whether the server still considers an
// enrolment pending -- but nothing reconciled the cached token against
// it. After a restart the pending token is gone server-side (it was
// never persisted), yet the wizard went on counting its cached
// `enrolment` down to zero, showing a countdown against a token the
// server had already forgotten.
describe('refresh reconciles the cached enrolment against the poll', () => {
  function deviceRow(over: Partial<import('./types').Device> = {}): import('./types').Device {
    return {
      id: 'edge-1',
      name: 'edge-1',
      sourceIp: '',
      configured: true,
      firstSeen: '2026-09-19T09:00:00Z',
      lastSeen: '2026-09-19T09:00:00Z',
      eventCount: 0,
      status: 'live',
      ...over,
    }
  }

  it('drops the cached token once the polled device no longer shows one pending', async () => {
    wizardState.status = status()
    wizardState.ledgerDevice = 'edge-1'
    wizardState.enrolment = { token: 'examplenotarealtoken', expiresAt: '2026-09-19T12:15:00Z' }
    wizardState.enrolmentMintedAt = '2026-09-19T12:00:00Z'

    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([deviceRow()])

    await wizardState.refresh()

    expect(wizardState.enrolment).toBeNull()
    expect(wizardState.enrolmentMintedAt).toBe('')
  })

  it('keeps the cached token while the poll still shows it pending', async () => {
    wizardState.status = status()
    wizardState.ledgerDevice = 'edge-1'
    wizardState.enrolment = { token: 'examplenotarealtoken', expiresAt: '2026-09-19T12:15:00Z' }
    wizardState.enrolmentMintedAt = '2026-09-19T12:00:00Z'

    vi.mocked(fetchSetupStatus).mockResolvedValue(status())
    vi.mocked(fetchDevices).mockResolvedValue([
      deviceRow({ enrolment: { pending: true, expiresAt: '2026-09-19T12:15:00Z' } }),
    ])

    await wizardState.refresh()

    expect(wizardState.enrolment).not.toBeNull()
  })
})

// close() deliberately keeps a walk's state so reopening the same door
// resumes it. Run setup… is a different door: it asks about the
// instance, not about whichever router a Re-enrol… walk was in the
// middle of. Left behind, ledgerDevice made the first-run ledger read
// as that router's -- Name your router already done, and Send logs
// offering its live token to reroll.
describe('Run setup… does not inherit a router walk (#1284)', () => {
  it('clears the router and its token', () => {
    wizardState.status = status()
    wizardState.openReEnrol('edge-1')
    wizardState.enrolment = { token: 'examplenotarealtoken', expiresAt: '2026-09-19T12:15:00Z' }
    wizardState.enrolmentMintedAt = '2026-09-19T12:00:00Z'
    wizardState.token = 'an-ingest-token'
    wizardState.close()

    wizardState.launch()

    expect(wizardState.ledgerDevice).toBe('')
    expect(wizardState.tokenDevice).toBe('')
    expect(wizardState.token).toBe('')
    expect(wizardState.enrolment).toBeNull()
    expect(wizardState.enrolmentMintedAt).toBe('')
    expect(wizardState.steps).toEqual(SETUP_STEPS)
  })

  // Reopening the router door itself must still resume where it was.
  it('but Re-enrol… still reopens at Send logs for its own router', () => {
    wizardState.openReEnrol('edge-1')
    expect(wizardState.ledgerDevice).toBe('edge-1')
    expect(wizardState.steps).toEqual(ROUTER_STEPS)
  })
})

// The mint form binds straight to the singleton, so whatever is typed
// into it belongs to the walk it was typed in and to nothing after.
// Found by the v0.6.0 audit's Safety stage (#1257).
describe('a typed password and address do not outlive their walk (#1291)', () => {
  it('closing the wizard clears the mint form', () => {
    wizardState.openReEnrol('edge-1')
    wizardState.enrolExpectedAddress = '192.0.2.50'
    wizardState.enrolPassword = 'the-admin-password'

    wizardState.close()

    expect(wizardState.enrolPassword).toBe('')
    expect(wizardState.enrolExpectedAddress).toBe('')
  })

  // The one that mints a real token against the wrong address: the
  // operator abandons router A's walk and opens router B's, and the
  // form still holds A's address with nothing marking it stale. The
  // server cannot catch this -- it only checks the address parses.
  it('opening a second router does not inherit the first one\'s address', () => {
    wizardState.openReEnrol('edge-1')
    wizardState.enrolExpectedAddress = '192.0.2.50'
    wizardState.enrolPassword = 'the-admin-password'
    wizardState.close()

    wizardState.openReEnrol('edge-2')

    expect(wizardState.enrolExpectedAddress).toBe('')
    expect(wizardState.enrolPassword).toBe('')
  })

  it('Add a router does not inherit them either', () => {
    wizardState.openReEnrol('edge-1')
    wizardState.enrolExpectedAddress = '192.0.2.50'
    wizardState.enrolPassword = 'the-admin-password'

    wizardState.openAddRouter()

    expect(wizardState.enrolExpectedAddress).toBe('')
    expect(wizardState.enrolPassword).toBe('')
  })
})

// Two refused addresses are offered side by side, so clicking the wrong
// one and then the right one is an ordinary correction. Without a guard
// the window ends up at whichever answer came back last, both calls
// report success, and the bound address is never shown again -- so the
// router keeps being refused with nothing on screen explaining why.
// Found by the v0.6.0 audit's Safety stage (#1257).
describe('rebinding the enrolment window is one at a time (#1291)', () => {
  it('ignores a second click while the first is still in flight', async () => {
    vi.mocked(fetchRefusedSenders).mockResolvedValue([])
    let settleFirst: (v: string | null) => void = () => {}
    vi.mocked(rebindEnrolment)
      .mockReturnValueOnce(
        new Promise<string | null>((resolve) => {
          settleFirst = resolve
        }),
      )
      .mockResolvedValue(null)

    wizardState.openReEnrol('edge-1')
    const first = wizardState.rebindEnrolmentWindow('192.0.2.50')
    await wizardState.rebindEnrolmentWindow('192.0.2.99')

    expect(rebindEnrolment).toHaveBeenCalledTimes(1)
    settleFirst(null)
    await first
    expect(wizardState.enrolExpectedAddress).toBe('192.0.2.50')
    expect(wizardState.enrolRebinding).toBe(false)
  })
})
