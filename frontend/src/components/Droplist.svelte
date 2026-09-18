<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Settings' "drop list" group (#1225, #461): the third router-facing
  // thing mikroview hands over besides ingest and router backups -- a
  // list of addresses the router pulls and drops on its own, admin-only
  // like the backups group beside it (EngineRoom mounts this only for
  // isAdmin, and the server matches). Same shape as RouterBackups.svelte:
  // this component draws from the `resp` its parent polled, and every
  // mutating control here calls the API directly and then asks the
  // parent to refresh rather than guessing the new state itself.
  //
  // Left column: one honest drift line per router that has actually
  // reported its address-list snapshot (never "nothing to report" read
  // as agreement -- see the no-routers row below), the pull key's own
  // row and its one-time mint reveal, and the setup card -- four printed
  // blocks the operator pastes onto the router by hand. Right column:
  // the add form and the entries themselves, newest first.
  import { tick } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { topologyNavState } from '../lib/topologyNav.svelte'
  import { droplistNavState } from '../lib/droplistNav.svelte'
  import { wizardState } from '../lib/wizard.svelte'
  import { createDroplistEntry, deleteDroplistEntry, mintDroplistKey, revokeDroplistKey } from '../lib/api'
  import { copyToClipboard } from '../lib/clipboard'
  import { formatRelative } from '../lib/format'
  import type { DroplistResponse } from '../lib/types'

  let {
    resp,
    onrefresh,
  }: {
    resp: DroplistResponse
    /** Re-reads GET /api/droplist -- called after every mutation below
     * rather than each control inferring the new state itself. */
    onrefresh: () => Promise<void>
  } = $props()

  const sortedEntries = $derived(
    [...resp.entries].sort((a, b) => new Date(b.addedAt).getTime() - new Date(a.addedAt).getTime()),
  )

  // --- the pull key (mint/replace/revoke) ---------------------------------
  let submittingKey = $state(false)
  let keyError = $state<string | null>(null)
  let justMinted = $state<{ key: string; scheduler: string } | null>(null)
  let armedRevokeKey = $state(false)

  async function mintKey() {
    keyError = null
    submittingKey = true
    // wizardState.address (#1213) is the operator's own saved answer to
    // "what address can your router reach mikroview on?", not this
    // tab's own window.location.host -- see EngineRoom.svelte's
    // copyRouterLines for the same rule applied to the ingest push
    // script. Reading it live at mint time rather than once, since the
    // operator may save it well after this card first mounted.
    const result = await mintDroplistKey(wizardState.address)
    submittingKey = false
    if (typeof result === 'string') {
      keyError = result
      return
    }
    justMinted = { key: result.key, scheduler: result.scheduler }
    await onrefresh()
  }

  function onRevokeKeyClick(e: MouseEvent) {
    e.stopPropagation()
    if (armedRevokeKey) {
      armedRevokeKey = false
      void doRevokeKey()
      return
    }
    disarmAll()
    armedRevokeKey = true
  }

  async function doRevokeKey() {
    keyError = null
    submittingKey = true
    const err = await revokeDroplistKey()
    submittingKey = false
    if (err) {
      keyError = err
      return
    }
    justMinted = null
    await onrefresh()
  }

  // --- the setup card (#1225): printed, never applied ---------------------
  let setupOpen = $state(false)
  let copiedLabel = $state<string | null>(null)
  let copiedTimer: ReturnType<typeof setTimeout> | undefined

  async function copyBlock(key: string, text: string) {
    const ok = await copyToClipboard(text)
    if (!ok) return
    copiedLabel = key
    if (copiedTimer) clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => {
      if (copiedLabel === key) copiedLabel = null
    }, 1500)
  }

  // --- the add form --------------------------------------------------------
  let newCidr = $state('')
  let newReason = $state('')
  let newFlagID = $state<string | undefined>(undefined)
  let adding = $state(false)
  let addError = $state<string | null>(null)
  let addWarning = $state<string | null>(null)
  let addressInputEl = $state<HTMLInputElement | null>(null)

  async function submitAdd() {
    addError = null
    addWarning = null
    adding = true
    const result = await createDroplistEntry({ cidr: newCidr.trim(), reason: newReason.trim(), flagID: newFlagID })
    adding = false
    if (typeof result === 'string') {
      addError = result
      return
    }
    if (result.warning) addWarning = result.warning
    newCidr = ''
    newReason = ''
    newFlagID = undefined
    await onrefresh()
  }

  // #1225's flag-drawer handoff: Flags.svelte's "block…" fills this in
  // and switches to this section; taken once, whether that lands on our
  // own mount or the instant it changes under an already-mounted
  // Settings tab (EngineRoom stays mounted like Docket's own tabs do).
  $effect(() => {
    const fill = droplistNavState.pendingDraft
    if (!fill) return
    droplistNavState.pendingDraft = null
    newCidr = fill.cidr
    newReason = fill.reason
    newFlagID = fill.flagID
    addError = null
    addWarning = null
    tick().then(() => addressInputEl?.focus())
  })

  // --- entries: remove, and the "from flag" deep link ----------------------
  let armedRemove = $state<string | null>(null)
  let removeError = $state<string | null>(null)

  function onRemoveClick(e: MouseEvent, cidr: string) {
    e.stopPropagation()
    if (armedRemove === cidr) {
      armedRemove = null
      void doRemove(cidr)
      return
    }
    disarmAll()
    armedRemove = cidr
  }

  async function doRemove(cidr: string) {
    removeError = null
    const err = await deleteDroplistEntry(cidr)
    if (err) {
      removeError = err
      return
    }
    await onrefresh()
  }

  // openFlag is the same handoff Topography.svelte's dial rows use
  // (#724) to open a flag's drawer on the flags tab -- topologyNav.svelte.ts's
  // pendingFlagId, read and cleared by Flags.svelte on arrival.
  function openFlag(id: string) {
    topologyNavState.requestFlag(id)
    appState.view = 'flags'
  }

  // Round 28's arm-then-confirm gesture (EngineRoom.svelte's own
  // revoke/remove buttons): a click anywhere that isn't the armed
  // button itself disarms it, so a stray click elsewhere can't trigger
  // a revoke or a removal.
  function disarmAll() {
    armedRevokeKey = false
    armedRemove = null
  }
