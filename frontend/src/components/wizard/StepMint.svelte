<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Step 2 · Mint the token (DESIGN.md): the password, asked at the
  // moment of minting (#1291) -- ported from the prototype's passBody.
  // The footer's "Mint the token" (Wizard.svelte) spends it on one call
  // through wizardState; a refusal reads back here.
  //
  // The step issue under #1374 fills in the rest: the token's 15-minute
  // life and Reroll are the paste step's line.

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
