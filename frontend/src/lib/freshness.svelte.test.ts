// SPDX-License-Identifier: AGPL-3.0-only
//
// #1363: after a server upgrade, an already-open tab keeps running the
// bundle it loaded with -- reproduced here first as the gap this file
// closes: fetching a healthz whose version differs from the bundle's
// own baked-in build id (__MIKROVIEW_VERSION__, "dev:local" in every
// test) did nothing at all before freshness.svelte.ts existed. These
// pin the reload/banner split the ratified 2026-09-30 design calls for,
// each busy source, the one-reload-per-version sessionStorage rule, and
// 7a's "manual once shown".

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { authState, onUnauthorizedClaim } from './auth.svelte'
import * as authModule from './auth.svelte'
import { configEditorState } from './configEditor.svelte'
import { dossierState } from './dossier.svelte'
import { freshnessState } from './freshness.svelte'
import { leaveGuard } from './leaveGuard.svelte'
import { appState } from './state.svelte'
import { wanDoorsState } from './wanDoors.svelte'
import { wizardState } from './wizard.svelte'
import { wizardJourney } from './wizardJourney.svelte'

const RELOADED_FOR_KEY = 'mikroview.freshnessReloadedFor'

function serveHealthz(version: string) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      headers: new Headers(),
      json: async () => ({
        status: 'ok',
        time: '2026-09-30T00:00:00Z',
        uptime: '1h',
        uptimeSeconds: 3600,
        version,
        geoip: false,
        geoSource: null,
      }),
    }),
  )
}

/** Every busy source the ratified design lists, as an on/off pair so
 * the same test body can drive each one in turn. */
const busySources: Array<{ name: string; on: () => void; off: () => void }> = [
  {
    name: 'the live view is held',
    on: () => {
      appState.autoscroll = false
    },
    off: () => {
      appState.autoscroll = true
    },
  },
  {
    name: 'an unsaved-edit guard is held',
    on: () => leaveGuard.hold('test-guard'),
    off: () => leaveGuard.release('test-guard'),
  },
  {
    name: "ConfigEditor's edited folds into the unsaved-edit guard",
    on: () => {
      configEditorState.text = 'changed'
    },
    off: () => {
      configEditorState.text = configEditorState.saved
    },
  },
  {
    name: 'a focused text field has content',
    on: () => {
      const input = document.createElement('input')
      input.value = 'typing'
      document.body.appendChild(input)
      input.focus()
    },
    off: () => {
      ;(document.activeElement as HTMLElement | null)?.blur()
      document.querySelectorAll('input').forEach((el) => el.remove())
    },
  },
  {
    name: 'the host dossier is open',
    on: () => {
      dossierState.ip = '10.0.0.1'
    },
    off: () => {
      dossierState.ip = null
    },
  },
  {
    name: 'the WAN doors sheet is open',
    on: () => {
      wanDoorsState.isOpen = true
    },
    off: () => {
      wanDoorsState.isOpen = false
    },
  },
  {
    name: 'the SSO link overlay is open',
    on: () => {
      authState.showSSOLink = true
    },
    off: () => {
      authState.showSSOLink = false
    },
  },
  {
    name: 'the change-password overlay is open',
    on: () => {
      authState.showChangePassword = true
    },
    off: () => {
      authState.showChangePassword = false
    },
  },
  {
    name: 'the setup wizard is open',
    on: () => {
      wizardState.open = true
    },
    off: () => {
      wizardState.open = false
    },
  },
  {
    name: "the wizard's door journey is active",
    on: () => {
      wizardJourney.phase = 'in'
    },
    off: () => {
      wizardJourney.phase = 'idle'
    },
  },
  {
    name: 'the config editor is open',
    on: () => {
      configEditorState.visible = true
    },
    off: () => {
      configEditorState.visible = false
    },
  },
]

beforeEach(() => {
  vi.restoreAllMocks()
  vi.spyOn(authModule.pageReload, 'now').mockImplementation(() => {})
  sessionStorage.clear()
  ;(freshnessState as unknown as { banner: boolean }).banner = false
  ;(freshnessState as unknown as { erroredCheckSpent: boolean }).erroredCheckSpent = false
  ;(freshnessState as unknown as { inflight: unknown }).inflight = null
  freshnessState.setRegistration(undefined)
})

describe('no mismatch', () => {
  it('does nothing when healthz reports the bundle it was built with', async () => {
    serveHealthz('dev:local')
    await freshnessState.checkNow()
    expect(authModule.pageReload.now).not.toHaveBeenCalled()
    expect(freshnessState.banner).toBe(false)
  })
})

describe('busy rule (each source keeps the reload from happening)', () => {
  for (const source of busySources) {
    it(`shows the banner instead of reloading while ${source.name}`, async () => {
      source.on()
      try {
        serveHealthz('v0.6.1')
        await freshnessState.checkNow()
        expect(freshnessState.banner).toBe(true)
        expect(authModule.pageReload.now).not.toHaveBeenCalled()
      } finally {
        source.off()
      }
    })
  }
})

