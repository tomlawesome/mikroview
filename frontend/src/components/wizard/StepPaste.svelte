<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Step 3 · Paste once, and the router's turn (DESIGN.md, "The steps,
  // in detail"; #1383) -- ported from the prototype's pasteBody and
  // watchBody (docs/design/screens/wizard/round-15/wizard.js), the
  // track from track.js, and the two recoveries (refused sender,
  // ahead of review).
  //
  // The block is never hand-written here: wizardRun.blockSections reads
  // what POST /api/setup/commands already rendered
  // (wizardState.commands, kept current by Wizard.svelte's mint flow
  // and this step's own ingest-token effect below) and only orders and
  // numbers it. What the shell already provides: wizardRun.markCopied()
  // starts the router's turn; wizardRun.evidence carries every proof
  // the track and the observation line read; the footer's Next is
  // disabled until everything chosen has arrived (wizardRun.arrivedAll).

  import { createToken, fetchEvents } from '../../lib/api'
  import { copyToClipboard } from '../../lib/clipboard'
  import { wizardState } from '../../lib/wizard.svelte'
  import { wizardRun } from '../../lib/wizardRun.svelte'
  import Track from './Track.svelte'

  const ev = $derived(wizardRun.evidence)
  const arrived = $derived(wizardRun.arrivedAll)
  const sections = $derived(wizardRun.blockSections)
  const blockText = $derived(wizardRun.blockText)
  const stations = $derived(wizardRun.trackStations)
  const latest = $derived(wizardRun.latestArrival)

  // The push and backup sections need a persistent ingest token
  // (internal/api/ingest.go's bearer, distinct from the one-shot
  // enrolment marker Mint minted) to authenticate the router's own
  // posts -- see routeros.PushScript/BackupScript, embedded in
  // steps.schedule/steps.backup by POST /api/setup/commands. Minted
  // once per open walk and silently: it needs no password of its own,
  // the same way the retired modal's "Create token & script" button
  // never asked for one -- #1291's re-check belongs to the enrolment
  // token alone.
  $effect(() => {
    if (wizardRun.stage !== 'paste') return
    if (wizardRun.push !== true && wizardRun.backup !== true) return
    const device = wizardState.ledgerDevice
    if (!device) return
    if (wizardState.token && wizardState.tokenDevice === device) return
    let stale = false
    createToken(`setup-${device}`, 'ingest', device).then((result) => {
      if (stale || typeof result === 'string' || !result.value) return
      wizardState.token = result.value
      wizardState.tokenDevice = device
      wizardState.refreshCommands({ device, token: result.value })
    })
    return () => {
      stale = true
    }
  })

  // The stream of arriving lines (watchBody's own `stream`): the same
  // events endpoint the live view reads, narrowed to this router, never
  // a router-side read -- MikroView only reads what it has already
  // ingested.
  interface StreamLine {
    time: string
    pre: string
    text: string
  }
  let streamLines = $state<StreamLine[]>([])
  $effect(() => {
    if (wizardRun.stage !== 'watch' || !ev.enrol) {
      streamLines = []
      return
    }
    const device = wizardState.ledgerDevice
    let stale = false
    const poll = () => {
      fetchEvents({ device, limit: 6 })
        .then((result) => {
          if (stale) return
          streamLines = [...result.events].reverse().map((e) => ({
            time: new Date(e.time).toLocaleTimeString(undefined, { hour12: false }),
            pre: e.ruleLabel,
            text: `${e.chain}: ${e.srcIp ?? ''}${e.srcPort ? ':' + e.srcPort : ''} → ${e.dstIp ?? ''}${e.dstPort ? ':' + e.dstPort : ''}`,
          }))
        })
        .catch(() => {})
    }
    poll()
    const timer = setInterval(poll, 5000)
    return () => {
      stale = true
      clearInterval(timer)
    }
  })

  async function copy() {
    await copyToClipboard(blockText)
    wizardRun.markCopied()
  }

  let showReroll = $state(false)
  let rerollPass = $state('')

  function toggleReroll() {
    showReroll = !showReroll
    rerollPass = ''
    wizardState.enrolmentError = null
  }

  async function submitReroll() {
    if (!rerollPass) return
    const pass = rerollPass
    rerollPass = ''
    await wizardRun.reroll(pass)
    if (!wizardState.enrolmentError) showReroll = false
  }

  let showUseOther = $state(false)
  let useOtherPass = $state('')

  function toggleUseOther() {
    showUseOther = !showUseOther
    useOtherPass = ''
    wizardState.enrolmentError = null
  }

  async function submitUseOther() {
    if (!useOtherPass) return
    const pass = useOtherPass
    useOtherPass = ''
    await wizardRun.enrolAtOther(pass)
    if (!wizardState.enrolmentError) showUseOther = false
  }
