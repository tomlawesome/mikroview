<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Settings' "country and network owner" group (#1352), beside the
  // disk group, admin-only like the rest of that grid (EngineRoom mounts
  // this only for isAdmin, and the server matches). Built to the Fable
  // call on #1352: one line saying which source the flags come from,
  // then three rows, one per source, the live one marked "in use".
  //
  //   - DB-IP Lite needs no key; it shows its download state only.
  //   - IPinfo Lite takes one token; MaxMind GeoLite2 takes an account ID
  //     and a licence key. Both are write-once, the drop list's pull-key
  //     pattern (Droplist.svelte): once set the row reads "key set", who
  //     set it and when, and a remove that arms on the first click and
  //     removes on the second. The value is never shown again -- the
  //     server never sends it back.
  //
  // Precedence is fixed server-side (IPinfo, then MaxMind, then DB-IP);
  // there is no picker here. Every write answers the whole new state,
  // which is handed to the parent through onchanged rather than guessed.
  import { appState } from '../lib/state.svelte'
  import { removeGeoIpinfoToken, removeGeoMaxmindKey, setGeoIpinfoToken, setGeoMaxmindKey } from '../lib/api'
  import { formatRelative } from '../lib/format'
  import { formatUntil, inUseLine } from '../lib/geo'
  import type { GeoKeyedSourceStatus, GeoSettings, GeoSourceStatus } from '../lib/types'

  let {
    settings,
    onchanged,
  }: {
    settings: GeoSettings
    /** Called with the server's new state after it accepted a change. */
    onchanged: (next: GeoSettings) => void
  } = $props()

  const src = $derived(settings.sources)

  // --- IPinfo --------------------------------------------------------------
  let ipinfoToken = $state('')
  let ipinfoBusy = $state(false)
  let ipinfoError = $state<string | null>(null)
  let armedIpinfo = $state(false)

  async function setIpinfo() {
    const token = ipinfoToken.trim()
    if (!token || ipinfoBusy) return
    ipinfoBusy = true
    ipinfoError = null
    const result = await setGeoIpinfoToken(token)
    ipinfoBusy = false
    if (typeof result === 'string') {
      ipinfoError = result
      return
    }
    // The key has gone to the server; it is not kept here either.
    ipinfoToken = ''
    onchanged(result)
  }

  function onRemoveIpinfoClick(e: MouseEvent) {
    e.stopPropagation()
    if (armedIpinfo) {
      armedIpinfo = false
      void removeIpinfo()
      return
    }
    disarmAll()
    armedIpinfo = true
  }

  async function removeIpinfo() {
    ipinfoBusy = true
    ipinfoError = null
    const result = await removeGeoIpinfoToken()
    ipinfoBusy = false
    if (typeof result === 'string') {
      ipinfoError = result
      return
    }
    onchanged(result)
  }

  // --- MaxMind -------------------------------------------------------------
  let maxmindAccount = $state('')
  let maxmindKey = $state('')
  let maxmindBusy = $state(false)
  let maxmindError = $state<string | null>(null)
  let armedMaxmind = $state(false)

  async function setMaxmind() {
    const account = maxmindAccount.trim()
    const key = maxmindKey.trim()
    if (!account || !key || maxmindBusy) return
    maxmindBusy = true
    maxmindError = null
    const result = await setGeoMaxmindKey(account, key)
    maxmindBusy = false
    if (typeof result === 'string') {
      maxmindError = result
      return
    }
    maxmindAccount = ''
    maxmindKey = ''
    onchanged(result)
  }

  function onRemoveMaxmindClick(e: MouseEvent) {
    e.stopPropagation()
    if (armedMaxmind) {
      armedMaxmind = false
      void removeMaxmind()
      return
    }
    disarmAll()
    armedMaxmind = true
  }

  async function removeMaxmind() {
    maxmindBusy = true
    maxmindError = null
    const result = await removeGeoMaxmindKey()
    maxmindBusy = false
    if (typeof result === 'string') {
      maxmindError = result
      return
    }
    onchanged(result)
  }

  function onFieldKeydown(e: KeyboardEvent, submit: () => void) {
    if (e.key === 'Enter') {
      e.preventDefault()
      submit()
    }
  }

  // --- the download state line ---------------------------------------------
  // "last fetched 3h ago · next refresh in 6d", or "not fetched yet".
  function stateLine(s: GeoSourceStatus): string {
    const parts = [s.fetchedAt ? `last fetched ${formatRelative(s.fetchedAt, appState.now)}` : 'not fetched yet']
    if (s.nextRefresh) parts.push(`next refresh ${formatUntil(s.nextRefresh, appState.now)}`)
    return parts.join(' · ')
  }

  // "key set · by tom, 3h ago" -- who and when, never the key.
  function keyLine(s: GeoKeyedSourceStatus): string {
    let line = 'key set'
    if (s.setBy) line += ` · by ${s.setBy}`
    if (s.setAt) line += `${s.setBy ? ',' : ' ·'} ${formatRelative(s.setAt, appState.now)}`
    return line
  }

  // Round 28's arm-then-confirm gesture (EngineRoom.svelte's own
  // revoke/remove buttons): a click anywhere that isn't the armed
  // button itself disarms it.
  function disarmAll() {
    armedIpinfo = false
    armedMaxmind = false
  }
</script>

<svelte:window onclick={disarmAll} />

