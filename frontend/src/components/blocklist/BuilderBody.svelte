<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The blocklist builder's body (#1360, BUILD.md part 7), ported from
  // round 2's builder.html (bodySupported, bodyBelowFloor) and tail.html
  // (buildBody, the same body in the wizard's frame): the head and the
  // version line, a card per list with its choices, the folded record of
  // lists left out, and on the right the block, the copy row, the
  // observation line and "Where it stands".
  //
  // Rendered by Builder.svelte (the page, from Settings ▸ drop list) and
  // by the wizard's first-run tail (variant="tail"), so what the operator
  // learns on first run is what they find later under Settings.
  //
  // Every RouterOS line here comes from the server; the page holds no
  // RouterOS syntax and never connects to a router.

  import type { BlocklistCatalogueEntry, BlocklistPart } from '../../lib/api'
  import { blocklistState } from '../../lib/blocklist.svelte'
  import {
    blockHead,
    hintsFor,
    hmLocal,
    inkFor,
    ledgerView,
    marked,
    newPartsNote,
    partsSummary,
    refreshLabel,
    versionLine,
    waitingLists,
    type Seg,
  } from '../../lib/blocklistBuild'
  import './builder.css'

  let { variant = 'page', onpick }: { variant?: 'page' | 'tail'; onpick?: (device: string) => void } = $props()

  const data = $derived(blocklistState.data)
  const choices = $derived(blocklistState.choices)
  const block = $derived(blocklistState.block)
  const name = $derived(data?.deviceName ?? '')
  const days = $derived(!!data?.catalogue.some((e) => e.refresh.some((r) => r.value === 'weekdays')))
  const onCount = $derived(data ? data.catalogue.filter((e) => choices[e.key]?.on).length : 0)
  // With every list at Not now there is nothing to paste: no parts, not
  // the push alone (built review, 3a).
  const parts = $derived(onCount === 0 ? [] : (block?.parts ?? []))
  const ledger = $derived(data ? ledgerView(data, choices) : [])
  const waiting = $derived(data ? waitingLists(data, choices) : [])
  const anyHeld = $derived(!!data?.lists.some((l) => l.state === 'held'))
  const vline = $derived(data ? versionLine(data) : null)
  const note = $derived(data ? newPartsNote(data, parts, name) : '')
  // The push after the last one: the wizard's push runs every 20 minutes.
  const nextPush = $derived.by(() => {
    if (!data?.reportedAt) return ''
    const at = Date.parse(data.reportedAt) + 20 * 60 * 1000
    return at > Date.now() ? hmLocal(new Date(at).toISOString()) : ''
  })

  // The foot's undo for everything opens under the ledger, below the
  // fold with eight rows: bring it into view (the body scrolls).
  let undoAllEl = $state<HTMLElement | null>(null)
  $effect(() => {
    if (blocklistState.showUndoAll && undoAllEl) undoAllEl.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' })
  })

  function partHeadInk(p: BlocklistPart): string | null {
    return p.ink ? inkFor(p.ink) : null
  }

  function words(n: number): string {
    return ['', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight', 'nine', 'ten', 'eleven', 'twelve'][n] ?? String(n)
  }
</script>

{#snippet segs(list: Seg[])}{#each list as s, i (i)}{#if s.b}<b>{s.text}</b>{:else if s.em}<em>{s.text}</em>{:else}{s.text}{/if}{/each}{/snippet}

{#snippet yesno(key: string, on: boolean)}
  <div class="seg">
    <button type="button" class:on aria-pressed={on} onclick={() => blocklistState.setChoice(key, { on: true })}>Yes</button>
    <button type="button" class:on={!on} class:no={!on} aria-pressed={!on} onclick={() => blocklistState.setChoice(key, { on: false })}>Not now</button>
  </div>
{/snippet}

{#snippet card(e: BlocklistCatalogueEntry)}
  {@const c = choices[e.key]}
  {@const h = hintsFor(e.key, days)}
  <section class="lcard" class:on={c?.on} class:off={!c?.on} style:--ink={inkFor(e.key)} aria-label={e.name} data-list={e.key}>
    <div class="top"><span class="dot"></span><h4>{e.name}</h4>{@render yesno(e.key, !!c?.on)}</div>
    <div class="factsrow">{@render segs(marked(e.facts))}</div>
    <p class="guide">{@render segs(marked(e.guide))}</p>
    {#if c?.on}
      <div class="choices">
        <div class="ch">
          <span class="k">block</span>
          <div class="seg sm">
            <button type="button" class:on={c.direction === 'from'} onclick={() => blocklistState.setChoice(e.key, { direction: 'from' })}>from them</button>
            <button type="button" class:on={c.direction === 'both'} onclick={() => blocklistState.setChoice(e.key, { direction: 'both' })}>both ways</button>
          </div>
          <small>{@render segs(marked(h.block))}</small>
        </div>
        {#if e.ipv6}
          <div class="ch">
            <span class="k">ipv6 too</span>
            <div class="seg sm">
              <button type="button" class:on={c.ipv6} onclick={() => blocklistState.setChoice(e.key, { ipv6: true })}>yes</button>
              <button type="button" class:on={!c.ipv6} onclick={() => blocklistState.setChoice(e.key, { ipv6: false })}>no</button>
            </div>
            <small>{@render segs(marked(h.ipv6))}</small>
          </div>
        {/if}
        <div class="ch">
          <span class="k">log the drops</span>
          <div class="seg sm">
            <button type="button" class:on={c.log} onclick={() => blocklistState.setChoice(e.key, { log: true })}>yes</button>
            <button type="button" class:on={!c.log} onclick={() => blocklistState.setChoice(e.key, { log: false })}>no</button>
          </div>
          <small>{@render segs(marked(h.log))}</small>
        </div>
        <div class="ch">
          <span class="k">refresh</span>
          <div class="seg sm">
            {#each e.refresh as r (r.value)}
              <button type="button" class:on={c.refresh === r.value} onclick={() => blocklistState.setChoice(e.key, { refresh: r.value })}
                >{refreshLabel(r.value)}{#if r.since}<small>{r.since}</small>{/if}</button
              >
            {/each}
          </div>
          <small>{@render segs(marked(h.refresh))}</small>
        </div>
      </div>
    {/if}
  </section>
{/snippet}

{#snippet spans(list: { text: string; mark?: string }[])}{#each list as s, i (i)}{#if s.mark === 'elided'}<span class="k">{s.text}</span>{:else if s.mark === 'version'}<span class="v">{s.text}</span>{:else}{s.text}{/if}{/each}{/snippet}

{#if data && data.standing !== 'ok'}
  <div class="body wide one">
    <div class="head">
      <h3>Block known-bad addresses on {#if variant === 'tail'}{name}.{:else}<span class="sel">{name} ▾{#if data.devices.length > 1}<select aria-label="Router" value={data.device} onchange={(ev) => onpick?.((ev.currentTarget as HTMLSelectElement).value)}>{#each data.devices as d (d.id)}<option value={d.id}>{d.name}</option>{/each}</select>{/if}</span>{/if}</h3>
      <p class="hint">Not yet on this router.</p>
    </div>
    <div class="col left">
      <div class="warnbox">
        {#if data.standing === 'below-floor'}
          <p><b>{name} runs RouterOS {data.routerosVersion}.</b> MikroView supports {data.minimumVersion} and later, and this page writes its block for those — an older router cannot read a list file in pieces, so nothing here would load.</p>
          <p>Upgrade the router first, in its terminal:</p>
          <pre>{data.upgrade}</pre>
          <p>It reboots. The push after the reboot tells this page the new version, and the lists appear here.</p>
        {:else}
          <p><b>{name} has not pushed yet.</b> This page writes its block for the RouterOS version the push reports, and nothing has arrived from this router.</p>
          <p>Run setup first (Admin ▸ Run setup…): its one paste installs the push.</p>
          <p>The first push tells this page the version, and the lists appear here.</p>
        {/if}
      </div>
    </div>
  </div>
{:else if data}
  <div class="body wide">
    <div class="head">
      {#if variant === 'tail'}
        <h3>Block known-bad addresses on {name}.</h3>
        <p class="hint">Optional, and offered here on first run only. Pick the lists; MikroView builds one block that makes the router fetch each list itself, straight from its source, and drop what is on it — in <b>raw, before anything else</b>. Paste it once into the router’s <b>terminal</b>, like the block before. Safe to paste again: it sets, never adds.</p>
      {:else}
        <h3>Block known-bad addresses on <span class="sel">{name} ▾{#if data.devices.length > 1}<select aria-label="Router" value={data.device} onchange={(ev) => onpick?.((ev.currentTarget as HTMLSelectElement).value)}>{#each data.devices as d (d.id)}<option value={d.id}>{d.name}</option>{/each}</select>{/if}</span></h3>
        <p class="hint">Pick the lists; MikroView builds one block that makes the router fetch each list itself, straight from its source, and drop what is on it — in <b>raw, before anything else</b>. Paste it once into the router’s <b>terminal</b> (WinBox ▸ New Terminal, or ssh). Safe to paste again: it sets, never adds.</p>
      {/if}
      {#if vline}
        <p class="vline">written for <b>{vline.version}</b>{vline.rest}</p>
      {/if}
    </div>
    <div class="col left">
      {#each data.catalogue as e (e.key)}
        {@render card(e)}
      {/each}
      <details class="more">
        <summary>Considered and left out — {words(data.leftOut.length)} lists<span class="cat">catalogue reviewed {data.catalogueDate}</span></summary>
        <ul>
          {#each data.leftOut as l (l.name)}
            <li><b>{l.name}</b><span>{l.why}</span></li>
          {/each}
        </ul>
      </details>
    </div>
    <div class="col right">
      <div class="blockhead">
        <h5>The block</h5>
        <span class="n">{block ? blockHead(data, { ...block, parts }) : ''}</span>
        <span class="live">rebuilt as you click</span>
      </div>
      <pre class="script" aria-label="RouterOS block to paste">{#each parts as p, i (p.ordinal)}{#if i > 0}{'\n'}{/if}<span class="sec" style:--ink={partHeadInk(p)}># {p.ordinal} · {#if p.title}<b>{p.title}</b> — {/if}{@render spans(p.note)}</span>{#each p.shown as line, j (j)}{'\n'}{@render spans(line)}{/each}{#if p.fold?.length}{'\n'}<span class="fold">{@render spans(p.fold)}</span>{/if}{/each}{#if block && onCount === 0}<span class="fold"># nothing to paste — every list is Not now</span>{/if}</pre>
      {#if blocklistState.blockError}
        <p class="note" aria-live="polite">{blocklistState.blockError}</p>
      {/if}
      <div class="copyrow">
        <button type="button" class="primary" disabled={!block?.copyText || onCount === 0} onclick={() => blocklistState.copy()}>{partsSummary(onCount, parts.length)}</button>
        <span class="note">{onCount === 0 ? 'Turn a list on above; the block is empty.' : note}</span>
      </div>
      {#if waiting.length && anyHeld}
        <div class="obs waiting"><b>Waiting for the router.</b> The next push (every 20 min) says what {name} holds in {waiting.join(' and ')} and how often each rule has fired.</div>
      {:else if waiting.length}
        <div class="obs quiet"><b>Nothing to wait for until you paste.</b> After it, the router runs {onCount === 1 ? 'the script' : onCount === 2 ? 'both scripts' : 'the scripts'} at once; {nextPush ? `the push at ${nextPush}` : 'the next push'} says what {name} holds and how often each rule has fired.</div>
      {:else if onCount > 0}
        <div class="obs quiet"><b>Nothing waiting.</b> {name} holds every list in the block; a paste now only applies a changed choice — it sets, never adds.</div>
      {/if}
      <div class="ledgerh"><h5>Where it stands</h5><span>what {name} holds, from its own push{variant === 'page' ? ' · Undo per row' : ''}</span></div>
      <div class="ledger">
        {#each ledger as r (r.key)}
          <div class="row" style:--ink={r.ink}>
            <span class="step-n {r.state}">{r.n}</span>
            {#if r.state === 'skip'}
              <span class="skip"><s>{r.title}</s><div class="r skip">{r.receipt}</div></span>
            {:else}
              <span>{r.title}<div class="r {r.state}">{r.receipt}</div></span>
            {/if}
            {#if r.undo}
              <button type="button" class="linkish" onclick={() => blocklistState.toggleUndo(r.key)}>{blocklistState.undoOpen === r.key ? 'Hide' : 'Undo'}</button>
            {:else}
              <span></span>
            {/if}
            {#if r.undo && blocklistState.undoOpen === r.key}
              <div class="undo">
                <pre>{r.undo}</pre>
                <p class="note">Paste on the router. MikroView notices when the list leaves the push and this row goes back to <b>not now</b>. To stop dropping without removing anything: <span class="mono">{data.disableAll}</span>.</p>
              </div>
            {/if}
          </div>
        {/each}
      </div>
      {#if blocklistState.showUndoAll}
        <div class="undo" bind:this={undoAllEl}>
          <pre aria-label="Undo everything">{data.undoAll}</pre>
          <p class="note">Paste on the router. Every list goes — rules, scripts, schedulers and the lists themselves; the push script stays, so MikroView sees them leave at the next push and every row goes back to <b>not now</b>.</p>
        </div>
      {/if}
    </div>
  </div>
{/if}
