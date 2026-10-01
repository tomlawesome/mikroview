// SPDX-License-Identifier: AGPL-3.0-only
//
// #1345 Q5B-F4: main.ts registers the service worker only in a built
// app (#1314). A dev server has no generated worker to register, so
// the gate must stay shut there; a built app must still register. Both
// branches are pinned here by importing main.ts fresh under each value
// of import.meta.env.PROD, with everything it starts mocked out.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const registerServiceWorker = vi.fn(() => Promise.resolve(undefined))

vi.mock('svelte', async (importOriginal) => ({
  ...(await importOriginal<typeof import('svelte')>()),
  mount: vi.fn(() => ({})),
}))
vi.mock('./App.svelte', () => ({ default: {} }))
vi.mock('./app.css', () => ({}))
vi.mock('./lib/cancelled', () => ({ installCancellationGuard: vi.fn() }))
vi.mock('./lib/serviceWorker', () => ({ registerServiceWorker }))

describe('main.ts service-worker gate (#1314)', () => {
  beforeEach(() => {
    vi.resetModules()
    registerServiceWorker.mockClear()
    const target = document.createElement('div')
    target.id = 'app'
    document.body.appendChild(target)
  })

  afterEach(() => {
    vi.unstubAllEnvs()
    document.getElementById('app')?.remove()
  })

  it('does not register a worker on the dev server', async () => {
    vi.stubEnv('PROD', false)
    await import('./main')
    expect(registerServiceWorker).not.toHaveBeenCalled()
  })

  it('registers the worker in a built app', async () => {
    vi.stubEnv('PROD', true)
    await import('./main')
    expect(registerServiceWorker).toHaveBeenCalledOnce()
  })
})
