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
  import { authState } from '../lib/auth.svelte'
  import { journeyState } from '../lib/journey.svelte'
  import AuthScreen from './AuthScreen.svelte'

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
  />
{:else}
  <AuthScreen gate onEnter={() => (entered = true)} />
{/if}
