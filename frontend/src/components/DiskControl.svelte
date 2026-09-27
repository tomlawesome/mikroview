<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The on-disk history's switch, as Settings' disk group (#910). Round
  // 42 draws it directly under the memory group
  // (docs/design/concepts/round-42/disk.html, `#diskg`) as the same two
  // things one storey down: a bar of the days held, one cell a day, and
  // a track for the days allowed on a doubling scale from 1 d to 365 d,
  // with the byte cap as a figure in the row that opens a field in place,
  // and off as a link.
  //
  // Every change that would delete something is a proposal until a link
  // that names the deletion is taken -- `delete 13 days · keep all 27`
  // -- the memory slider's own shrink idiom (round 39, MemoryControl).
  // Turning on deletes nothing, so it is immediate: it takes what memory
  // already holds and every day after (owner, 2026-09-03). Turning off
  // deletes nothing either since #1354, so it is immediate too: the
  // files stay, the off state shows them, and `Delete history files`
  // deletes them -- armed by a first click (EngineRoom's armedRemove
  // idiom), then an inline password field (PasskeysOverlay's
  // removePassword idiom). Admin only; a reader sees neither.
  //
  // Without a key there is no control at all: the group is two
  // statements and a link to the guide, the ingest group's `plain syslog
  // · off — loopback only` idiom. An operator still finds the feature;
  // there is no dead switch.
  //
  // Round 43 (#921) adds the `state` row -- the state store, the other
  // thing mikroview keeps on disk -- beside the key. The unanswered
  // state (`dfail`, the settings GET failed) has no settings to render
  // from, so EngineRoom draws that one row itself.
  //
  // The section wrapper (#diskg, .stsection.wide, its state classes and
  // the <h3>) is EngineRoom's, so the card's own grid and dividers apply
  // to it; this component renders the two columns inside and publishes
  // which of the round's states it is in through `phase`.
  import { deleteHistoryFiles, setHistorySettings } from '../lib/api'
  import { TRACK_X0, TRACK_X1, formatSize } from '../lib/memory'
  import {
    DAYS_MAX,
    DAYS_MIN,
    DAY_TICKS,
    DELETE_FILES_LABEL,
    HOW_TO_MOUNT_URL,
    barLabel,
    bytesOfMib,
    capMark,
    dayX,
    daysAtX,
    deleteArmedLabel,
    deletePasswordLabel,
    heldRow,
    memoryHint,
    mibOf,
    pageStepDays,
    proposeCap,
    proposeDays,
    stepDays,
    type DiskPhase,
    type DiskProposal,
  } from '../lib/history'
  import type { HistorySettings, Stats } from '../lib/types'

  let {
    settings,
    stats,
    canEdit,
    stateStore = null,
    phase = $bindable('rest'),
    onchanged,
  }: {
    settings: HistorySettings
    stats: Stats | null
    /** Whether this caller may move anything -- admin only. */
    canEdit: boolean
    /**
     * The `state` row (round 43): which backend keeps flags, definitions,
     * watchlist entries, entities and tokens -- the other thing on disk,
     * beside the key. Null leaves the row out.
     */
    stateStore?: string | null
    /** Which of round 42's states the group is in, for the section's classes. */
    phase?: DiskPhase
    /** Called with the server's new state after it has accepted a change. */
    onchanged?: (next: HistorySettings) => void
  } = $props()

  // One proposal at a time: days from the track or a cap from the
  // field. Opening one closes the other.
  let proposedDays = $state<number | null>(null)
  let capEditing = $state(false)
  let capText = $state('')
  let applying = $state(false)
  let error = $state<string | null>(null)
  let dragging = $state(false)
  let capInput = $state<HTMLInputElement | null>(null)

  // The delete action (#1354): at rest, armed by a first click, then
  // asking for the password after a second. A click anywhere else
  // disarms it; once asking, only its own `keep them` or a success
  // closes the field, so a mistyped password can be tried again.
  let deleteArmed = $state(false)
  let deleteAsking = $state(false)
  let deletePassword = $state('')
  let deleting = $state(false)
  let deleteError = $state<string | null>(null)
  let passwordInput = $state<HTMLInputElement | null>(null)

  const keptDays = $derived(!settings.enabled && settings.held ? settings.held.days : 0)

  $effect(() => {
    if (deleteAsking) passwordInput?.focus()
  })

  // Nothing held any more (deleted, or turned back on elsewhere): the
  // action has nothing to act on, so it goes back to rest.
  $effect(() => {
    if (keptDays === 0) resetDelete()
  })

  const proposal = $derived.by<DiskProposal | null>(() => {
    if (proposedDays !== null) return proposeDays(settings, proposedDays)
    if (capEditing) {
      const bytes = bytesOfMib(capText)
      return bytes === null ? null : proposeCap(settings, bytes)
    }
    return null
  })

  const current = $derived.by<DiskPhase>(() => {
    if (!settings.keyed) return 'dnokey'
    if (proposal) return proposal.kind
    if (!settings.enabled) return 'dstopped'
    if (settings.capped) return 'dcapped'
    return 'rest'
  })

  $effect(() => {
    phase = current
  })

  $effect(() => {
    if (capEditing) capInput?.focus()
  })

  const shownDays = $derived(proposedDays ?? settings.days)
  const handleX = $derived(dayX(shownDays))
  const ghostX = $derived(dayX(settings.days))
  const mark = $derived(capMark(proposal?.kind === 'dcap' ? proposal.maxBytes : settings.maxBytes, settings.bytesPerDay))
  const row = $derived(heldRow(settings))
  const restLabel = $derived(barLabel(settings))

  // The ring's real reach, oldestHeld to now, for the stopped state's
  // "~9 h of them" -- the same axis the memory bar's own labels claim.
  const memoryReach = $derived.by(() => {
    const iso = stats?.oldestHeld
    if (!iso) return null
    const t = Date.parse(iso)
    if (!Number.isFinite(t)) return null
    return Math.max(0, (Date.now() - t) / 3600000)
  })

  // The bar: one cell per held day, the oldest at the left. Round 42
  // shades each day by what it held; the API carries one figure for the
  // whole window, so every cell wears the same shade (see the PR).
  const cellCount = $derived(settings.held?.days ?? 0)
  const cellW = $derived(cellCount > 0 ? 500 / cellCount : 0)
  const weekTicks = $derived.by(() => {
    const out: number[] = []
    for (let i = 7; i < cellCount; i += 7) out.push(i)
    return out
  })
  const cutX = $derived(proposal?.cut === null || proposal?.cut === undefined ? null : 8 + proposal.cut * cellW)

  function proposeFromPointer(event: PointerEvent, svg: SVGSVGElement) {
    const box = svg.getBoundingClientRect()
    if (box.width <= 0) return
    // The viewBox is 520 units wide whatever the element's pixel width
    // is, so the pointer is mapped back into those units first.
    openDays(daysAtX(((event.clientX - box.left) / box.width) * 520))
  }

  function openDays(days: number) {
    capEditing = false
    proposedDays = days
    error = null
  }

  function onPointerDown(event: PointerEvent) {
    const svg = event.currentTarget as SVGSVGElement
    svg.setPointerCapture(event.pointerId)
    dragging = true
    proposeFromPointer(event, svg)
  }

  function onPointerMove(event: PointerEvent) {
    if (!dragging) return
    proposeFromPointer(event, event.currentTarget as SVGSVGElement)
  }

  function onPointerUp(event: PointerEvent) {
    const svg = event.currentTarget as SVGSVGElement
    if (svg.hasPointerCapture(event.pointerId)) svg.releasePointerCapture(event.pointerId)
    dragging = false
  }

  function onKeyDown(event: KeyboardEvent) {
    switch (event.key) {
      case 'ArrowLeft':
      case 'ArrowDown':
        openDays(stepDays(shownDays, -1))
        break
      case 'ArrowRight':
      case 'ArrowUp':
        openDays(stepDays(shownDays, 1))
        break
      case 'PageUp':
        openDays(pageStepDays(shownDays, 1))
        break
      case 'PageDown':
        openDays(pageStepDays(shownDays, -1))
        break
      case 'Home':
        openDays(DAYS_MIN)
        break
      case 'End':
        openDays(DAYS_MAX)
        break
      case 'Enter':
        if (proposal) apply()
        break
      case 'Escape':
        keep()
        break
      default:
        return
    }
    event.preventDefault()
  }

  function openCap() {
    proposedDays = null
    capText = String(mibOf(settings.maxBytes))
    capEditing = true
    error = null
  }

  function onCapKeyDown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault()
      if (proposal) apply()
    } else if (event.key === 'Escape') {
      event.preventDefault()
      keep()
    }
  }

  async function send(body: { enabled: boolean; days: number; maxBytes: number }): Promise<boolean> {
    if (applying) return false
    applying = true
    error = null
    const result = await setHistorySettings(body)
    applying = false
    if (typeof result === 'string') {
      // The server's own words -- which bound was hit, or why it
      // refused -- rather than "failed".
      error = result
      return false
    }
    onchanged?.(result)
    return true
  }

  async function apply() {
    if (!proposal) return
    const ok = await send({ enabled: proposal.enabled, days: proposal.days, maxBytes: proposal.maxBytes })
    if (ok) keep()
  }

  function keep() {
    proposedDays = null
    capEditing = false
    error = null
  }

  async function turnOn() {
    // Deletes nothing, so it is not a proposal: immediate.
    keep()
    await send({ enabled: true, days: settings.days, maxBytes: settings.maxBytes })
  }

  async function turnOff() {
    // Keeps the files (#1354), so, like turning on, not a proposal.
    keep()
    await send({ enabled: false, days: settings.days, maxBytes: settings.maxBytes })
  }

  function onDeleteClick(event: MouseEvent) {
    // Stopped so the window's own click, which disarms, does not undo
    // the arming this click just did.
    event.stopPropagation()
    if (deleteArmed) {
      deleteArmed = false
      deleteAsking = true
      deleteError = null
      return
    }
    deleteArmed = true
  }

  function disarmDelete() {
    deleteArmed = false
  }

  function resetDelete() {
    deleteArmed = false
    deleteAsking = false
    deletePassword = ''
    deleteError = null
  }

  async function submitDelete() {
    if (deleting || !deletePassword) return
    deleting = true
    deleteError = null
    let result: HistorySettings | string
    try {
      result = await deleteHistoryFiles(deletePassword)
    } catch {
      // An unreadable answer: the delete may or may not have happened,
      // so say so and let the card's own refresh show which.
      result = 'the server answered, but not in a form this page could read -- reload to see what is on disk'
    } finally {
      deleting = false
    }
    if (typeof result === 'string') {
      // The server's own words -- "incorrect password", "turn history
      // off first" -- and the field stays open for another try.
      deleteError = result
      return
    }
    resetDelete()
    onchanged?.(result)
  }

  function onPasswordKeyDown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault()
      submitDelete()
    } else if (event.key === 'Escape') {
      event.preventDefault()
      resetDelete()
    }
  }
