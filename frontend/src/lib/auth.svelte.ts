// SPDX-License-Identifier: AGPL-3.0-only

import {
  fetchAuthSession,
  login,
  logout,
  PENDING_LOGIN_EXPIRED,
  register,
  setNewPasswordAfterReset,
  signOutEverywhere,
  submitLoginFactor,
} from "./api";
import { loginWithPasskey as runPasskeyLogin } from "./passkeys.svelte";
import { appState } from "./state.svelte";
import { flagsState } from "./flags.svelte";
import { watchlistState } from "./watchlist.svelte";
import { wizardState } from "./wizard.svelte";
import { logEveryRuleWorkState } from "./logEveryRuleWork.svelte";
import { forgetHistoryKeyForSession } from "./history";
import { tokensState } from "./tokens.svelte";
import { usersState } from "./users.svelte";
import { auditState } from "./audit.svelte";
import { persistenceState } from "./persistence.svelte";
import { configProblemsState } from "./configProblems.svelte";
import { configUpgradeState } from "./configUpgrade.svelte";
import { preferencesState } from "./preferences.svelte";
import type { AuthSession } from "./types";

// 2a of the v0.6.0 audit's #1083 follow-up: after a sign-out or a 401
// bounce the page is reloaded, so nothing held in any module-level
// singleton -- reset or not -- can reach the next account on this tab.
// clearSessionState() still runs first: it keeps the tests honest about
// what each store holds, and covers the one path that must not reload
// (a failed logout, see AuthState.logout). Indirected through an object
// so tests can stub it: jsdom cannot navigate.
export const pageReload = {
  now(): void {
    location.reload();
  },
};

// The way-out beat flag has to outlive the reload, so it rides in
// sessionStorage (tab-scoped, like the wizard's history key) and is
// consumed exactly once by consumeJustSignedOut().
const JUST_SIGNED_OUT_KEY = "mikroview.justSignedOut";

// #1083: signing out (or being bounced by a 401) must not leave this
// account's events, filters, devices, stats or watchlist visible to the
// next person who signs in on this tab -- these are module-level
// singletons that survive a logout/login pair, not a fresh page load.
//
// flagsState (lib/flags.svelte.ts) has no reset() of its own -- another
// agent owns that file for #1083's batch -- so its fields are cleared
// here directly through its existing public API: clearPins() is already
// exported for this, and `list`/`timeSeries`/`loaded`/`baselinesWarming`
// are plain public $state fields with no setter to go through, so they
// are set straight back to what FlagsState's own field initialisers use.
function clearSessionState() {
  appState.reset();
  watchlistState.reset();
  flagsState.list = [];
  flagsState.timeSeries = [];
  flagsState.loaded = false;
  flagsState.baselinesWarming = undefined;
  flagsState.clearPins();
  // wizardState, logEveryRuleWorkState and the wizard's minted history
  // key were all missed when #1083 first went round (v0.6.0 pre-release
  // audit, Security stage). Each is module-lifetime and each carries
  // something the next person to sign in on this tab should not be
  // handed: an ingest bearer token still live for a router, the
  // previous operator's pasted firewall export, and the key that
  // decrypts this instance's stored history.
  wizardState.reset();
  logEveryRuleWorkState.reset();
  forgetHistoryKeyForSession();
  // Same audit, same batch, still missed: tokensState, usersState,
  // auditState, persistenceState and configProblemsState are all
  // admin-only, module-lifetime singletons too. Between them they carry
  // a live API/ingest bearer token, the admin account list, the
  // admin-action log, this deployment's backend/disk info and its
  // config diagnostics -- none of it meant for whoever signs in next on
  // this tab.
  tokensState.reset();
  usersState.reset();
  auditState.reset();
  persistenceState.reset();
  configProblemsState.reset();
  // And configUpgradeState, found a round later still: the same shape
  // as auditState, and the last GET-only admin route (see the
  // accessAdmin rows of internal/api/authz_matrix_test.go) whose
  // answer lived in a module-level store rather than a component.
  configUpgradeState.reset();
  // #1283: the shared per-user preferences record (presets, top-talker
  // widgets, colorway, and the rest of the nine modules that used to
  // read/write localStorage directly). logout() below has already
  // flushed anything pending while the session was still good; this
  // just drops the in-memory copy so the next sign-in on this tab
  // starts from ensureLoaded() again rather than the previous
  // account's cached record.
  preferencesState.reset();
}

