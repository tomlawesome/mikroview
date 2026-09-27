// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'

// Same approach as AuthLogin.svelte.test.ts: mock lib/api.ts (never
// auth.svelte.ts itself), so these tests exercise the real AuthState
// transitions and the door's real markup with only the network faked.
// Spreads the real module (importOriginal) rather than a bare object
// literal so ApiError stays the real class -- the door catches it with
// `instanceof ApiError` to route a 401 through authState.handleUnauthorized().
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
    fetchPersistence: vi.fn(),
    fetchMyPreferences: vi.fn(),
    saveMyPreferences: vi.fn(),
  }
})

// The passkey ceremony is a browser boundary jsdom cannot cross --
// mocked whole, the same way auth.svelte.test.ts already stubs
// loginWithPasskey (registerPasskey's own behaviour has its own file,
// lib/passkeys.svelte.test.ts).
vi.mock('../lib/passkeys.svelte', () => ({
  registerPasskey: vi.fn(),
  loginWithPasskey: vi.fn(),
}))

// jsdom has no canvas 2D context for the real action to draw into; the
// markup around the canvas is what this file asserts on.
vi.mock('../lib/qrcode', () => ({
  qrCode: () => {},
}))

import {
  ApiError,
  confirmTOTP,
  enrolTOTP,
  fetchAuthSession,
  fetchMyPreferences,
  saveMyPreferences,
  type PasskeyRegistrationFinish,
} from '../lib/api'
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

beforeEach(() => {
  vi.resetAllMocks()
  // handleUnauthorized() reloads the page (jsdom has no real navigation) --
  // stubbed the same way auth.svelte.test.ts and App.svelte.test.ts do.
  vi.spyOn(pageReload, 'now').mockImplementation(() => {})
  authState.state = 'must-enrol-factor'
  authState.mustEnrolSecondFactor = true
  authState.username = 'meredith'
  authState.role = 'viewer'
  authState.hasTOTP = false
  authState.passkeyCount = 0
  // Both keys live by default; the unusable-deployment tests override.
  authState.passkeyStatus = 'ready'
  authState.passkeyOrigin = location.origin
  // apply() fires preferencesState.ensureLoaded() the moment the door
  // opens into 'authenticated' -- give it the same safe default
  // auth.svelte.test.ts does.
  preferencesState.reset()
  vi.mocked(fetchMyPreferences).mockResolvedValue({ version: 1, prefs: {} })
  vi.mocked(saveMyPreferences).mockResolvedValue(null)
})

// Choose the authenticator key and arrive on the prove-it stage.
async function enterTotpStage() {
  vi.mocked(enrolTOTP).mockResolvedValue({
    uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
  })
  await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
  await screen.findByLabelText('Code from the app')
}