</script>

<svelte:window onclick={disarmDelete} />

{#snippet track()}
  <line x1={TRACK_X0} y1="24" x2={TRACK_X1} y2="24" class="mrail" />
  {#each DAY_TICKS as tick (tick)}
    <line x1={dayX(tick)} y1="27" x2={dayX(tick)} y2="31" class="mrail" />
  {/each}
  <!-- Where the cap runs out at today's rate: a handle right of this
       mark means the cap decides, not the days. -->
  {#if mark}
    <line x1={dayX(mark.days)} y1="14" x2={dayX(mark.days)} y2="34" class="mcap" />
  {/if}
  <line x1={TRACK_X0} y1="24" x2={handleX} y2="24" class="mfill" />
  {#if proposedDays !== null && proposedDays !== settings.days}
    <circle class="mghost" cx={ghostX} cy="24" r="4.5" />
  {/if}
  <circle class="mh" cx={handleX} cy="24" r="6.5" />
  <text x={handleX} y="11" text-anchor="middle" class="sp-k">{shownDays} d</text>
  <text x={TRACK_X0} y="46" class="sp-n">1 d</text>
  {#if mark}
    <text x={dayX(mark.days)} y="46" text-anchor="middle" class="sp-n quiet">{mark.label}</text>
  {/if}
  <text x={TRACK_X1} y="46" text-anchor="end" class="sp-n">365 d</text>
{/snippet}

{#if settings.keyed}
  <div class="wleft">
    {#if settings.enabled && settings.held && cellCount > 0}
      <svg
        class="stmem"
        viewBox="0 0 520 58"
        role="img"
        aria-label="The days held on disk, the oldest at the left; the newest is today"
      >
        <rect x="8" y="20" width="500" height="10" rx="5" fill="var(--bg-hover)" />
        {#each { length: cellCount } as _, i (i)}
          <rect x={8 + i * cellW} y="20" width={cellW} height="10" fill="var(--accent)" opacity="0.16" />
        {/each}
        {#each weekTicks as i (i)}
          <line x1={8 + i * cellW} y1="31" x2={8 + i * cellW} y2="35" class="mrail thin" />
        {/each}
        <rect x="504" y="15" width="3" height="20" rx="1.5" fill="var(--now)" />
        <!-- The days a proposal would let go, dimmed where they are, with
             the new oldest day marked -- as the hours are on the memory
             bar. Drawn only when something really would go. -->
        {#if proposal && cutX !== null}
          <g class="dcut">
            <rect x="8" y="20" width={Math.max(0, Math.min(cutX, 502) - 8)} height="10" rx="5" fill="var(--bg)" opacity="0.75" />
            {#if proposal.newOldest !== null}
              <line x1={cutX} y1="15" x2={cutX} y2="35" stroke="var(--fg-muted)" stroke-width="1.2" />
              <text
                x={cutX > 300 ? cutX - 4 : cutX + 4}
                y="50"
                text-anchor={cutX > 300 ? 'end' : 'start'}
                class="sp-n">{proposal.cutLabel}</text
              >
            {:else}
              <text x="8" y="50" class="sp-n">{proposal.cutLabel}</text>
            {/if}
          </g>
        {:else if restLabel}
          <text x="8" y="50" class="sp-n">{restLabel}</text>
        {/if}
        <text x="508" y="50" text-anchor="end" class="sp-k">today</text>
      </svg>
    {/if}
    {#if settings.enabled}
      <p class="oghint">one encrypted file a day; the oldest day lets go when the days or the cap is reached, whichever first</p>
    {:else}
      <p class="oghint">{memoryHint(memoryReach, keptDays > 0)}</p>
    {/if}
    {#if canEdit && keptDays > 0}
      <div class="delfiles">
        {#if deleteAsking}
          <label class="pwlabel">
            {deletePasswordLabel(keptDays)}
            <input
              class="oin pw"
              type="password"
              autocomplete="current-password"
              bind:this={passwordInput}
              bind:value={deletePassword}
              onkeydown={onPasswordKeyDown}
            />
          </label>
          <button class="olink" disabled={deleting || !deletePassword} onclick={submitDelete}>{deleting ? 'deleting…' : 'submit'}</button>
          ·
          <button class="olink" disabled={deleting} onclick={resetDelete}>keep them</button>
        {:else}
          <button class="olink delete" class:armed={deleteArmed} onclick={onDeleteClick}>
            {deleteArmed ? deleteArmedLabel(keptDays) : DELETE_FILES_LABEL}
          </button>
        {/if}
      </div>
      {#if deleteError}
        <p class="oghint err" role="alert">{deleteError}</p>
      {/if}
    {/if}

    <!-- An admin drags it; everyone else reads it. No lock icon and no
         disabled styling for the reader: the same track and the same
         figure, simply without the drag on offer (MemoryControl's rule). -->
    {#if canEdit}
      <svg
        class="stmemctl"
        viewBox="0 0 520 54"
        role="slider"
        tabindex="0"
        aria-label="Days kept on disk"
        aria-valuemin={DAYS_MIN}
        aria-valuemax={DAYS_MAX}
        aria-valuenow={shownDays}
        aria-valuetext="{shownDays} days, under a {formatSize(settings.maxBytes)} cap"
        onpointerdown={onPointerDown}
        onpointermove={onPointerMove}
        onpointerup={onPointerUp}
        onpointercancel={onPointerUp}
        onkeydown={onKeyDown}
      >
        {@render track()}
      </svg>
    {:else}
      <svg
        class="stmemctl still"
        viewBox="0 0 520 54"
        role="img"
        aria-label="{settings.days} days are kept on disk, under a {formatSize(settings.maxBytes)} cap"
      >
        {@render track()}
      </svg>
    {/if}

    {#if proposal}
      <p class="oghint memnote">
        {proposal.sentence} —
        <button class="olink" disabled={applying} onclick={apply}>{applying ? 'applying…' : proposal.applyLabel}</button>
        ·
        <button class="olink" disabled={applying} onclick={keep}>{proposal.keepLabel}</button>
      </p>
    {/if}
    {#if error}
      <p class="oghint err" role="alert">{error}</p>
    {/if}
  </div>
{/if}

<div class="wrows">
  <div class="orow">
    <span>on disk</span>
    <span class="ov" class:dim={row === null}>{row ?? 'nothing'}</span>
  </div>
  {#if settings.keyed}
    <div class="orow">
      <span>allowed</span>
      <span class="ov">
        {shownDays} days · at most
        {#if capEditing}
          <input
            class="oin"
            bind:this={capInput}
            bind:value={capText}
            aria-label="Byte cap, MiB"
            inputmode="numeric"
            onkeydown={onCapKeyDown}
          /> MiB
        {:else if canEdit}
          <button class="olink" onclick={openCap}>{formatSize(settings.maxBytes)}</button>
        {:else}
          {formatSize(settings.maxBytes)}
        {/if}
        {#if !settings.enabled && !canEdit}
          · <span class="dim">off</span>
        {:else if !settings.enabled}
          · <button class="olink" disabled={applying} onclick={turnOn}>turn on</button>
        {:else if canEdit}
          · <button class="olink" disabled={applying} onclick={turnOff}>turn off</button>
        {/if}
      </span>
    </div>
  {/if}
  <div class="orow">
    <span>key</span>
    <span class="ov">
      {#if settings.keyed}
        <span class="dim">mounted at start</span>
      {:else}
        none mounted — nothing is kept on disk without one ·
        <a
          class="olink ext"
          href={HOW_TO_MOUNT_URL}
          target="_blank"
          rel="noopener noreferrer"
          title="Opens the setup guide on github.com, in a new tab"
        >how to mount one</a>
      {/if}
    </span>
  </div>
  {#if stateStore}
    <div class="orow">
      <span>state</span>
      <span class="ov dim">{stateStore}</span>
    </div>
  {/if}
</div>

<style>
  /* Ported from docs/design/concepts/round-42/disk.html's #diskg, which
     carries round 39's .stmem/.stmemctl/.orow grammar plus its own .oin
     and .dcut. The mockup's tokens land on this app's custom properties
     the way MemoryControl's do (--hair-2 -> --border, --raised -> --bg,
     --ink-3 -> --fg-dim, --ink-2 -> --fg-muted, --void -> --bg). The
     two columns sit in EngineRoom's .stsection.wide grid. */
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

  .stmem,
  .stmemctl {
    width: 100%;
    height: auto;
    display: block;
  }

  .stmemctl {
    margin: 10px 0 0;
  }

  .stmemctl:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .mrail {
    stroke: var(--border);
    stroke-width: 2;
    stroke-linecap: round;
  }

  .mrail.thin {
    stroke-width: 1;
  }

  .mcap {
    stroke: var(--fg-dim);
    stroke-dasharray: 2 2;
  }

  .mfill {
    stroke: var(--accent);
    stroke-width: 2;
    stroke-linecap: round;
    opacity: 0.55;
  }

  .mh {
    fill: var(--bg);
    stroke: var(--accent);
    stroke-width: 1.6;
    cursor: grab;
  }

  .stmemctl.still .mh {
    cursor: default;
  }

  .mghost {
    fill: none;
    stroke: var(--fg-dim);
    stroke-dasharray: 2 2;
  }

  .sp-n {
    font-family: var(--font-mono);
    font-size: 9.5px;
    fill: var(--fg-dim);
  }

  .sp-n.quiet {
    opacity: 0.75;
  }

  .sp-k {
    font-family: var(--font-mono);
    font-size: 10px;
    fill: var(--fg-muted);
  }

  .oghint {
    margin: 2px 0 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
  }

  .memnote {
    color: var(--fg-muted);
    margin-top: 2px;
  }

  .oghint.err {
    color: var(--reject);
    font-style: normal;
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

  .orow .ov.dim,
  .orow .ov .dim {
    color: var(--fg-dim);
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

  /* The delete action (#1354): quiet at rest, the alarm colour once
     armed, as EngineRoom's armed remove is. */
  .delfiles {
    margin-top: 6px;
    font-size: 11.5px;
    color: var(--fg-muted);
  }

  .olink.delete {
    color: var(--fg-dim);
  }

  .olink.delete.armed {
    color: var(--alarm);
  }

  .pwlabel {
    margin-right: 6px;
  }

  .oin.pw {
    width: 16ch;
    text-align: left;
    margin-left: 6px;
  }

  /* The cap's own field, in the row where the figure was. */
  .oin {
    font: 600 11px var(--font-mono);
    color: var(--fg);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 4px;
    width: 5ch;
    padding: 1px 4px;
    text-align: right;
  }
</style>
