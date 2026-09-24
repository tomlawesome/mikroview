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

  // #1251: after signing in with the one-time code an administrator read
  // out, the door does not open -- it asks for a password of your own
  // first, and there is nothing else on the screen until it has one. The
  // server agrees: this session can reach the change-password route and
  // nothing else, so a half-drawn app would be an app of failed
  // requests. Same door, same beat, one field swapped.
  const settingNewPassword = $derived(authState.state === 'must-change-password')

  // #1249's second step: the password was right, but the account holds
  // a factor -- authState.login() already left this in 'pending-factor'
  // rather than 'authenticated', with the server's own pending cookie
  // carrying the login the rest of the way.
  const enteringCode = $derived(authState.state === 'pending-factor')

  // The wording here has to follow what login() actually told us this
  // account holds (authState.pendingSecondFactor), not assume an
  // authenticator app -- a passkey-only account was being told to open
  // an app it doesn't have. AuthScreen's own factorOnly rendering already
  // picks the right control per entry (passkey button / code box /
  // recovery note); this only has to agree with it in words.
  const hasPasskeyFactor = $derived(authState.pendingSecondFactor.includes('passkey'))
  const hasTotpFactor = $derived(authState.pendingSecondFactor.includes('totp'))
  const factorTitle = $derived(hasPasskeyFactor && !hasTotpFactor ? 'Use your passkey' : 'Enter your code')
  const factorSubtitle = $derived(
    hasPasskeyFactor && hasTotpFactor
      ? "Your password was right. Use your passkey, or enter the current code from your authenticator app, to finish signing in."
      : hasPasskeyFactor
        ? 'Your password was right. Use your passkey to finish signing in.'
        : hasTotpFactor
          ? "Your password was right. Enter the current code from your authenticator app to finish signing in."
          : 'Your password was right. Enter one of your recovery codes to finish signing in.',
  )
</script>

{#if settingNewPassword}
  <AuthScreen
    title="Set a new password"
    subtitle="Your one-time code got you in. Choose a password only you know, and that is the last you'll need the code for."
    submitLabel="Set password"
    passwordOnly
    onsubmit={(_username, password) => authState.setNewPassword(password)}
  />
{:else if enteringCode}
  <AuthScreen
    title={factorTitle}
    subtitle={factorSubtitle}
    submitLabel="Continue"
    factorOnly
    onSubmitFactor={(code) => authState.submitFactor(code)}
    onLoginWithPasskey={() => authState.loginWithPasskey()}
  />
{:else}
  <!-- No title: on the door the framed wordmark is the title, and the
       submit is the scene's own "Enter" (round-29 door, #645). -->
  <AuthScreen
    submitLabel="Enter"
    onsubmit={(username, password) => authState.login(username, password)}
    ssoAvailable={authState.ssoAvailable}
    {reverseBeat}
  />
{/if}
