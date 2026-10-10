// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202 (spec 34): a first passkey is held, with its ten codes, until the
// codes are confirmed. finishPasskeyRegistration reports that with
// `pendingConfirmation: true`; registerPasskey must hand the field on, or the
// codes screen cannot know to call recovery-codes/confirm.

import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./api', () => ({
  beginPasskeyRegistration: vi.fn(),
  finishPasskeyRegistration: vi.fn(),
  beginPasskeyLogin: vi.fn(),
  submitPasskeyLoginAssertion: vi.fn(),
}))

import { beginPasskeyRegistration, finishPasskeyRegistration } from './api'
import { registerPasskey } from './passkeys.svelte'

function stubCeremony() {
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
  vi.stubGlobal('navigator', {
    credentials: { create: vi.fn(async () => ({ toJSON: () => ({ id: 'cred-1', response: {} }) })) },
  })
  vi.mocked(beginPasskeyRegistration).mockResolvedValue({ publicKey: { challenge: 'c' } })
}

const passkey = {
  id: 'cred-1',
  name: 'my key',
  createdAt: '2026-10-10T00:00:00Z',
  transports: [],
  stale: false,
  rpId: 'mikroview.example.org',
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.unstubAllGlobals()
  Object.defineProperty(window, 'isSecureContext', { value: undefined, configurable: true })
})

describe('registerPasskey and a held first passkey (#1202 spec 34)', () => {
  it('hands on pendingConfirmation: true with the codes', async () => {
    stubCeremony()
    vi.mocked(finishPasskeyRegistration).mockResolvedValue({
      passkey,
      recoveryCodes: ['a', 'b'],
      pendingConfirmation: true,
    })

    const result = await registerPasskey('my key', 'hunter2')

    expect(result).toMatchObject({ recoveryCodes: ['a', 'b'], pendingConfirmation: true })
  })

  it('reports no pending confirmation when the server did not say so', async () => {
    stubCeremony()
    vi.mocked(finishPasskeyRegistration).mockResolvedValue({ passkey, recoveryCodes: ['a', 'b'] })

    const result = await registerPasskey('my key', 'hunter2')

    expect((result as { pendingConfirmation?: boolean }).pendingConfirmation).toBeFalsy()
  })
})
