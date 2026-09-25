// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings' "router backups" group at the component (#394, round 44):
// the no-key and nothing-yet statements, a router's receipt at rest,
// the amber missed-push receipt and its "is it gone?" link, the
// download links round 44's newest-pair line offers, #1115's vault
// passphrase row -- all four states, its forms, the ratified copy, and
// the download gate -- and #1126's kept backups: the keep form, the
// earlier expander, the kept group, the release question, the low-space
// line and the viewer's read-only view of all of it.
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchRouterBackups: vi.fn(),
  fetchRouterBackupText: vi.fn(),
  fetchRouterBackupDiff: vi.fn(),
  routerBackupDownloadUrl: vi.fn(
    (device: string, generation: string, kind: string) => `/api/router-backups/${device}/${generation}/${kind}`,
  ),
  unlockRouterBackupVault: vi.fn(),
  lockRouterBackupVault: vi.fn(),
  setRouterBackupPassphrase: vi.fn(),
  removeRouterBackupPassphrase: vi.fn(),
  changeRouterBackupPassphrase: vi.fn(),
  keepRouterBackup: vi.fn(),
  releaseRouterBackup: vi.fn(),
  setRouterBackupComment: vi.fn(),
}))

// Blob/URL.createObjectURL are unreliable in jsdom -- faked at the
// module boundary the same way LogEveryRule.svelte.test.ts fakes
// lib/export's downloadText, so what is tested here is this
// component's own wiring (which URL, which filename, what happens on a
// 403) rather than jsdom's approximation of a browser save.
vi.mock('../lib/export', () => ({
  downloadFromUrl: vi.fn(),
}))

import {
  changeRouterBackupPassphrase,
  fetchRouterBackupDiff,
  fetchRouterBackups,
  fetchRouterBackupText,
  keepRouterBackup,
  lockRouterBackupVault,
  releaseRouterBackup,
  removeRouterBackupPassphrase,
  setRouterBackupComment,
  setRouterBackupPassphrase,
  unlockRouterBackupVault,
} from '../lib/api'
import { authState } from '../lib/auth.svelte'
import { downloadFromUrl } from '../lib/export'
import RouterBackups from './RouterBackups.svelte'
import type { RouterBackupsResponse, VaultLock } from '../lib/types'

function lock(over: Partial<VaultLock> = {}): VaultLock {
  return {
    passphraseSet: false,
    locked: false,
    unlockedForYou: false,
    minPassphraseLength: 12,
    idleTimeoutSeconds: 900,
    ...over,
  }
}

function resp(over: Partial<RouterBackupsResponse> = {}): RouterBackupsResponse {
  return {
    enabled: true,
    keyUnreadable: false,
    routers: [],
    totalGenerations: 0,
    totalRouters: 0,
    totalBytes: 0,
    port: ':47022',
    lock: lock(),
    ...over,
  }
}

// The keep controls are admin-only on screen as well as on the server
// (#1126's viewer clause), and authState is a module-level singleton --
// so every test states the role it is rendering as.
beforeEach(() => {
  authState.role = 'admin'
})

const router = {
  device: 'rb5009',
  generations: [
    {
      id: 'g0',
      backupArrivedAt: '2026-08-24T03:00:00Z',
      rscArrivedAt: '2026-08-24T03:00:05Z',
      backupBytes: 412000,
      rscBytes: 38000,
      header: 'plain',
    },
  ],
  intervalKnown: false,
  missed: 0,
}

