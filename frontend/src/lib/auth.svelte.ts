// SPDX-License-Identifier: AGPL-3.0-only

import {
  fetchAuthSession,
  login,
  logout,
  register,
} from "./api";
import type { AuthSession } from "./types";

// 'loading' only lasts for the initial check() call on app boot; after
// that it's always one of the other three. App.svelte renders a
// different top-level view for each (see appState.view for the same
// independent-view pattern used by Metrics) rather than layering
// anything as a modal.
export type AuthViewState =
  | "loading"
  | "setup-required"
  | "unauthenticated"
  | "authenticated";

class AuthState {
  state = $state<AuthViewState>("loading");
  username = $state("");
  role = $state<"admin" | "user" | "">("");
  // Drives UsersOverlay -- a modal toggled from the rail's Admin group, mounted at the
  // App root.
  showUsers = $state(false);
  // Drives TokensOverlay (issue #101) -- same pattern as showUsers
  // above, toggled from the rail's admin-only Admin group.
  showTokens = $state(false);
  // Whether the backend has OIDC/SSO configured at all -- gates
  // rendering the "Sign in with SSO" link (see AuthLogin.svelte/
  // AuthSetup.svelte). Independent of state above: SSO can be
  // available in every state except 'authenticated'.
  ssoAvailable = $state(false);
  // Whether this account still has a local password. False for an
  // account provisioned through SSO, or one converted by linking --
  // gates whether "Connect SSO" is offered at all.
  hasLocalPassword = $state(true);
  // Drives SSOLinkOverlay -- the confirm-and-warn step before an
  // irreversible conversion to SSO-only.
  // Whether the change-password dialog is open (#294 item 4), kept
  // beside showSSOLink because the two are the same kind of thing: an
  // account action reached from the menu.
  showChangePassword = $state(false);
  showSSOLink = $state(false);
  // Set after a successful link (the callback redirects with
  // ?ssoLinked=1), so the UI can confirm what just happened rather than
  // leaving the person to notice their password stopped working.
  ssoLinked = $state(false);
  // Set by consumeSSOErrorFromURL() below after a failed OIDC callback
  // redirect (see internal/api/oidc.go's redirectWithSSOError) --
  // deliberately a fixed message chosen from the opaque error code,
  // never the raw code or any provider-supplied text.
  ssoError = $state<string | null>(null);

  // Reads and strips a ?ssoError=<code> query param left by a failed
  // OIDC callback redirect -- called once on App.svelte's mount. Uses
  // history.replaceState (not pushState) so a page refresh afterward
  // doesn't re-show the message, same reasoning appState's filter-sync
  // effect already applies to its own URL updates.
  consumeSSOErrorFromURL() {
    const params = new URLSearchParams(location.search);
    if (!params.has("ssoError")) return;
    // 'not_permitted' is the one code worth distinguishing: the account
    // authenticated correctly and was then refused by this deployment's
    // access policy (see internal/oidc.Policy). Telling that user to
    // "try again" would be advice that can never work. Which condition
    // they failed still isn't disclosed -- that's in the server log.
    // Each message is chosen here from a fixed set keyed on the opaque
    // code -- never the code itself, and never anything the provider
    // supplied. The linking codes are distinguished because "try again"
    // is useless advice for both of them: one needs a different
    // identity, the other needs the person to still be signed in as
    // themselves.
    const code = params.get("ssoError");
    if (code === "not_permitted") {
      this.ssoError =
        "Your account signed in successfully but is not permitted to use this mikroview. Contact whoever administers it.";
    } else if (code === "link_identity_taken") {
      this.ssoError =
        "That SSO identity is already connected to a different account, so it can't be connected to this one. Nothing was changed.";
    } else if (code === "link_session_changed") {
      this.ssoError =
        "You were signed in as someone else by the time SSO came back, so nothing was connected. Sign in again and retry.";
    } else if (code === "link_failed") {
      this.ssoError = "Connecting SSO to your account failed. Nothing was changed.";
    } else {
      this.ssoError = "SSO sign-in failed -- try again, or sign in with your password below.";
    }
    params.delete("ssoError");
    const qs = params.toString();
    history.replaceState(null, "", location.pathname + (qs ? `?${qs}` : ""));
  }

  // Same pattern for the success side: the callback redirects with
  // ?ssoLinked=1 after a successful conversion, stripped here so a
  // refresh doesn't re-announce it.
  consumeSSOLinkedFromURL() {
    const params = new URLSearchParams(location.search);
    if (!params.has("ssoLinked")) return;
    this.ssoLinked = true;
    params.delete("ssoLinked");
    const qs = params.toString();
    history.replaceState(null, "", location.pathname + (qs ? `?${qs}` : ""));
  }

  async check() {
    try {
      const session = await fetchAuthSession();
      this.apply(session);
    } catch {
      // Can't reach the API at all -- treat as unauthenticated rather
      // than stalling on 'loading' forever; the live view's own
      // connection handling already surfaces a disconnected backend.
      this.state = "unauthenticated";
    }
  }

  private apply(session: AuthSession) {
    this.ssoAvailable = session.ssoAvailable;
    if (session.setupRequired) {
      this.state = "setup-required";
    } else if (session.authenticated) {
      this.state = "authenticated";
      this.username = session.username ?? "";
      this.role = (session.role as "admin" | "user") ?? "";
      // Absent on an older server: treated as "has one", which only
      // ever offers a link that the server would then refuse -- the
      // safe direction to be wrong in.
      this.hasLocalPassword = session.hasLocalPassword ?? true;
    } else {
      this.state = "unauthenticated";
      this.username = "";
      this.role = "";
      this.hasLocalPassword = true;
    }
  }

  // register/login/logout are thin wrappers over lib/api.ts's calls,
  // updating local state on success -- register/login return an error
  // string on failure (for the form to display) rather than throwing.
  async register(username: string, password: string): Promise<string | null> {
    const err = await register(username, password);
    if (err) return err;
    await this.check();
    return null;
  }


  async login(username: string, password: string): Promise<string | null> {
    const err = await login(username, password);
    if (err) return err;
    await this.check();
    return null;
  }

  // The local session is cleared either way, deliberately: a user who
  // pressed Sign out must not be left looking signed in because the
  // request failed. The error is returned so the caller can say the
  // server-side session may still be live, which is the part that
  // actually matters to them.
  async logout(): Promise<string | null> {
    const err = await logout();
    this.state = "unauthenticated";
    this.username = "";
    this.role = "";
    return err;
  }

  // Called by any fetch wrapper that gets a 401 mid-session (an expired
  // or reset-invalidated session) -- bounces straight to the login view
  // without a full page reload.
  handleUnauthorized() {
    if (this.state === "authenticated") {
      this.state = "unauthenticated";
      this.username = "";
      this.role = "";
    }
  }
}

export const authState = new AuthState();
