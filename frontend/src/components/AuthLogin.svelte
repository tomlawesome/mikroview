<script lang="ts">
  import { authState } from '../lib/auth.svelte'
  import AuthScreen from './AuthScreen.svelte'
</script>

<AuthScreen
  title="Sign in"
  subtitle="Sign in to continue."
  submitLabel="Sign in"
  onsubmit={(username, password) => authState.login(username, password)}
  ssoAvailable={authState.ssoAvailable}
/>
