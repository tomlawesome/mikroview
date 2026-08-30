<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  import { authState } from '../lib/auth.svelte'
  import AuthScreen from './AuthScreen.svelte'
</script>

<AuthScreen
  title="Sign in"
  submitLabel="Sign in"
  onsubmit={(username, password) => authState.login(username, password)}
  ssoAvailable={authState.ssoAvailable}
/>
