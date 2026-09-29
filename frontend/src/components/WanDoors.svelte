<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The doors panel (#1319): "Doors from the internet" -- what the
  // pushed input-chain and dst-nat tables let the internet reach, and
  // the pushed /ip service rows beside them. Ported from
  // HostDossier.svelte's own sheet (same .sheet/.head, same .from/
  // .absent/.probe/.honesty vocabulary) rather than built from an
  // impression of it.
  //
  // One section per device, in name order. Within a device, two groups
  // in this fixed order: "to the router itself" (input-chain accepts on
  // the WAN interface, plus the pushed /ip service rows once #1329 is
  // pushed) and "through to a host" (enabled dst-nat rules). Rows keep
  // the pushed table's own rule order.
  //
  // Two marks, never a judgement: "allowed through ○" is always present
  // on a rule row -- a pushed rule names the door, whatever traffic did.
  // "seen arriving ●" is the event buffer's own answer, kept apart the
  // same way #1018's port doors keep policy and traffic apart. This
  // panel evaluates no precedence and claims no port is safe or unsafe.
  import { wanDoorsState, portUnion } from '../lib/wanDoors.svelte'
  import { formatRelative } from '../lib/format'
  import CopyButton from './CopyButton.svelte'
  import GhostRows from './GhostRows.svelte'
  import type { WanDeviceDoors, WanDoor, WanService } from '../lib/api'

  const titleId = 'wan-doors-title'
  let sheetEl: HTMLDivElement | undefined = $state()

  // One clock read per open sheet, the same reasoning HostDossier's own
  // nowMs carries: a ticking "3m ago" under a static answer would imply
  // a freshness this panel does not have.
  const nowMs = $derived.by(() => {
    wanDoorsState.devices
    return Date.now()
  })

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') wanDoorsState.close()
  }

  $effect(() => {
    if (wanDoorsState.isOpen && sheetEl) sheetEl.focus()
  })

  function destinationText(door: WanDoor): string {
    if (!door.to) return 'this router'
    return door.to.name ? `${door.to.ip} · ${door.to.name}` : door.to.ip
  }

  // A rule that names no dst-port at all is still a door -- it covers
  // every port, not none -- so the row says so in words rather than
  // rendering the field's own empty string.
  function portText(door: WanDoor): string {
    if (!door.dstPort) return 'any port'
    return door.proto ? `${door.dstPort}/${door.proto}` : door.dstPort
  }

  // Line 2 of a rule row: the comment if any, then the two marks. A
  // door with no traffic in the window still reads "allowed through" --
  // that mark is the pushed table's own statement, not a claim about
  // what happened.
  function markLine(door: WanDoor): string {
    const parts: string[] = []
    if (door.comment) parts.push(door.comment)
    parts.push('allowed through ○')
    parts.push(door.lastSeen ? `seen arriving ● ${formatRelative(door.lastSeen, nowMs)}` : 'nothing arrived in the window')
    return parts.join(' · ')
  }

  function serviceLine(svc: WanService): string {
    return `${svc.name} · ${svc.port}/tcp · ${svc.address ? `from ${svc.address}` : 'any address'}`
  }

  function probeCommand(d: WanDeviceDoors): string {
    const ports = portUnion(d)
    if (ports.length === 0) return ''
    return `nmap -Pn -p ${ports.join(',')} ${d.publicAddress || 'your-public-address'}`
  }

  function routerDoors(d: WanDeviceDoors): WanDoor[] {
    return d.doors.filter((door) => !door.to)
  }

  function hostDoors(d: WanDeviceDoors): WanDoor[] {
    return d.doors.filter((door) => Boolean(door.to))
  }

  function hasAnything(d: WanDeviceDoors): boolean {
    return d.doors.length > 0 || (d.services !== null && d.services.length > 0)
  }
</script>

<svelte:window onkeydown={onKeydown} />

