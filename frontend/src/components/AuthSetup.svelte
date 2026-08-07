<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Shown while zero accounts exist (AuthSession.setupRequired) --
  // whoever completes this becomes the super-admin. See
  // docs/configuration.md's "Authentication" section for why this is a
  // one-time, self-service path rather than open registration.
  import { authState } from '../lib/auth.svelte'
  import AuthScreen from './AuthScreen.svelte'
</script>

<AuthScreen
  title="Create the admin account"
  subtitle="No account exists yet. Whoever completes this form becomes the admin."
  submitLabel="Create account"
  confirmPassword
  onsubmit={(username, password) => authState.register(username, password)}
  ssoAvailable={authState.ssoAvailable}
/>
