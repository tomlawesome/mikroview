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
  //
  // #1115 draws the optional vault passphrase #956 built the whole
  // backend for: a `passphrase` row beside the facts above, whose word
  // is one of off/locked/unlocked/unlocked elsewhere, and the controls
  // that follow from it. Every control below replaces what is shown
  // from the VaultLock its own call returned -- never from which call
  // was made -- and downloads gate on it: set and not open for this
  // session, the per-generation links are replaced by one quiet line
  // naming why. Nothing else about a router's block changes while
  // locked; the index is an observation of what arrived, and the
  // passphrase gates opening it, not knowing it arrived.
  import {
    changeRouterBackupPassphrase,
    fetchRouterBackups,
    keepRouterBackup,
    lockRouterBackupVault,
    releaseRouterBackup,
    removeRouterBackupPassphrase,
    routerBackupDownloadUrl,
    setRouterBackupComment,
    setRouterBackupPassphrase,
    unlockRouterBackupVault,
  } from '../lib/api'
  import { authState } from '../lib/auth.svelte'
  import { downloadFromUrl } from '../lib/export'
  import {
    isGone,
    newestGeneration,
    oldestArrival,
    receiptLine,
    MAX_GENERATIONS,
    MAX_KEEP_COMMENT,
  } from '../lib/backups'
  import { formatDayMonth, formatHM } from '../lib/format'
  import { formatSize } from '../lib/memory'
  import { portOf } from '../lib/setupsteps'
  import type { RouterBackupGeneration, RouterBackupRouter, RouterBackupsResponse, VaultLock } from '../lib/types'

  let {
    resp,
    onopenlost,
  }: {
    resp: RouterBackupsResponse
    /** Round 44's "is it gone?" link: opens the wizard's step 6 in its
     * lost-router shape (round 45), reached only from here. */
    onopenlost: (device: string) => void
  } = $props()

  // routers is kept locally for the same reason lock is: a keep control
  // answers with the router's whole block, and the screen shows that
  // straight away rather than waiting up to a minute for the parent's
  // own poll to come round again.
  let routers = $state<RouterBackupRouter[]>(resp.routers)
  $effect(() => {
    routers = resp.routers
  })

  function applyRow(row: RouterBackupRouter) {
    routers = routers.map((r) => (r.device === row.device ? row : r))
  }

  // canKeep is the viewer floor: a viewer reads the kept list and the
  // comments on it, and is offered none of the three controls.
  const canKeep = $derived(authState.role === 'admin')

  /** arrivedAt is whichever half of the pair landed, most recently --
   * the same rule the server orders generations by. */
  function arrivedAt(g: RouterBackupGeneration): string | null {
    if (g.rscArrivedAt && (!g.backupArrivedAt || g.rscArrivedAt > g.backupArrivedAt)) return g.rscArrivedAt
    return g.backupArrivedAt ?? g.rscArrivedAt ?? null
  }

  /** when writes a generation's own line-opening date: "12 Sep 04:00". */
  function when(g: RouterBackupGeneration): string {
    const at = arrivedAt(g)
    return at ? `${formatDayMonth(at)} ${formatHM(at)}` : '—'
  }

  /** sizeOf is the .backup's size, the figure round 44's newest line
   * already shows -- or the .rsc's, for a generation the export alone
   * opened. */
  function sizeOf(g: RouterBackupGeneration): string {
    return formatSize(g.backupBytes ?? g.rscBytes ?? 0)
  }

  /** earlier is a router's cycling generations bar the newest, newest
   * first so the oldest sits last. */
  function earlier(router: RouterBackupRouter): RouterBackupGeneration[] {
    return router.generations.slice(0, -1).reverse()
  }

  /** keptOf is a router's kept pool, newest first. The server sends it
   * oldest first, like the cycling set. */
  function keptOf(router: RouterBackupRouter): RouterBackupGeneration[] {
    return (router.protected ?? []).slice().reverse()
  }

  // Which routers have their earlier generations expanded, and which
  // kept backup is being asked about -- both are this tab's own state,
  // not the vault's.
  let expanded = $state<Record<string, boolean>>({})
  let releasing = $state<{ device: string; generation: string } | null>(null)

  // strip renders round 44's ten-slot generation strip: filled slots for
  // what is kept, the newest at the right, each a touch darker than the
  // last so the eye reads "newer" without needing a label on every one.
  function slotOpacity(index: number, kept: number): number {
    const first = MAX_GENERATIONS - kept
    return 0.1 + 0.017 * (index - first)
  }

  // --- the vault passphrase (#1115, #956) ---------------------------------
  //
  // lock is kept in local state, seeded from resp.lock and re-synced by
  // the effect below whenever a fresh resp lands from the parent's own
  // poll -- but a control's own call updates it immediately from the
  // VaultLock that call returned, without waiting for the next poll.
  let lock: VaultLock = $state(resp.lock)
  $effect(() => {
    lock = resp.lock
  })

  type PassState = 'off' | 'locked' | 'unlocked' | 'unlocked elsewhere'
  function passState(l: VaultLock): PassState {
    if (!l.passphraseSet) return 'off'
    if (l.unlockedForYou) return 'unlocked'
    if (l.locked) return 'locked'
    return 'unlocked elsewhere'
  }
  const passphraseState = $derived(passState(lock))

  // gated is round 44's download gate: a passphrase is set and this
  // session does not hold the unlock, whether nobody has it open
  // (locked) or another of the admin's own sign-ins does.
  const gated = $derived(lock.passphraseSet && !lock.unlockedForYou)

  // 'keep' and 'edit' are the same one-field form (#1126): keeping a
  // backup and rewriting why it is kept are the same sentence, typed
  // in the same place.
  type FormKind = 'set' | 'change' | 'remove' | 'unlock' | 'keep' | 'edit' | null
  let openKind = $state<FormKind>(null)
  let keepTarget = $state<{ device: string; generation: string } | null>(null)
  let keepComment = $state('')
  let newPassphrase = $state('')
  let confirmPassphrase = $state('')
  let currentPassphrase = $state('')
  let removePassphraseText = $state('')
  let unlockPassphraseText = $state('')
  let formError = $state<string | null>(null)
  let submitting = $state(false)

  function openForm(kind: FormKind) {
    openKind = kind
    keepTarget = null
    keepComment = ''
    newPassphrase = ''
    confirmPassphrase = ''
    currentPassphrase = ''
    removePassphraseText = ''
    unlockPassphraseText = ''
    formError = null
  }

  function closeForm() {
    openForm(null)
  }

  // Rune count, not .length: the server's own floor
  // (backupvault.MinPassphraseRunes) is measured the same way, so a
  // passphrase with characters outside the BMP is judged the same on
  // both sides.
  function runeCount(s: string): number {
    return Array.from(s).length
  }

  async function submitSet() {
    formError = null
    if (runeCount(newPassphrase) < lock.minPassphraseLength) {
      formError = `the vault passphrase must be at least ${lock.minPassphraseLength} characters`
      return
    }
    if (newPassphrase !== confirmPassphrase) {
      formError = "the two don't match"
      return
    }
    submitting = true
    const result = await setRouterBackupPassphrase(newPassphrase)
    submitting = false
    if (typeof result === 'string') {
      formError = result
      return
    }
    lock = result
    closeForm()
  }

  async function submitChange() {
    formError = null
    if (!currentPassphrase) {
      formError = 'enter the current vault passphrase'
      return
    }
    if (runeCount(newPassphrase) < lock.minPassphraseLength) {
      formError = `the vault passphrase must be at least ${lock.minPassphraseLength} characters`
      return
    }
    if (newPassphrase !== confirmPassphrase) {
      formError = "the two don't match"
      return
    }
    submitting = true
    // One call (#1222), not the current passphrase's remove followed by
    // the new one's set: a process that died between those two left the
    // vault with no passphrase at all. changeRouterBackupPassphrase
    // re-wraps the existing key under the new passphrase server-side, so
    // there is nothing an interruption can leave half-done.
    const result = await changeRouterBackupPassphrase(currentPassphrase, newPassphrase)
    submitting = false
    if (typeof result === 'string') {
      formError = result
      return
    }
    lock = result
    closeForm()
  }

  async function submitRemove() {
    formError = null
    if (!removePassphraseText) return
    submitting = true
    const result = await removeRouterBackupPassphrase(removePassphraseText)
    submitting = false
    if (typeof result === 'string') {
      formError = result
      return
    }
    lock = result
    closeForm()
  }

  async function submitUnlock() {
    formError = null
    if (!unlockPassphraseText) return
    submitting = true
    const result = await unlockRouterBackupVault(unlockPassphraseText)
    submitting = false
    if (typeof result === 'string') {
      formError = result
      return
    }
    lock = result
    closeForm()
  }

  // openKeepForm opens the keep/edit field against one generation.
  // Releasing is not a form: it is one question asked in place, on the
  // line it is about.
  function openKeepForm(kind: 'keep' | 'edit', device: string, generation: string, comment: string) {
    releasing = null
    openForm(kind)
    keepTarget = { device, generation }
    keepComment = comment
  }

  async function submitKeep() {
    formError = null
    if (!keepTarget) return
    const comment = keepComment.trim()
    if (comment.length === 0) {
      formError = 'say why you are keeping it'
      return
    }
    if (runeCount(comment) > MAX_KEEP_COMMENT) {
      formError = `at most ${MAX_KEEP_COMMENT} characters`
      return
    }
    submitting = true
    const result =
      openKind === 'edit'
        ? await setRouterBackupComment(keepTarget.device, keepTarget.generation, comment)
        : await keepRouterBackup(keepTarget.device, keepTarget.generation, comment)
    submitting = false
    if (typeof result === 'string') {
      formError = result
      return
    }
    applyRow(result)
    closeForm()
  }

  async function doRelease(device: string, generation: string) {
    formError = null
    submitting = true
    const result = await releaseRouterBackup(device, generation)
    submitting = false
    if (typeof result === 'string') {
      formError = result
      return
    }
    applyRow(result)
    releasing = null
  }

  async function doLock() {
    formError = null
    submitting = true
    const result = await lockRouterBackupVault()
    submitting = false
    if (typeof result === 'string') {
      formError = result
      return
    }
    lock = result
  }

  // refreshLock re-reads the lock object alone, off the back of the
  // group's own GET. It is the download gate's answer to a stale
  // unlock -- the idle timeout lapsing between the link being drawn and
  // the click -- rather than a client-side clock guessing at the
  // server's; see download() below.
  async function refreshLock() {
    try {
      const r = await fetchRouterBackups()
      lock = r.lock
    } catch {
      // the parent's own periodic refresh will catch up
    }
  }

  async function download(device: string, generation: string, kind: 'backup' | 'rsc') {
    const outcome = await downloadFromUrl(routerBackupDownloadUrl(device, generation, kind), `${device}.${kind}`)
    if (outcome === 'forbidden') await refreshLock()
  }