describe('no key mounted', () => {
  it('says the drop box is closed, with no per-router block', () => {
    render(RouterBackups, { props: { resp: resp({ enabled: false }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByText(/none mounted — a backup that arrives has nowhere safe to go/)).toBeTruthy()
  })
})

// #1264 finding 5: enabled: false alone cannot tell "no key configured"
// from "a key is configured but could not be read" apart -- both leave
// the vault disabled the same way. keyUnreadable is what tells them
// apart, and a broken key must never read like the plain no-key state,
// since that state's fix (mint one) is exactly the action that strands
// every backup already encrypted under the broken one.
describe('a configured key that could not be read', () => {
  it('says the key could not be read and warns against minting a new one, not "none mounted"', () => {
    render(RouterBackups, {
      props: { resp: resp({ enabled: false, keyUnreadable: true }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    expect(screen.queryByText(/none mounted — a backup that arrives has nowhere safe to go/)).toBeNull()
    expect(screen.getByText(/could not be read/)).toBeTruthy()
    expect(screen.getByText(/do not\s+mint a new one/)).toBeTruthy()
  })
})

describe('nothing pushed yet', () => {
  it('points at the wizard step that prints the script', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByText(/no router has pushed one yet/)).toBeTruthy()
    expect(screen.getByText(/the wizard's step 6 prints the script/)).toBeTruthy()
  })
})

describe('a router at rest', () => {
  it('reads the kept count, the cadence and the oldest date, with download buttons', () => {
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [router], totalGenerations: 1, totalRouters: 1, totalBytes: 450000 }),
        fetchedAt: 0, onopenlost: vi.fn(),
      },
    })
    expect(screen.getByText('rb5009')).toBeTruthy()
    expect(screen.getByText('1 kept')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'download .backup' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '.rsc' })).toBeTruthy()
    // Nothing has been missed, so round 44's link is not offered.
    expect(screen.queryByRole('button', { name: 'is it gone?' })).toBeNull()
  })

  it('downloads through the fetch-and-save path, not a plain link', async () => {
    vi.mocked(downloadFromUrl).mockResolvedValue('ok')
    render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    expect(downloadFromUrl).toHaveBeenCalledWith('/api/router-backups/rb5009/g0/backup', 'rb5009.backup')
  })

  it('refreshes the lock, not the link, when a download 403s', async () => {
    vi.mocked(downloadFromUrl).mockResolvedValue('forbidden')
    vi.mocked(fetchRouterBackups).mockResolvedValue(resp({ lock: lock({ passphraseSet: true, locked: true }) }))
    render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    await waitFor(() => expect(fetchRouterBackups).toHaveBeenCalled())
    expect(await screen.findByText('locked')).toBeTruthy()
  })

  // v0.6.0 audit Lows, R1: a 403 download used to re-read the lock
  // silently and, on the comment's own word, leave it to "the parent's
  // own periodic refresh" to catch up -- which can be up to a minute
  // away. A refusal the operator just caused deserves the same kind of
  // answer a plain failed download already gets.
  it('says so when the lock refresh itself fails after a 403, rather than leaving it to the next periodic poll', async () => {
    vi.mocked(downloadFromUrl).mockResolvedValue('forbidden')
    vi.mocked(fetchRouterBackups).mockRejectedValue(new Error('network unreachable'))
    render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    await waitFor(() => expect(fetchRouterBackups).toHaveBeenCalled())

    expect((await screen.findByRole('alert')).textContent).toBe(
      "The download was refused, and mikroview couldn't refresh the lock status. Try again.",
    )
  })

  // The button used to do nothing and say nothing on anything but a
  // 403 -- a dropped connection or a 5xx looked identical to a click
  // that never happened.
  it('says so when a download fails outright, rather than doing nothing', async () => {
    vi.mocked(downloadFromUrl).mockResolvedValue('failed')
    render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    expect((await screen.findByRole('alert')).textContent).toBe('The download failed. Try again.')
  })

  it('clears a previous download error once a later download succeeds', async () => {
    vi.mocked(downloadFromUrl).mockResolvedValueOnce('failed')
    render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    expect(await screen.findByRole('alert')).toBeTruthy()

    vi.mocked(downloadFromUrl).mockResolvedValueOnce('ok')
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    await waitFor(() => expect(screen.queryByRole('alert')).toBeNull())
  })
})