describe('safe reload path', () => {
  it('reloads once automatically, marking the version in sessionStorage', async () => {
    serveHealthz('v0.6.1')
    await freshnessState.checkNow()

    expect(sessionStorage.getItem(RELOADED_FOR_KEY)).toBe('v0.6.1')
    expect(authModule.pageReload.now).toHaveBeenCalledOnce()
    expect(freshnessState.banner).toBe(false)
  })

  it('shows the banner instead of reloading again if still mismatched after that reload', async () => {
    sessionStorage.setItem(RELOADED_FOR_KEY, 'v0.6.1')
    serveHealthz('v0.6.1')

    await freshnessState.checkNow()

    expect(authModule.pageReload.now).not.toHaveBeenCalled()
    expect(freshnessState.banner).toBe(true)
  })

  it('gets its own fresh attempt for a version it has not tried yet', async () => {
    sessionStorage.setItem(RELOADED_FOR_KEY, 'v0.6.1')
    serveHealthz('v0.6.2')

    await freshnessState.checkNow()

    expect(sessionStorage.getItem(RELOADED_FOR_KEY)).toBe('v0.6.2')
    expect(authModule.pageReload.now).toHaveBeenCalledOnce()
    expect(freshnessState.banner).toBe(false)
  })
})

describe('7a: stays manual once shown', () => {
  it('never reloads on its own again after the banner has shown, even once nothing is busy any more', async () => {
    appState.autoscroll = false
    serveHealthz('v0.6.1')
    await freshnessState.checkNow()
    expect(freshnessState.banner).toBe(true)

    appState.autoscroll = true
    await freshnessState.checkNow()

    expect(freshnessState.banner).toBe(true)
    expect(authModule.pageReload.now).not.toHaveBeenCalled()
  })

  it('reload is what the banner itself calls, and it uses the same safe-reload mechanics', async () => {
    appState.autoscroll = false
    serveHealthz('v0.6.1')
    await freshnessState.checkNow()
    appState.autoscroll = true

    await freshnessState.reloadNow()

    expect(authModule.pageReload.now).toHaveBeenCalledOnce()
  })
})

describe('the fetch-error signal (api.ts, 401/403/404/5xx)', () => {
  it('checks at most once per tab', async () => {
    serveHealthz('v0.6.1')
    freshnessState.checkOnErrorSignal()
    freshnessState.checkOnErrorSignal()
    await Promise.resolve()
    await Promise.resolve()

    expect(globalThis.fetch).toHaveBeenCalledTimes(1)
  })
})

// What actually happens to an open tab across a real upgrade, caught by
// scripts/live-freshness-reload.sh in CI (pipeline 1896): the restart
// ends every session, the tab's next poll answers 401 within five
// seconds, and handleUnauthorized reloaded the page at once -- a busy
// one included, typing and all -- before any freshness check had
// answered. Wired here exactly as main.ts wires it.
describe('a 401 from the restart itself (authState.handleUnauthorized)', () => {
  /** Lets the claim's healthz round-trip and everything chained on it run. */
  const settle = async () => {
    for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0))
  }

  beforeEach(() => {
    onUnauthorizedClaim(() => freshnessState.claimUnauthorized())
    authState.state = 'authenticated'
    authState.username = 'tom'
  })

  afterEach(() => {
    onUnauthorizedClaim(null)
    authState.state = 'unauthenticated'
    authState.username = ''
    ;(document.activeElement as HTMLElement | null)?.blur()
    document.querySelectorAll('input').forEach((el) => el.remove())
  })

  it('leaves a busy tab alone on an upgrade: the banner, no reload, the typing kept (7a)', async () => {
    const input = document.createElement('input')
    input.value = 'freshness-busy'
    document.body.appendChild(input)
    input.focus()
    serveHealthz('v0.6.1')

    // A burst, the way several polls answer together.
    authState.handleUnauthorized()
    authState.handleUnauthorized()
    await settle()
    authState.handleUnauthorized()
    await settle()

    expect(freshnessState.banner).toBe(true)
    expect(authModule.pageReload.now).not.toHaveBeenCalled()
    expect(authState.state).toBe('authenticated')
    expect(input.isConnected).toBe(true)
    expect(input.value).toBe('freshness-busy')
  })

  it('lets a quiet tab take the safe reload once, not the 401 bounce on top of it', async () => {
    serveHealthz('v0.6.1')

    authState.handleUnauthorized()
    authState.handleUnauthorized()
    await settle()

    expect(authModule.pageReload.now).toHaveBeenCalledOnce()
    expect(sessionStorage.getItem(RELOADED_FOR_KEY)).toBe('v0.6.1')
    expect(freshnessState.banner).toBe(false)
  })

  it('still bounces a genuine expiry (same build) to sign-in, as before', async () => {
    serveHealthz('dev:local')

    authState.handleUnauthorized()
    await settle()

    expect(authState.state).toBe('unauthenticated')
    expect(authState.username).toBe('')
    expect(authModule.pageReload.now).toHaveBeenCalledOnce()
  })

  it('still bounces when healthz cannot say', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))

    authState.handleUnauthorized()
    await settle()

    expect(authState.state).toBe('unauthenticated')
    expect(authModule.pageReload.now).toHaveBeenCalledOnce()
  })

  it('shares one healthz answer with a check already in flight', async () => {
    serveHealthz('v0.6.1')
    leaveGuard.hold('test-guard')
    try {
      // api.ts fires the error signal on the same 401, just before the
      // poll's own catch reaches handleUnauthorized.
      freshnessState.checkOnErrorSignal()
      authState.handleUnauthorized()
      await settle()

      expect(globalThis.fetch).toHaveBeenCalledTimes(1)
      expect(freshnessState.banner).toBe(true)
      expect(authModule.pageReload.now).not.toHaveBeenCalled()
    } finally {
      leaveGuard.release('test-guard')
    }
  })
})