describe('AuthEnrolFactor (the forced-enrolment door, #1336)', () => {
  it('offers both factor kinds, the authenticator app first', () => {
    render(AuthEnrolFactor)

    const keys = screen.getAllByRole('button', { name: /set it up/i })
    expect(keys.length).toBe(2)
    expect(keys[0].textContent).toContain('Authenticator app')
    expect(keys[1].textContent).toContain('Passkey')
  })

  it('has no way past it: no links out, no cancel, no skip, no sign-out -- and says so', () => {
    const { container } = render(AuthEnrolFactor)

    // The whole door carries not one anchor -- nothing navigates away.
    expect(container.querySelector('a')).toBeNull()
    expect(screen.queryByRole('button', { name: /cancel|close|skip|sign out|log out/i })).toBeNull()
    // The no-way-out is said, quietly, in the mono caption voice.
    expect(screen.getByText(/no skipping this one/i)).toBeTruthy()
  })

  it('keeps the passkey key visible but disabled, with the one line why, when the deployment cannot offer one', () => {
    authState.passkeyStatus = 'ip'
    authState.passkeyOrigin = undefined

    const { container } = render(AuthEnrolFactor)

    // Never a hidden row (the ratified rule): the key is still drawn...
    const disabled = container.querySelector('.key[aria-disabled="true"]')
    expect(disabled?.textContent).toContain('Passkey')
    // ...but it is not a control, and it says why in the mockup's line.
    expect(screen.getAllByRole('button', { name: /set it up/i }).length).toBe(1)
    expect(screen.getByText(/passkeys need a web address/i)).toBeTruthy()
  })

  it('walks the authenticator through prove-it to the codes, and "I have saved these" opens the app', async () => {
    render(AuthEnrolFactor)

    await enterTotpStage()

    // The mockup's grouped presentation of the same secret the QR holds.
    expect(screen.getByTestId('totp-secret').textContent).toBe(
      'GQ4T MNZV G5UW K2LN MFRG YZLB OR2W CZ3F',
    )

    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: TEN_CODES, alreadyIssued: false })
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    expect(confirmTOTP).toHaveBeenCalledWith('123456')
    expect(authState.hasTOTP).toBe(true)
    expect(screen.getByText('Keep the codes')).toBeTruthy()
    expect(screen.getByTestId('recovery-codes').textContent).toContain('p2xk-9qfw')
    // Still no way out on the codes stage: only "Copy all" and the
    // explicit acknowledgement exist.
    expect(screen.queryByRole('button', { name: /cancel|close|skip/i })).toBeNull()

    // The door has not opened yet -- only the acknowledgement opens it.
    expect(authState.state).toBe('must-enrol-factor')

    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'meredith',
      role: 'viewer',
      mustEnrolSecondFactor: false,
      ssoAvailable: false,
    })
    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
  })

  // Q4-F3 (round 61 door audit): chooseTOTP() mirrors AuthenticatorOverlay's
  // startEnrol() -- a string result is shown inline and the caller stays
  // on the choose stage, same as that overlay's own equivalent test.
  it('shows the enrol error and stays on the choose screen when it fails', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue('the server could not do that (500)')

    render(AuthEnrolFactor)
    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))

    expect(await screen.findByText('the server could not do that (500)')).toBeTruthy()
    expect(screen.queryByTestId('totp-secret')).toBeNull()
  })

  // FR2-F1: a string result now always re-checks the session (see the
  // describe block below) -- a wrong code leaves mustEnrolSecondFactor
  // true, so this still reads as a plain refusal, not the codes-lost line.
  it('shows the server refusal on a wrong code and stays on the door', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })
    vi.mocked(confirmTOTP).mockResolvedValue('invalid code')
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'meredith',
      role: 'viewer',
      mustEnrolSecondFactor: true,
      ssoAvailable: false,
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '000000' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(await screen.findByText('invalid code')).toBeTruthy()
    expect(authState.state).toBe('must-enrol-factor')
    expect(fetchAuthSession).toHaveBeenCalled()
  })

  // This door has no cancel, skip or sign-out -- a 401 from any of its
  // three network calls (most often a second device finishing enrolment
  // first, which ends every other session on the account) has to leave
  // through the same door an ordinary session expiry does, rather than
  // stranding the caller on an error line with nothing else on screen.
  describe('a session that died mid-enrolment (401) leaves through the ordinary door', () => {
    it('from choosing the authenticator app', async () => {
      vi.mocked(enrolTOTP).mockRejectedValue(new ApiError('sign in first', 401))
      render(AuthEnrolFactor)

      await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))

      await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
    })

    it('from confirming the code', async () => {
      vi.mocked(enrolTOTP).mockResolvedValue({
        uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
      })
      vi.mocked(confirmTOTP).mockRejectedValue(new ApiError('sign in first', 401))
      render(AuthEnrolFactor)
      await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
      await screen.findByLabelText('Code from the app')
      await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })

      await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

      await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
    })

    it('from adding a passkey', async () => {
      vi.mocked(registerPasskey).mockRejectedValue(new ApiError('sign in first', 401))
      render(AuthEnrolFactor)
      await fireEvent.click(screen.getAllByRole('button', { name: /set it up/i })[1])
      await screen.findByLabelText('Name')
      await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })

      await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))

      await vi.waitFor(() => expect(pageReload.now).toHaveBeenCalled())
    })
  })

  // A click on "Use a passkey instead" while confirm() is still in
  // flight would move the stage before that request's result lands --
  // the stray result (success or "invalid code") would then apply to
  // the wrong stage. The choose-stage keys already guard against this
  // with disabled={busy}; the switch-method links need the same guard.
  it('disables the switch-to-passkey link while confirming the authenticator code', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })
    let resolveConfirm: (v: { recoveryCodes: string[]; alreadyIssued: boolean }) => void
    vi.mocked(confirmTOTP).mockReturnValue(
      new Promise((resolve) => {
        resolveConfirm = resolve
      }),
    )

    render(AuthEnrolFactor)
    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    expect(screen.getByRole('button', { name: /use a passkey instead/i })).toHaveProperty('disabled', true)

    resolveConfirm!({ recoveryCodes: TEN_CODES, alreadyIssued: false })
    await screen.findByTestId('recovery-codes')
  })

  // Same guard, the other direction: choosePasskey is synchronous, but
  // addPasskey() (the passkey-stage submit) is not, and its own switch
  // link must not be clickable while it is in flight.
  it('disables the switch-to-authenticator-app link while adding a passkey', async () => {
    let resolveRegister: (v: PasskeyRegistrationFinish) => void
    vi.mocked(registerPasskey).mockReturnValue(
      new Promise((resolve) => {
        resolveRegister = resolve
      }),
    )

    render(AuthEnrolFactor)
    await fireEvent.click(screen.getAllByRole('button', { name: /set it up/i })[1])
    await screen.findByLabelText('Name')
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))

    expect(screen.getByRole('button', { name: /use an authenticator app instead/i })).toHaveProperty('disabled', true)

    resolveRegister!({
      passkey: {
        id: 'cred-1',
        name: 'this laptop',
        createdAt: '2026-09-23T10:00:00Z',
        transports: [],
        stale: false,
        rpId: 'view.brandt.example',
      },
      recoveryCodes: TEN_CODES,
    })
    await screen.findByTestId('recovery-codes')
  })

  it('walks the passkey through naming and the ceremony to the codes', async () => {
    vi.mocked(registerPasskey).mockResolvedValue({
      passkey: {
        id: 'cred-1',
        name: 'this laptop',
        createdAt: '2026-09-23T10:00:00Z',
        transports: [],
        stale: false,
        rpId: 'view.brandt.example',
      },
      recoveryCodes: TEN_CODES,
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getAllByRole('button', { name: /set it up/i })[1])
    await screen.findByLabelText('Name')
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))

    await screen.findByTestId('recovery-codes')
    expect(registerPasskey).toHaveBeenCalledWith('this laptop')
    expect(authState.passkeyCount).toBe(1)
    expect(screen.getByText('Keep the codes')).toBeTruthy()
  })

  // Q4-F2 (round 61 door audit): addPasskey() mirrors PasskeysOverlay's
  // submitAdd() -- a string result (the ceremony's own refusal) is shown
  // inline and the caller stays on the passkey stage, same as that
  // overlay's own equivalent test.
  it("shows the passkey ceremony's own refusal and stays put to retry", async () => {
    vi.mocked(registerPasskey).mockResolvedValue("That didn't complete -- try again, or use another way in.")
    // FR2-F1: addPasskey() re-checks the session on any string result --
    // unchanged here, so this still reads as a plain ceremony refusal.
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'meredith',
      role: 'viewer',
      mustEnrolSecondFactor: true,
      ssoAvailable: false,
    })

    render(AuthEnrolFactor)
    await fireEvent.click(screen.getAllByRole('button', { name: /set it up/i })[1])
    await screen.findByLabelText('Name')
    await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
    await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))

    expect(await screen.findByText(/didn't complete/i)).toBeTruthy()
    expect(authState.passkeyCount).toBe(0)
  })

  it('each prove stage offers the other key as the quiet link, when that key is live', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    expect(screen.getByRole('button', { name: /use a passkey instead/i })).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: /use a passkey instead/i }))
    await screen.findByLabelText('Name')
    expect(screen.getByRole('button', { name: /use an authenticator app instead/i })).toBeTruthy()
  })

  it('omits the "use a passkey instead" link when the deployment cannot offer one', async () => {
    authState.passkeyStatus = 'unset'
    authState.passkeyOrigin = undefined
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    expect(screen.queryByRole('button', { name: /use a passkey instead/i })).toBeNull()
  })

  it('opens the app straight from a confirm whose codes were already issued -- there is nothing to show', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: null, alreadyIssued: true })
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'meredith',
      role: 'viewer',
      mustEnrolSecondFactor: false,
      ssoAvailable: false,
    })

    render(AuthEnrolFactor)

    await fireEvent.click(screen.getByRole('button', { name: /authenticator app/i }))
    await screen.findByLabelText('Code from the app')
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    expect(screen.queryByTestId('recovery-codes')).toBeNull()
  })

  it('rains the ratified fullfall behind the door, masked out of its own wider centre', () => {
    const { container } = render(AuthEnrolFactor)

    expect(container.querySelectorAll('.fullfall.enrol i').length).toBe(40)
  })

  // FR2-F1: the server can 500 (or otherwise fail) after already
  // committing the factor -- confirmTOTP/registerPasskey then answer with
  // plain text, indistinguishable by status from an ordinary ceremony
  // refusal. Re-checking the session is what tells them apart: if
  // mustEnrolSecondFactor has flipped to false, the factor is on and only
  // the codes failed to save, so the door offers Enter instead of asking
  // to retry a ceremony that can't be repeated.
  describe('FR2-F1: the factor commits but the recovery codes fail to save', () => {
    function committedSession(): Awaited<ReturnType<typeof fetchAuthSession>> {
      return {
        setupRequired: false,
        authenticated: true,
        username: 'meredith',
        role: 'viewer',
        mustEnrolSecondFactor: false,
        ssoAvailable: false,
      }
    }

    it('confirm(): shows the codes-lost line and Enter when the session says the factor is already on', async () => {
      vi.mocked(enrolTOTP).mockResolvedValue({
        uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
      })
      vi.mocked(confirmTOTP).mockResolvedValue(
        'the passkey is now active, but recovery codes could not be saved',
      )
      vi.mocked(fetchAuthSession).mockResolvedValue(committedSession())

      render(AuthEnrolFactor)
      await enterTotpStage()
      await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
      await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))

      expect(await screen.findByText(/your second step is on, but the recovery codes couldn.t be saved/i)).toBeTruthy()
      expect(screen.getByRole('button', { name: /^enter$/i })).toBeTruthy()
      // The ordinary retry path is gone -- a fresh secret would be needed.
      expect(screen.queryByRole('button', { name: /^confirm$/i })).toBeNull()

      await fireEvent.click(screen.getByRole('button', { name: /^enter$/i }))
      await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    })

    it('addPasskey(): shows the codes-lost line and Enter when the session says the factor is already on', async () => {
      vi.mocked(registerPasskey).mockResolvedValue(
        'the authenticator app is now active, but recovery codes could not be saved',
      )
      vi.mocked(fetchAuthSession).mockResolvedValue(committedSession())

      render(AuthEnrolFactor)
      await fireEvent.click(screen.getAllByRole('button', { name: /set it up/i })[1])
      await screen.findByLabelText('Name')
      await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'this laptop' } })
      await fireEvent.click(screen.getByRole('button', { name: /add passkey/i }))

      expect(await screen.findByText(/your second step is on, but the recovery codes couldn.t be saved/i)).toBeTruthy()
      expect(screen.getByRole('button', { name: /^enter$/i })).toBeTruthy()
      expect(screen.queryByRole('button', { name: /add passkey/i })).toBeNull()
    })
  })
})