// 'loading' only lasts for the initial check() call on app boot; after
// that it's always one of the other three. App.svelte renders a
// different top-level view for each (see appState.view for the same
// independent-view pattern used by Metrics) rather than layering
// anything as a modal.
// 'must-change-password' is a real session that may reach nothing but
// the change-password route (#1251): an administrator reset this account
// and it signed in with the one-time code. It is its own view state
// rather than a flag on 'authenticated' because the app is not open in
// it -- every other request would 403 -- so App.svelte must draw the
// door's set-a-new-password form and nothing else.
// 'pending-factor' is the same shape for #1249's second step: the
// password was right, but the server set a pending cookie rather than a
// session (see login() below) and everything but
// POST /api/auth/login/factor still 403s. Set by login() alone, straight
// from its own response -- never by apply()/check(), since a bare
// GET /api/auth/session has no way to tell "pending" from "signed out"
// apart, and a page reload mid-step is meant to fall back to asking for
// the password again rather than resurrecting this state from nothing.
// 'must-enrol-factor' is #1253's door (#1336, round 61): a real session
// on a local account with no second factor, which may reach nothing but
// the four enrolment routes (internal/api/auth.go's
// secondFactorEnrolPaths). Unlike 'pending-factor' it IS set by
// apply()/check(), straight from the server's own mustEnrolSecondFactor
// on GET /api/auth/session -- the flag is computed from the account,
// not the session, so a page reload mid-enrolment lands back on this
// door rather than falling out of it. 'must-change-password' wins when
// both hold, matching requireAuth's own gate order: a session owing
// both is sent to set a password first.
export type AuthViewState =
  | "loading"
  | "setup-required"
  | "unauthenticated"
  | "must-change-password"
  | "must-enrol-factor"
  | "pending-factor"
  | "authenticated";

