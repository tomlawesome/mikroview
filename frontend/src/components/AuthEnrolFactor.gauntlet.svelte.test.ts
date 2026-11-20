// SPDX-License-Identifier: AGPL-3.0-only
//
// #1202: the door under gauntlet. Two changes reach AuthEnrolFactor
// (docs/decisions/gauntlet-adoption.md decision 4, build notes A3, B7):
//
//  - spec 32: an admin held at must-enrol-passkey sees the same door with
//    passkey-only wording and no authenticator-app option;
//  - spec 34: a first factor is held with its ten codes until
//    POST /api/auth/recovery-codes/confirm, so "I have saved these" calls
//    confirm first and only then re-reads the session. Without the
//    `pendingConfirmation` field (a factor that went live at once) it behaves as before.
//
// Same mocking as AuthEnrolFactor.svelte.test.ts: lib/api.ts is faked at the
// network edge (the real module spread in, so ApiError stays real), the
// passkey ceremony is faked whole, and AuthState is the real one.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>()
  return {
    ...actual,
    fetchAuthSession: vi.fn(),
    login: vi.fn(),
    logout: vi.fn(),
    register: vi.fn(),
    setNewPasswordAfterReset: vi.fn(),
    signOutEverywhere: vi.fn(),
    submitLoginFactor: vi.fn(),
    enrolTOTP: vi.fn(),
    confirmTOTP: vi.fn(),
    confirmRecoveryCodes: vi.fn(),
    fetchPersistence: vi.fn(),
    fetchMyPreferences: vi.fn(),
    saveMyPreferences: vi.fn(),
  }
})

vi.mock('../lib/passkeys.svelte', () => ({
  registerPasskey: vi.fn(),
  loginWithPasskey: vi.fn(),
  passkeysSupported: vi.fn(() => true),
}))

vi.mock('../lib/qrcode', () => ({
  qrCode: () => {},
}))

import { confirmRecoveryCodes, confirmTOTP, enrolTOTP, fetchAuthSession, fetchMyPreferences, saveMyPreferences } from '../lib/api'
import { registerPasskey } from '../lib/passkeys.svelte'
import { authState, pageReload } from '../lib/auth.svelte'
import { preferencesState } from '../lib/preferences.svelte'
import AuthEnrolFactor from './AuthEnrolFactor.svelte'

const TEN_CODES = [
  'p2xk-9qfw',
  'j6nm-4rvt',
  'w8cd-1lsz',
  'm3hg-7ypb',
  'q9vr-5kdn',
  't4bj-8wmf',
  'x1sl-6czh',
  'd7pn-3qgy',
  'f5wt-2jrk',
  'b8mz-9xvc',
]

const PASSKEY = {
  id: 'cred-1',
  name: 'this laptop',
  createdAt: '2026-10-10T10:00:00Z',
  transports: [],
  stale: false,
  rpId: 'view.brandt.example',
}

// A session that has come out of the door: nothing is held any more.
const OPEN_SESSION = {
  setupRequired: false,
  authenticated: true,
  username: 'meredith',
  role: 'admin' as const,
  mustEnrolSecondFactor: false,
  mustEnrolPasskey: false,
  ssoAvailable: false,
}

// A promise the test settles by hand, to look at the screen mid-call.
function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

// Lets any call the click set off run to its first await.
async function flush() {
  await new Promise((r) => setTimeout(r, 0))
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  authState.state = 'must-enrol-factor'
  authState.mustEnrolSecondFactor = true
  authState.mustEnrolPasskey = false
  authState.username = 'meredith'
  authState.role = 'viewer'
  authState.hasTOTP = false
  authState.passkeyCount = 0
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
  preferencesState.reset()
  vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
  vi.mocked(saveMyPreferences).mockResolvedValue(null)
})

async function enterTotpStage() {
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
  })
  await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /^continue$/i }))
  await screen.findByLabelText('Code from the app')
}

async function confirmTotpCode() {
  await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
  await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
  await screen.findByTestId('recovery-codes')
}

// Reaches the passkey name-and-password stage from either door: the any-factor
// door has a "Passkey / set it up" key to choose first, the passkey-only door
// may go straight there.
async function enterPasskeyStage() {
  const keys = screen.queryAllByRole('button', { name: /set it up/i })
  if (keys.length > 0) await fireEvent.click(keys[keys.length - 1])
  await screen.findByLabelText('Name')
}

async function addPasskey() {
  await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
  await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
  await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))
  await screen.findByTestId('recovery-codes')
}