// X4-F1: the ten codes exist in clear nowhere else, so a reload before
// the explicit acknowledgement loses them for good -- same guard as
// LogEveryRule's own beforeunload (its own test file's own pattern,
// reused here).
describe('X4-F1: beforeunload guard on the codes stage', () => {
  function dispatchBeforeUnload(): Event {
    const evt = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(evt)
    return evt
  }

  it('is not set before the codes stage is ever reached', () => {
    render(AuthEnrolFactor)
    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)
  })

  // Unlike the overlays, "I have saved these" here doesn't move `stage`
  // away from 'codes' -- it hands off to the app (enter(), which flips
  // authState.state), and it's App.svelte that then unmounts this door
  // in the real app. Simulated here with an explicit unmount(), which is
  // what actually runs the $effect's cleanup and drops the listener.
  it('guards the codes stage, and the guard is gone once the door hands off to the app', async () => {
    vi.mocked(enrolTOTP).mockResolvedValue({
      uri: 'otpauth://totp/MikroView:meredith?secret=GQ4TMNZVG5UWK2LNMFRGYZLBOR2WCZ3F&issuer=MikroView',
    })
    vi.mocked(confirmTOTP).mockResolvedValue({ recoveryCodes: TEN_CODES, alreadyIssued: false })
    vi.mocked(fetchAuthSession).mockResolvedValue({
      setupRequired: false,
      authenticated: true,
      username: 'meredith',
      role: 'viewer',
      mustEnrolSecondFactor: false,
      ssoAvailable: false,
    })

    const { unmount } = render(AuthEnrolFactor)
    await enterTotpStage()
    await fireEvent.input(screen.getByLabelText('Code from the app'), { target: { value: '123456' } })
    await fireEvent.click(screen.getByRole('button', { name: /^confirm$/i }))
    await screen.findByTestId('recovery-codes')

    expect(dispatchBeforeUnload().defaultPrevented).toBe(true)

    await fireEvent.click(screen.getByRole('button', { name: /i have saved these/i }))
    await vi.waitFor(() => expect(authState.state).toBe('authenticated'))
    unmount()

    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)
  })
})
