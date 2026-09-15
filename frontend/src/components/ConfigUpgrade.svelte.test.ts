// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

// ConfigUpgrade.svelte itself makes no requests directly -- this stops
// configUpgradeState.refresh()/dismiss() (called onMount / on click)
// from reaching for the network under jsdom, the same guard
// AuditLog.svelte.test.ts uses for auditState.
vi.mock('../lib/api', () => ({
  fetchConfigUpgrade: vi.fn(),
  dismissConfigUpgrade: vi.fn(),
}))
// The Clipboard API is not implemented under jsdom -- mocked so a click
// on Copy doesn't fall through to the legacy execCommand path and rely
// on jsdom's own (unrelated) support for it.
vi.mock('../lib/clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

import { fetchConfigUpgrade, dismissConfigUpgrade } from '../lib/api'
import { copyToClipboard } from '../lib/clipboard'
import type { ConfigUpgradeResponse } from '../lib/configUpgrade'
import ConfigUpgrade from './ConfigUpgrade.svelte'

function response(overrides: Partial<ConfigUpgradeResponse> = {}): ConfigUpgradeResponse {
  return {
    version: 'v1.2.3',
    settings: [],
    dismissed: false,
    ...overrides,
  }
}

async function renderPanel(res: ConfigUpgradeResponse) {
  vi.mocked(fetchConfigUpgrade).mockResolvedValue(res)
  render(ConfigUpgrade)
  await Promise.resolve()
  await Promise.resolve()
  flushSync()
}

describe('ConfigUpgrade (#1218)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
  })

  it('says plainly when nothing is missing, rather than an empty page', async () => {
    await renderPanel(response({ settings: [] }))
    expect(screen.getByText(/Nothing new/)).toBeTruthy()
  })

  it('surfaces a failed fetch, not the empty-settings message', async () => {
    vi.mocked(fetchConfigUpgrade).mockRejectedValue(new Error('network unreachable'))
    render(ConfigUpgrade)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    expect(screen.getByText(/Could not check for new settings: network unreachable/)).toBeTruthy()
    expect(screen.queryByText(/Nothing new/)).toBeNull()
  })

  it('renders each missing setting as its own paste block, verbatim', async () => {
    await renderPanel(
      response({
        settings: [
          { key: 'geoip', block: '# geoip:\n#   dbPath: /etc/mikroview/GeoLite2-Country.mmdb' },
          { key: 'backup', block: '# backup:\n#   enabled: false' },
        ],
      }),
    )
    const blocks = Array.from(document.querySelectorAll('pre.script')).map((el) => el.textContent)
    expect(blocks).toEqual([
      '# geoip:\n#   dbPath: /etc/mikroview/GeoLite2-Country.mmdb',
      '# backup:\n#   enabled: false',
    ])
  })

  it('copies the exact block text and shows copied on the button that was clicked', async () => {
    await renderPanel(
      response({ settings: [{ key: 'geoip', block: '# geoip:\n#   dbPath: x' }] }),
    )
    const button = screen.getByRole('button', { name: 'copy' })
    await fireEvent.click(button)
    flushSync()

    expect(copyToClipboard).toHaveBeenCalledWith('# geoip:\n#   dbPath: x')
    expect(screen.getByRole('button', { name: 'copied' })).toBeTruthy()
  })

  it('shows a Dismiss control when the notice has not been dismissed yet', async () => {
    await renderPanel(response({ settings: [{ key: 'geoip', block: '# geoip:' }], dismissed: false }))
    expect(screen.getByRole('button', { name: /dismiss for this version/ })).toBeTruthy()
  })

  it('dismissing calls the API and replaces the button with a note', async () => {
    await renderPanel(response({ settings: [{ key: 'geoip', block: '# geoip:' }], dismissed: false }))
    vi.mocked(dismissConfigUpgrade).mockResolvedValue(response({ dismissed: true }))

    await fireEvent.click(screen.getByRole('button', { name: /dismiss for this version/ }))
    flushSync()

    expect(dismissConfigUpgrade).toHaveBeenCalled()
    expect(screen.getByText(/Dismissed for this version/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /^dismiss for this version$/ })).toBeNull()
  })

  it('already dismissed on load shows the note, not the Dismiss control', async () => {
    await renderPanel(response({ settings: [{ key: 'geoip', block: '# geoip:' }], dismissed: true }))
    expect(screen.getByText(/Dismissed for this version/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /dismiss for this version/ })).toBeNull()
  })
})
