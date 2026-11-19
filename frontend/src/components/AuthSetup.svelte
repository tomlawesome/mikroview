<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Shown while zero accounts exist (AuthSession.setupRequired) --
  // whoever completes this becomes the super-admin. See
  // docs/configuration.md's "Authentication" section for why this is a
  // one-time, self-service path rather than open registration.
  //
  // #646's first-run flow (scope note on #645): the door goes in front
  // of this flow rather than replacing it -- the same chrome as the
  // login door, but the submit button's place holds an Enter button
  // (identical look, label "enter"). Clicking it reveals the account
  // creation form below, unchanged from before this issue.
  //
  // What follows creation is the wizard's way in (#1386; owner,
  // 2026-09-30, retiring #646's attach/connecting/glass beats): the
  // confirmation's Continue opens the app and arms the journey, which
  // plays and lands on the wizard.
  //
  // #1252, owner's ruling: first run always creates a local admin, with
  // a username and a password. SSO is never offered as an alternative
  // here -- the local account is step one either way, so there is
  // nothing to withhold and nothing to explain. What follows creation
  // is one of two routes:
  //
  //   - OIDC configured: every new local account is held at the
  //     second-step door (gauntlet's must-enrol-factor gate; oidc/link
  //     is not among the paths it admits), so the first run never hands
  //     off to the provider. The confirmation names the order instead --
  //     a second step first, then the account menu's "Use single
  //     sign-on" (SSOLinkOverlay). There, session continuity plus the
  //     password re-check is the proof that it is the right person, so
  //     no email is compared, and none is stored.
  //   - No OIDC: the confirmation below, saying the account exists and
  //     where the provider's details go.
  //
  // The admin keeps its password when linked (auth.Store's
  // LinkOIDCIdentity), which is the whole point of the ruling: the
  // deployment always has a way in that does not need the provider.
  import { register } from '../lib/api'
  import { authState } from '../lib/auth.svelte'
  import { wizardJourney } from '../lib/wizardJourney.svelte'
  import AuthScreen from './AuthScreen.svelte'

  let entered = $state(false)
  let created = $state(false)
  const confirmation = $derived(
    authState.ssoAvailable
      ? "You're signed in as the admin. Next you add a second step — an authenticator app or a passkey. After " +
        'that, open the account menu and choose Use single sign-on to connect this account to your identity ' +
        'provider; it keeps this password either way.'
      : "You're signed in as the admin. To add single sign-on later, put your provider details in the " +
        "oidc section of MikroView's config file and restart — you can connect this account to it " +
        'afterwards, and it keeps this password either way.',
  )

  // #1415: the form also asks for the one-time setup code from the
  // server's log (docs/design/screens/setup-code/DESIGN.md), so both
  // variants say so.
  const createSubtitle = $derived(
    authState.ssoAvailable
      ? "No account exists yet. Whoever completes this form, with the setup code from MikroView's log, becomes " +
        'the admin — then you add a second step and connect SSO from the account menu. This password stays as ' +
        'your way in if your provider is ever unreachable.'
      : "No account exists yet. Whoever completes this form, with the setup code from MikroView's log, becomes the admin.",
  )

  async function createAdmin(
    setupCode: string,
    username: string,
    password: string,
    _forgetPassword: () => void,
  ): Promise<string | null> {
    const err = await register(username, password, setupCode)
    if (err) return err

    created = true
    return null
  }

  // Nothing here has re-read the session yet, so AuthSetup is still the
  // view -- which is what keeps the confirmation on screen. Continuing
  // is the re-read, and that is what opens the app.
  async function enterApp() {
    await authState.check()
    wizardJourney.signedIn()
  }
</script>

{#if created}
  <AuthScreen
    gate
    title="Admin account created"
    subtitle={confirmation}
    enterLabel="Continue"
    onEnter={enterApp}
  />
{:else if entered}
  <AuthScreen
    title="Create the admin account"
    subtitle={createSubtitle}
    submitLabel="Create account"
    confirmPassword
    setupCode
    onSubmitSetup={createAdmin}
  />
{:else}
  <AuthScreen gate onEnter={() => (entered = true)} />
{/if}