// B7 item 32 (the screen half).
describe('the passkey-only door (must-enrol-passkey, #1202 spec 32)', () => {
  beforeEach(() => {
    authState.state = 'must-enrol-passkey'
    authState.mustEnrolSecondFactor = false
    authState.mustEnrolPasskey = true
    authState.role = 'admin'
  })

  it('offers no authenticator-app option, and still offers a passkey', () => {
    render(AuthEnrolFactor)

    expect(screen.queryByRole('button', { name: /authenticator app/i })).toBeNull()
    expect(screen.queryByText(/authenticator app/i)).toBeNull()
    expect(screen.getAllByText(/passkey/i).length).toBeGreaterThan(0)
    expect(screen.queryAllByRole('button', { name: /set it up|add passkey/i }).length).toBeGreaterThan(0)
  })

  // Pair for the above: the ordinary any-factor door keeps both options, so the
  // test cannot pass merely because the app option never renders.
  it('the any-factor door (must-enrol-factor) still offers the authenticator app beside the passkey', () => {
    authState.state = 'must-enrol-factor'
    authState.mustEnrolSecondFactor = true
    authState.mustEnrolPasskey = false

    render(AuthEnrolFactor)

    expect(screen.getByRole('button', { name: /authenticator app/i })).toBeTruthy()
    expect(screen.getAllByRole('button', { name: /set it up/i }).length).toBe(2)
  })

  it('has no way past it and no way across to the authenticator app, from the passkey stage either', async () => {
    const { container } = render(AuthEnrolFactor)
    await enterPasskeyStage()

    expect(container.querySelector('a')).toBeNull()
    expect(screen.queryByRole('button', { name: /cancel|close|skip|sign out|log out/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /authenticator app/i })).toBeNull()
  })

  it('registers a passkey from here with the name and the password', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: TEN_CODES })

    render(AuthEnrolFactor)
    await enterPasskeyStage()
    await addPasskey()

    expect(registerPasskey).toHaveBeenCalledWith('this laptop', 'hunter2')
    expect(authState.passkeyCount).toBe(1)
  })

  it('an admin who already has a factor gets no codes screen, and opens the app once the session says the door is open', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: null })
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    render(AuthEnrolFactor)
    await enterPasskeyStage()
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(screen.queryByTestId('recovery-codes')).toBeNull()
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()
  })
})

// B7 item 34 (the screen half): the save button confirms a held factor.
describe('"I have saved these" confirms a held first factor (#1202 spec 34)', () => {
  it('after an authenticator-app confirm with pendingConfirmation, calls recovery-codes/confirm and only then re-reads the session', async () => {
    const confirmed = deferred<null>()
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: TEN_CODES, alreadyIssued: false, pendingConfirmation: true })
    vi.mocked(confirmRecoveryCodes).mockReturnValue(confirmed.promise)
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    render(AuthEnrolFactor)
    await enterTotpStage()
    await confirmTotpCode()

    // Showing the codes confirms nothing: the factor is still on hold.
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
    await flush()

    expect(confirmRecoveryCodes).toHaveBeenCalledTimes(1)
    // The call has not been answered: the session is not re-read and the door has not opened.
    expect(fetchAuthSession).not.toHaveBeenCalled()
    expect(authState.state).toBe('must-enrol-factor')

    confirmed.resolve(null)

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(fetchAuthSession).toHaveBeenCalled()
    expect(vi.mocked(confirmRecoveryCodes).mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(fetchAuthSession).mock.invocationCallOrder[0],
    )
  })

  // Pair for the above: without the field the button never calls confirm.
  it('without pendingConfirmation (gauntlet omits it unless true), does not call recovery-codes/confirm and re-reads the session as before', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: TEN_CODES, alreadyIssued: false })
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    render(AuthEnrolFactor)
    await enterTotpStage()
    await confirmTotpCode()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()
    expect(fetchAuthSession).toHaveBeenCalled()
  })

  it('with pendingConfirmation explicitly false, does not call recovery-codes/confirm', async () => {
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: TEN_CODES, alreadyIssued: false, pendingConfirmation: false })
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    render(AuthEnrolFactor)
    await enterTotpStage()
    await confirmTotpCode()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()
  })

  it('after a passkey registration with pendingConfirmation, calls recovery-codes/confirm and only then re-reads the session', async () => {
    const confirmed = deferred<null>()
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: TEN_CODES, pendingConfirmation: true })
    vi.mocked(confirmRecoveryCodes).mockReturnValue(confirmed.promise)
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    render(AuthEnrolFactor)
    await enterPasskeyStage()
    await addPasskey()

    expect(confirmRecoveryCodes).not.toHaveBeenCalled()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
    await flush()

    expect(confirmRecoveryCodes).toHaveBeenCalledTimes(1)
    expect(fetchAuthSession).not.toHaveBeenCalled()
    expect(authState.state).toBe('must-enrol-factor')

    confirmed.resolve(null)

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(fetchAuthSession).toHaveBeenCalled()
  })

  it('after a passkey registration without the field, does not call recovery-codes/confirm', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: TEN_CODES })
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    render(AuthEnrolFactor)
    await enterPasskeyStage()
    await addPasskey()

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(confirmRecoveryCodes).not.toHaveBeenCalled()
  })

  it('in the passkey-only door, a held first passkey is confirmed before the door opens', async () => {
    authState.state = 'must-enrol-passkey'
    authState.mustEnrolSecondFactor = false
    authState.mustEnrolPasskey = true
    authState.role = 'admin'
    vi.mocked(registerPasskey).mockResolvedValue({ passkey: PASSKEY, recoveryCodes: TEN_CODES, pendingConfirmation: true })
    vi.mocked(confirmRecoveryCodes).mockResolvedValue(null)
    vi.mocked(fetchAuthSession).mockResolvedValue(OPEN_SESSION)

    render(AuthEnrolFactor)
    await enterPasskeyStage()
    await addPasskey()
    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(confirmRecoveryCodes).toHaveBeenCalledTimes(1)
  })
})