describe('a router that has missed its usual push', () => {
  it('goes amber and offers "is it gone?", which names the router', async () => {
    const onopenlost = vi.fn()
    const missedRouter = {
      device: 'hap-ax2',
      generations: [
        { id: 'g0', backupArrivedAt: '2026-08-27T03:00:00Z', rscArrivedAt: '2026-08-27T03:00:05Z' },
      ],
      intervalKnown: true,
      intervalSeconds: 86400,
      lastArrival: '2026-08-30T03:00:00Z',
      missed: 3,
    }
    render(RouterBackups, { props: { resp: resp({ routers: [missedRouter] }), fetchedAt: 0, onopenlost } })
    expect(screen.getByText(/3 missed/)).toBeTruthy()
    const link = screen.getByRole('button', { name: 'is it gone?' })
    link.click()
    expect(onopenlost).toHaveBeenCalledWith('hap-ax2')
  })
})

describe('the facts column', () => {
  it('states the arrive-by port and the fixed allowance', () => {
    const restingRouter = { device: 'rb5009', generations: [], intervalKnown: false, missed: 0 }
    render(RouterBackups, { props: { resp: resp({ routers: [restingRouter] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByText(/SFTP on port 47022/)).toBeTruthy()
    expect(screen.getByText(/10 pairs a router · 16 MiB a file/)).toBeTruthy()
  })
})

describe('the vault passphrase (#1115)', () => {
  it('off: offers to set one, with the loss warning above the confirm field and a client-side length refusal', async () => {
    render(RouterBackups, { props: { resp: resp(), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByText('off')).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'set…' }))
    expect(
      screen.getByText(
        'If this passphrase is lost, the stored backups are lost with it. There is no reset and no recovery — not from mikroview, and not from the router. Keep it wherever you keep your other recovery keys.',
      ),
    ).toBeTruthy()

    await fireEvent.input(screen.getByLabelText('passphrase'), { target: { value: 'short' } })
    await fireEvent.input(screen.getByLabelText('confirm passphrase'), { target: { value: 'short' } })
    await fireEvent.click(screen.getByRole('button', { name: 'set' }))
    expect(screen.getByText(/at least 12 characters/)).toBeTruthy()
    expect(setRouterBackupPassphrase).not.toHaveBeenCalled()

    vi.mocked(setRouterBackupPassphrase).mockResolvedValue(lock({ passphraseSet: true, unlockedForYou: true }))
    await fireEvent.input(screen.getByLabelText('passphrase'), { target: { value: 'a very long passphrase' } })
    await fireEvent.input(screen.getByLabelText('confirm passphrase'), { target: { value: 'a very long passphrase' } })
    await fireEvent.click(screen.getByRole('button', { name: 'set' }))
    expect(setRouterBackupPassphrase).toHaveBeenCalledWith('a very long passphrase')
    expect(await screen.findByText('unlocked')).toBeTruthy()
  })

  it('locked: offers unlock only, and gates the router block downloads with the ratified line', () => {
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [router], lock: lock({ passphraseSet: true, locked: true }) }),
        fetchedAt: 0, onopenlost: vi.fn(),
      },
    })
    expect(screen.getByText('locked')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'unlock…' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'download .backup' })).toBeNull()
    expect(screen.queryByRole('button', { name: '.rsc' })).toBeNull()
    expect(screen.getByText('locked — the vault passphrase opens downloads')).toBeTruthy()
    // Sizes are not hidden while locked, only the links -- and keeping
    // a backup is not a read of it, so keep… stays (#1126).
    expect(screen.getByText(/402 KiB/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'keep…' })).toBeTruthy()
  })

  it('unlocked elsewhere: offers unlock only, with the other-sign-in copy on the router block', () => {
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [router], lock: lock({ passphraseSet: true, locked: false, unlockedForYou: false }) }),
        fetchedAt: 0, onopenlost: vi.fn(),
      },
    })
    expect(screen.getByText('unlocked elsewhere')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'unlock…' })).toBeTruthy()
    expect(screen.getByText('unlocked by another of your sign-ins — unlock here to download')).toBeTruthy()
  })

  it('unlocked here: offers change, remove and lock, and downloads render normally', async () => {
    vi.mocked(lockRouterBackupVault).mockResolvedValue(lock({ passphraseSet: true, locked: true }))
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [router], lock: lock({ passphraseSet: true, unlockedForYou: true }) }),
        fetchedAt: 0, onopenlost: vi.fn(),
      },
    })
    expect(screen.getByText('unlocked')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'download .backup' })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'lock' }))
    expect(lockRouterBackupVault).toHaveBeenCalled()
    expect(await screen.findByText('locked')).toBeTruthy()
  })

  it('remove confirms with the ratified copy and needs the current passphrase', async () => {
    vi.mocked(removeRouterBackupPassphrase).mockResolvedValue(lock())
    render(RouterBackups, {
      props: {
        resp: resp({ lock: lock({ passphraseSet: true, unlockedForYou: true }) }),
        fetchedAt: 0, onopenlost: vi.fn(),
      },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'remove…' }))
    expect(
      screen.getByText('Removing the passphrase re-seals every stored backup under the retention key. Any admin can read them again.'),
    ).toBeTruthy()
    await fireEvent.input(screen.getByLabelText('current passphrase'), { target: { value: 'the-current-one' } })
    await fireEvent.click(screen.getByRole('button', { name: 'remove' }))
    expect(removeRouterBackupPassphrase).toHaveBeenCalledWith('the-current-one')
    expect(await screen.findByText('off')).toBeTruthy()
  })

  it('change makes one atomic call rather than a remove followed by a set', async () => {
    vi.mocked(changeRouterBackupPassphrase).mockResolvedValue(lock({ passphraseSet: true, unlockedForYou: true }))
    // Cleared rather than asserted from zero: an earlier test in this file
    // exercises the remove-only form and leaves these mocks with calls of
    // their own, which is not this test's business.
    vi.mocked(removeRouterBackupPassphrase).mockClear()
    vi.mocked(setRouterBackupPassphrase).mockClear()
    render(RouterBackups, {
      props: {
        resp: resp({ lock: lock({ passphraseSet: true, unlockedForYou: true }) }),
        fetchedAt: 0, onopenlost: vi.fn(),
      },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'change…' }))
    await fireEvent.input(screen.getByLabelText('current passphrase'), { target: { value: 'old-passphrase-1' } })
    await fireEvent.input(screen.getByLabelText('new passphrase'), { target: { value: 'brand-new-passphrase' } })
    await fireEvent.input(screen.getByLabelText('confirm new passphrase'), { target: { value: 'brand-new-passphrase' } })
    await fireEvent.click(screen.getByRole('button', { name: 'change' }))
    expect(changeRouterBackupPassphrase).toHaveBeenCalledWith('old-passphrase-1', 'brand-new-passphrase')
    expect(removeRouterBackupPassphrase).not.toHaveBeenCalled()
    expect(setRouterBackupPassphrase).not.toHaveBeenCalled()
    expect(await screen.findByText('unlocked')).toBeTruthy()
  })

  it('unlock takes a single field and shows no warning of its own', async () => {
    vi.mocked(unlockRouterBackupVault).mockResolvedValue(lock({ passphraseSet: true, unlockedForYou: true }))
    render(RouterBackups, {
      props: { resp: resp({ lock: lock({ passphraseSet: true, locked: true }) }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'unlock…' }))
    expect(screen.queryByText(/lost/i)).toBeNull()
    await fireEvent.input(screen.getByLabelText('vault passphrase'), { target: { value: 'the-vault-passphrase' } })
    await fireEvent.click(screen.getByRole('button', { name: 'unlock' }))
    expect(unlockRouterBackupVault).toHaveBeenCalledWith('the-vault-passphrase')
    expect(await screen.findByText('unlocked')).toBeTruthy()
  })
})

