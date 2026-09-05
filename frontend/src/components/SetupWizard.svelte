<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The setup wizard as a modal (#487), replacing the wizard page
  // wholesale. Built from docs/design/screens/wizard/DESIGN.md, the
  // ratified record -- where it and a mockup disagree, the record wins.
  //
  // The model is a claim ledger. Mikroview never connects to a router
  // (the AGENTS.md invariant), so every check here is an observation of
  // what arrived, and each step ends in exactly one of: done, with its
  // receipt; skipped, quietly; or forced past, recorded. The record is
  // the feature -- a forced-past line reaches the step list, the audit
  // log, and every empty state whose silence it explains.
  //
  // Mounted once, beside the other overlays in App.svelte rather than
  // inside the rail that opens it: the rail unmounts when it is docked
  // and does not exist at all on a phone, and the modal outlives both.

  import { createToken, routerBackupDownloadUrl } from '../lib/api'
  import { authState } from '../lib/auth.svelte'
  import { appState } from '../lib/state.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import { trapFocus } from '../lib/focusTrap'
  import { wizardState, FINISH_PANE } from '../lib/wizard.svelte'
  import { journeyState } from '../lib/journey.svelte'
  import { newestGeneration } from '../lib/backups'
  import { HOW_TO_MOUNT_URL } from '../lib/history'
  import {
    announceStep,
    backupReceiptForDevice,
    deviceStanza,
    finishHeadline,
    forcedPastRecord,
    notObserved,
    prose,
    sourceSplits,
    arrivingAddresses,
    srcAddressCommand,
    undeclaredDevices,
    SKIP_CONSEQUENCES,
    STEP_COUNT,
    type LedgerStep,
  } from '../lib/setupsteps'
  import type { RouterosStanding } from '../lib/types'
  import MemoryControl from './MemoryControl.svelte'

  // Steps land seconds to minutes apart (the documented push scheduler
  // runs every 20 minutes), so this polls rather than streaming -- and
  // only while the modal is open, which is why the interval is set up
  // and torn down by the same effect that watches `open`.
  const POLL_MS = 5000

  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  // One read of the ledger on sign-in, whether or not the modal is ever
  // opened: the surfaces that explain their own silence (the Stream's
  // empty state) need the marks, and #490 makes setup status readable by
  // any signed-in user for exactly that kind of reason.
  $effect(() => {
    if (authState.state !== 'authenticated') return
    wizardState.refresh()
  })

  // The record's auto-launch: first admin sign-in with no router
  // sending, after the shell has painted. appState.initialLoadDone is
  // that paint -- the shell has its rail, its ghost rows and its honest
  // empty states before anything is layered over them.
  $effect(() => {
    if (!isAdmin) return
    if (!appState.initialLoadDone) return
    // The journey (#646) owns first-run launch timing on the path it
    // covers -- right after a brand-new instance's admin account is
    // made -- walking Attach, Connecting and the glass before it opens
    // this itself. Deferring here keeps this check exactly as it was
    // for every other path (a returning admin whose router still has
    // nothing sent).
    if (journeyState.active) return
    wizardState.maybeAutoLaunch(appState.devices.length > 0)
  })

  $effect(() => {
    if (!wizardState.open) return
    const timer = setInterval(() => wizardState.refresh(), POLL_MS)
    return () => clearInterval(timer)
  })

  // Step 6's own evidence (#394): the same admin-only endpoint the
  // Settings backups group reads, polled the same way status/devices
  // above are -- and, like them, only while the modal is actually open.
  $effect(() => {
    if (!wizardState.open) return
    wizardState.refreshBackups()
    const timer = setInterval(() => wizardState.refreshBackups(), POLL_MS)
    return () => clearInterval(timer)
  })

  // openLostRouter (#394) always names the router step 6 is about, so
  // the token picker below is pre-set to it rather than left on
  // whichever device step 4 last touched.
  $effect(() => {
    const device = wizardState.lostRouterDevice
    if (device) tokenDevice = device
  })

  const ledger = $derived(wizardState.ledger)
  const step = $derived<LedgerStep | undefined>(
    wizardState.pane <= STEP_COUNT ? ledger[wizardState.pane - 1] : undefined,
  )
  const onFinish = $derived(wizardState.pane === FINISH_PANE)
  const undeclared = $derived(undeclaredDevices(wizardState.devices))

  // The source-address split (#442): each declared device the server has
  // paired with undeclared addresses that are streaming, and those
  // addresses once. Step 2's body below the observation line states
  // both facts and prints the remedy; it never claims the two are one
  // router, because only the operator knows.
  const splits = $derived(sourceSplits(wizardState.devices))
  const arriving = $derived(arrivingAddresses(splits))

  // The heavy warning takes the step body in place, rather than stacking
  // a second dialog on the first. Cleared on every pane change: a
  // warning is about the step it was raised on and nothing else.
  let warning = $state(false)
  let busy = $state(false)
  let copied = $state('')

  // Step 4 acts before it waits: the token is created on entry, with its
  // own audit line, and the script below is written with it already in
  // place. Minting on entry is only unambiguous when mikroview knows
  // exactly one router -- with several, which router the token is scoped
  // to is the operator's call, so the picker stands in for "entry".
  let token = $state('')
  let tokenDevice = $state('')
  let tokenError = $state<string | null>(null)
  let minting = $state(false)
  // Whether entering step 4 has already reached for a token. Without
  // it, a mint that fails leaves the effect's conditions exactly as it
  // found them, and the effect tries again immediately -- a failing
  // token endpoint would be called in a tight loop rather than reported
  // once.
  let mintAttempted = false

  $effect(() => {
    // Reading pane is what re-runs this on every move.
    void wizardState.pane
    warning = false
    copied = ''
  })

  $effect(() => {
    if ((wizardState.pane !== 4 && wizardState.pane !== 6) || !wizardState.open) return
    if (token || minting || mintAttempted) return
    const known = wizardState.devices
    if (known.length === 1) {
      mintAttempted = true
      tokenDevice = known[0].id
      mintToken()
    }
  })

  // Step 4 and step 6 share this one token: the ingest token step 4
  // mints is the same credential internal/backupsftp checks against
  // (#394 -- "no new credential type"), so a router that has already
  // been pushed to has everything step 6's script needs, and minting
  // again here is only ever a fresh one for a router that skipped step
  // 4 or is being replaced (mintNewBackupToken below).
  async function mintToken() {
    tokenError = null
    if (!tokenDevice) {
      tokenError = 'Choose which router this token is for.'
      return
    }
    minting = true
    const result = await createToken(`setup-${tokenDevice}`, 'ingest', tokenDevice)
    minting = false
    if (typeof result === 'string') {
      tokenError = result
      return
    }
    token = result.value ?? ''
  }

  // mintNewBackupToken is round 45's "mint a new one", offered only in
  // the lost-router shape: the old token still opens the drop box (it
  // is never revoked here -- Settings ▸ keys already offers that,
  // deliberately not duplicated), this just gives the replacement a
  // fresh one of its own to use instead.
  function mintNewBackupToken() {
    token = ''
    mintToken()
  }

  // The RouterOS command blocks (#436) come from the server now, keyed
  // to a plain string rather than to wizardState.status directly -- the
  // status poll (POLL_MS above) reassigns that object every 5s whether
  // or not anything relevant changed, and re-requesting commands on
  // every poll tick would be wasted work. This key only changes when
  // something the request actually carries changes.
  const commandsKey = $derived(
    wizardState.status
      ? JSON.stringify([
          wizardState.address,
          wizardState.status.instance.syslogPort,
          wizardState.status.pushKinds,
          token,
          tokenDevice,
          wizardState.pickedVersion,
        ])
      : '',
  )

  $effect(() => {
    if (!commandsKey) return
    wizardState.refreshCommands({ token, device: tokenDevice })
  })

  // The router-standing warning (#436): one line per router outside the
  // dialect table's covered range, plus one for the operator's own pick
  // when it is. Never per code block, never a diagnosis about a specific
  // step -- see setupsteps.ts's module comment for why the commands
  // themselves moved server-side.
  interface WarningEntry {
    key: string
    kind: 'below-minimum' | 'ahead-of-review'
    name: string
    version: string
  }

  function isWarned(standing: RouterosStanding): standing is 'below-minimum' | 'ahead-of-review' {
    return standing === 'below-minimum' || standing === 'ahead-of-review'
  }

  const warningEntries = $derived.by((): WarningEntry[] => {
    const commands = wizardState.commands
    if (!commands) return []
    const entries: WarningEntry[] = commands.routers
      .filter((r) => isWarned(r.standing))
      .map((r) => ({ key: r.id, kind: r.standing as 'below-minimum' | 'ahead-of-review', name: r.name, version: r.routerosVersion }))
    if (commands.picked && isWarned(commands.picked.standing)) {
      entries.push({
        key: 'picked',
        kind: commands.picked.standing as 'below-minimum' | 'ahead-of-review',
        name: 'Your picked version',
        version: commands.picked.version,
      })
    }
    return entries
  })

  // Step 6's own three departures from every other step's fixed
  // lead/header (#394, round 45): the title gains "<router> is gone" in
  // the lost-router shape, the lead reads one of three ways depending
  // on state (rest/warrived share one, blocked and lost each have their
  // own), and the observation line reads this one router's own kept
  // count rather than the ledger's fleet-wide receipt.
  const lostRouterTitle = $derived(
    step && step.n === 6 && wizardState.lostRouterDevice ? `${step.title} — ${wizardState.lostRouterDevice} is gone` : null,
  )

  const leadText = $derived.by(() => {
    if (!step) return ''
    if (step.n === 6) {
      if (wizardState.lostRouterDevice) {
        return (
          'The router that pushed these is not answering. Everything a replacement needs from this ' +
          'side is here, in the order it needs it: trust the certificate, send logs, then run the ' +
          'backup script again so the new router keeps pushing. Its backups are under Settings.'
        )
      }
      if (step.status.state === 'blocked') {
        return (
          'Mikroview keeps a backup only under a key it does not hold, and none is mounted. Mount ' +
          'one and this step prints the script; until then the drop box is closed and a push would ' +
          'be refused.'
        )
      }
    }
    return step.lead
  })

  const lostGeneration = $derived.by(() => {
    const device = wizardState.lostRouterDevice
    if (!device) return null
    const router = wizardState.backups?.routers.find((r) => r.device === device)
    return router ? newestGeneration(router) : null
  })

  const lostObservationText = $derived(
    wizardState.lostRouterDevice ? backupReceiptForDevice(wizardState.backups, wizardState.lostRouterDevice) : '',
  )

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      copied = label
      setTimeout(() => (copied = ''), 1500)
    } catch {
      // Clipboard access can fail (permissions, non-secure context).
      // The block stays selectable either way.
    }
  }

  // Next runs the check where one exists. Arrived proceeds; waiting
  // hands the body to the heavy warning instead of moving. Steps that
  // count, and the step with nothing to wait for, always proceed.
  function onNext() {
    if (!step) return
    if (!step.hasCheck || step.outcome !== 'open') {
      wizardState.next()
      return
    }
    warning = true
  }

  async function onSkip() {
    if (!step || busy) return
    busy = true
    await wizardState.record(step.n, 'skipped', notObserved(step))
    busy = false
    wizardState.next()
  }

  async function onForce() {
    if (!step || busy) return
    busy = true
    await wizardState.record(step.n, 'forced', notObserved(step))
    busy = false
    warning = false
    wizardState.next()
  }

  function onKeydown(e: KeyboardEvent) {
    if (!wizardState.open || e.key !== 'Escape') return
    // Explicit close, both ways. Esc is not a click-outside: it is a
    // deliberate keystroke, and the record names it alongside the ✕.
    dismiss()
  }

  // Leads out to the landing -- the fall, mikroview's real landing page
  // since #616 (this read "Stream is the landing" until then; #646
  // makes it explicit: the wizard ends by taking the operator back to
  // the fall, whichever path opened it). Only from the finish, where the
  // record says the primary and the ✕ do the same thing -- closing
  // part-way through is a modal closing, and it leaves the operator on
  // the page they opened it from rather than moving them.
  function leaveToLanding() {
    appState.view = 'fall'
    wizardState.close()
  }

  // The finish screen's own door into Tune logging (#435 decision 2):
  // its other way in besides the topography's coverage lens. Closes the
  // modal rather than leaving it open behind the new page, the same way
  // leaveToLanding above does.
  function openTuneLogging() {
    appState.view = 'tune-logging'
    wizardState.close()
  }

  // "see it in Settings" (round 45's observation line): the same door,
  // for the same reason -- the backups group this observation is a
  // preview of lives there, not in a second copy of it here.
  function openBackupsInSettings() {
    appState.view = 'engineroom'
    wizardState.close()
  }

  // "done — the replacement is pushing" (round 45's lost-router
  // footer): the operator's own word that the repair is finished. It
  // only ever clears the flag this step's whole shape is keyed on --
  // mikroview has no way to confirm a replacement is pushing other than
  // waiting for the next arrival, so this is the same "your act, your
  // word" pattern the wizard's other decisions already use.
  function finishLostRouter() {
    wizardState.lostRouterDevice = null
    wizardState.next()
  }

  function dismiss() {
    if (onFinish) {
      leaveToLanding()
      return
    }
    wizardState.close()
  }

  const announcement = $derived(step ? announceStep(step) : onFinish ? finishHeadline(ledger) : '')

  const closeLabel = 'Close setup — finish later from your account menu ▸ Run setup…'
