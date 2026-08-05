import { createUser, fetchAuthSession, login, logout, register, skipAuthSetup } from './api'
import type { AuthSession } from './types'

// 'loading' only lasts for the initial check() call on app boot; after
// that it's always one of the other four. App.svelte renders a
// different top-level view for each (see appState.view for the same
// independent-view pattern used by Metrics) rather than layering
// anything as a modal. 'auth-disabled' means this deployment explicitly
// skipped auth (see internal/auth.Store.Disable) -- a stable, permanent
// state, not "still pending" like 'setup-required'.
export type AuthViewState = 'loading' | 'setup-required' | 'auth-disabled' | 'unauthenticated' | 'authenticated'

class AuthState {
  state = $state<AuthViewState>('loading')
  username = $state('')
  role = $state<'admin' | 'user' | ''>('')
  // Drives AddUserOverlay -- mirrors flagsState.open's pattern (a modal
  // toggled from Toolbar, mounted at the App root).
  showAddUser = $state(false)
  // Whether the backend has OIDC/SSO configured at all -- gates
  // rendering the "Sign in with SSO" link (see AuthLogin.svelte/
  // AuthSetup.svelte). Independent of state above: SSO can be
  // available in every state except 'authenticated'.
  ssoAvailable = $state(false)
  // Set by consumeSSOErrorFromURL() below after a failed OIDC callback
  // redirect (see internal/api/oidc.go's redirectWithSSOError) --
  // deliberately one generic message regardless of the opaque error
  // code, never the raw code/provider text surfaced to the user.
  ssoError = $state<string | null>(null)

  // Reads and strips a ?ssoError=<code> query param left by a failed
  // OIDC callback redirect -- called once on App.svelte's mount. Uses
  // history.replaceState (not pushState) so a page refresh afterward
  // doesn't re-show the message, same reasoning appState's filter-sync
  // effect already applies to its own URL updates.
  consumeSSOErrorFromURL() {
    const params = new URLSearchParams(location.search)
    if (!params.has('ssoError')) return
    this.ssoError = 'SSO sign-in failed -- try again, or sign in with your password below.'
    params.delete('ssoError')
    const qs = params.toString()
    history.replaceState(null, '', location.pathname + (qs ? `?${qs}` : ''))
  }

  async check() {
    try {
      const session = await fetchAuthSession()
      this.apply(session)
    } catch {
      // Can't reach the API at all -- treat as unauthenticated rather
      // than stalling on 'loading' forever; the live view's own
      // connection handling already surfaces a disconnected backend.
      this.state = 'unauthenticated'
    }
  }

  private apply(session: AuthSession) {
    this.ssoAvailable = session.ssoAvailable
    // authDisabled takes priority: once a deployment has explicitly
    // skipped auth, Count()==0 no longer implies "show the choice
    // screen" -- a choice has already been made.
    if (session.authDisabled) {
      this.state = 'auth-disabled'
    } else if (session.setupRequired) {
      this.state = 'setup-required'
    } else if (session.authenticated) {
      this.state = 'authenticated'
      this.username = session.username ?? ''
      this.role = (session.role as 'admin' | 'user') ?? ''
    } else {
      this.state = 'unauthenticated'
      this.username = ''
      this.role = ''
    }
  }

  // register/login/logout are thin wrappers over lib/api.ts's calls,
  // updating local state on success -- register/login return an error
  // string on failure (for the form to display) rather than throwing.
  async register(username: string, password: string): Promise<string | null> {
    const err = await register(username, password)
    if (err) return err
    await this.check()
    return null
  }

  // skip permanently disables auth for this deployment -- see
  // api.ts's skipAuthSetup for why this can't be undone from the UI.
  async skip(): Promise<string | null> {
    const err = await skipAuthSetup()
    if (err) return err
    await this.check()
    return null
  }

  async login(username: string, password: string): Promise<string | null> {
    const err = await login(username, password)
    if (err) return err
    await this.check()
    return null
  }

  async logout() {
    await logout()
    this.state = 'unauthenticated'
    this.username = ''
    this.role = ''
  }

  async createUser(username: string, password: string, role: 'admin' | 'user'): Promise<string | null> {
    return createUser(username, password, role)
  }

  // Called by any fetch wrapper that gets a 401 mid-session (an expired
  // or reset-invalidated session) -- bounces straight to the login view
  // without a full page reload.
  handleUnauthorized() {
    if (this.state === 'authenticated') {
      this.state = 'unauthenticated'
      this.username = ''
      this.role = ''
    }
  }
}

export const authState = new AuthState()
