<script lang="ts">
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
  skipLabel="Continue without an account"
  skipHint="Anyone who can reach mikroview will be able to view and control it, with no login required. This can only be reversed later from the command line (mikroview -enable-auth-setup) or with access to the container/host -- not from this UI."
  onskip={() => authState.skip()}
/>
