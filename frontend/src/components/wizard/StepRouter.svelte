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

  import { configProblemsState } from '../../lib/configProblems.svelte'
  import { portOf } from '../../lib/setupsteps'
  import type { RouterBackupSwitchState } from '../../lib/types'
  import { wizardState } from '../../lib/wizard.svelte'
  import { wizardRun } from '../../lib/wizardRun.svelte'
  import RouterBackupOpenDialog from '../RouterBackupOpenDialog.svelte'

  const problem = $derived(wizardRun.addrProblem)

  // The router's name field has focus when the step arrives (the
  // record's last beat of the way in).
  let nameEl = $state<HTMLInputElement | null>(null)
  $effect(() => {
    nameEl?.focus()
  })

  // The drop box, when it is closed (#1361; DESIGN.md, "1 · The
  // router"): the one instance-wide fact this per-router form has to
  // show, because a Yes here prints a script whose backup would have
  // nowhere to arrive. It sits under the Back up nightly choice -- where
  // backups are chosen -- as a caution with one act, "Open it now",
  // which unfolds the same dialog and endpoint Settings → router backups
  // uses (RouterBackupOpenDialog). Yes and No stay what they are: a
  // per-router answer never closes the drop box for the whole instance,
  // and a Yes with the box still closed is allowed -- the operator can
  // open it later from Settings, and this caution stays until then.
  //
  // wizardState.dropBoxClosed is read off the backups poll the wizard
  // already runs (GET /api/router-backups' live port), so nothing new is
  // fetched here, and nothing is said before that read has landed.
  const dropBoxClosed = $derived(wizardState.dropBoxClosed)
  let opening = $state(false)
  // openedPort is the receipt for an open made from here: shown in the
  // caution's place until the step is left, so the act reads as done.
  let openedPort = $state('')

  async function dropBoxOpened(state: RouterBackupSwitchState) {
    opening = false
    openedPort = state.port ?? ''
    await wizardState.dropBoxSwitched(state)
    // The admin banner (configProblems.svelte.ts) carries every switch
    // change for seven days; ask for it now, the way EngineRoom does,
    // so it stands when the wizard lets the shell back in.
    configProblemsState.refresh()
  }
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
    {#if dropBoxClosed}
      <div class="cautionbox dropbox" aria-live="polite">
        <b>The drop box is closed</b> — a backup would have nowhere to arrive.
        {#if !opening}
          <button type="button" class="linkish" onclick={() => (opening = true)}>Open it now</button>, or later from
          Settings → router backups.
        {:else}
          Opening it listens on a new port for the routers to send to.
        {/if}
      </div>
      {#if opening}
        <RouterBackupOpenDialog look="wizard" onopened={dropBoxOpened} oncancel={() => (opening = false)} />
      {/if}
    {:else if openedPort}
      <p class="opened" aria-live="polite">
        The drop box is open on port {portOf(openedPort)} — the router must be able to reach this host there.
      </p>
    {/if}
  </div>
</div>
