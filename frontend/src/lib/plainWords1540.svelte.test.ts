// SPDX-License-Identifier: AGPL-3.0-only
//
// #1540 (C4): the stores that pass the server's text through also pass the
// client's own texts through -- "fetchHostDossier: 503" (a function name and
// a status) and the browser's "Failed to fetch". The operator reads plain
// words instead: lib/api.ts's own register for a status ("the server could
// not do that (503)"), and "can't reach MikroView — check the connection and
// try again" when no answer came at all. Driven through the real api module
// with only the network stubbed, so it holds wherever the mapping lives.
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dossierState } from './dossier.svelte'
import { ipLookupState } from './ipLookup.svelte'
import { routerLookupState } from './routerLookup.svelte'

const NO_ANSWER = "can't reach MikroView — check the connection and try again"

async function settle() {
  for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 0))
}

function serverDown(status: number) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response('', { status })))
}

function networkDown() {
  vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('Failed to fetch'))))
}

afterEach(() => {
  vi.unstubAllGlobals()
  dossierState.close()
  ipLookupState.close()
  routerLookupState.close()
})

describe('client texts become plain words (#1540 C4)', () => {
  it('dossier: a 503 reads "the server could not do that (503)", not "fetchHostDossier: 503"', async () => {
    serverDown(503)
    dossierState.open('192.0.2.7')
    await settle()
    expect(dossierState.error).toBe('the server could not do that (503)')
  })

  it('dossier: no answer at all reads as the plain no-connection words, not "Failed to fetch"', async () => {
    networkDown()
    dossierState.open('192.0.2.7')
    await settle()
    expect(dossierState.error).toBe(NO_ANSWER)
  })

  it('ipLookup: a 502 and a dropped connection read as plain words', async () => {
    serverDown(502)
    ipLookupState.open('192.0.2.7', new DOMRect(1, 1, 1, 1))
    await settle()
    expect(ipLookupState.error).toBe('the server could not do that (502)')

    networkDown()
    ipLookupState.open('192.0.2.8', new DOMRect(1, 1, 1, 1))
    await settle()
    expect(ipLookupState.error).toBe(NO_ANSWER)
  })

  it('routerLookup: a 500 and a dropped connection read as plain words', async () => {
    serverDown(500)
    routerLookupState.openRule('rb5009', 'drop-bad', new DOMRect(1, 1, 1, 1))
    await settle()
    expect(routerLookupState.error).toBe('the server could not do that (500)')

    networkDown()
    routerLookupState.openRule('rb5009', 'drop-bad', new DOMRect(1, 1, 1, 1))
    await settle()
    expect(routerLookupState.error).toBe(NO_ANSWER)
  })
})
