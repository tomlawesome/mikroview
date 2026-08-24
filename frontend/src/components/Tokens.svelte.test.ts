// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  createToken: vi.fn(),
  revokeToken: vi.fn(),
  fetchTokens: vi.fn(async () => []),
  fetchDevices: vi.fn(async () => []),
}))

import { createToken, fetchTokens, revokeToken } from '../lib/api'
import { tokensState } from '../lib/tokens.svelte'
import Tokens from './Tokens.svelte'

// #548: the page successor to TokensOverlay.svelte -- same create/revoke
// behaviour, minus the modal chrome.
describe('Tokens page (#548)', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    tokensState.list = []
    tokensState.justCreated = null
  })

  it('has a page header titled Tokens', () => {
    vi.mocked(fetchTokens).mockResolvedValue([])
    render(Tokens)
    expect(screen.getByRole('heading', { name: 'Tokens' })).toBeTruthy()
  })

  it('loads the token list on mount', async () => {
    vi.mocked(fetchTokens).mockResolvedValue([
      { id: 't1', name: 'birdcage', kind: 'api', createdAt: '2026-08-01T00:00:00Z' },
    ])
    render(Tokens)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    expect(screen.getByText('birdcage')).toBeTruthy()
  })

  it('submits the create form to tokensState.create with the read-only default', async () => {
    vi.mocked(fetchTokens).mockResolvedValue([])
    vi.mocked(createToken).mockResolvedValue({ id: 't2', name: 'ci-reader', kind: 'api', value: 'secret-value', createdAt: '2026-08-01T00:00:00Z' })
    render(Tokens)
    await Promise.resolve()

    await fireEvent.input(screen.getByPlaceholderText('Token name (e.g. birdcage)'), { target: { value: 'ci-reader' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    expect(createToken).toHaveBeenCalledWith('ci-reader', 'api', undefined)
  })

  it('refuses to submit an ingest token with no device chosen', async () => {
    vi.mocked(fetchTokens).mockResolvedValue([])
    render(Tokens)
    await Promise.resolve()

    await fireEvent.input(screen.getByPlaceholderText('Token name (e.g. birdcage)'), { target: { value: 'router-push' } })
    await fireEvent.change(screen.getByLabelText('Token kind'), { target: { value: 'ingest' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    expect(createToken).not.toHaveBeenCalled()
    expect(screen.getByText(/needs a device/)).toBeTruthy()
  })

  it('revokes a token after confirmation', async () => {
    vi.mocked(fetchTokens).mockResolvedValue([{ id: 't3', name: 'old-script', kind: 'api', createdAt: '2026-08-01T00:00:00Z' }])
    vi.mocked(revokeToken).mockResolvedValue(null)
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    render(Tokens)
    await Promise.resolve()
    await Promise.resolve()
    flushSync()

    await fireEvent.click(screen.getByRole('button', { name: 'Revoke' }))

    expect(revokeToken).toHaveBeenCalledWith('t3')
  })
})