// --- kept backups (#1126) ------------------------------------------------

const keptGeneration = {
  id: 'g0',
  backupArrivedAt: '2026-08-24T03:00:00Z',
  rscArrivedAt: '2026-08-24T03:00:05Z',
  backupBytes: 412000,
  rscBytes: 38000,
  header: 'plain',
  comment: 'before the 7.16 upgrade',
  protectedAt: '2026-09-12T10:00:00Z',
  protectedBy: 'tom',
}

/** A router holding one kept backup and nothing in the cycling ten. */
const routerWithKept = {
  device: 'rb5009',
  generations: [],
  protected: [keptGeneration],
  intervalKnown: false,
  missed: 0,
}

/** A router with three cycling generations, for the earlier expander. */
const routerWithThree = {
  device: 'rb5009',
  generations: [
    { id: 'g0', backupArrivedAt: '2026-09-10T04:00:00Z', backupBytes: 400000 },
    { id: 'g1', backupArrivedAt: '2026-09-11T04:00:00Z', backupBytes: 410000 },
    {
      id: 'g2',
      backupArrivedAt: '2026-09-12T04:00:00Z',
      backupBytes: 420000,
      rscArrivedAt: '2026-09-12T04:00:05Z',
      rscBytes: 38000,
    },
  ],
  intervalKnown: false,
  missed: 0,
}

