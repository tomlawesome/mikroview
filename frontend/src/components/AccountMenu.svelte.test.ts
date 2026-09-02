// SPDX-License-Identifier: AGPL-3.0-only
//
// The account menu, slimmed by #647 (#634 round 23): Settings, Fleet and
// Entities left for cards of their own on the deck (Fleet folded into
// Entities' own card), and Audit log has lived on the docket's tab since
// rounds 17-19 -- so the menu carries no page links at all now, only
// Run setup… (admin-gated), the account actions, and About & licence.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  fetchAuthSession: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(async () => null),
  register: vi.fn(),
  // The menu's foot reads the running build's version and uptime off
  // /api/healthz (versionState, and UptimeBadge via uptimeState).
  fetchHealthz: vi.fn(async () => ({ version: '0.9', uptimeSeconds: 12 * 86_400 + 4 * 3600 })),
}))

import { authState } from '../lib/auth.svelte'

// jsdom has no window.matchMedia -- AccountMenu mounts ThemeMenu, which
// pulls in lib/viewport.svelte.ts; its ViewportState singleton calls
// matchMedia at module-load time, so this has to land before the
// dynamic import below (same fix Flags.svelte.test.ts already needed).
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

const { default: AccountMenu } = await import('./AccountMenu.svelte')

async function openMenu() {
  await fireEvent.click(screen.getByTitle('Account and operate pages'))
  flushSync()
}

beforeEach(() => {
  authState.username = 'tom'
  authState.hasLocalPassword = true
  authState.ssoAvailable = false
})

describe('the slimmed account menu (#647)', () => {
  it("an admin's menu carries no page links -- Run setup… is the only operate row left", async () => {
    authState.role = 'admin'
    render(AccountMenu)
    await openMenu()

    const rows = screen.getAllByRole('menuitem').map((el) => el.textContent?.trim())
    expect(rows).toContain('Run setup…')
    for (const retired of ['Settings', 'Fleet', 'Entities', 'Audit log']) {
      expect(rows).not.toContain(retired)
    }
  })

  it("a viewer's menu has no Run setup… row -- absent, not disabled", async () => {
    authState.role = 'user'
    render(AccountMenu)
    await openMenu()

    const rows = screen.getAllByRole('menuitem').map((el) => el.textContent?.trim())
    expect(rows).not.toContain('Run setup…')
    expect(rows).toContain('Sign out')
    // The About row now carries the build line too (version · licence ·
    // uptime), so it is no longer an exact-match string.
    expect(rows.some((r) => r?.startsWith('About & licence'))).toBe(true)
  })
})

// Rounds 37-38 (#804). Two facts land on this component: who you are,
// said once here rather than on every page, and what build this is,
// beside the licence in the menu's foot.
describe('the account chip declares the read-only viewer once (#804)', () => {
  it('reads "anna (viewer) · read-only" for a viewer', () => {
    authState.username = 'anna'
    authState.role = 'viewer'
    render(AccountMenu)

    expect(screen.getByTitle('Account and operate pages').textContent?.replace(/\s+/g, ' ').trim()).toBe(
      'anna (viewer) · read-only',
    )
  })

  it('reads "tom (admin)" for an admin -- no read-only claim', () => {
    authState.username = 'tom'
    authState.role = 'admin'
    render(AccountMenu)

    const chip = screen.getByTitle('Account and operate pages').textContent?.replace(/\s+/g, ' ').trim()
    expect(chip).toBe('tom (admin)')
    expect(chip).not.toContain('read-only')
  })

  // "user" can edit, so calling that tier read-only would be a plain
  // untruth -- and the drawing gives it no variant of its own.
  it('claims nothing for a user', () => {
    authState.username = 'sam'
    authState.role = 'user'
    render(AccountMenu)

    const chip = screen.getByTitle('Account and operate pages').textContent?.replace(/\s+/g, ' ').trim()
    expect(chip).toBe('sam')
    expect(chip).not.toContain('read-only')
  })
})

describe("the menu's foot carries the build line (#804)", () => {
  it('shows the licence beside About, with uptime when the server has reported it', async () => {
    authState.role = 'admin'
    const { container } = render(AccountMenu)
    await openMenu()

    const ver = container.querySelector('.ver')
    expect(ver).not.toBeNull()
    expect(ver?.textContent).toContain('AGPL-3.0')
  })
})
