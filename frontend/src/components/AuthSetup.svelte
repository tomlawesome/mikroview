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
  //   - OIDC configured: straight on to the provider to sign in
  //     (startSSOLink), and the identity that comes back is linked to
  //     the admin just created. The proof that it is the right person
  //     is session continuity -- the browser that made the account is
  //     the browser carried to the provider and back -- so no email is
  //     compared, and none is stored.
  //   - No OIDC: the confirmation below, saying the account exists and
  //     where the provider's details go.
  //
  // The admin keeps its password when linked (auth.Store's
  // LinkOIDCIdentity), which is the whole point of the ruling: the
  // deployment always has a way in that does not need the provider.
  import { register, startSSOLink } from '../lib/api'
  import { authState } from '../lib/auth.svelte'
  import { wizardJourney } from '../lib/wizardJourney.svelte'
  import AuthScreen from './AuthScreen.svelte'

  let entered = $state(false)
  let created = $state(false)
  // Set only when SSO was configured and the hand-off to it failed. The
  // account exists by then, so this is a state to explain rather than
  // an error to retry on the creation form.
  let linkError = $state<string | null>(null)

  const confirmation = $derived(
    linkError
      ? `The account is ready, but MikroView couldn't hand you over to your identity provider: ${linkError} ` +
        'Connect SSO from the account menu once it can be reached.'
      : "You're signed in as the admin. To add single sign-on later, put your provider details in the " +
        "oidc section of MikroView's config file and restart — you can connect this account to it " +
        'afterwards, and it keeps this password either way.',
  )

  // Said before the redirect rather than after it: with SSO configured,
  // creating the account sends the browser to the provider, and being
  // bounced somewhere unannounced is how a sign-in page loses someone.
  //
  // #1415: the form also asks for the one-time setup code from the
  // server's log (docs/design/screens/setup-code/DESIGN.md), so both
  // variants say so.
  const createSubtitle = $derived(
    authState.ssoAvailable
      ? "No account exists yet. Whoever completes this form, with the setup code from MikroView's log, becomes " +
        'the admin — then you sign in with SSO to connect it, and this password stays as your way in if your ' +
        'provider is ever unreachable.'
      : "No account exists yet. Whoever completes this form, with the setup code from MikroView's log, becomes the admin.",
  )

  async function createAdmin(setupCode: string, username: string, password: string): Promise<string | null> {
    const err = await register(username, password, setupCode)
    if (err) return err

    if (authState.ssoAvailable) {
      // The account already exists by this point (register() above
      // succeeded), so a thrown exception here -- api.ts's send() turns
      // a dropped connection into an error string, but a body that is
      // not JSON still throws -- must not propagate: it
      // would skip both the redirect and the `created = true` below,
      // leaving AuthScreen's handleSubmit forever mid-`await` and its
      // submit button stuck on "Please wait…" with no way to retry
      // register() against an account that is already made. Treated the
      // same as startSSOLink() answering with an error string: the
      // confirmation screen below, with a working way in.
      let result: { url: string } | string
      try {
        result = await startSSOLink()
      } catch (err) {
        result = err instanceof Error ? err.message : String(err)
      }
      if (typeof result !== 'string') {
        // A real top-level navigation, not a fetch: the provider has to
        // see the browser to show its own sign-in page. The admin comes
        // back to the ordinary app, where the wizard's plain auto-launch
        // opens it without the way in -- the cost of the round trip.
        location.href = result.url
        return null
      }
      linkError = result
    }

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
