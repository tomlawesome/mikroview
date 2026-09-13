// SPDX-License-Identifier: AGPL-3.0-only
//
// Settings' "router backups" group at the component (#394, round 44):
// the no-key and nothing-yet statements, a router's receipt at rest,
// the amber missed-push receipt and its "is it gone?" link, the
// download links round 44's newest-pair line offers, and #1115's vault
// passphrase row -- all four states, its forms, the ratified copy, and
// the download gate.
import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte'

vi.mock('../lib/api', () => ({
  fetchRouterBackups: vi.fn(),
  routerBackupDownloadUrl: vi.fn(
    (device: string, generation: string, kind: string) => `/api/router-backups/${device}/${generation}/${kind}`,
  ),
  unlockRouterBackupVault: vi.fn(),
  lockRouterBackupVault: vi.fn(),
  setRouterBackupPassphrase: vi.fn(),
  removeRouterBackupPassphrase: vi.fn(),
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
  fetchRouterBackups,
  lockRouterBackupVault,
  removeRouterBackupPassphrase,
  setRouterBackupPassphrase,
  unlockRouterBackupVault,
} from '../lib/api'
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
    routers: [],
    totalGenerations: 0,
    totalRouters: 0,
    totalBytes: 0,
    port: ':47022',
    lock: lock(),
    ...over,
  }
}

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
    render(RouterBackups, { props: { resp: resp({ enabled: false }), onopenlost: vi.fn() } })
    expect(screen.getByText(/none mounted — a backup that arrives has nowhere safe to go/)).toBeTruthy()
  })
})

describe('nothing pushed yet', () => {
  it('points at the wizard step that prints the script', () => {
    render(RouterBackups, { props: { resp: resp({ routers: [] }), onopenlost: vi.fn() } })
    expect(screen.getByText(/no router has pushed one yet/)).toBeTruthy()
    expect(screen.getByText(/the wizard's step 6 prints the script/)).toBeTruthy()
  })
})

describe('a router at rest', () => {
  it('reads the kept count, the cadence and the oldest date, with download buttons', () => {
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [router], totalGenerations: 1, totalRouters: 1, totalBytes: 450000 }),
        onopenlost: vi.fn(),
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
      props: { resp: resp({ routers: [router] }), onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    expect(downloadFromUrl).toHaveBeenCalledWith('/api/router-backups/rb5009/g0/backup', 'rb5009.backup')
  })

  it('refreshes the lock, not the link, when a download 403s', async () => {
    vi.mocked(downloadFromUrl).mockResolvedValue('forbidden')
    vi.mocked(fetchRouterBackups).mockResolvedValue(resp({ lock: lock({ passphraseSet: true, locked: true }) }))
    render(RouterBackups, {
      props: { resp: resp({ routers: [router] }), onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'download .backup' }))
    await waitFor(() => expect(fetchRouterBackups).toHaveBeenCalled())
    expect(await screen.findByText('locked')).toBeTruthy()
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
    render(RouterBackups, { props: { resp: resp({ routers: [missedRouter] }), onopenlost } })
    expect(screen.getByText(/3 missed/)).toBeTruthy()
    const link = screen.getByRole('button', { name: 'is it gone?' })
    link.click()
    expect(onopenlost).toHaveBeenCalledWith('hap-ax2')
  })
})

describe('the facts column', () => {
  it('states the arrive-by port and the fixed allowance', () => {
    const restingRouter = { device: 'rb5009', generations: [], intervalKnown: false, missed: 0 }
    render(RouterBackups, { props: { resp: resp({ routers: [restingRouter] }), onopenlost: vi.fn() } })
    expect(screen.getByText(/SFTP on port 47022/)).toBeTruthy()
    expect(screen.getByText(/10 pairs a router · 16 MiB a file/)).toBeTruthy()
  })
})

describe('the vault passphrase (#1115)', () => {
  it('off: offers to set one, with the loss warning above the confirm field and a client-side length refusal', async () => {
    render(RouterBackups, { props: { resp: resp(), onopenlost: vi.fn() } })
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
        onopenlost: vi.fn(),
      },
    })
    expect(screen.getByText('locked')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'unlock…' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'download .backup' })).toBeNull()
    expect(screen.queryByRole('button', { name: '.rsc' })).toBeNull()
    expect(screen.getByText('locked — the vault passphrase opens downloads')).toBeTruthy()
    // Sizes are not hidden while locked, only the links.
    expect(screen.getByText('402 KiB')).toBeTruthy()
  })

  it('unlocked elsewhere: offers unlock only, with the other-sign-in copy on the router block', () => {
    render(RouterBackups, {
      props: {
        resp: resp({ routers: [router], lock: lock({ passphraseSet: true, locked: false, unlockedForYou: false }) }),
        onopenlost: vi.fn(),
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
        onopenlost: vi.fn(),
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
        onopenlost: vi.fn(),
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

  it('change composes the current passphrase’s remove and the new one’s set', async () => {
    vi.mocked(removeRouterBackupPassphrase).mockResolvedValue(lock())
    vi.mocked(setRouterBackupPassphrase).mockResolvedValue(lock({ passphraseSet: true, unlockedForYou: true }))
    render(RouterBackups, {
      props: {
        resp: resp({ lock: lock({ passphraseSet: true, unlockedForYou: true }) }),
        onopenlost: vi.fn(),
      },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'change…' }))
    await fireEvent.input(screen.getByLabelText('current passphrase'), { target: { value: 'old-passphrase-1' } })
    await fireEvent.input(screen.getByLabelText('new passphrase'), { target: { value: 'brand-new-passphrase' } })
    await fireEvent.input(screen.getByLabelText('confirm new passphrase'), { target: { value: 'brand-new-passphrase' } })
    await fireEvent.click(screen.getByRole('button', { name: 'change' }))
    expect(removeRouterBackupPassphrase).toHaveBeenCalledWith('old-passphrase-1')
    expect(setRouterBackupPassphrase).toHaveBeenCalledWith('brand-new-passphrase')
    expect(await screen.findByText('unlocked')).toBeTruthy()
  })

  it('unlock takes a single field and shows no warning of its own', async () => {
    vi.mocked(unlockRouterBackupVault).mockResolvedValue(lock({ passphraseSet: true, unlockedForYou: true }))
    render(RouterBackups, {
      props: { resp: resp({ lock: lock({ passphraseSet: true, locked: true }) }), onopenlost: vi.fn() },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'unlock…' }))
    expect(screen.queryByText(/lost/i)).toBeNull()
    await fireEvent.input(screen.getByLabelText('vault passphrase'), { target: { value: 'the-vault-passphrase' } })
    await fireEvent.click(screen.getByRole('button', { name: 'unlock' }))
    expect(unlockRouterBackupVault).toHaveBeenCalledWith('the-vault-passphrase')
    expect(await screen.findByText('unlocked')).toBeTruthy()
  })
})
