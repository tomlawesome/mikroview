<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The blocklist builder as a full screen (#1360, BUILD.md part 7), from
  // Settings ▸ drop list ▸ "Block known-bad addresses…": round 2's
  // builder.html in the wizard's frame (components/wizard/wizard.css --
  // the bar, the strip, the rail, the main and the foot), its body in
  // BuilderBody.svelte. Opened through blocklistState.openFor(device);
  // mounted once in App.svelte beside the wizard.
  //
  // MikroView never connects to the router: the page prints the block,
  // the router fetches each list from its source, and what the rail and
  // the ledger say comes back in the router's own push.

  import { untrack } from 'svelte'
  import { blocklistState } from '../../lib/blocklist.svelte'
  import { FOOT_HINT, RAIL_NOTE, hmLocal, inkFor, num, partsSummary, railRows } from '../../lib/blocklistBuild'
  import { fallState, laneColors } from '../../lib/fall.svelte'
  import { wizardState } from '../../lib/wizard.svelte'
  import BuilderBody from './BuilderBody.svelte'
  import '../wizard/wizard.css'
  import './builder.css'

  // What the router holds changes at its push, every 20 minutes; the
  // page re-reads it while open, and the live rate on the bar with it.
  const POLL_MS = 10000

  const data = $derived(blocklistState.data)
  const choices = $derived(blocklistState.choices)
  const rows = $derived(data ? railRows(data, choices) : [])
  const below = $derived(!!data && data.standing !== 'ok')
  const onCount = $derived(data ? data.catalogue.filter((e) => choices[e.key]?.on).length : 0)
  const partCount = $derived(blocklistState.block?.parts.length ?? 0)

  let rate = $state(0)
  let lastLines = -1
  let lastAt = 0
  const lines = $derived.by(() => {
    let n = 0
    for (const d of wizardState.status?.devices ?? []) if (d.device === blocklistState.device) n += d.events
    return n
  })

  $effect(() => {
    if (!blocklistState.open) return
    untrack(() => {
      wizardState.refresh()
      fallState.refresh()
    })
    const timer = setInterval(() => {
      blocklistState.refresh()
      wizardState.refresh().then(() => {
        const now = Date.now()
        if (lastLines >= 0 && now > lastAt) rate = Math.max(0, Math.round(((lines - lastLines) * 1000) / (now - lastAt)))
        lastLines = lines
        lastAt = now
      })
    }, POLL_MS)
    return () => clearInterval(timer)
  })

  // The strip: the fall's boundaries in their lanes once the push has
  // named them, one tick otherwise -- the wizard's own strip.
  const ticks = $derived.by(() => {
    const lanes = laneColors(fallState.boundaries)
    if (fallState.boundaries.length > 0) {
      return fallState.boundaries.map((b) => ({
        lane: b.coverage === 'observed' ? (lanes.get(b.key) ?? '') : '',
        on: b.coverage === 'observed',
        dark: b.coverage === 'dark',
      }))
    }
    return [{ lane: lines > 0 ? 'var(--accept)' : '', on: lines > 0, dark: false }]
  })

  function pick(device: string) {
    blocklistState.load(device, false)
  }

  function point(key: string) {
    blocklistState.current = key
    document.querySelector(`.wiz .lcard[data-list="${CSS.escape(key)}"]`)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  }
</script>

{#if blocklistState.open}
  <div class="page wiz live bl-page" role="dialog" aria-modal="true" aria-label="Block known-bad addresses">
    <div class="bar">
      <span class="wm">MIKRO<em>VIEW</em></span>
      <div class="chips" aria-label="Proofs so far">
        {#if data}
          <span class="att router"><i></i>{data.deviceName}</span>
          {#if data.routerosVersion}
            <span class="att push"><i></i>push {hmLocal(data.reportedAt)} · RouterOS {data.routerosVersion}</span>
          {/if}
          {#if data.ownDroplist.held}
            <span class="att own"><i></i>drop list · {num(data.ownDroplist.held)} held</span>
          {/if}
          {#if data.standing === 'below-floor'}
            <span class="att warn"><i></i>RouterOS below {data.minimumVersion} — unsupported</span>
          {:else}
            {#each data.lists.filter((l) => l.state === 'held') as l (l.key)}
              <span class="att list" style:--ink={inkFor(l.key)}><i></i>{data.catalogue.find((e) => e.key === l.key)?.name} · {num(l.count)} held</span>
            {/each}
          {/if}
        {/if}
      </div>
      <div class="right">
        {#if lines > 0}
          <span class="att logs"><i></i>live · {rate}/s</span>
          <span>{lines.toLocaleString()} lines</span>
        {/if}
      </div>
    </div>
    <div class="striprow">
      <div class="ovstrip" aria-hidden="true">
        {#each ticks as t, i (i)}
          <span class="ovtick" class:on={t.on} class:dark={t.dark} style:background={t.lane || null}></span>
        {/each}
      </div>
    </div>
    <nav class="rail bl" class:dim={below} aria-label="Lists on this router">
      <div class="h">Lists on {data?.deviceName ?? ''}</div>
      <ol>
        {#each rows as r (r.key)}
          <li>
            <button
              type="button"
              class="step-row {r.state}"
              class:current={!below && r.key === blocklistState.current}
              style:--ink={r.ink}
              onclick={() => point(r.key)}
            >
              <span class="step-n">{r.n}</span>
              <span class="step-text">
                <span class="step-title">{r.title}</span>
                <span class="step-receipt">{r.receipt}</span>
              </span>
            </button>
          </li>
        {/each}
      </ol>
      <p class="foot-note">{RAIL_NOTE}</p>
    </nav>
    <div class="main">
      {#if blocklistState.error}
        <div class="body"><p class="hint" aria-live="polite">{blocklistState.error}</p></div>
      {:else}
        <BuilderBody variant="page" onpick={pick} />
      {/if}
      <div class="foot">
        <button type="button" onclick={() => blocklistState.close()}>Back to the drop list</button>
        {#if below}
          <span class="fhint">MikroView never connects to the router — the upgrade is yours to run</span>
        {:else}
          <button type="button" onclick={() => blocklistState.toggleUndoAll()}>{blocklistState.showUndoAll ? 'Hide the undo lines' : `Undo everything on ${data?.deviceName ?? ''}`}</button>
          <span class="fhint">{FOOT_HINT}</span>
          <button type="button" class="primary" disabled={!blocklistState.block?.copyText} onclick={() => blocklistState.copy()}>{partsSummary(onCount, partCount)}</button>
        {/if}
      </div>
    </div>
  </div>
{/if}