</script>

<svelte:window onkeydown={onKeydown} />

<!-- commandsHead (#436) sits at the head of every RouterOS command step
     (1-4): the version pick-list, and the router-standing warning --
     never blocking, never per code block, worded once here rather than
     once per step. -->
{#snippet commandsHead()}
  {#if wizardState.commands}
    {@const commands = wizardState.commands}
    <div class="routeros-version">
      <label for="routeros-version-select">Your RouterOS version</label>
      <select
        id="routeros-version-select"
        value={wizardState.pickedVersion}
        onchange={(e) => (wizardState.pickedVersion = (e.currentTarget as HTMLSelectElement).value)}
      >
        <option value="">Not sure — the router will report it</option>
        {#each commands.routeros.rows as row (row.from)}
          <option value={row.from}>{row.from === row.to ? row.from : `${row.from}–${row.to}`}</option>
        {/each}
      </select>
    </div>
    {#each warningEntries as w (w.key)}
      <p class="note" class:below-minimum={w.kind === 'below-minimum'}>
        {#if w.kind === 'below-minimum'}
          <strong>{w.name} runs RouterOS {w.version}.</strong> These commands were written for {commands
            .routeros.minimum} and later; on {w.version} some may not apply as written. Check each against
          your router before running it.
        {:else}
          <strong>{w.name} runs RouterOS {w.version}.</strong> These commands were last checked against {commands
            .routeros.newest}. Newer releases rarely change them, but if one is refused, that is the first
          thing to suspect.
        {/if}
      </p>
    {/each}
  {/if}
{/snippet}

{#if wizardState.open}
  <!-- No click-outside dismissal, per the record: the veil is a veil and
       nothing else, so it carries no click handler and is not a button.
       Losing a half-finished setup to a stray click is exactly what
       "explicit close only" exists to prevent. Below the pointer-width
       breakpoint the modal is the screen and there is no veil at all. -->
  <div class="veil" class:sheet={viewportState.isMobile} role="presentation">
    <div
      class="modal setup-wizard"
      class:sheet={viewportState.isMobile}
      role="dialog"
      aria-modal="true"
      aria-labelledby="setup-wizard-title"
      tabindex="-1"
      use:trapFocus
    >
      <header>
        {#if viewportState.isMobile}
          <!-- The step list becomes a view of its own on a phone, and
               the thing that flips to it is a real button with a spoken
               label -- not a swipe, not a tab strip. -->
          <button
            type="button"
            class="flip"
            onclick={() => (wizardState.showStepList = !wizardState.showStepList)}
          >
            {wizardState.showStepList ? 'Show this step' : 'Show setup steps'}
          </button>
        {/if}
        <span class="crumb">
          {#if step}Step {step.n} of {STEP_COUNT}{:else}Setup{/if}
        </span>
        <h2 id="setup-wizard-title">
          {#if lostRouterTitle && step}
            {step.title} — <span class="lost">{wizardState.lostRouterDevice} is gone</span>
          {:else}
            {step ? step.title : 'Where setup stands'}
          {/if}
        </h2>
        <button type="button" class="close" onclick={dismiss} aria-label={closeLabel}>✕</button>
      </header>

      <div class="middle">
        {#if !viewportState.isMobile || wizardState.showStepList}
          <!-- The step list carries each step's receipt sub-line for the
               wizard's life: what arrived and when, or the decision that
               was recorded, or an honest gap. -->
          <nav class="steps" aria-label="Setup steps">
            <ol>
              {#each ledger as s (s.n)}
                <li>
                  <button
                    type="button"
                    class="step-row {s.outcome}"
                    class:current={s.n === wizardState.pane}
                    aria-current={s.n === wizardState.pane ? 'step' : undefined}
                    onclick={() => {
                      wizardState.goTo(s.n)
                      wizardState.showStepList = false
                    }}
                  >
                    <span class="step-n">{s.n}</span>
                    <span class="step-text">
                      <span class="step-title">{s.title}</span>
                      {#if s.n === 6 && wizardState.lostRouterDevice}
                        <!-- Round 45's lost-router receipt: this one
                             router's own kept count, not the ledger's
                             fleet-wide "arrived ..." line. -->
                        <span class="step-receipt">
                          {lostObservationText || 'nothing kept for this router yet'}
                        </span>
                      {:else if s.receipt}
                        <span class="step-receipt">{s.receipt}</span>
                      {:else if s.outcome === 'open'}
                        <span class="step-receipt gap">nothing has arrived yet</span>
                      {/if}
                      {#if s.outcome === 'skipped'}
                        <span class="step-receipt consequence">{SKIP_CONSEQUENCES[s.n - 1]}</span>
                      {/if}
                    </span>
                  </button>
                </li>
              {/each}
              <li>
                <button
                  type="button"
                  class="step-row finish-row"
                  class:current={onFinish}
                  aria-current={onFinish ? 'step' : undefined}
                  onclick={() => {
                    wizardState.goTo(FINISH_PANE)
                    wizardState.showStepList = false
                  }}
                >
                  <span class="step-n">✓</span>
                  <span class="step-text"><span class="step-title">Where setup stands</span></span>
                </button>
              </li>
            </ol>
          </nav>
        {/if}

        {#if !viewportState.isMobile || !wizardState.showStepList}
          <div class="body scrollbar">
            {#if wizardState.error}
              <p class="load-error">Could not load setup status: {wizardState.error}</p>
            {/if}

            {#if warning && step}
              <!-- The heavy warning takes the body in place. Two
                   choices, no third option and no "are you sure": the
                   amber button quotes the exact record it will write. -->
              <div class="heavy">
                <h3>Mikroview cannot check the router's side</h3>
                <p>
                  It only sees what arrives here, and {notObserved(step)}. That is not the same as
                  the step having failed — it may simply not have happened yet.
                </p>
                <p class="quote">{forcedPastRecord(step, authState.username, new Date())}</p>
                <div class="heavy-actions">
                  <button type="button" class="primary" onclick={() => (warning = false)}>
                    Keep waiting
                  </button>
                  <button type="button" class="amber" onclick={onForce} disabled={busy}>
                    Go on anyway — recorded
                  </button>
                </div>
              </div>
            {:else if step}
              <p class="lead">{leadText}</p>

              {#if step.n === 6 && step.status.state !== 'blocked'}
                <!-- Round 45's caveat, in the amber the heavy warning
                     above already uses, before the script rather than
                     after: RouterOS never verifies who it is sending a
                     backup to (#394's measured finding), so this is
                     said before the operator copies anything. -->
                <div class="wzcaveat">
                  <b>Only on a network you trust.</b> RouterOS never checks who it is sending a backup to
                  — anyone on the path between the router and {wizardState.address} could read the pair, and
                  the token with it. On a LAN you control that is fine; across the internet it is not, and
                  mikroview cannot tell the difference from here.
                </div>
              {/if}

              {#if step.n <= 4 || step.n === 6}
                {@render commandsHead()}
              {/if}

              {#if step.n === 1 && wizardState.status}
                {#if step.status.state !== 'blocked'}
                  <pre>{wizardState.commands?.steps.caTrust.commands ?? ''}</pre>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.caTrust.commands ?? '', 'ca')}
                  >
                    {copied === 'ca' ? 'Copied' : 'Copy'}
                  </button>
                  {#if wizardState.commands?.steps.caTrust.note}
                    <p class="note">{wizardState.commands.steps.caTrust.note}</p>
                  {/if}
                  <p class="note">
                    <code>check-certificate=no</code> belongs on this one line only — it is fetching
                    the thing everything else checks against.
                  </p>
                {/if}
              {:else if step.n === 2 && wizardState.status}
                {#if step.status.state !== 'blocked'}
                  <pre>{wizardState.commands?.steps.syslog.commands ?? ''}</pre>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.syslog.commands ?? '', 'syslog')}
                  >
                    {copied === 'syslog' ? 'Copied' : 'Copy'}
                  </button>
                  {#if wizardState.commands?.steps.syslog.note}
                    <p class="note">{wizardState.commands.steps.syslog.note}</p>
                  {/if}
                {/if}
              {:else if step.n === 3}
                <pre>{wizardState.commands?.steps.ruleTagging.commands ?? ''}</pre>
                <button
                  type="button"
                  class="copy"
                  onclick={() => copy(wizardState.commands?.steps.ruleTagging.commands ?? '', 'rules')}
                >
                  {copied === 'rules' ? 'Copied' : 'Copy'}
                </button>
                {#if wizardState.commands?.steps.ruleTagging.note}
                  <p class="note">{wizardState.commands.steps.ruleTagging.note}</p>
                {/if}
                <p class="note">
                  The letter is how mikroview knows what a rule did — <code>A</code>ccept,
                  <code>D</code>rop, <code>R</code>eject, <code>L</code>og. The trailing
                  <code>|</code> is required.
                </p>
              {:else if step.n === 4 && wizardState.status}
                {#if !token}
                  <div class="mint">
                    <select bind:value={tokenDevice} aria-label="Router this token is for">
                      <option value="" disabled>Which router is this for?…</option>
                      {#each wizardState.devices as d (d.id)}
                        <option value={d.id}>
                          {d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}
                        </option>
                      {/each}
                    </select>
                    <button type="button" class="primary" onclick={mintToken} disabled={minting}>
                      {minting ? 'Creating…' : 'Create token & script'}
                    </button>
                  </div>
                  {#if wizardState.devices.length === 0}
                    <p class="note">
                      No routers known yet — finish step 2 first, and this list fills in on its own.
                    </p>
                  {/if}
                  {#if tokenError}<p class="load-error">{tokenError}</p>{/if}
                {:else}
                  <p class="note token-note">
                    The token below is shown once and is already in the script. Anyone who can read
                    the script on the router can read the token, so it is scoped to that one router.
                  </p>
                  <pre class="script">{wizardState.commands?.steps.push.commands ?? ''}</pre>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.push.commands ?? '', 'script')}
                  >
                    {copied === 'script' ? 'Copied' : 'Copy script'}
                  </button>
                  {#if wizardState.commands?.steps.push.note}
                    <p class="note">{wizardState.commands.steps.push.note}</p>
                  {/if}
                  <p class="note">Then save it and run it once:</p>
                  <pre>{wizardState.commands?.steps.schedule.commands ?? ''}</pre>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.schedule.commands ?? '', 'sched')}
                  >
                    {copied === 'sched' ? 'Copied' : 'Copy'}
                  </button>
                  {#if wizardState.commands?.steps.schedule.note}
                    <p class="note">{wizardState.commands.steps.schedule.note}</p>
                  {/if}
                {/if}
              {:else if step.n === 5}
                {#each undeclared as d (d.id)}
                  <pre>{deviceStanza(d.sourceIp, d.name)}</pre>
                {/each}
              {:else if step.n === 6}
                {#if step.status.state === 'blocked'}
                  <pre class="wzpre-dim">   — the script prints here once a key is mounted —</pre>
                  <p class="note">
                    <a class="olink" href={HOW_TO_MOUNT_URL} target="_blank" rel="noopener noreferrer">
                      how to mount one
                    </a>
                    · the same key keeps the event history and the state store.
                  </p>
                {:else if !token}
                  <!-- Round 45 draws no mint form here at all: it
                       assumes step 4 already minted the token this
                       script needs (the same credential, #394 -- see
                       mintToken's comment). This is only reached when
                       that has not happened yet -- step 4 skipped, or a
                       fresh session -- and without it there is nothing
                       to print, which the "done when" bar does not
                       allow. -->
                  <div class="mint">
                    <select bind:value={tokenDevice} aria-label="Router this token is for">
                      <option value="" disabled>Which router is this for?…</option>
                      {#each wizardState.devices as d (d.id)}
                        <option value={d.id}>
                          {d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}
                        </option>
                      {/each}
                    </select>
                    <button type="button" class="primary" onclick={mintToken} disabled={minting}>
                      {minting ? 'Creating…' : 'Create token & script'}
                    </button>
                  </div>
                  {#if wizardState.devices.length === 0}
                    <p class="note">
                      No routers known yet — finish step 2 first, and this list fills in on its own.
                    </p>
                  {/if}
                  {#if tokenError}<p class="load-error">{tokenError}</p>{/if}
                {:else}
                  {#if wizardState.lostRouterDevice}
                    <p class="note token-note">
                      The old router's token still opens the drop box, so a replacement can use this
                      script as it stands;
                      <button type="button" class="link" onclick={mintNewBackupToken}>mint a new one</button>
                      retires the old.
                    </p>
                  {:else}
                    <p class="note token-note">
                      The token is shown once. Anyone who can read the script on the router can read it,
                      so it is scoped to that one router and to this drop box.
                    </p>
                  {/if}
                  <pre class="script">{wizardState.commands?.steps.backup.commands ?? ''}</pre>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.backup.commands ?? '', 'backup')}
                  >
                    {copied === 'backup' ? 'Copied' : 'Copy script'}
                  </button>
                  {#if wizardState.commands?.steps.backup.note}
                    <p class="note">{wizardState.commands.steps.backup.note}</p>
                  {/if}
                  <p class="note">Then have it run nightly, and once now to test it:</p>
                  <pre>{wizardState.commands?.steps.backupSchedule.commands ?? ''}</pre>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.backupSchedule.commands ?? '', 'backupsched')}
                  >
                    {copied === 'backupsched' ? 'Copied' : 'Copy'}
                  </button>
                {/if}
              {/if}

              <!-- The observation line. Four flavours and no more:
                   waiting is patient and never says "error" (nothing is
                   wrong, nothing has arrived), arrived is dated and
                   sourced, counting only counts upward, quiet has
                   nothing to wait for. `attention` is the inherited
                   mikroview-side check logic (#371/#374), not an
                   observation -- see setupsteps.ts. Step 6's lost-router
                   shape reads this one router's own kept count instead
                   of the ledger's fleet-wide receipt (lostObservationText). -->
              <p class="observation {step.n === 6 && wizardState.lostRouterDevice ? (lostGeneration ? 'arrived' : 'waiting') : step.flavour}">
                {#if step.n === 6 && wizardState.lostRouterDevice}
                  {#if step.flavour !== 'arrived' && !lostGeneration}<span class="dot" aria-hidden="true"></span>{/if}
                  {lostObservationText || 'nothing kept for this router yet'}
                  {#if lostGeneration}
                    ·
                    <a
                      class="olink"
                      href={routerBackupDownloadUrl(wizardState.lostRouterDevice ?? '', lostGeneration.id, 'backup')}
                    >
                      download the newest .backup
                    </a>
                    to restore the replacement, then run the script above
                  {/if}
                {:else}
                  {#if step.flavour === 'waiting'}<span class="dot" aria-hidden="true"></span>{/if}
                  {step.status.detail}
                  {#if step.n === 6 && step.status.state === 'done'}
                    ·
                    <button type="button" class="link" onclick={openBackupsInSettings}>see it in Settings</button>
                  {/if}
                {/if}
              </p>
              {#if step.n === 2 && step.status.state === 'partial' && splits.length > 0}
                <!-- The source-address split (#442), under the
                     observation line. The mismatch sentence's shape:
                     what you told mikroview, what the router shows, no
                     diagnosis; "MikroView can't tell X. You can."; then
                     the printed command. #436 reads the same way. -->
                <div class="split">
                  <p class="note">
                    MikroView can't tell whether these are the same router — a router holds an address
                    on every network it routes, and its logs arrive stamped with whichever one faces
                    this instance. You can tell.
                  </p>
                  <p class="note">
                    <strong>If they are the same router</strong>, pick the address it should be known
                    by here:
                  </p>
                  {#each splits as split (split.declared)}
                    <p class="note">
                      <strong>Keep {split.declared}</strong> (recommended). Run this on the router — it
                      makes the logs arrive from the address you declared:
                    </p>
                    <pre>{srcAddressCommand(split.declared)}</pre>
                    <button
                      type="button"
                      class="copy"
                      onclick={() => copy(srcAddressCommand(split.declared), `src-${split.declared}`)}
                    >
                      {copied === `src-${split.declared}` ? 'Copied' : 'Copy'}
                    </button>
                    <p class="note">
                      Recommended because everything else — the token step 4 mints, the tables it
                      pushes — follows the declared identity, so nothing has to be reissued.
                    </p>
                    <p class="note">
                      {#if arriving.length === 1}
                        <strong>Or keep {arriving[0]}</strong>: change <code>sourceIp</code> to
                        {arriving[0]} in config.yaml and restart.
                      {:else}
                        <strong>Or keep the arriving address</strong>: change <code>sourceIp</code> to
                        whichever of {prose(arriving)} this router is, in config.yaml, and restart.
                      {/if}
                      MikroView then matches what actually arrives. If a token was already minted for
                      {split.declared}, reissue it afterwards — a token keeps the identity it was minted
                      for.
                    </p>
                  {/each}
                  <p class="note">
                    <strong>If they are two different routers, nothing is wrong.</strong>
                    {prose(arriving)} just {arriving.length === 1 ? "hasn't" : "haven't"} been named yet
                    — step 5 covers that — and this notice clears itself when
                    {prose(splits.map((s) => s.declared))}
                    {splits.length === 1 ? 'sends its first log' : 'send their first logs'}.
                  </p>
                </div>
              {/if}
              {#if step.outcome === 'skipped'}
                <p class="decision skipped">
                  Skipped — {SKIP_CONSEQUENCES[step.n - 1]}. {step.receipt}
                </p>
              {:else if step.outcome === 'forced'}
                <p class="decision forced">Forced past — {step.receipt}</p>
              {/if}
            {:else if onFinish}
              <p class="headline">{finishHeadline(ledger)}</p>
              <ol class="readback">
                {#each ledger as s (s.n)}
                  <li class={s.outcome}>
                    <span class="rb-title">{s.n}. {s.title}</span>
                    <span class="rb-detail">
                      {s.receipt || (s.status.state === 'quiet' ? s.status.detail : 'nothing arrived')}
                    </span>
                  </li>
                {/each}
              </ol>
              <!-- The one place the wizard offers the buffer's size
                   (#796): the same track and the same sentence as
                   Settings' memory group, once, on the pane where the
                   operator has finished pointing a router at mikroview
                   and is about to go and look at what arrives. It is
                   asked here rather than as a step of its own because
                   nothing about it can be checked or waited for -- it is
                   a choice, not an observation, and the wizard's steps
                   are all observations. -->
              {#if appState.stats?.memory}
                <div class="memory">
                  <h3>How much to hold</h3>
                  <p class="note">
                    Every event lives in memory and nothing else. This is how much of this machine's
                    memory to spend on it; the oldest events fall away as new ones arrive.
                  </p>
                  <MemoryControl
                    mem={appState.stats.memory}
                    stats={appState.stats}
                    canEdit={isAdmin}
                    onapplied={() => appState.refreshDevicesAndStats().catch(() => {})}
                  />
                </div>
              {/if}
              <p class="note">Run setup… reopens this any time, from the Admin group.</p>
              <p class="note">
                <button type="button" class="link" onclick={openTuneLogging}>Tune logging…</button>
                turns a dark connection into a watched one once mikroview has been listening a day.
              </p>
            {/if}
          </div>
        {/if}
      </div>

      <footer>
        <button type="button" class="ghost" onclick={() => wizardState.back()} disabled={wizardState.pane === 1}>
          Back
        </button>
        <div class="footer-right">
          {#if step && step.hasCheck && step.outcome === 'open' && !warning}
            <span class="hint">Next checks what has arrived</span>
          {/if}
          {#if step && step.n === 6 && wizardState.lostRouterDevice}
            <!-- Round 45's lost-router footer: no skip (there is
                 nothing to skip past -- the router this step is about
                 is already gone), and the primary button is the
                 operator's own word that the repair is done rather
                 than a check on evidence mikroview cannot see. -->
            <button type="button" class="primary" onclick={finishLostRouter}>
              done — the replacement is pushing
            </button>
          {:else if step}
            <button type="button" class="ghost" onclick={onSkip} disabled={busy}>Skip this step</button>
            <button type="button" class="primary" onclick={onNext} disabled={busy}>
              {step.n === STEP_COUNT ? 'Finish' : 'Next'}
            </button>
          {:else}
            <button type="button" class="primary" onclick={leaveToLanding}>Take me to the fall</button>
          {/if}
        </div>
      </footer>
    </div>
  </div>

  <!-- Clipped, not hidden: display:none would take it out of the
       accessibility tree and silence the announcement it carries. -->
  <p class="sr-only" role="status">{announcement}</p>
{/if}

<style>
  .veil {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 3vh 3vw;
    z-index: 60;
  }

  /* Below the pointer-width breakpoint the modal is the screen: a
     full-bleed sheet, no veil. */
  .veil.sheet {
    background: var(--bg);
    padding: 0;
  }

  .modal {
    /* The round-1 size correction, applied: "use more of the screen, no
       need to squash things in". */
    width: 940px;
    max-width: 94%;
    max-height: 92vh;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 10px;
    display: flex;
    flex-direction: column;
    box-shadow: 0 24px 60px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }

  .modal.sheet {
    width: 100%;
    max-width: 100%;
    height: 100%;
    max-height: 100%;
    border: none;
    border-radius: 0;
    box-shadow: none;
  }

  header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .crumb {
    font-size: 12px;
    color: var(--fg-muted);
    white-space: nowrap;
  }

  h2 {
    margin: 0;
    flex: 1;
    font-size: 16px;
    color: var(--fg);
    min-width: 0;
  }

  .close,
  .flip {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    font-size: 12px;
    line-height: 1;
    padding: 8px 10px;
  }

  .close {
    width: 30px;
    height: 30px;
    padding: 0;
    flex: none;
  }

  .close:hover,
  .flip:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .middle {
    flex: 1;
    display: flex;
    min-height: 0;
  }

  .steps {
    width: 224px;
    flex: none;
    border-right: 1px solid var(--border);
    overflow-y: auto;
    padding: 10px 8px;
  }

  .modal.sheet .steps {
    width: 100%;
    border-right: none;
  }

  .steps ol {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .step-row {
    width: 100%;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    text-align: left;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 6px;
    padding: 9px 10px;
    color: var(--fg-muted);
  }

  .step-row:hover {
    background: var(--bg-hover);
  }

  .step-row.current {
    background: var(--bg-elevated);
    border-color: var(--border);
    color: var(--fg);
  }

  .step-n {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    flex: none;
    border-radius: 50%;
    background: var(--bg);
    border: 1px solid var(--border);
    font-size: 11px;
  }

  .step-row.done .step-n {
    border-color: var(--accept);
    color: var(--accept);
  }

  .step-row.forced .step-n {
    border-color: var(--log);
    color: var(--log);
  }

  /* Dashes are quiet, amber is loud -- the two stay visually distinct. */
  .step-row.skipped .step-n {
    border-style: dashed;
  }

  .step-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .step-title {
    font-size: 13px;
  }

  .step-receipt {
    font-size: 11px;
    line-height: 1.4;
    color: var(--accept);
    word-break: break-word;
  }

  .step-row.skipped .step-receipt,
  .step-receipt.gap,
  .step-receipt.consequence {
    color: var(--fg-muted);
  }

  .step-row.forced .step-receipt {
    color: var(--log);
  }

  .body {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    padding: 18px 22px 22px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .lead,
  .note,
  .headline {
    margin: 0;
    font-size: 14px;
    line-height: 1.6;
    color: var(--fg);
  }

  .note {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  /* The below-minimum router-standing warning (#436): the same amber
     already used for this wizard's "heavy" caution register (.amber,
     .heavy below), as a left rule rather than a new colour -- a router
     outside the table's floor never blocks, but the note still reads as
     the loudest thing on the step. */
  .note.below-minimum {
    border-left: 3px solid var(--log);
    padding-left: 10px;
  }

  /* Step 6's caveat (#394, round 45): the same amber the heavy warning
     above already uses, ahead of the script rather than after it. */
  .wzcaveat {
    align-self: stretch;
    border-left: 2px solid var(--log);
    padding: 6px 12px;
    margin: 0;
    font-size: 12px;
    line-height: 1.5;
    color: var(--fg-muted);
  }

  .wzcaveat b {
    color: var(--log);
    font-weight: 600;
  }

  .wzpre-dim {
    color: var(--fg-dim);
  }

  /* The lost-router title (#394, round 45): "<router> is gone" in the
     same amber. */
  .lost {
    color: var(--log);
    font-weight: 600;
  }

  .routeros-version {
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .routeros-version label {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .routeros-version select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-size: 13px;
  }

  .headline {
    font-size: 15px;
  }

  /* The buffer-size control on the finish pane (#796) -- separated from
     the read-back above it the way the wizard already separates its own
     blocks, and left to MemoryControl for everything inside. */
  .memory {
    margin-top: 18px;
    padding-top: 14px;
    border-top: 1px solid var(--border);
  }

  .memory h3 {
    margin: 0 0 4px;
    font-size: 13px;
    font-weight: 600;
  }

  pre {
    margin: 0;
    align-self: stretch;
    padding: 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-size: 12.5px;
    line-height: 1.6;
    overflow-x: auto;
    white-space: pre;
    color: var(--fg);
    user-select: all;
  }

  pre.script {
    max-height: 300px;
    overflow-y: auto;
  }

  /* Commands come pre-broken to phone width so nothing scrolls sideways;
     Copy matters more here, not less. */
  .modal.sheet pre {
    white-space: pre-wrap;
    word-break: break-word;
    font-size: 12px;
  }

  button {
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  button:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  button.amber {
    background: var(--log);
    border-color: var(--log);
    color: var(--bg);
    font-weight: 600;
  }

  /* Tune logging's own door on the finish pane (#435): reads as a link
     inline with the sentence beside it, not a second boxed button next
     to "Run setup… reopens this". Step 6's "see it in Settings", "mint
     a new one" and "how to mount one"/"download the newest .backup"
     (#394, round 45) read the same way, the last two as real <a>
     elements rather than buttons since they navigate. */
  button.link,
  a.olink {
    display: inline;
    border: none;
    padding: 0;
    background: none;
    color: var(--accent);
    text-decoration: underline;
    font-size: inherit;
    cursor: pointer;
  }

  button:disabled {
    opacity: 0.5;
  }

  .observation {
    margin: 0;
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    line-height: 1.5;
    padding: 9px 12px;
    border-radius: 6px;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .observation.waiting {
    border-style: dashed;
  }

  .observation.arrived,
  .observation.counting {
    border-color: var(--accept);
    color: var(--accept);
  }

  .observation.attention {
    border-color: var(--reject);
    color: var(--reject);
  }

  .dot {
    width: 7px;
    height: 7px;
    flex: none;
    border-radius: 50%;
    background: var(--fg-muted);
    animation: pulse 1.8s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 0.35;
    }
    50% {
      opacity: 1;
    }
  }

  /* The source-address split's body (#442): the wizard's own note and
     pre blocks, grouped so they read as one explanation under the
     observation line rather than as loose notes. */
  .split {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }

  .split strong {
    color: var(--fg);
  }

  .decision {
    margin: 0;
    align-self: stretch;
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--fg-muted);
  }

  .decision.forced {
    color: var(--log);
  }

  .heavy {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    gap: 12px;
    border: 1px solid var(--log);
    border-radius: 8px;
    padding: 16px;
    background: var(--bg-elevated);
  }

  .heavy h3 {
    margin: 0;
    font-size: 15px;
    color: var(--log);
  }

  .heavy p {
    margin: 0;
    font-size: 13.5px;
    line-height: 1.6;
    color: var(--fg);
  }

  .heavy .quote {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
    padding: 10px 12px;
    border: 1px dashed var(--log);
    border-radius: 6px;
    color: var(--log);
    word-break: break-word;
  }

  .heavy-actions {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }

  .readback {
    align-self: stretch;
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .readback li {
    display: flex;
    flex-direction: column;
    gap: 2px;
    border-left: 2px solid var(--border);
    padding: 2px 0 2px 12px;
  }

  .readback li.done {
    border-left-color: var(--accept);
  }

  .readback li.forced {
    border-left-color: var(--log);
  }

  .readback li.skipped {
    border-left-style: dashed;
  }

  .rb-title {
    font-size: 13px;
    color: var(--fg);
  }

  .rb-detail {
    font-size: 12px;
    color: var(--fg-muted);
  }

  .load-error {
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  .mint {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }

  .mint select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-size: 13px;
  }

  .token-note {
    color: var(--fg);
  }

  code {
    font-size: 11.5px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 4px;
  }

  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 18px;
    border-top: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .footer-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .hint {
    font-size: 12px;
    color: var(--fg-muted);
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  /* Reduced motion stops the waiting-dot pulse and makes the phone's
     body <-> ledger flip instant, per the record. */
  @media (prefers-reduced-motion: reduce) {
    .dot {
      animation: none;
      opacity: 0.7;
    }
  }
</style>
