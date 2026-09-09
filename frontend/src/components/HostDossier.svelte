<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The device dossier card (#410), built to the composition settled on
  // that issue ("Build go-ahead: the card", 2026-09-09) and to the
  // ratified "dossier of field marks" vocabulary in
  // docs/design/screens/entities/DESIGN.md.
  //
  // Seven sections, top to bottom, in this order and no other:
  //   1. Head            — the address, its name or "unnamed", last seen
  //   2. Reads as        — the suggestion, its confidence in words, its
  //                        evidence, and what else it could be
  //   3. Field marks     — Seen, Names, MAC, Address, Traffic, Firewall
  //   4. Not available   — the backend's `absent` list, one line
  //   5. Suggested probe — printed, never run
  //   6. Honesty line    — nothing here was probed or looked up
  //   7. Name this device— the footer action into Entities
  //
  // Rules the design fixes and this file keeps: identity is text, so
  // confidence is the backend's own word and never a percentage or a
  // colour; alarm ink appears nowhere; everything survives greyscale.
  // The card is one region with headings. If a block does not fit, the
  // card scrolls -- the type never shrinks.
  //
  // A single instance is mounted at the app root (App.svelte) and driven
  // by lib/dossier.svelte.ts's singleton, so the four places that open a
  // dossier -- the per-IP investigate popover, the Entities host row,
  // the city building card and the topography host card -- each need one
  // call and no state of their own.
  import { dossierState } from '../lib/dossier.svelte'
  import { nameEditorState } from '../lib/nameEditor.svelte'
  import { countryFlag, formatRelative } from '../lib/format'
  import CopyButton from './CopyButton.svelte'
  import GhostRows from './GhostRows.svelte'
  import type { DossierPeer } from '../lib/types'

  const titleId = 'host-dossier-title'

  let sheetEl: HTMLDivElement | undefined = $state()
  let nameBtnEl: HTMLButtonElement | undefined = $state()
  let headEditEl: HTMLButtonElement | undefined = $state()

  const d = $derived(dossierState.data)
  // One clock read per assembled card. The dossier is a snapshot with
  // its own generatedAt, not a live view, so a ticking "12m ago" that
  // kept moving under a static card would imply a freshness it does not
  // have.
  const nowMs = $derived.by(() => {
    dossierState.data
    return Date.now()
  })

  const displayName = $derived(d?.names.name ?? '')

  function ago(iso: string | undefined): string {
    return iso ? formatRelative(iso, nowMs) : 'never'
  }

  // The peer line: flag, name or address, how much traffic, and the
  // ports it involved. The flag is decoration next to the country code
  // it stands for, never the only carrier of the fact.
  function peerPorts(p: DossierPeer): string {
    return p.ports && p.ports.length > 0 ? ` · ports ${p.ports.join(', ')}` : ''
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') dossierState.close()
  }

  // Focus moves into the card on open and back to the trigger on close
  // (lib/dossier.svelte.ts holds the trigger). Not a trap: like the
  // app's other overlays this closes on Esc or a backdrop click, and
  // lib/focusTrap.ts's own comment reserves the real trap for the one
  // control that has no other route.
  $effect(() => {
    if (dossierState.isOpen && sheetEl) sheetEl.focus()
  })

  // 7. Name this device. The editor prefills with the suggested label
  // when the host has no name of its own, so the common case is
  // confirming a reading rather than typing one.
  function nameThisDevice() {
    const ip = dossierState.ip
    if (!ip || !nameBtnEl) return
    nameEditorState.open('host', ip, '', nameBtnEl.getBoundingClientRect(), d?.identity.label ?? '')
  }

  // 1. The head's own pencil, the same editor by the same route.
  function editName() {
    const ip = dossierState.ip
    if (!ip || !headEditEl) return
    nameEditorState.open('host', ip, '', headEditEl.getBoundingClientRect(), d?.identity.label ?? '')
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if dossierState.isOpen}
  <div class="scrim" onclick={() => dossierState.close()} role="presentation"></div>
  <div
    bind:this={sheetEl}
    class="sheet"
    role="dialog"
    aria-modal="true"
    aria-labelledby={titleId}
    tabindex="-1"
    data-testid="host-dossier"
  >
    <!-- 1. Head. Present in every state, so a card that is still
         loading or that failed still says which address it is about. -->
    <div class="head">
      <h2 id={titleId}>
        <span class="ip">{dossierState.ip}</span>
        <span class="name" class:unnamed={!displayName}>{displayName || 'unnamed'}</span>
      </h2>
      <div class="head-acts">
        {#if dossierState.ip}
          <CopyButton value={dossierState.ip} label="host address" />
        {/if}
        {#if nameEditorState.available}
          <button
            bind:this={headEditEl}
            type="button"
            class="edit"
            onclick={editName}
            title="Edit name for {dossierState.ip}"
            aria-label="Edit name for {dossierState.ip}">✎</button
          >
        {/if}
        <button type="button" class="close" onclick={() => dossierState.close()} aria-label="Close">✕</button>
      </div>
    </div>

    {#if dossierState.loading}
      <GhostRows label="Assembling the dossier…" rows={7} />
    {:else if dossierState.error}
      <div class="failed" role="alert">
        <p>{dossierState.error}</p>
        <button type="button" class="retry" onclick={() => dossierState.retry()}>Try again</button>
      </div>
    {:else if d}
      <div class="body">
        <p class="seen-line">last seen {ago(d.seen.lastSeen)}</p>

        <!-- 2. Reads as. Never more than the backend says: no
             percentage, no colour, no claim the evidence cannot carry.
             With nothing suggested, the backend's note is the whole
             section. -->
        <section aria-labelledby="hd-reads">
          <h3 id="hd-reads">Reads as</h3>
          {#if d.identity.suggested}
            <p class="reads">
              <strong>reads as {d.identity.label}</strong>
              {#if d.identity.confidence}<span class="conf"> · {d.identity.confidence}</span>{/if}
            </p>
            {#if d.identity.because}<p class="because">because {d.identity.because}</p>{/if}
          {:else}
            <p class="note">{d.identity.note}</p>
          {/if}
          {#if d.identity.evidence && d.identity.evidence.length > 0}
            <ul class="evidence">
              {#each d.identity.evidence as e (e.signal + e.detail)}
                <li><span class="sig">{e.signal}</span> — {e.detail}</li>
              {/each}
            </ul>
          {/if}
          {#if d.identity.alternatives && d.identity.alternatives.length > 0}
            <p class="alts">could also be {d.identity.alternatives.join(', ')}</p>
          {/if}
          {#if d.identity.suggested && d.identity.note}<p class="note">{d.identity.note}</p>{/if}
        </section>

        <!-- 3. Field marks, in the order the design fixes. -->
        <section aria-labelledby="hd-seen">
          <h3 id="hd-seen">Seen</h3>
          {#if d.seen.known}
            <p>first seen {ago(d.seen.firstSeen)} · last seen {ago(d.seen.lastSeen)} · {d.seen.events} events</p>
            {#if d.seen.firstSeenSource}<p class="from">first seen from {d.seen.firstSeenSource}</p>{/if}
            {#if d.seen.interfaces && d.seen.interfaces.length > 0}
              <p class="from">on {d.seen.interfaces.join(', ')}</p>
            {/if}
          {/if}
          {#if d.seen.note}<p class="note">{d.seen.note}</p>{/if}
        </section>

        <section aria-labelledby="hd-names">
          <h3 id="hd-names">Names</h3>
          {#if d.names.known}
            <p>{d.names.name}</p>
            {#if d.names.sourceNote}<p class="from">{d.names.sourceNote}</p>{/if}
            {#if d.names.ownLabel && d.names.ownLabel !== d.names.name}
              <p class="from">your own label “{d.names.ownLabel}” is saved, but is not what is shown</p>
            {/if}
          {/if}
          {#if d.names.note}<p class="note">{d.names.note}</p>{/if}
        </section>

        <section aria-labelledby="hd-mac">
          <h3 id="hd-mac">MAC</h3>
          {#if d.mac.known}
            <p class="mono">
              {d.mac.address}
              {#if d.mac.source}<span class="from"> · {d.mac.source}</span>{/if}
            </p>
            <!-- The locally-administered bit, on its own line and
                 prominent: it is the fact this issue exists to surface,
                 because it redirects the whole identification. -->
            {#if d.mac.locallyAdministered}
              <p class="laa" data-testid="dossier-laa">
                {d.mac.locallyAdministeredNote ||
                  'no vendor exists: this is a VM, container or randomised MAC'}
              </p>
            {:else if d.mac.vendor.known}
              <p>{d.mac.vendor.name}{d.mac.vendor.oui ? ` · ${d.mac.vendor.oui}` : ''}</p>
            {:else if d.mac.vendor.reason}
              <p class="note">{d.mac.vendor.reason}</p>
            {/if}
            {#if d.mac.groupNote}<p class="note">{d.mac.groupNote}</p>{/if}
            <!-- A vendor name is never shown without the registry that
                 supplied it, and a stale registry says so. -->
            {#if d.mac.vendor.known && d.mac.registry.source}
              <p class="from">
                vendor from {d.mac.registry.source}{d.mac.registry.stale ? ' · stale' : ''}
              </p>
            {/if}
            {#if d.mac.registry.note}<p class="from">{d.mac.registry.note}</p>{/if}
          {/if}
          {#if d.mac.note}<p class="note">{d.mac.note}</p>{/if}
        </section>

        <section aria-labelledby="hd-address">
          <h3 id="hd-address">Address</h3>
          <p>{d.address.note}</p>
          {#if d.address.hostname}<p class="from">hostname {d.address.hostname}</p>{/if}
          {#if d.address.leaseMac}<p class="from mono">lease held by {d.address.leaseMac}</p>{/if}
          {#if d.address.arpMac}<p class="from mono">ARP pairs it with {d.address.arpMac}</p>{/if}
          {#if d.address.consulted && d.address.consulted.length > 0}
            <p class="from">consulted {d.address.consulted.join(', ')}</p>
          {/if}
        </section>

        <section aria-labelledby="hd-traffic">
          <h3 id="hd-traffic">Traffic</h3>
          {#if d.traffic.known}
            {#if d.traffic.destinations && d.traffic.destinations.length > 0}
              <p class="sub">talks to</p>
              <ul class="peers">
                {#each d.traffic.destinations as p (p.ip)}
                  <li>
                    {#if p.country}<span class="flag" aria-hidden="true">{countryFlag(p.country)}</span>{/if}
                    <span class="peer">{p.name || p.ip}</span>
                    <span class="from">{p.country ? p.country + ' · ' : ''}{p.events} events{peerPorts(p)}</span>
                  </li>
                {/each}
              </ul>
              {#if d.traffic.moreDestinations}<p class="from">and {d.traffic.moreDestinations} more</p>{/if}
            {/if}
            {#if d.traffic.talkers && d.traffic.talkers.length > 0}
              <p class="sub">talked to by</p>
              <ul class="peers">
                {#each d.traffic.talkers as p (p.ip)}
                  <li>
                    {#if p.country}<span class="flag" aria-hidden="true">{countryFlag(p.country)}</span>{/if}
                    <span class="peer">{p.name || p.ip}</span>
                    <span class="from">{p.country ? p.country + ' · ' : ''}{p.events} events{peerPorts(p)}</span>
                  </li>
                {/each}
              </ul>
              {#if d.traffic.moreTalkers}<p class="from">and {d.traffic.moreTalkers} more</p>{/if}
            {/if}
            {#if d.traffic.ports && d.traffic.ports.length > 0}
              <p class="sub">ports</p>
              <ul class="ports">
                {#each d.traffic.ports as p (p.direction + p.protocol + p.port)}
                  <li>
                    <span class="peer">{p.port}{p.protocol ? '/' + p.protocol : ''}{p.name ? ` ${p.name}` : ''}</span>
                    <span class="from">{p.direction === 'in' ? 'reached on it' : 'reached out'} · {p.events} events</span>
                  </li>
                {/each}
              </ul>
              {#if d.traffic.morePorts}<p class="from">and {d.traffic.morePorts} more</p>{/if}
            {/if}
            {#if d.traffic.cadence.known && d.traffic.cadence.shape}
              <p class="cadence">cadence: {d.traffic.cadence.shape}</p>
            {/if}
            {#if d.traffic.cadence.note}<p class="from">{d.traffic.cadence.note}</p>{/if}
          {/if}
          {#if d.traffic.note}<p class="note">{d.traffic.note}</p>{/if}
        </section>

        <section aria-labelledby="hd-firewall">
          <h3 id="hd-firewall">Firewall</h3>
          {#if d.firewall.known && d.firewall.rules}
            <ul class="rules">
              {#each d.firewall.rules as r (r.device + r.label)}
                <li>
                  <span class="peer">{r.name || r.label}</span>
                  <span class="from">
                    {[r.chain, r.action, r.device].filter(Boolean).join(' · ')} · {r.events} events
                  </span>
                  {#if r.commentKnown && r.comment}<span class="from">“{r.comment}”</span>{/if}
                </li>
              {/each}
            </ul>
            {#if d.firewall.more}<p class="from">and {d.firewall.more} more</p>{/if}
          {/if}
          {#if d.firewall.note}<p class="note">{d.firewall.note}</p>{/if}
        </section>

        <!-- 4. Not available: the card's honesty surface, one line. -->
        {#if d.absent && d.absent.length > 0}
          <p class="absent" data-testid="dossier-absent">Not available: {d.absent.join('; ')}</p>
        {/if}

        <!-- 5. Suggested probe. Printed, never run -- mikroview is a
             passive observer and connects to nothing on the LAN. -->
        {#if d.suggestedProbe && (d.suggestedProbe.command || d.suggestedProbe.url)}
          <section aria-labelledby="hd-probe" data-testid="dossier-probe">
            <h3 id="hd-probe">Suggested probe</h3>
            {#if d.suggestedProbe.command}
              <p class="probe mono">
                <code>{d.suggestedProbe.command}</code>
                <CopyButton value={d.suggestedProbe.command} label="suggested probe command" />
              </p>
            {/if}
            {#if d.suggestedProbe.url}
              <p class="probe mono">
                <code>{d.suggestedProbe.url}</code>
                <CopyButton value={d.suggestedProbe.url} label="suggested probe URL" />
              </p>
            {/if}
            <p class="from">mikroview never runs this; you would.</p>
            {#if d.suggestedProbe.note}<p class="from">{d.suggestedProbe.note}</p>{/if}
          </section>
        {/if}

        <!-- 6. The honesty line, on every card, always. -->
        <p class="honesty">
          Nothing here was probed or looked up outside the router's own pushes and the OUI registry.
        </p>

        <!-- 7. The footer action: identification flowing into Entities. -->
        {#if nameEditorState.available}
          <div class="footer">
            <button bind:this={nameBtnEl} type="button" class="name-it" onclick={nameThisDevice}>
              Name this device
            </button>
          </div>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 50;
  }

  /* Desktop: the app's existing detail-sheet presentation, anchored
     right. Phone: one full screen. The card scrolls; the type does
     not shrink (the legibility floor). */
  .sheet {
    position: fixed;
    top: 0;
    right: 0;
    bottom: 0;
    width: min(460px, 100vw);
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated);
    border-left: 1px solid var(--border);
    box-shadow: -12px 0 32px -12px rgba(0, 0, 0, 0.5);
    z-index: 51;
    font-size: 13px;
    color: var(--fg);
    overflow: hidden;
  }

  @media (max-width: 640px) {
    .sheet {
      width: 100vw;
      border-left: none;
    }
  }

  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 8px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--border);
  }

  .head h2 {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }

  .ip {
    font-family: var(--font-mono);
  }

  .name {
    font-weight: 400;
    color: var(--fg-muted);
  }

  .name.unnamed {
    font-style: italic;
    color: var(--fg-dim);
  }

  .head-acts {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .edit,
  .close {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    width: 22px;
    height: 22px;
    font-size: 11px;
    line-height: 1;
    cursor: pointer;
  }

  .edit:hover,
  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .body {
    flex: 1;
    overflow-y: auto;
    padding: 12px 14px 18px;
  }

  .seen-line {
    margin: 0 0 10px;
    color: var(--fg-muted);
  }

  section {
    margin: 0 0 14px;
  }

  h3 {
    margin: 0 0 4px;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  p {
    margin: 0 0 3px;
  }

  /* Identity is text: the confidence is a word, in the same ink as the
     rest of the line. */
  .reads strong {
    font-weight: 600;
  }

  .conf,
  .from,
  .note,
  .alts,
  .cadence {
    color: var(--fg-muted);
  }

  .sub {
    color: var(--fg-dim);
    margin-top: 6px;
  }

  .laa {
    margin: 4px 0;
    padding: 6px 8px;
    border: 1px solid var(--border);
    border-radius: 5px;
    font-weight: 600;
  }

  ul {
    margin: 0 0 4px;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 6px;
    padding: 1px 0;
  }

  .sig {
    font-weight: 600;
  }

  .mono,
  .peer {
    font-family: var(--font-mono);
  }

  .absent {
    margin: 10px 0;
    color: var(--fg-muted);
  }

  .probe {
    display: flex;
    align-items: center;
    gap: 6px;
    word-break: break-all;
  }

  .honesty {
    margin: 14px 0 0;
    padding-top: 10px;
    border-top: 1px solid var(--border);
    color: var(--fg-dim);
  }

  .footer {
    margin-top: 12px;
  }

  .name-it {
    width: 100%;
    padding: 8px 10px;
    background: transparent;
    border: 1px solid var(--accent);
    border-radius: 6px;
    color: var(--accent);
    font-size: 13px;
    cursor: pointer;
  }

  .name-it:hover {
    background: var(--accent-bg-hover);
  }

  .failed {
    padding: 16px 14px;
  }

  .retry {
    margin-top: 8px;
    padding: 5px 10px;
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 5px;
    color: var(--fg);
    cursor: pointer;
  }
</style>
