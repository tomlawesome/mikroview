// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 (spec 32), end to end through the real api.ts: a 403 carrying
// `X-Auth-Gate: must-enrol-passkey` makes the app re-read the session and
// render the passkey-only enrolment screen (no authenticator-app option).
// Only the network edge (global fetch) and the live socket are faked.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'

vi.mock('./lib/ws', () => ({
  liveSocket: { connect: vi.fn(), disconnect: vi.fn(), onChange: vi.fn(() => vi.fn()) },
}))

if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
}

if (typeof ResizeObserver === 'undefined') {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}

if (typeof IntersectionObserver === 'undefined') {
  class FakeIntersectionObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  window.IntersectionObserver = FakeIntersectionObserver as unknown as typeof IntersectionObserver
}

const { default: App } = await import('./App.svelte')
const { authState, pageReload } = await import('./lib/auth.svelte')

const ERRORS_MD = 'https://github.com/tomlawesome/gauntlet/blob/main/docs/api/errors.md'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

// The server as a signed-in admin meets it: the first session read is the
// full one (the page opened fine); every other route is then refused at the
// door named by `gate`, and from then on the session says it is held there.
function serverHeldAt(gate: 'must-enrol-passkey' | 'must-enrol-factor') {
  let sessionReads = 0
  return vi.fn(async (url: string) => {
    if (url === '/api/auth/session') {
      sessionReads++
      const open = sessionReads === 1
      return json({
        setupRequired: false,
        authenticated: true,
        username: 'alex',
        role: 'admin',
        ssoAvailable: false,
        adminPasskeyRequired: gate === 'must-enrol-passkey',
        mustEnrolPasskey: !open && gate === 'must-enrol-passkey',
        mustEnrolSecondFactor: !open && gate === 'must-enrol-factor',
      })
    }
    return new Response(
      JSON.stringify({
        type: `${ERRORS_MD}#${gate}`,
        title: 'held at a door',
        status: 403,
        detail: 'held at a door',
      }),
      { status: 403, headers: { 'Content-Type': 'application/problem+json', 'X-Auth-Gate': gate } },
    )
  })
}

describe('the app and the must-enrol-passkey door (#1202 spec 32)', () => {
  beforeEach(() => {
    vi.spyOn(pageReload, 'now').mockImplementation(() => {})
    authState.state = 'loading'
    authState.role = ''
    authState.mustEnrolPasskey = false
    authState.mustEnrolSecondFactor = false
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('renders the passkey-only enrolment screen after a 403 with X-Auth-Gate: must-enrol-passkey', async () => {
    vi.stubGlobal('fetch', serverHeldAt('must-enrol-passkey'))

    render(App)

    await vi.waitFor(() => expect(authState.state).toBe('must-enrol-passkey'))
    expect(screen.queryByText(/authenticator app/i)).toBeNull()
    expect(screen.queryByRole('button', { name: /authenticator app/i })).toBeNull()
    expect(screen.getAllByText(/passkey/i).length).toBeGreaterThan(0)
  })

  // Pair for the above: the same path through must-enrol-factor lands on the
  // any-factor screen, which does offer the authenticator app. Without it the
  // test above could pass on any screen that merely lacks the option.
  it('renders the any-factor enrolment screen, with the authenticator app, after a 403 with X-Auth-Gate: must-enrol-factor', async () => {
    vi.stubGlobal('fetch', serverHeldAt('must-enrol-factor'))

    render(App)

    await vi.waitFor(() => expect(authState.state).toBe('must-enrol-factor'))
    expect(screen.getByRole('button', { name: /authenticator app/i })).toBeTruthy()
  })
})