class AuthState {
  state = $state<AuthViewState>("loading");
  username = $state("");
  role = $state<"admin" | "user" | "viewer" | "">("");
  // Tier checks (issue #653's three roles: admin ⊇ user ⊇ viewer). An
  // unknown or empty role -- not yet checked, or signed out -- lands in
  // neither, the same "lowest tier, no edit rights" default the pencil
  // gate below already used for a non-admin.
  isAdmin = $derived(this.role === "admin");
  canEdit = $derived(this.role === "admin" || this.role === "user");
  // Whether the backend has OIDC/SSO configured at all -- gates
  // rendering the "Sign in with SSO" link (see AuthLogin.svelte/
  // AuthSetup.svelte). Independent of state above: SSO can be
  // available in every state except 'authenticated'.
  ssoAvailable = $state(false);
  // Whether this account still has a local password. False for an
  // account provisioned through SSO, or one converted by linking --
  // gates whether "Connect SSO" is offered at all.
  hasLocalPassword = $state(true);
  // Whether this account already has an SSO identity attached. The
  // admin keeps its password when it connects (#1252), so this is what
  // says there is nothing left to connect for that account -- offering
  // it again could only mean a second identity, which the server
  // refuses.
  ssoConnected = $state(false);
  // Drives SSOLinkOverlay -- the confirm-and-warn step before an
  // irreversible conversion to SSO-only.
  // Whether the change-password dialog is open (#294 item 4), kept
  // beside showSSOLink because the two are the same kind of thing: an
  // account action reached from the menu.
  showChangePassword = $state(false);
  showSSOLink = $state(false);
  // Whether this account has an active authenticator-app factor.
  // Mirrors sessionResponse.hasTOTP -- set here, read by AccountMenu to
  // decide what its row says and which screen the overlay opens on, and
  // flipped straight after a successful confirm/disable rather than
  // waiting on a full re-check() for something the caller already knows.
  hasTOTP = $state(false);
  // #1250: this account's passkeys, mirroring sessionResponse.passkeys.
  // count/status feed AccountMenu's "Passkeys · N" row and
  // PasskeysOverlay's unavailable copy; origin is set only while status
  // is 'ready', compared against location.origin (see
  // lib/passkeys.svelte.ts's passkeysUsableAt) to catch a capable
  // browser sitting at the wrong address.
  passkeyCount = $state(0);
  passkeyStatus = $state<"ready" | "unset" | "ip" | "insecure">("unset");
  passkeyOrigin = $state<string | undefined>(undefined);
  // Set by login() below when the account holds at least one usable
  // factor of either kind -- which kinds are still owed for *this*
  // pending login, passkey first (server order, see the design). Empty
  // is itself meaningful (#1250): an account whose only factor is a
  // stale passkey still never signs in on the password alone, and
  // AuthScreen falls back to asking for a recovery code with that said
  // in words. pendingPasskeyOrigin is set iff 'passkey' is listed --
  // this pending login's own origin, which may differ from an older
  // stale passkey's if the deployment's address changed since.
  pendingSecondFactor = $state<string[]>([]);
  pendingPasskeyOrigin = $state<string | undefined>(undefined);
  // Set after a successful link (the callback redirects with
  // ?ssoLinked=1), so the UI can confirm what just happened rather than
  // leaving the person to notice their password stopped working.
  ssoLinked = $state(false);
  // Set by consumeSSOErrorFromURL() below after a failed OIDC callback
  // redirect (see internal/api/oidc.go's redirectWithSSOError) --
  // deliberately a fixed message chosen from the opaque error code,
  // never the raw code or any provider-supplied text.
  ssoError = $state<string | null>(null);
  // Set by submitFactor()/loginWithPasskey() below on api.ts's
  // PENDING_LOGIN_EXPIRED -- the 5-minute pending-login cookie outlived
  // whoever was filling in the code box, recovery code or passkey
  // prompt. The pending-factor screen has no timeout of its own to
  // notice this, so without it the code box just sits there answering
  // "invalid code" forever to whatever is typed once the cookie is
  // already gone; AuthScreen reads this the same way it reads ssoError,
  // as a banner on the password form this falls back to.
  signInTimedOut = $state(false);
  // Mirrors sessionResponse.mustChangePassword. Kept beside `state`
  // rather than replacing it so the rest of the app can keep asking the
  // one question it asks today ("are we signed in?") without learning
  // about the reset flow.
  mustChangePassword = $state(false);
  // Mirrors sessionResponse.mustEnrolSecondFactor (#1336), kept beside
  // `state` for the same reason mustChangePassword above is.
  mustEnrolSecondFactor = $state(false);
  // #677's sessions row ("this device ... signed in 4 d") -- this
  // session's own IssuedAt, RFC3339, from sessionResponse.signedInSince.
  // Empty while unauthenticated or against an older server.
  signedInSince = $state("");
  // Set by logout() below, consumed once by AuthLogin.svelte via
  // consumeJustSignedOut() -- the door's "way out" (#645, round 5)
  // plays its beat in reverse only when this mount followed an actual
  // sign-out, never a plain page load or a 401 bounce
  // (handleUnauthorized() deliberately leaves this alone: that is a
  // forced session expiry, not the door's way-out beat).
  justSignedOut = $state(false);

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
        "Your account signed in successfully but is not permitted to use this MikroView. Contact whoever administers it.";
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

