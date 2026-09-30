<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Step 1 · The router (DESIGN.md, "The steps, in detail"; #1382): one
  // form -- name, address, push Yes / No, backup Yes / No -- ported
  // from the prototype's routerBody. The form binds to the run's
  // answers; the footer's Next (Wizard.svelte) is free once all four
  // are answered, and the rail's receipt reads "name · address · push
  // yes/not now · backup yes/not now" from the same answers.
  //
  // The address is checked as you type (#1380), the problem worded
  // under the field in an aria-live line, with the server's own rule
  // (wizardRun.ts, addrProblem). Nothing here touches the server: the
  // router record is made at the moment of minting, on the next step.
  // The push and backup labels are spans, not labels -- a radiogroup
  // has no one control for a label to point at -- named by
  // aria-labelledby instead.

  import { wizardRun } from '../../lib/wizardRun.svelte'

  const problem = $derived(wizardRun.addrProblem)

  // The router's name field has focus when the step arrives (the
  // record's last beat of the way in).
  let nameEl = $state<HTMLInputElement | null>(null)
  $effect(() => {
    nameEl?.focus()
  })
</script>

<h3>Your first router.</h3>
<p class="hint">
  Four things, then one paste. Nothing touches the router until you paste, and MikroView never connects to it — the
  router sends.
</p>
<div class="form">
  <label class="k" for="f-name">Name<small>What MikroView calls it on the fall, the stream and Entities.</small></label>
  <div class="v">
    <input id="f-name" type="text" bind:value={wizardRun.name} bind:this={nameEl} placeholder="rb5009" autocomplete="off" />
  </div>
  <label class="k" for="f-addr"
    >Its address<small
      >Its own address on the network MikroView sits on. The enrolment window opens for this address alone.</small
    ></label
  >
  <div class="v">
    <input
      id="f-addr"
      type="text"
      bind:value={wizardRun.addr}
      placeholder="192.168.13.1"
      autocomplete="off"
      class:bad={!!problem}
      aria-invalid={problem ? 'true' : 'false'}
    />
    <p class="problem" aria-live="polite">{problem}</p>
  </div>
  <span class="k" id="l-push"
    >Push router state<small>Every 20 minutes the router posts its rule table, address lists, leases and interfaces.</small
    ></span
  >
  <div class="v">
    <div class="seg" role="radiogroup" aria-labelledby="l-push">
      <button type="button" class:on={wizardRun.push === true} role="radio" aria-checked={wizardRun.push === true} onclick={() => (wizardRun.push = true)}>Yes</button>
      <button type="button" class:on={wizardRun.push === false} class:no={wizardRun.push === false} role="radio" aria-checked={wizardRun.push === false} onclick={() => (wizardRun.push = false)}>No</button>
    </div>
  </div>
  <span class="k" id="l-backup"
    >Back up nightly<small
      >At 03:00 the router exports its configuration and posts it, kept encrypted under your key file.</small
    ></span
  >
  <div class="v">
    <div class="seg" role="radiogroup" aria-labelledby="l-backup">
      <button type="button" class:on={wizardRun.backup === true} role="radio" aria-checked={wizardRun.backup === true} onclick={() => (wizardRun.backup = true)}>Yes</button>
      <button type="button" class:on={wizardRun.backup === false} class:no={wizardRun.backup === false} role="radio" aria-checked={wizardRun.backup === false} onclick={() => (wizardRun.backup = false)}>No</button>
    </div>
  </div>
</div>
