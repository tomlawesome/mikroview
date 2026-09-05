<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Settings' "router backups" group (#394, round 44,
  // docs/design/concepts/round-44/backups.html), third of the three
  // things mikroview holds beside memory and disk. Admin-only, like the
  // disk group's own `key`/`state` rows -- EngineRoom mounts this only
  // for isAdmin, and the server matches (handleRouterBackupsList 403s
  // anyone else), so a viewer sees nothing here at all.
  //
  // Real states, wired to what GET /api/router-backups actually reports:
  // rest (one block per router, amber once a push has been missed),
  // bnone (the drop box is on but nothing has pushed yet), bnokey (no
  // retention key, so the drop box is closed). EngineRoom itself draws
  // the round's `bfail` state (the GET did not answer) the same way it
  // draws the disk group's `dfail`, since that one has no settings
  // object to render from.
  //
  // Not wired: round 44 also draws `brecv` (a push arriving right now)
  // and `brefused`/`bquota` (the last push was rejected). Nothing on the
  // server persists a queryable "receiving now" or "last refusal" fact
  // to poll -- backupvault.Store only logs a refusal (v.log.Warn) -- so
  // there is no live signal these two states could read today. Left for
  // a later issue rather than guessed at; the copy that would need it
  // (round 44's README) is quoted there.
  import { routerBackupDownloadUrl } from '../lib/api'
  import { isGone, newestGeneration, oldestArrival, receiptLine, MAX_GENERATIONS } from '../lib/backups'
  import { formatSize } from '../lib/memory'
  import { portOf } from '../lib/setupsteps'
  import type { RouterBackupsResponse } from '../lib/types'

  let {
    resp,
    onopenlost,
  }: {
    resp: RouterBackupsResponse
    /** Round 44's "is it gone?" link: opens the wizard's step 6 in its
     * lost-router shape (round 45), reached only from here. */
    onopenlost: (device: string) => void
  } = $props()

  // strip renders round 44's ten-slot generation strip: filled slots for
  // what is kept, the newest at the right, each a touch darker than the
  // last so the eye reads "newer" without needing a label on every one.
  function slotOpacity(index: number, kept: number): number {
    const first = MAX_GENERATIONS - kept
    return 0.1 + 0.017 * (index - first)
  }
</script>

{#if !resp.enabled}
  <div class="wrows">
    <div class="orow">
      <span>kept</span>
      <span class="ov dim">nothing</span>
    </div>
    <div class="orow">
      <span>key</span>
      <span class="ov">
        none mounted — a backup that arrives has nowhere safe to go, so the drop box is closed
      </span>
    </div>
  </div>
{:else if resp.routers.length === 0}
  <div class="wrows">
    <div class="orow">
      <span>kept</span>
      <span class="ov dim">nothing — no router has pushed one yet · the wizard's step 6 prints the script</span>
    </div>
    {#if resp.port}
      <div class="orow">
        <span>arrive by</span>
        <span class="ov dim">SFTP on port {portOf(resp.port)} · a drop box the router writes into and nothing reads out of</span>
      </div>
    {/if}
  </div>
{:else}
  <div class="wleft">
    {#each resp.routers as router (router.device)}
      {@const receipt = receiptLine(router, oldestArrival(router))}
      {@const newest = newestGeneration(router)}
      {@const kept = router.generations.length}
      <div class="brtr" class:brquiet={receipt.amber}>
        <div class="brhead">
          <b>{router.device}</b>
          <span class:brwarn={receipt.amber}>{receipt.text}</span>
        </div>
        <svg
          class="brstrip"
          viewBox="0 0 520 58"
          role="img"
          aria-label="{kept} of {MAX_GENERATIONS} backups kept for {router.device}"
        >
          <rect x="8" y="20" width="500" height="10" rx="5" fill="var(--bg-hover)" />
          {#each { length: kept } as _, i (i)}
            <rect
              x={8 + (MAX_GENERATIONS - kept + i) * 50.6}
              y="20"
              width="46"
              height="10"
              rx="3"
              fill="var(--accent)"
              opacity={slotOpacity(MAX_GENERATIONS - kept + i, kept)}
            />
          {/each}
          <rect x="504" y="15" width="3" height="20" rx="1.5" fill="var(--now)" />
        </svg>
        {#if newest}
          <p class="oghint brnewest">
            {#if newest.backupArrivedAt}{formatSize(newest.backupBytes ?? 0)}
              <a class="olink" href={routerBackupDownloadUrl(router.device, newest.id, 'backup')}>download .backup</a> ·{/if}
            {#if newest.rscArrivedAt}<a class="olink" href={routerBackupDownloadUrl(router.device, newest.id, 'rsc')}>.rsc</a>{/if}
            {#if isGone(router)}
              · <button type="button" class="olink" onclick={() => onopenlost(router.device)}>is it gone?</button>
            {/if}
          </p>
        {/if}
      </div>
    {/each}
    <p class="oghint">
      each push is a pair — the binary .backup that restores the router whole, and the .rsc export it can be read
      from · the eleventh pair lets the oldest go · a download is written to the audit log with your name
    </p>
  </div>

  <div class="wrows">
    <div class="orow">
      <span>kept</span>
      <span class="ov">
        {resp.totalGenerations}
        {resp.totalGenerations === 1 ? 'pair' : 'pairs'} · {resp.totalRouters}
        {resp.totalRouters === 1 ? 'router' : 'routers'} · {formatSize(resp.totalBytes)}
      </span>
    </div>
    {#if resp.port}
      <div class="orow">
        <span>arrive by</span>
        <span class="ov dim">SFTP on port {portOf(resp.port)} · a drop box the router writes into and nothing reads out of</span>
      </div>
    {/if}
    <div class="orow">
      <span>allowed</span>
      <span class="ov dim">{MAX_GENERATIONS} pairs a router · 16 MiB a file · the oldest lets go</span>
    </div>
    <div class="orow">
      <span>key</span>
      <span class="ov dim">mounted at start — every pair is encrypted under it; admins read them, and each read is audited</span>
    </div>
    <div class="orow">
      <span>path</span>
      <span class="ov"><span class="brwarn">the router never checks who it is sending to</span> — anyone on the path could read the pair and the token, so only on a network you trust</span>
    </div>
  </div>
{/if}

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

  .brtr {
    padding: 6px 0 2px;
  }

  .brtr + .brtr {
    border-top: 1px solid var(--border);
    margin-top: 6px;
  }

  .brhead {
    display: flex;
    justify-content: space-between;
    gap: 14px;
    font-size: 12px;
    color: var(--fg-muted);
  }

  .brhead b {
    font: 600 11px var(--font-mono);
    color: var(--fg);
  }

  .brstrip {
    width: 100%;
    height: auto;
    display: block;
  }

  .brnewest {
    color: var(--fg-muted);
    margin-top: 2px;
  }

  .brwarn {
    color: var(--now);
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

  .oghint {
    margin: 2px 0 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
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
</style>