  // Reads and clears the one-shot flag logout() sets -- AuthLogin.svelte
  // calls this once at mount to decide whether to play the door's way-
  // out beat before its ordinary entrance.
  consumeJustSignedOut(): boolean {
    // Two readers, one each: the old page's login screen mounts in the
    // instant between logout() flipping the state and the reload landing,
    // and it must take only the in-memory flag -- if it also stripped the
    // storage key, the reloaded page (memory flag gone, only storage
    // left) would find nothing and skip the way-out beat.
    if (this.justSignedOut) {
      this.justSignedOut = false;
      return true;
    }
    const was = sessionStorage.getItem(JUST_SIGNED_OUT_KEY) === "1";
    sessionStorage.removeItem(JUST_SIGNED_OUT_KEY);
    return was;
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
      this.mustChangePassword = session.mustChangePassword ?? false;
      this.mustEnrolSecondFactor = session.mustEnrolSecondFactor ?? false;
      // The password change comes first when both are owed, matching
      // requireAuth's gate order (see AuthViewState's own comment).
      this.state = this.mustChangePassword
        ? "must-change-password"
        : this.mustEnrolSecondFactor
          ? "must-enrol-factor"
          : "authenticated";
      this.username = session.username ?? "";
      this.role = (session.role as "admin" | "user" | "viewer") ?? "";
      // Absent on an older server: treated as "has one", which only
      // ever offers a link that the server would then refuse -- the
      // safe direction to be wrong in.
      this.hasLocalPassword = session.hasLocalPassword ?? true;
      this.ssoConnected = session.ssoConnected ?? false;
      this.signedInSince = session.signedInSince ?? "";
      this.hasTOTP = session.hasTOTP ?? false;
      this.passkeyCount = session.passkeys?.count ?? 0;
      this.passkeyStatus = session.passkeys?.status ?? "unset";
      this.passkeyOrigin = session.passkeys?.origin;
      // #1283: the one place preferences load from the server, rather
      // than each of the nine modules reading localStorage at import
      // time. Gated on the real 'authenticated' view, not
      // 'must-change-password' -- that session 403s everything but the
      // set-a-new-password route (see AuthViewState's own comment), so
      // there is nothing yet for this account to fetch or apply. Not
      // awaited -- check() runs on every boot and re-check (login,
      // register, signOutEverywhere), and ensureLoaded() is itself
      // idempotent (a no-op once loaded, joins the same fetch if one is
      // already in flight), so calling it here on every pass is cheap
      // and simpler than tracking "did this transition freshly into
      // authenticated" separately.
      if (this.state === "authenticated") void preferencesState.ensureLoaded();
    } else {
      this.state = "unauthenticated";
      this.username = "";
      this.role = "";
      this.hasLocalPassword = true;
      this.ssoConnected = false;
      this.mustChangePassword = false;
      this.mustEnrolSecondFactor = false;
      this.signedInSince = "";
      this.hasTOTP = false;
      this.passkeyCount = 0;
      this.passkeyStatus = "unset";
      this.passkeyOrigin = undefined;
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


  // A right password on an account holding a factor (#1249) does not
  // sign the caller in -- login() answers with the methods still owed
  // instead of an error, told apart from failure by its own return shape
  // (see LoginPendingFactor). That lands here as 'pending-factor' rather
  // than a re-check(): the server issued a pending cookie, not a session,
  // and GET /api/auth/session has no field that would tell "pending"
  // apart from "signed out" if this asked it right now.
  async login(username: string, password: string): Promise<string | null> {
    const result = await login(username, password);
    if (typeof result === "string") return result;
    if (result) {
      this.pendingSecondFactor = result.secondFactor;
      this.pendingPasskeyOrigin = result.passkeyOrigin;
      this.state = "pending-factor";
      return null;
    }
    await this.check();
    return null;
  }

  // submitFactor is the second step: the pending cookie login() above
  // left behind carries this request, whichever of a TOTP code or a
  // recovery code AuthLogin's box held -- the server is what tells them
  // apart, not this call. Success re-checks the session the same way
  // login() does, since this is what actually signs the caller in.
  async submitFactor(code: string): Promise<string | null> {
    const err = await submitLoginFactor(code);
    if (err) return this.pendingFactorFailed(err);
    await this.check();
    return null;
  }

  // loginWithPasskey is the passkey alternative to submitFactor above,
  // for the same pending cookie -- lib/passkeys.svelte.ts carries the
  // actual ceremony (begin -> browser prompt -> finish); this just wires
  // its result into the same re-check() every other successful step here
  // already does.
  async loginWithPasskey(): Promise<string | null> {
    const err = await runPasskeyLogin();
    if (err) return this.pendingFactorFailed(err);
    await this.check();
    return null;
  }

  // Shared by submitFactor/loginWithPasskey above: both answer 401 with
  // the exact same PENDING_LOGIN_EXPIRED text once the pending-login
  // cookie has already expired -- the one case here that isn't an
  // ordinary wrong code/assertion for AuthScreen's factorOnly box to
  // show inline. That case instead falls back to the password form the
  // same way an unauthenticated visitor sees it, with a word for why
  // rather than a code box nothing can ever satisfy again. Returns what
  // the caller (submitFactor/loginWithPasskey) should itself return, so
  // each stays a one-line `if (err) return ...`.
  private pendingFactorFailed(err: string): string | null {
    if (err !== PENDING_LOGIN_EXPIRED) return err;
    this.state = "unauthenticated";
    this.pendingSecondFactor = [];
    this.pendingPasskeyOrigin = undefined;
    this.signInTimedOut = true;
    return null;
  }

  // setNewPassword completes a forced change: the session established
  // with a one-time code trades it for a password only its owner knows,
  // and the app opens. No current password is asked for because there is
  // none -- the reset replaced it with an unmatchable hash, and the code
  // was spent by the login that got here.
  async setNewPassword(newPassword: string): Promise<string | null> {
    const err = await setNewPasswordAfterReset(newPassword);
    if (err) return err;
    await this.check();
    return null;
  }

  // The local session is cleared either way, deliberately: a user who
  // pressed Sign out must not be left looking signed in because the
  // request failed. The error is returned so the caller can say the
  // server-side session may still be live, which is the part that
  // actually matters to them -- and that is also why the reload only
  // happens on success: with the cookie still valid, a reload would
  // check() straight back into the account the user just tried to
  // leave.
  async logout(): Promise<string | null> {
    // #1283: send any preference change still sitting inside its 500ms
    // debounce window before the server call below ends the session --
    // clearSessionState() further down drops the in-memory record
    // (preferencesState.reset()) but does not itself flush, since by
    // then the session may already be gone.
    await preferencesState.flush();
    const err = await logout();
    this.state = "unauthenticated";
    this.username = "";
    this.role = "";
    this.mustChangePassword = false;
    this.mustEnrolSecondFactor = false;
    this.pendingSecondFactor = [];
    this.pendingPasskeyOrigin = undefined;
    this.justSignedOut = true;
    clearSessionState();
    if (err) return err;
    sessionStorage.setItem(JUST_SIGNED_OUT_KEY, "1");
    pageReload.now();
    return null;
  }

  // #1253: called by AuthenticatorOverlay/PasskeysOverlay when
  // disableTOTP/disablePasskey answers signedOut -- removing the
  // account's last second factor, which the server already turned into
  // signing the caller out everywhere, this browser's session included.
  // logout()'s own local half, minus its network call: that call would
  // only 401 against a session the server has already revoked, and the
  // removal itself is the action being confirmed here, not a second one.
  async signOutAfterFactorRemoved(): Promise<void> {
    await preferencesState.flush();
    this.state = "unauthenticated";
    this.username = "";
    this.role = "";
    this.mustChangePassword = false;
    this.mustEnrolSecondFactor = false;
    this.pendingSecondFactor = [];
    this.pendingPasskeyOrigin = undefined;
    this.justSignedOut = true;
    clearSessionState();
    sessionStorage.setItem(JUST_SIGNED_OUT_KEY, "1");
    pageReload.now();
  }

  // signOutEverywhere is #677's sessions row action. Unlike logout()
  // above, the caller stays signed in on this tab -- the server issues
  // a fresh session in the same response (see handleAuthLogoutAll) --
  // so this just re-reads the session to pick up the new
  // signedInSince, rather than dropping to 'unauthenticated'.
  async signOutEverywhere(): Promise<string | null> {
    const err = await signOutEverywhere();
    if (!err) await this.check();
    return err;
  }

  // Called by any fetch wrapper that gets a 401 mid-session (an expired
  // or reset-invalidated session). The state guard is what stops a
  // burst of 401s from several in-flight polls reloading more than
  // once.
  handleUnauthorized() {
    if (
      this.state === "authenticated" ||
      this.state === "must-change-password" ||
      this.state === "must-enrol-factor"
    ) {
      this.state = "unauthenticated";
      this.username = "";
      this.role = "";
      this.mustChangePassword = false;
      this.mustEnrolSecondFactor = false;
      clearSessionState();
      pageReload.now();
    }
  }
}

export const authState = new AuthState();