describe('keeping a backup', () => {
  it('offers keep… on the newest line and sends the typed reason', async () => {
    vi.mocked(keepRouterBackup).mockResolvedValue(routerWithKept)
    render(RouterBackups, { props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'keep…' }))
    await fireEvent.input(screen.getByLabelText('why keep this one'), {
      target: { value: 'before the 7.16 upgrade' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'keep' }))

    expect(keepRouterBackup).toHaveBeenCalledWith('rb5009', 'g0', 'before the 7.16 upgrade')
    // The block the call answered with is what is drawn, without waiting
    // for the parent's own poll to come round again.
    expect(await screen.findByText('kept', { selector: 'p' })).toBeTruthy()
    expect(screen.getByText(/before the 7.16 upgrade/)).toBeTruthy()
  })

  it('will not keep one without a reason', async () => {
    vi.mocked(keepRouterBackup).mockClear()
    render(RouterBackups, { props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'keep…' }))
    await fireEvent.click(screen.getByRole('button', { name: 'keep' }))
    expect(screen.getByText('say why you are keeping it')).toBeTruthy()
    expect(keepRouterBackup).not.toHaveBeenCalled()
  })

  // #1218 audit finding 6: the parent (EngineRoom) polls GET
  // /api/router-backups on its own timer and hands down whatever it
  // gets as a fresh `resp` object. A poll that was already in flight
  // when the keep above landed resolves moments later with the
  // *pre-keep* row -- this reproduces that arriving as a prop update
  // right after the optimistic one, and expects the kept block to
  // survive it rather than being stomped back to "not kept".
  it('keeps the optimistic row when a stale poll lands right after it', async () => {
    vi.mocked(keepRouterBackup).mockResolvedValue(routerWithKept)
    const { rerender } = render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'keep…' }))
    await fireEvent.input(screen.getByLabelText('why keep this one'), {
      target: { value: 'before the 7.16 upgrade' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'keep' }))
    expect(await screen.findByText(/before the 7.16 upgrade/)).toBeTruthy()

    // The stale poll: still the pre-keep router, as a brand new object
    // (a real poll never hands back the exact same reference), from a
    // request issued five seconds before the keep landed -- which is
    // the whole point, so it is dated rather than left at the 0 the
    // other renders here use as "no poll yet".
    await rerender({
      resp: resp({ routers: [{ ...router }] }),
      fetchedAt: Date.now() - 5000,
      onopenlost: vi.fn(),
    })

    expect(screen.getByText(/before the 7.16 upgrade/)).toBeTruthy()
  })

  it('lets a poll that has genuinely caught up take over from the optimistic row', async () => {
    vi.mocked(keepRouterBackup).mockResolvedValue(routerWithKept)
    const { rerender } = render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'keep…' }))
    await fireEvent.input(screen.getByLabelText('why keep this one'), {
      target: { value: 'before the 7.16 upgrade' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'keep' }))
    expect(await screen.findByText(/before the 7.16 upgrade/)).toBeTruthy()

    // A poll issued after the keep landed -- the parent has genuinely
    // caught up, so a later change (another admin editing the same
    // comment) has to be able to reach the screen rather than being
    // pinned to this tab's own copy forever. It does not have to match
    // what the keep returned: a row carries a missed count and an
    // interval estimate that move on their own, so waiting for a match
    // could mean waiting for ever.
    await rerender({
      resp: resp({ routers: [{ ...routerWithKept, protected: [{ ...keptGeneration }] }] }),
      fetchedAt: Date.now() + 1000, onopenlost: vi.fn(),
    })
    await rerender({
      resp: resp({
        routers: [{ ...routerWithKept, protected: [{ ...keptGeneration, comment: 'edited by someone else' }] }],
      }),
      fetchedAt: Date.now() + 2000, onopenlost: vi.fn(),
    })

    expect(await screen.findByText(/edited by someone else/)).toBeTruthy()
  })

  // The fault the freshness check replaced an equality check to fix. A
  // row carries a missed-backup count, an interval estimate and the
  // generations themselves, all of which move without anyone touching
  // this screen. So a polled row need never equal the one a keep
  // returned -- and while the override was dropped only on a match, it
  // was never dropped at all: that router's block stayed frozen on this
  // tab's copy, hiding whatever arrived since, until a page reload.
  it('lets a poll take over even when its row never matches the optimistic one', async () => {
    vi.mocked(keepRouterBackup).mockResolvedValue(routerWithKept)
    const { rerender } = render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'keep…' }))
    await fireEvent.input(screen.getByLabelText('why keep this one'), {
      target: { value: 'before the 7.16 upgrade' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'keep' }))
    expect(await screen.findByText(/before the 7.16 upgrade/)).toBeTruthy()

    // The nightly backup lands, so the next poll's row differs from the
    // keep's answer in a way it will never recover from.
    await rerender({
      resp: resp({
        routers: [{ ...routerWithKept, protected: [{ ...keptGeneration, comment: 'kept by the night shift' }], missed: 3 }],
      }),
      fetchedAt: Date.now() + 1000,
      onopenlost: vi.fn(),
    })

    expect(await screen.findByText(/kept by the night shift/)).toBeTruthy()
  })
})

