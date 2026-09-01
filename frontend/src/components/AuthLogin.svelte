<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { authState } from '../lib/auth.svelte'
  import AuthScreen from './AuthScreen.svelte'

  // The way out (#645, round 5): a sign-out plays the door's beat in
  // reverse before the ordinary entrance. authState.consumeJustSignedOut()
  // reads and clears the one-shot flag AuthState.logout() sets, so a
  // plain page load (never signed out) never replays it. Read once at
  // mount, matching AuthState's other consume-once URL flags.
  const reverseBeat = authState.consumeJustSignedOut()
</script>

<!-- No title: on the door the framed wordmark is the title, and the
     submit is the scene's own "Enter" (round-29 door, #645). -->
<AuthScreen
  submitLabel="Enter"
  onsubmit={(username, password) => authState.login(username, password)}
  ssoAvailable={authState.ssoAvailable}
  {reverseBeat}
/>
