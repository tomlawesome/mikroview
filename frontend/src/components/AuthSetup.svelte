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
  import { authState } from '../lib/auth.svelte'
  import AuthScreen from './AuthScreen.svelte'

  let entered = $state(false)
</script>

{#if entered}
  <AuthScreen
    title="Create the admin account"
    subtitle="No account exists yet. Whoever completes this form becomes the admin."
    submitLabel="Create account"
    confirmPassword
    onsubmit={(username, password) => authState.register(username, password)}
    ssoAvailable={authState.ssoAvailable}
  />
{:else}
  <AuthScreen gate onEnter={() => (entered = true)} />
{/if}