describe('the earlier generations', () => {
  it('are behind earlier…, and each can be kept', async () => {
    render(RouterBackups, { props: { resp: resp({ routers: [routerWithThree] }), fetchedAt: 0, onopenlost: vi.fn() } })

    // Only the newest line is drawn until the expander is used.
    expect(screen.queryByRole('button', { name: '.backup' })).toBeNull()
    await fireEvent.click(screen.getByRole('button', { name: 'earlier…' }))

    // The other two, newest first: g1 then g0.
    expect(screen.getAllByRole('button', { name: '.backup' }).length).toBe(2)
    // One keep… for the newest line and one for each earlier line.
    expect(screen.getAllByRole('button', { name: 'keep…' }).length).toBe(3)
  })

  it('is not offered when a router has only the one generation', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.queryByRole('button', { name: 'earlier…' })).toBeNull()
  })
})

describe('the kept group', () => {
  it('states the reason beside the pair, with edit… and release…', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [routerWithKept] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByText('kept', { selector: 'p' })).toBeTruthy()
    expect(screen.getByText(/✱/)).toBeTruthy()
    expect(screen.getByText(/before the 7.16 upgrade/)).toBeTruthy()
    expect(screen.getByRole('button', { name: '.backup' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '.rsc' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'edit…' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'release…' })).toBeTruthy()
  })

  it('edit… opens the same field with the reason already in it', async () => {
    const rewritten = { ...routerWithKept, protected: [{ ...keptGeneration, comment: 'before the office move' }] }
    vi.mocked(setRouterBackupComment).mockResolvedValue(rewritten)
    render(RouterBackups, { props: { resp: resp({ routers: [routerWithKept] }), fetchedAt: 0, onopenlost: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'edit…' }))
    const field = screen.getByLabelText('why keep this one') as HTMLInputElement
    expect(field.value).toBe('before the 7.16 upgrade')
    await fireEvent.input(field, { target: { value: 'before the office move' } })
    await fireEvent.click(screen.getByRole('button', { name: 'keep' }))

    expect(setRouterBackupComment).toHaveBeenCalledWith('rb5009', 'g0', 'before the office move')
    expect(await screen.findByText(/before the office move/)).toBeTruthy()
  })

  it('release… asks once, and says what releasing costs', async () => {
    vi.mocked(releaseRouterBackup).mockResolvedValue({
      ...routerWithKept,
      protected: [],
      generations: [keptGeneration],
    })
    render(RouterBackups, { props: { resp: resp({ routers: [routerWithKept] }), fetchedAt: 0, onopenlost: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'release…' }))
    expect(screen.getByText(/release this one\? it goes back into the ten and the oldest may go/)).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'release' }))
    expect(releaseRouterBackup).toHaveBeenCalledWith('rb5009', 'g0')
    await waitFor(() => expect(screen.queryByText('kept', { selector: 'p' })).toBeNull())
  })

  it('cancel leaves the kept backup alone', async () => {
    vi.mocked(releaseRouterBackup).mockClear()
    render(RouterBackups, { props: { resp: resp({ routers: [routerWithKept] }), fetchedAt: 0, onopenlost: vi.fn() } })
    await fireEvent.click(screen.getByRole('button', { name: 'release…' }))
    await fireEvent.click(screen.getByRole('button', { name: 'cancel' }))
    expect(releaseRouterBackup).not.toHaveBeenCalled()
    expect(screen.getByText('kept', { selector: 'p' })).toBeTruthy()
  })
})