</script>

<svelte:window onclick={disarmAll} />

<div class="wleft">
  {#if resp.routers && resp.routers.length > 0}
    {#each resp.routers as r (r.device)}
      <div class="orow">
        <span>{r.device}</span>
        <span class="ov">holds {r.held} of {r.total} · confirmed {formatRelative(r.confirmedAt, appState.now)}</span>
      </div>
    {/each}
  {:else}
    <div class="orow">
      <span>router</span>
      <span class="ov dim">nothing heard yet — no router has reported its address lists</span>
    </div>
  {/if}

  <div class="orow">
    <span>key</span>
    <span class="ov">
      {#if resp.key.present}
        minted {formatRelative(resp.key.createdAt ?? '', appState.now)} by {resp.key.createdBy}
        · {resp.key.lastUsedAt ? `last fetched ${formatRelative(resp.key.lastUsedAt, appState.now)}` : 'never fetched'}
        · <button type="button" class="olink" disabled={submittingKey} onclick={mintKey}>replace key</button>
        ·
        <button
          type="button"
          class="olink revoke"
          class:armed={armedRevokeKey}
          disabled={submittingKey}
          onclick={onRevokeKeyClick}
        >
          {armedRevokeKey ? 'confirm — the router can no longer fetch the list' : 'revoke'}
        </button>
      {:else}
        no key — the router cannot fetch the list until one is minted ·
        <button type="button" class="olink" disabled={submittingKey} onclick={mintKey}>mint key</button>
      {/if}
    </span>
  </div>
  {#if keyError}<p class="oghint err" role="alert">{keyError}</p>{/if}

  {#if justMinted}
    <div class="reveal">
      <div class="rk">
        <code>{justMinted.key}</code>
        <button type="button" class="olink" onclick={() => copyBlock('reveal-key', justMinted?.key ?? '')}>
          {copiedLabel === 'reveal-key' ? 'copied' : 'copy'}
        </button>
      </div>
      <div class="rnote">shown once — mikroview does not keep it in the clear, so copy it now</div>
      <div class="rk">
        <code>{justMinted.scheduler}</code>
        <button type="button" class="olink" onclick={() => copyBlock('reveal-scheduler', justMinted?.scheduler ?? '')}>
          {copiedLabel === 'reveal-scheduler' ? 'copied' : 'copy'}
        </button>
      </div>
      <button type="button" class="olink quiet" onclick={() => (justMinted = null)}>done</button>
    </div>
  {/if}

  <div class="orow">
    <span>setup</span>
    <span class="ov">
      <button type="button" class="olink" onclick={() => (setupOpen = !setupOpen)}>
        {setupOpen ? 'setup ▾' : 'setup ▸'}
      </button>
    </span>
  </div>

  {#if setupOpen}
    <div class="setupcard">
      <p class="oghint">printed, never applied — paste these on the router yourself</p>
      <div class="setupblock">
        <span class="sblab">scheduler</span>
        <code>{resp.setup.scheduler}</code>
        <button type="button" class="olink" onclick={() => copyBlock('setup-scheduler', resp.setup.scheduler)}>
          {copiedLabel === 'setup-scheduler' ? 'copied' : 'copy'}
        </button>
      </div>
      <div class="setupblock">
        <span class="sblab">drop rule</span>
        <code>{resp.setup.rule}</code>
        <button type="button" class="olink" onclick={() => copyBlock('setup-rule', resp.setup.rule)}>
          {copiedLabel === 'setup-rule' ? 'copied' : 'copy'}
        </button>
      </div>
      <div class="setupblock">
        <span class="sblab">emergency: disable the rule</span>
        <code>{resp.setup.disableRule}</code>
        <button type="button" class="olink" onclick={() => copyBlock('setup-disable', resp.setup.disableRule)}>
          {copiedLabel === 'setup-disable' ? 'copied' : 'copy'}
        </button>
      </div>
      <div class="setupblock">
        <span class="sblab">emergency: empty the list</span>
        <code>{resp.setup.emptyList}</code>
        <button type="button" class="olink" onclick={() => copyBlock('setup-empty', resp.setup.emptyList)}>
          {copiedLabel === 'setup-empty' ? 'copied' : 'copy'}
        </button>
      </div>
    </div>
  {/if}

  {#if !resp.ownRangesKnown}
    <p class="oghint">
      no router has reported its addresses yet, so new entries are not checked against the router's own ranges
    </p>
  {/if}
</div>

<div class="wrows">
  <div class="dform">
    <label class="lab">
      address
      <input
        type="text"
        placeholder="203.0.113.0/24"
        disabled={adding}
        bind:value={newCidr}
        bind:this={addressInputEl}
      />
    </label>
    <label class="lab">
      reason
      <input type="text" placeholder="why" disabled={adding} bind:value={newReason} />
    </label>
    <button type="button" class="olink" disabled={adding} onclick={submitAdd}>
      {adding ? 'adding…' : 'add'}
    </button>
  </div>
  {#if addError}<p class="oghint err" role="alert">{addError}</p>{/if}
  {#if addWarning}<p class="oghint">{addWarning}</p>{/if}

  {#if sortedEntries.length === 0}
    <p class="oghint">nothing dropped — add an address here, or use Block… on a flag</p>
  {:else}
    {#each sortedEntries as e (e.cidr)}
      <div class="drow">
        <div class="dline">
          <b>{e.cidr}</b>
          <span class="ov">{e.reason}</span>
        </div>
        <div class="dline">
          <span class="oghint dfoot">
            by {e.addedBy} {formatRelative(e.addedAt, appState.now)}
            {#if e.flagID}
              · <button type="button" class="olink" onclick={() => openFlag(e.flagID ?? '')}>from flag</button>
            {/if}
          </span>
          <button
            type="button"
            class="olink remove"
            class:armed={armedRemove === e.cidr}
            onclick={(ev) => onRemoveClick(ev, e.cidr)}
          >
            {armedRemove === e.cidr ? 'confirm — the router stops dropping it at its next fetch' : 'remove'}
          </button>
        </div>
      </div>
    {/each}
  {/if}
  {#if removeError}<p class="oghint err" role="alert">{removeError}</p>{/if}
</div>

<style>
  .wleft {
    grid-column: 1;
    min-width: 0;
  }

  .wrows {
    grid-column: 2;
    min-width: 0;
  }

  @media (max-width: 1100px) {
    .wleft,
    .wrows {
      grid-column: 1;
    }
  }

  .olink {
    background: none;
    border: none;
    padding: 0;
    font-size: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }

  .olink:hover {
    text-decoration-color: currentColor;
  }

  .olink:disabled {
    cursor: default;
    opacity: 0.6;
  }

  .olink.quiet {
    color: var(--fg-dim);
  }

  .olink.armed {
    color: var(--alarm);
  }

  .oghint {
    margin: 4px 0 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
  }

  .oghint.err {
    color: var(--reject);
    font-style: normal;
  }

  .oghint.dfoot {
    margin: 0;
  }

  .orow {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 14px;
    padding: 7px 0;
    font-size: 12px;
  }

  .orow + .orow {
    margin-top: 3px;
  }

  .orow > span:first-child {
    color: var(--fg-dim);
    flex: none;
  }

  .orow .ov {
    color: var(--fg-muted);
    text-align: right;
  }

  .orow .ov.dim {
    color: var(--fg-dim);
  }

  /* A freshly minted key: the one moment the secret exists on screen
     (copied from EngineRoom.svelte's own token reveal -- Svelte scopes
     styles per component, so this component draws its own copy). */
  .reveal {
    border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
    background: color-mix(in srgb, var(--accent) 6%, transparent);
    border-radius: 8px;
    padding: 9px 14px 8px;
    margin: 6px 0 4px;
  }

  .reveal .rk {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 12.5px;
    color: var(--fg-muted);
    flex-wrap: wrap;
  }

  .reveal code {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    background: var(--bg);
    border-radius: 5px;
    padding: 3px 9px;
    user-select: all;
    letter-spacing: 0.02em;
    word-break: break-all;
  }

  .reveal .olink {
    margin-left: auto;
  }

  .rnote {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 6px;
  }

  .setupcard {
    margin-top: 4px;
    padding-top: 4px;
  }

  .setupblock {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: 8px;
  }

  .setupblock .sblab {
    font-size: 11px;
    color: var(--fg-dim);
  }

  .setupblock code {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--fg);
    background: var(--bg-hover);
    border-radius: 5px;
    padding: 6px 9px;
    white-space: pre-wrap;
    word-break: break-all;
  }

  .setupblock .olink {
    align-self: flex-start;
  }

  .dform {
    display: flex;
    align-items: flex-end;
    gap: 14px;
    flex-wrap: wrap;
    padding-bottom: 8px;
    border-bottom: 1px solid var(--border);
  }

  .dform .lab {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 11px;
    color: var(--fg-dim);
    flex: 1;
    min-width: 140px;
  }

  .dform input {
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    padding: 3px 0;
    outline: none;
  }

  .dform input:focus {
    border-bottom-color: var(--accent);
  }

  .drow {
    padding: 6px 0;
  }

  .drow + .drow {
    border-top: 1px solid var(--border);
  }

  .dline {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
    font-size: 12px;
  }

  .dline b {
    font-family: var(--font-mono);
    font-weight: 600;
    color: var(--fg);
  }

  .dline .ov {
    color: var(--fg-muted);
    text-align: right;
  }
</style>