</script>

{#snippet passphraseRow()}
  <div class="orow">
    <span>passphrase</span>
    <span class="ov">
      <span class="pstate">{passphraseState}</span>
      {#if passphraseState === 'off'}
        · <button type="button" class="olink" onclick={() => openForm('set')}>set…</button>
      {:else if passphraseState === 'unlocked'}
        · <button type="button" class="olink" onclick={() => openForm('change')}>change…</button>
        · <button type="button" class="olink" onclick={() => openForm('remove')}>remove…</button>
        · <button type="button" class="olink" disabled={submitting} onclick={doLock}>lock</button>
      {:else}
        · <button type="button" class="olink" onclick={() => openForm('unlock')}>unlock…</button>
      {/if}
    </span>
  </div>
{/snippet}

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
    {@render passphraseRow()}
  </div>
{:else}
  <div class="wleft">
    {#if resp.lowSpace}
      <p class="oghint brwarn brlow">
        disk is getting low · the vault is cycling the ten · releasing a kept backup is the one way to free space
        here
      </p>
    {/if}
    {#each routers as router (router.device)}
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
            {#if gated}
              {#if newest.backupArrivedAt}{formatSize(newest.backupBytes ?? 0)}{/if}
            {:else}
              {#if newest.backupArrivedAt}{formatSize(newest.backupBytes ?? 0)}
                <button type="button" class="olink" onclick={() => download(router.device, newest.id, 'backup')}>
                  download .backup
                </button> ·{/if}
              {#if newest.rscArrivedAt}
                <button type="button" class="olink" onclick={() => download(router.device, newest.id, 'rsc')}>.rsc</button>
              {/if}
            {/if}
            {#if isGone(router)}
              · <button type="button" class="olink" onclick={() => onopenlost(router.device)}>is it gone?</button>
            {/if}
            {#if canKeep}
              · <button type="button" class="olink" onclick={() => openKeepForm('keep', router.device, newest.id, '')}>
                keep…
              </button>
            {/if}
          </p>
          {#if gated}
            <p class="oghint brnewest">
              {lock.locked ? 'locked — the vault passphrase opens downloads' : 'unlocked by another of your sign-ins — unlock here to download'}
            </p>
          {/if}
        {/if}
        {#if router.generations.length > 1}
          <p class="oghint brnewest">
            <button
              type="button"
              class="olink"
              onclick={() => (expanded = { ...expanded, [router.device]: !expanded[router.device] })}
            >
              earlier…
            </button>
          </p>
          {#if expanded[router.device]}
            {#each earlier(router) as g (g.id)}
              <p class="oghint brnewest">
                {when(g)} · {sizeOf(g)}
                {#if !gated}
                  {#if g.backupArrivedAt}
                    · <button type="button" class="olink" onclick={() => download(router.device, g.id, 'backup')}>
                      .backup
                    </button>
                  {/if}
                  {#if g.rscArrivedAt}
                    · <button type="button" class="olink" onclick={() => download(router.device, g.id, 'rsc')}>
                      .rsc
                    </button>
                  {/if}
                {/if}
                {#if canKeep}
                  · <button type="button" class="olink" onclick={() => openKeepForm('keep', router.device, g.id, '')}>
                    keep…
                  </button>
                {/if}
              </p>
            {/each}
          {/if}
        {/if}
        {#if keptOf(router).length > 0}
          <p class="oghint brkept">kept</p>
          {#each keptOf(router) as g (g.id)}
            <p class="oghint brnewest">
              ✱ {when(g)} · {sizeOf(g)} · {g.comment}
              {#if !gated}
                {#if g.backupArrivedAt}
                  · <button type="button" class="olink" onclick={() => download(router.device, g.id, 'backup')}>
                    .backup
                  </button>
                {/if}
                {#if g.rscArrivedAt}
                  · <button type="button" class="olink" onclick={() => download(router.device, g.id, 'rsc')}>
                    .rsc
                  </button>
                {/if}
              {/if}
              {#if canKeep}
                ·
                <button
                  type="button"
                  class="olink"
                  onclick={() => openKeepForm('edit', router.device, g.id, g.comment ?? '')}
                >
                  edit…
                </button>
                ·
                <button
                  type="button"
                  class="olink"
                  onclick={() => {
                    formError = null
                    releasing = { device: router.device, generation: g.id }
                  }}
                >
                  release…
                </button>
              {/if}
            </p>
            {#if releasing && releasing.device === router.device && releasing.generation === g.id}
              <p class="oghint brnewest">
                release this one? it goes back into the ten and the oldest may go ·
                <button type="button" class="olink" disabled={submitting} onclick={() => doRelease(router.device, g.id)}>
                  release
                </button>
                /
                <button type="button" class="olink" disabled={submitting} onclick={() => (releasing = null)}>
                  cancel
                </button>
                {#if formError}<span class="oghint err" role="alert">{formError}</span>{/if}
              </p>
            {/if}
          {/each}
        {/if}
      </div>
    {/each}
    <p class="oghint">
      each push is a pair — the binary .backup that restores the router whole, and the .rsc export it can be read
      from · the eleventh pair lets the oldest go · a download is written to the audit log with your name · a kept
      backup stays out of the ten until you release it
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
    {@render passphraseRow()}
  </div>
{/if}

{#if resp.enabled && openKind}
  <div class="pform">
    {#if openKind === 'set' || openKind === 'change'}
      {#if openKind === 'change'}
        <label class="lab">
          current passphrase
          <input type="password" autocomplete="current-password" disabled={submitting} bind:value={currentPassphrase} />
        </label>
      {/if}
      <label class="lab">
        {openKind === 'change' ? 'new passphrase' : 'passphrase'}
        <input type="password" autocomplete="new-password" disabled={submitting} bind:value={newPassphrase} />
      </label>
      <p class="oghint pnote">
        If this passphrase is lost, the stored backups are lost with it. There is no reset and no recovery — not
        from mikroview, and not from the router. Keep it wherever you keep your other recovery keys.
      </p>
      <label class="lab">
        confirm {openKind === 'change' ? 'new passphrase' : 'passphrase'}
        <input type="password" autocomplete="new-password" disabled={submitting} bind:value={confirmPassphrase} />
      </label>
    {:else if openKind === 'remove'}
      <p class="oghint pnote">
        Removing the passphrase re-seals every stored backup under the retention key. Any admin can read them again.
      </p>
      <label class="lab">
        current passphrase
        <input type="password" autocomplete="current-password" disabled={submitting} bind:value={removePassphraseText} />
      </label>
    {:else if openKind === 'unlock'}
      <label class="lab">
        vault passphrase
        <input type="password" autocomplete="current-password" disabled={submitting} bind:value={unlockPassphraseText} />
      </label>
    {:else if openKind === 'keep' || openKind === 'edit'}
      <label class="lab">
        <input
          type="text"
          aria-label="why keep this one"
          placeholder="why keep this one"
          maxlength={MAX_KEEP_COMMENT}
          disabled={submitting}
          bind:value={keepComment}
        />
      </label>
    {/if}
    {#if formError}<p class="oghint err" role="alert">{formError}</p>{/if}
    <span class="acts">
      <button type="button" class="olink" disabled={submitting} onclick={closeForm}>cancel</button>
      {#if openKind === 'set'}
        <button type="button" class="olink" disabled={submitting} onclick={submitSet}>{submitting ? 'setting…' : 'set'}</button>
      {:else if openKind === 'change'}
        <button type="button" class="olink" disabled={submitting} onclick={submitChange}>
          {submitting ? 'changing…' : 'change'}
        </button>
      {:else if openKind === 'remove'}
        <button type="button" class="olink" disabled={submitting} onclick={submitRemove}>
          {submitting ? 'removing…' : 'remove'}
        </button>
      {:else if openKind === 'unlock'}
        <button type="button" class="olink" disabled={submitting} onclick={submitUnlock}>
          {submitting ? 'unlocking…' : 'unlock'}
        </button>
      {:else if openKind === 'keep' || openKind === 'edit'}
        <button type="button" class="olink" disabled={submitting} onclick={submitKeep}>
          {submitting ? 'keeping…' : 'keep'}
        </button>
      {/if}
    </span>
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

  /* The low-space line sits above the routers, with room under it so it
     reads as a statement about the whole group rather than the first
     router's own receipt. */
  .brlow {
    margin-bottom: 6px;
  }

  /* The `kept` label: the quietest thing on the block, since the lines
     under it carry the ✱ that says what they are. */
  .brkept {
    margin-top: 4px;
    font-style: normal;
    letter-spacing: 0.04em;
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

  .oghint {
    margin: 2px 0 0;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
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

  .orow .ov.dim {
    color: var(--fg-dim);
  }

  /* The passphrase forms (#1115): full width, under both columns, in
     the same dashed-underline-input grammar EngineRoom's own "let
     someone in"/"mint a key" panels use, so it reads as one family of
     form even though each component draws its own copy of the rules
     (Svelte scopes styles per component). */
  .pform {
    grid-column: 1 / -1;
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px 0 2px;
    margin-top: 4px;
    border-top: 1px solid var(--border);
  }

  .pform .lab {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 11px;
    color: var(--fg-dim);
    max-width: 360px;
  }

  .pform input {
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    padding: 3px 0;
    outline: none;
  }

  .pform input:focus {
    border-bottom-color: var(--accent);
  }

  .pform .pnote {
    margin: 0;
    max-width: 480px;
  }

  .pform .acts {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
</style>
