// SPDX-License-Identifier: AGPL-3.0-only
//
// navigator.credentials.create()/.get() cannot run in jsdom -- there is
// no real authenticator behind them. Every test here stubs that
// boundary (a resolved/rejected Credential, or the global missing
// entirely) and asserts on what this file's own code does around it:
// which lib/api.ts calls it makes, with what body, and how it turns a
// browser refusal into the text an operator reads. None of it asserts
// that the browser's own prompt appeared -- that's WebKit/Chromium/
// Firefox's problem, covered instead by live-check's virtual
// authenticator (docs/plans/passkeys-second-factor.md's Tests section).

import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  beginPasskeyRegistration: vi.fn(),
  finishPasskeyRegistration: vi.fn(),
  beginPasskeyLogin: vi.fn(),
  submitPasskeyLoginAssertion: vi.fn(),
}))

import {
  beginPasskeyLogin,
  beginPasskeyRegistration,
  finishPasskeyRegistration,
  submitPasskeyLoginAssertion,
} from './api'
import { loginWithPasskey, passkeysSupported, passkeysUsableAt, registerPasskey } from './passkeys.svelte'

// A minimal stand-in for PublicKeyCredential's static JSON helpers --
// real browsers decode the server's base64url fields into the
// ArrayBuffers WebAuthn needs; this file's own code never touches that
// encoding, so the tests only need something callable that hands back
// what it was given.
function stubPasskeySupport() {
  vi.stubGlobal(
    'PublicKeyCredential',
    class {
      static parseCreationOptionsFromJSON(o: unknown) {
        return o
      }
      static parseRequestOptionsFromJSON(o: unknown) {
        return o
      }
    },
  )
  Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
})

describe('passkeysSupported', () => {
  it('is false with no PublicKeyCredential at all (jsdom\'s own default)', () => {
    expect(passkeysSupported()).toBe(false)
  })

  it('is false in an insecure context even with the JSON helpers present', () => {
    stubPasskeySupport()
    Object.defineProperty(window, 'isSecureContext', { value: false, configurable: true })
    expect(passkeysSupported()).toBe(false)
  })

  it('is false when PublicKeyCredential exists but lacks the JSON helper (an old browser)', () => {
    vi.stubGlobal('PublicKeyCredential', class {})
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    expect(passkeysSupported()).toBe(false)
  })

  it('is true once every check passes', () => {
    stubPasskeySupport()
    expect(passkeysSupported()).toBe(true)
  })
})

describe('passkeysUsableAt', () => {
  it('is false with no origin to compare against', () => {
    stubPasskeySupport()
    expect(passkeysUsableAt(undefined)).toBe(false)
  })

  it('is false when the browser is capable but sitting at a different origin', () => {
    stubPasskeySupport()
    expect(passkeysUsableAt('https://mikroview.example.org')).toBe(false)
  })

  it('is true when the browser is capable and already at the given origin', () => {
    stubPasskeySupport()
    expect(passkeysUsableAt(location.origin)).toBe(true)
  })
})

