import { createUser, fetchAuthSession, login, logout, register } from './api'

// 'loading' only lasts for the initial check() call on app boot; after
// that it's always one of the other three. App.svelte renders a
// different top-level view for each (see appState.view for the same
// independent-view pattern used by Metrics) rather than layering
// anything as a modal.
export type AuthViewState = 'loading' | 'setup-required' | 'unauthenticated' | 'authenticated'

class AuthState {
  state = $state<AuthViewState>('loading')
  username = $state('')
  role = $state<'admin' | 'user' | ''>('')
  // Drives AddUserOverlay -- mirrors flagsState.open's pattern (a modal
  // toggled from Toolbar, mounted at the App root).
  showAddUser = $state(false)

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

  private apply(session: { setupRequired: boolean; authenticated: boolean; username?: string; role?: string }) {
    if (session.setupRequired) {
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