describe('a disk that is getting low', () => {
  it('says so above the routers, and names the one thing that frees space', () => {
    render(RouterBackups, {
      props: { resp: resp({ routers: [routerWithKept], lowSpace: true }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    expect(
      screen.getByText(
        'disk is getting low · the vault is cycling the ten · releasing a kept backup is the one way to free space here',
      ),
    ).toBeTruthy()
  })

  it('is absent when the disk is fine', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [routerWithKept] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.queryByText(/disk is getting low/)).toBeNull()
  })
})

describe('the hint line', () => {
  it('says a kept backup stays out of the ten', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByText(/a kept backup stays out of the ten until you release it/)).toBeTruthy()
  })
})

describe('a viewer', () => {
  it('reads the kept backups and is offered none of the three controls', () => {
    authState.role = 'viewer'
    render(RouterBackups, {
      props: { resp: resp({ routers: [routerWithKept] }), fetchedAt: 0, onopenlost: vi.fn() },
    })
    expect(screen.getByText('kept', { selector: 'p' })).toBeTruthy()
    expect(screen.getByText(/before the 7.16 upgrade/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'keep…' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'edit…' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'release…' })).toBeNull()
  })
})

// Reading one stored export and comparing two (#895). Both are pulled
// through lib/api rather than navigated to, so both are mocked at the
// same module boundary the download already is, and what is tested
// here is this component's wiring: which generation it asks about,
// which pair it compares, and what it draws with the answer.
describe('reading one export, and comparing two', () => {
  const twoGenerations = {
    device: 'rb5009',
    generations: [
      { id: 'g0', backupArrivedAt: '2026-08-24T03:00:00Z', rscArrivedAt: '2026-08-24T03:00:05Z' },
      { id: 'g1', backupArrivedAt: '2026-08-25T03:00:00Z', rscArrivedAt: '2026-08-25T03:00:05Z' },
    ],
    intervalKnown: false,
    missed: 0,
  }

  it('offers read on the newest, and compare with previous once there are two', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [twoGenerations] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByRole('button', { name: 'read' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'compare with previous' })).toBeTruthy()
  })

  it('offers no comparison on the only generation there is', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [router] }), fetchedAt: 0, onopenlost: vi.fn() } })
    expect(screen.getByRole('button', { name: 'read' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'compare with previous' })).toBeNull()
  })

  it('shows the stored export, and says when something was taken out of it', async () => {
    vi.mocked(fetchRouterBackupText).mockResolvedValue({
      device: 'rb5009',
      generation: 'g1',
      text: '# mikroview: 1 secret values removed at ingest (lines 4)\n/ppp secret\nadd password="<removed>"',
      lines: 3,
      redacted: true,
    })
    render(RouterBackups, { props: { resp: resp({ routers: [twoGenerations] }), fetchedAt: 0, onopenlost: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'read' }))
    await waitFor(() => expect(fetchRouterBackupText).toHaveBeenCalledWith('rb5009', 'g1'))
    expect(await screen.findByText(/add password="<removed>"/)).toBeTruthy()
    expect(screen.getByText(/this export arrived with secrets still in it/)).toBeTruthy()
  })

  it('compares against the generation before, and draws the two colours', async () => {
    vi.mocked(fetchRouterBackupDiff).mockResolvedValue({
      device: 'rb5009',
      from: 'g0',
      to: 'g1',
      lines: [
        { op: '-', line: 7, text: 'add action=drop chain=forward comment=old' },
        { op: '+', line: 7, text: 'add action=drop chain=forward comment=new' },
      ],
      same: false,
    })
    const { container } = render(RouterBackups, {
      props: { resp: resp({ routers: [twoGenerations] }), fetchedAt: 0, onopenlost: vi.fn() },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'compare with previous' }))
    // The older of the two is `from`: a comparison always reads
    // forwards in time.
    await waitFor(() => expect(fetchRouterBackupDiff).toHaveBeenCalledWith('rb5009', 'g0', 'g1'))
    expect(await screen.findByText(/comment=new/)).toBeTruthy()
    expect(container.querySelectorAll('.dadd').length).toBe(1)
    expect(container.querySelectorAll('.ddel').length).toBe(1)
  })

  it('says so plainly when the only difference is the date the router stamped on it', async () => {
    vi.mocked(fetchRouterBackupDiff).mockResolvedValue({
      device: 'rb5009',
      from: 'g0',
      to: 'g1',
      lines: [],
      same: true,
    })
    render(RouterBackups, { props: { resp: resp({ routers: [twoGenerations] }), fetchedAt: 0, onopenlost: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'compare with previous' }))
    expect(await screen.findByText(/nothing changed between these two/)).toBeTruthy()
  })

  it('re-reads the lock when the vault refuses a read, rather than showing a stale unlock', async () => {
    vi.mocked(fetchRouterBackupText).mockResolvedValue('the vault is locked -- unlock it with the vault passphrase first')
    vi.mocked(fetchRouterBackups).mockResolvedValue(resp({ lock: lock({ passphraseSet: true, locked: true }) }))
    render(RouterBackups, { props: { resp: resp({ routers: [twoGenerations] }), fetchedAt: 0, onopenlost: vi.fn() } })

    await fireEvent.click(screen.getByRole('button', { name: 'read' }))
    expect(await screen.findByRole('alert')).toBeTruthy()
    await waitFor(() => expect(fetchRouterBackups).toHaveBeenCalled())
    expect(await screen.findByText('locked')).toBeTruthy()
  })

  it('offers no read at all while the vault is locked', () => {
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [twoGenerations], lock: lock({ passphraseSet: true, locked: true }) }),
        fetchedAt: 0, onopenlost: vi.fn(),
      },
    })
    expect(screen.queryByRole('button', { name: 'read' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'compare with previous' })).toBeNull()
  })
})