describe('registerPasskey', () => {
  it('refuses locally, with no round trip at all, when the browser cannot do passkeys', async () => {
    const result = await registerPasskey('my key')
    expect(result).toContain("can't create a passkey")
    expect(beginPasskeyRegistration).not.toHaveBeenCalled()
  })

  it('surfaces the begin call\'s own refusal without touching the browser', async () => {
    stubPasskeySupport()
    vi.mocked(beginPasskeyRegistration).mockResolvedValue('passkeys unavailable (ip)')
    vi.stubGlobal('navigator', { credentials: { create: vi.fn() } })

    const result = await registerPasskey('my key')

    expect(result).toBe('passkeys unavailable (ip)')
    expect(vi.mocked((navigator as unknown as { credentials: { create: ReturnType<typeof vi.fn> } }).credentials.create)).not
      .toHaveBeenCalled()
  })

  it('parses the begin response, runs the ceremony, and forwards the credential JSON with the name', async () => {
    stubPasskeySupport()
    const publicKey = { challenge: 'c', rp: { id: 'mikroview.example.org' } }
    vi.mocked(beginPasskeyRegistration).mockResolvedValue({ publicKey })
    const toJSON = vi.fn(() => ({ id: 'cred-1', response: {} }))
    const create = vi.fn(async () => ({ toJSON }))
    vi.stubGlobal('navigator', { credentials: { create } })
    vi.mocked(finishPasskeyRegistration).mockResolvedValue({
      passkey: {
        id: 'cred-1',
        name: 'my key',
        createdAt: '2026-09-23T00:00:00Z',
        transports: [],
        stale: false,
        rpId: 'mikroview.example.org',
      },
      recoveryCodes: ['a', 'b'],
    })

    const result = await registerPasskey('my key')

    expect(create).toHaveBeenCalledWith({ publicKey })
    expect(finishPasskeyRegistration).toHaveBeenCalledWith({ id: 'cred-1', response: {} }, 'my key')
    expect(result).not.toBe(typeof result === 'string' ? result : undefined)
    if (typeof result === 'string') throw new Error('expected a result object')
    expect(result.recoveryCodes).toEqual(['a', 'b'])
  })

  it('turns a cancelled/timed-out browser prompt into one plain-English refusal, whichever DOMException it was', async () => {
    stubPasskeySupport()
    vi.mocked(beginPasskeyRegistration).mockResolvedValue({ publicKey: {} })
    const create = vi.fn(async () => {
      throw new DOMException('the user dismissed the dialog', 'NotAllowedError')
    })
    vi.stubGlobal('navigator', { credentials: { create } })

    const result = await registerPasskey('my key')

    expect(result).toBe("That didn't complete -- try again, or use another way in.")
    expect(finishPasskeyRegistration).not.toHaveBeenCalled()
  })

  it('refuses when the browser resolves with no credential at all', async () => {
    stubPasskeySupport()
    vi.mocked(beginPasskeyRegistration).mockResolvedValue({ publicKey: {} })
    vi.stubGlobal('navigator', { credentials: { create: vi.fn(async () => null) } })

    const result = await registerPasskey('my key')

    expect(typeof result).toBe('string')
    expect(finishPasskeyRegistration).not.toHaveBeenCalled()
  })
})

describe('loginWithPasskey', () => {
  it('refuses locally when the browser cannot do passkeys', async () => {
    const result = await loginWithPasskey()
    expect(result).toContain("can't use a passkey")
    expect(beginPasskeyLogin).not.toHaveBeenCalled()
  })

  it('runs the assertion ceremony and forwards the credential JSON, returning null on success', async () => {
    stubPasskeySupport()
    const publicKey = { challenge: 'c' }
    vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey })
    const toJSON = vi.fn(() => ({ id: 'cred-1', response: {} }))
    const get = vi.fn(async () => ({ toJSON }))
    vi.stubGlobal('navigator', { credentials: { get } })
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValue(null)

    const result = await loginWithPasskey()

    expect(get).toHaveBeenCalledWith({ publicKey })
    expect(submitPasskeyLoginAssertion).toHaveBeenCalledWith({ id: 'cred-1', response: {} })
    expect(result).toBeNull()
  })

  it('surfaces the server\'s own refusal of the assertion', async () => {
    stubPasskeySupport()
    vi.mocked(beginPasskeyLogin).mockResolvedValue({ publicKey: {} })
    vi.stubGlobal('navigator', { credentials: { get: vi.fn(async () => ({ toJSON: () => ({}) })) } })
    vi.mocked(submitPasskeyLoginAssertion).mockResolvedValue("that passkey couldn't be verified")

    const result = await loginWithPasskey()

    expect(result).toBe("that passkey couldn't be verified")
  })
})
