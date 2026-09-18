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
  // A successful register() is also the journey's own trigger (#646):
  // journeyState.begin() starts the Attach beat the moment the account
  // exists, and only from here -- an ordinary later sign-in never calls
  // this, so a returning admin gets the plain app, not the walk.
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
  import { journeyState } from '../lib/journey.svelte'
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
  const createSubtitle = $derived(
    authState.ssoAvailable
      ? 'No account exists yet. Whoever completes this form becomes the admin — then you sign in with SSO ' +
        'to connect it, and this password stays as your way in if your provider is ever unreachable.'
      : 'No account exists yet. Whoever completes this form becomes the admin.',
  )

  async function createAdmin(username: string, password: string): Promise<string | null> {
    const err = await register(username, password)
    if (err) return err
    journeyState.begin()

    if (authState.ssoAvailable) {
      const result = await startSSOLink()
      if (typeof result !== 'string') {
        // A real top-level navigation, not a fetch: the provider has to
        // see the browser to show its own sign-in page. The journey
        // begun above goes with the page, which is the cost of the
        // round trip -- the admin comes back to the ordinary app.
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
  function enterApp() {
    void authState.check()
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
    onsubmit={createAdmin}
  />
{:else}
  <AuthScreen gate onEnter={() => (entered = true)} />
{/if}