</script>

{#if wizardRun.stage === 'paste'}
  <h3>One paste.</h3>
  <p class="hint">
    {sections.length} parts, in order — {sections.map((s) => s.title.toLowerCase()).join(', ')} — and the enrol line
    last. Into the router’s <b>terminal</b> (WinBox ▸ New Terminal, or ssh), not a script.
  </p>
  <pre class="script" aria-label="The block to paste">{blockText}</pre>
  <div class="copyrow">
    <button type="button" class="primary" onclick={copy}>Copy</button>
    <span class="token-life">
      Token good until {ev.tokenUntil} (15 minutes) ·
      <button type="button" class="linkish" onclick={toggleReroll}>Reroll</button>
    </span>
  </div>
  {#if showReroll}
    <div class="form single">
      <div class="v">
        <input
          type="password"
          bind:value={rerollPass}
          placeholder="password"
          aria-label="Your password, to reroll the token"
          onkeydown={(e) => {
            if (e.key === 'Enter') submitReroll()
          }}
        />
        {#if wizardState.enrolmentError}
          <p class="problem" aria-live="polite">{wizardState.enrolmentError}</p>
        {/if}
      </div>
    </div>
  {/if}
  <div class="obs waiting">
    {wizardRun.copied
      ? 'Copied. Waiting for the router — the certificate fetch comes first.'
      : 'Nothing has arrived yet. Copy, paste, and this line will say what the router did.'}
  </div>
{:else}
  {#if arrived}
    <h3>{wizardRun.name} is sending.</h3>
    <p class="hint">Everything you chose has arrived; each station stands on evidence.</p>
  {:else}
    <h3>The router’s turn.</h3>
    <p class="hint">
      The block is on the router. Each part answers in its own time — every station that lights is something that
      arrived.
    </p>
  {/if}
  <Track {stations} />
  {#if latest}
    <div class="obs arrived">{latest.text}<small>{latest.small}</small></div>
  {:else}
    <div class="obs waiting">Copied. Waiting for the router — the certificate fetch comes first.</div>
  {/if}
  {#if ev.refused}
    <div class="warnbox">
      <span>
        <b>Lines from {ev.refused} arrived without the enrol line and were refused.</b> If that is this router, its logging
        action is sending from another address — usually an older <span class="mono">remote</span> action, or a
        <span class="mono">src-address</span> left from a previous setup. Fix it on the router, then paste the last line
        of the block again:
      </span>
      <pre>{wizardRun.refusedFixWithEnrol}</pre>
      <span>
        Or, if {ev.refused} is the address it should send from:
        <button type="button" class="linkish" onclick={toggleUseOther}>enrol at {ev.refused} instead</button>.
      </span>
      {#if showUseOther}
        <div class="form single">
          <div class="v">
            <input
              type="password"
              bind:value={useOtherPass}
              placeholder="password"
              aria-label="Your password, to enrol at {ev.refused} instead"
              onkeydown={(e) => {
                if (e.key === 'Enter') submitUseOther()
              }}
            />
            {#if wizardState.enrolmentError}
              <p class="problem" aria-live="polite">{wizardState.enrolmentError}</p>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {/if}
  {#if ev.push && ev.ahead}
    <div class="cautionbox">
      <b>RouterOS {ev.version} is newer than these commands were reviewed on ({wizardRun.reviewedVersion}).</b> They ran
      and the push arrived. If anything reads wrong, this is the first suspect.
    </div>
  {/if}
  {#if ev.enrol}
    <div class="stream" aria-label="Lines arriving">
      {#each streamLines as l, i (i)}
        <div class="l"><span class="dim">{l.time}</span> <b>{l.pre}</b> {l.text}</div>
      {/each}
    </div>
  {/if}
{/if}