{#if wanDoorsState.isOpen}
  <div class="scrim" onclick={() => wanDoorsState.close()} role="presentation"></div>
  <div
    bind:this={sheetEl}
    class="sheet"
    role="dialog"
    aria-modal="true"
    aria-labelledby={titleId}
    tabindex="-1"
    data-testid="wan-doors"
  >
    <div class="head">
      <h2 id={titleId}>Doors from the internet</h2>
      <button type="button" class="close" onclick={() => wanDoorsState.close()} aria-label="Close">✕</button>
    </div>

    <div class="body">
      {#if wanDoorsState.loading && wanDoorsState.devices.length === 0}
        <GhostRows label="Reading the pushed tables…" rows={5} />
      {:else if wanDoorsState.error}
        <p class="failed" role="alert">{wanDoorsState.error}</p>
      {:else if wanDoorsState.devices.length === 0}
        <p class="from">No device has shown a WAN interface yet.</p>
      {:else}
        {#each wanDoorsState.devices as d (d.id)}
          <section aria-labelledby="wd-{d.id}">
            <h3 id="wd-{d.id}">{d.name || d.id} · {d.wan}</h3>

            {#if !hasAnything(d)}
              <p class="from" data-testid="wd-empty-{d.id}">
                Nothing in the pushed tables lets the internet in, and nothing arrived in the window.
              </p>
            {:else}
              <p class="sub">to the router itself</p>
              {#if routerDoors(d).length === 0 && d.services === null}
                <p class="absent" data-testid="wd-absent-{d.id}">
                  This router does not push /ip service yet; its own services are read from filter rules.
                </p>
              {:else}
                {#if routerDoors(d).length > 0}
                  <ul class="doors">
                    {#each routerDoors(d) as door (door.label)}
                      <li>
                        <span class="peer">{door.label} {portText(door)} → this router</span>
                        <span class="from">{markLine(door)}</span>
                      </li>
                    {/each}
                  </ul>
                {/if}
                {#if d.services !== null}
                  <p class="from">services the router itself runs</p>
                  <ul class="doors">
                    {#each d.services as svc (svc.name)}
                      <li><span class="peer">{serviceLine(svc)}</span></li>
                    {/each}
                  </ul>
                {/if}
              {/if}

              {#if hostDoors(d).length > 0}
                <p class="sub">through to a host</p>
                <ul class="doors">
                  {#each hostDoors(d) as door (door.label)}
                    <li>
                      <span class="peer">{door.label} {portText(door)} → {destinationText(door)}</span>
                      <span class="from">{markLine(door)}</span>
                    </li>
                  {/each}
                </ul>
              {/if}

              {#if probeCommand(d)}
                <p class="probe mono" data-testid="wd-probe-{d.id}">
                  <code>{probeCommand(d)}</code>
                  <CopyButton value={probeCommand(d)} label="suggested probe command for {d.name || d.id}" />
                </p>
                <p class="from">MikroView never runs this; you would.</p>
              {/if}
            {/if}
          </section>
        {/each}

        <p class="honesty">
          Rule order, a filter at your ISP and CGNAT can all keep a listed door shut; MikroView sees none of them.
          Nothing here was probed.
        </p>
      {/if}
    </div>
  </div>
{/if}

<style>
  /* Same right-hand sheet as HostDossier.svelte -- fixed right, one
     scrolling body, the type never shrinking past the legibility floor. */
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    z-index: 50;
  }

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
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }

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

  .close:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .body {
    flex: 1;
    overflow-y: auto;
    padding: 12px 14px 18px;
  }

  section {
    margin: 0 0 18px;
  }

  h3 {
    margin: 0 0 6px;
    font-size: 12px;
    font-weight: 700;
    color: var(--fg);
  }

  p {
    margin: 0 0 3px;
  }

  .sub {
    color: var(--fg-dim);
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    margin-top: 8px;
  }

  .from,
  .absent {
    color: var(--fg-muted);
  }

  ul.doors {
    margin: 4px 0 0;
    padding: 0;
    list-style: none;
  }

  ul.doors li {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 3px 0;
  }

  .peer {
    font-family: var(--font-mono);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .probe {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 8px;
    word-break: break-all;
  }

  .honesty {
    margin: 14px 0 0;
    padding-top: 10px;
    border-top: 1px solid var(--border);
    color: var(--fg-dim);
  }

  .failed {
    padding: 16px 0;
    color: var(--fg-muted);
  }
</style>