{#snippet inUse(live: boolean)}
  {#if live}{' '}<span class="ginuse">in use</span>{/if}
{/snippet}

<div class="wrows">
  <p class="gtop" data-testid="geo-in-use">{inUseLine(settings.source)}</p>

  <div class="gsrc" data-source="dbip">
    <div class="orow">
      <span>DB-IP Lite{@render inUse(settings.source === 'dbip')}</span>
      <span class="ov dim">no key needed</span>
    </div>
    <p class="oghint">{stateLine(src.dbip)}</p>
    {#if src.dbip.lastError}<p class="oghint err">{src.dbip.lastError}</p>{/if}
  </div>

  <div class="gsrc" data-source="ipinfo">
    <div class="orow">
      <span>IPinfo Lite{@render inUse(settings.source === 'ipinfo')}</span>
      <span class="ov">
        {#if src.ipinfo.keySet}
          {keyLine(src.ipinfo)} ·
          <button
            type="button"
            class="olink revoke"
            class:armed={armedIpinfo}
            disabled={ipinfoBusy}
            onclick={onRemoveIpinfoClick}
          >
            {armedIpinfo ? 'confirm — delete the key' : 'remove'}
          </button>
        {:else}
          <input
            class="gin"
            type="password"
            autocomplete="off"
            spellcheck="false"
            placeholder="token"
            aria-label="IPinfo token"
            disabled={ipinfoBusy}
            bind:value={ipinfoToken}
            onkeydown={(e) => onFieldKeydown(e, setIpinfo)}
          />
          <button type="button" class="olink" disabled={ipinfoBusy || !ipinfoToken.trim()} onclick={setIpinfo}>
            {ipinfoBusy ? 'setting…' : 'set'}
          </button>
        {/if}
      </span>
    </div>
    <p class="oghint">
      country and network owner{#if src.ipinfo.keySet} · {stateLine(src.ipinfo)}{/if}
    </p>
    {#if src.ipinfo.lastError}<p class="oghint err">{src.ipinfo.lastError}</p>{/if}
    {#if ipinfoError}<p class="oghint err" role="alert">{ipinfoError}</p>{/if}
  </div>

  <div class="gsrc" data-source="maxmind">
    <div class="orow">
      <span>MaxMind GeoLite2{@render inUse(settings.source === 'maxmind')}</span>
      <span class="ov">
        {#if src.maxmind.keySet}
          {keyLine(src.maxmind)} ·
          <button
            type="button"
            class="olink revoke"
            class:armed={armedMaxmind}
            disabled={maxmindBusy}
            onclick={onRemoveMaxmindClick}
          >
            {armedMaxmind ? 'confirm — delete the key' : 'remove'}
          </button>
        {:else}
          <input
            class="gin short"
            type="text"
            inputmode="numeric"
            autocomplete="off"
            spellcheck="false"
            placeholder="account ID"
            aria-label="MaxMind account ID"
            disabled={maxmindBusy}
            bind:value={maxmindAccount}
            onkeydown={(e) => onFieldKeydown(e, setMaxmind)}
          />
          <input
            class="gin"
            type="password"
            autocomplete="off"
            spellcheck="false"
            placeholder="licence key"
            aria-label="MaxMind licence key"
            disabled={maxmindBusy}
            bind:value={maxmindKey}
            onkeydown={(e) => onFieldKeydown(e, setMaxmind)}
          />
          <button
            type="button"
            class="olink"
            disabled={maxmindBusy || !maxmindAccount.trim() || !maxmindKey.trim()}
            onclick={setMaxmind}
          >
            {maxmindBusy ? 'setting…' : 'set'}
          </button>
        {/if}
      </span>
    </div>
    <p class="oghint">
      country only{#if src.maxmind.keySet} · {stateLine(src.maxmind)}{/if}
    </p>
    {#if src.maxmind.lastError}<p class="oghint err">{src.maxmind.lastError}</p>{/if}
    {#if maxmindError}<p class="oghint err" role="alert">{maxmindError}</p>{/if}
  </div>
</div>

<p class="oghint gfoot">Setting a key switches the source: IPinfo, then MaxMind, then DB-IP.</p>

<style>
  /* The row grammar is DiskControl's and Droplist's (.orow, .ov,
     .oghint, .olink) -- Svelte scopes styles per component, so this
     component draws its own copy, as those two do. The group has no
     diagram, so EngineRoom stacks it (`dnodiagram`) and .wrows takes
     the whole width. */
  .wrows {
    min-width: 0;
  }

  .gtop {
    margin: 0 0 4px;
    font-size: 12.5px;
    color: var(--fg);
  }

  .gsrc {
    padding: 2px 0 6px;
  }

  .gsrc + .gsrc {
    border-top: 1px solid var(--border);
  }

  .ginuse {
    margin-left: 4px;
    padding: 0 5px;
    border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
    border-radius: 4px;
    font-size: 10.5px;
    color: var(--accent);
  }

  .orow {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 14px;
    padding: 7px 0 2px;
    font-size: 12px;
  }

  .orow > span:first-child {
    color: var(--fg-muted);
    flex: none;
  }

  .orow .ov {
    color: var(--fg-muted);
    text-align: right;
  }

  .orow .ov.dim {
    color: var(--fg-dim);
  }

  .oghint {
    margin: 2px 0 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
  }

  .oghint.err {
    color: var(--reject);
    font-style: normal;
    overflow-wrap: anywhere;
  }

  .gfoot {
    margin-top: 8px;
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

  .olink.armed {
    color: var(--alarm);
  }

  /* The key fields: Droplist's add-form field, dashed underline and
     all, sized for a token rather than an address. */
  .gin {
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    padding: 2px 0;
    margin-right: 8px;
    width: 18ch;
    outline: none;
  }

  .gin.short {
    width: 10ch;
  }

  .gin:focus {
    border-bottom-color: var(--accent);
  }
</style>
