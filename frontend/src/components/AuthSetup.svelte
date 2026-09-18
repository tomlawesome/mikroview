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
  // #1252: SSO is additive, so the setup door does not offer it even
  // when it is configured. The first-ever sign-in creates the admin
  // (auth.Store.FindOrCreateOIDCUser), and an admin created that way
  // has no password -- a provider outage would then lock the whole
  // deployment out, with only `mikroview -transfer-admin` left. Saying
  // so here is the browser half; SECURITY.md carries the rule.
  import { authState } from '../lib/auth.svelte'
  import { journeyState } from '../lib/journey.svelte'
  import AuthScreen from './AuthScreen.svelte'

  const SSO_WITHHELD =
    'SSO is additive, so it is not offered yet: this admin account needs a password of its own, ' +
    'the one way back in if your identity provider is ever unreachable. Create it below — SSO signs ' +
    'everyone in as usual once it exists.'

  let entered = $state(false)

  async function register(username: string, password: string): Promise<string | null> {
    const err = await authState.register(username, password)
    if (!err) journeyState.begin()
    return err
  }
</script>

{#if entered}
  <AuthScreen
    title="Create the admin account"
    subtitle="No account exists yet. Whoever completes this form becomes the admin."
    submitLabel="Create account"
    confirmPassword
    onsubmit={register}
    ssoAvailable={authState.ssoAvailable}
    ssoWithheldReason={SSO_WITHHELD}
  />
{:else}
  <AuthScreen gate onEnter={() => (entered = true)} />
{/if}
