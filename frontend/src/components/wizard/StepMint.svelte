<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Step 2 · Mint the token (DESIGN.md, "The steps, in detail"; #1382):
  // the password, asked at the moment of minting (#1291: minting is
  // what opens the log port for the router's address, for 15 minutes,
  // so it asks every time; Reroll comes back here and asks again) --
  // ported from the prototype's passBody. Enter here and the footer's
  // "Mint the token" (Wizard.svelte) both run wizardRun.mint(), which
  // makes the router record if the walk has none yet and spends the
  // password on the one call. A refusal reads back under the field.
  // The token itself is never shown here: it goes at the end of the
  // block, on the paste step.

  import { wizardState } from '../../lib/wizard.svelte'
  import { wizardRun } from '../../lib/wizardRun.svelte'

  let passEl = $state<HTMLInputElement | null>(null)
  $effect(() => {
    passEl?.focus()
  })
</script>

<h3>Your password, to mint {wizardRun.name}’s token.</h3>
<p class="hint">
  Minting opens the log port for {wizardRun.addr}, for 15 minutes, so it asks for your password at that moment. The
  token goes at the end of the block.
</p>
<div class="form single">
  <div class="v">
    <input
      type="password"
      bind:value={wizardRun.pass}
      bind:this={passEl}
      placeholder="password"
      aria-label="Your password"
      onkeydown={(e) => {
        if (e.key === 'Enter') wizardRun.mint()
      }}
    />
    {#if wizardState.enrolmentError}
      <p class="problem" aria-live="polite">{wizardState.enrolmentError}</p>
    {/if}
  </div>
</div>
