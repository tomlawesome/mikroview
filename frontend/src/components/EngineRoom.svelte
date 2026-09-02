<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Settings as the shelf (#633, rounds 23-25, owner-approved): the
  // page reports live truth and is not a form until you touch it.
  // Seven groups -- your deck (the cards in the order you keep them,
  // drag to reorder, sign-in lands on the first), ingest (the one-way
  // pathway), keys (which machines may speak), detection (what fired
  // this hour, and the bench behind tune), memory (what the buffer
  // holds, hour by hour), account, people (who may look in). The
  // watcher bench (EngineRoomWatchers) unfolds from detection's tune
  // row.
  //
  // This replaces #490's five-station signal path wholesale; the
  // stations' facts survive inside the groups (the store -> memory, the
  // watchers/flags desk -> detection), and the room's honesty rule is
  // unchanged: the page never pretends a knob the server does not hold.
  //
  // Round 32 (#767) mounts keys and people directly in the card, in its
  // own row grammar -- the two doors EngineRoomDoors.svelte used to draw
  // below the shelf behind USERS_DOOR_ENABLED/TOKENS_DOOR_ENABLED
  // (round 30/#700/#691 had unmounted both). That component and its
  // flags are retired outright (no shim -- see AGENTS.md's "removals
  // are wholesale"); usersState/tokensState and their create/remove/
  // revoke calls are the same ones it used.
  //
  // Both groups are gated on isAdmin, not just the verbs: GET
  // /api/tokens and GET /api/auth/users are both admin-only today
  // (internal/api/tokens.go's handleTokensList, #657; internal/api/
  // auth.go's handleAuthListUsers), so a `user` or `viewer` session
  // cannot even read either list. Round 32's own drawing (settings-
  // doors.html) and its issue body (#767) describe keys as visible to
  // "anyone signed in", verbs admin-only -- that reading is not
  // reachable without reopening the read #657 deliberately closed, which
  // is a security-gate call this build does not make unilaterally. Kept
  // on the closer, backend-consistent reading (both groups admin-only,
  // like people already is) and flagged in the PR rather than guessed
  // past.
  import { onMount } from 'svelte'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import { deckCards } from '../lib/deckCards'
  import { deckOrderState } from '../lib/deckOrder.svelte'
  import { versionState } from '../lib/version.svelte'
  import { persistenceState } from '../lib/persistence.svelte'
  import { familyOf } from '../lib/flagPalette'
  import { fetchSetupStatus, fetchDevices } from '../lib/api'
  import { usersState } from '../lib/users.svelte'
  import { tokensState } from '../lib/tokens.svelte'
  import { formatEps, formatHM, formatRelative, parseGoDurationSeconds, formatDaysSince } from '../lib/format'
  import { portOf, pushScript } from '../lib/setupsteps'
  import type { SetupStatus, FlagType, Device } from '../lib/types'
  import EngineRoomWatchers from './EngineRoomWatchers.svelte'

  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')
  // The watchers station's own tier (#653): running the detector bench
  // is a normal operational action, open to user and admin alike --
  // unlike keys and people (tokens, users), which stay admin-only.
  const canEdit = $derived(authState.state === 'authenticated' && authState.canEdit)

  let status = $state<SetupStatus | null>(null)
  let benchOpen = $state(false)

  onMount(() => {
    fetchSetupStatus()
      .then((s) => (status = s))
      .catch(() => {
        // The ingest facts that come from config.yaml simply show
        // nothing until this resolves rather than a page-wide error.
      })
    detectorSettingsState.refresh().catch(() => {})
    versionState.ensureLoaded().catch(() => {})
    // 403s for a non-admin (see persistenceState's own doc comment) --
    // the persistence row below just states less for that caller,
    // same swallow-and-degrade shape as fetchSetupStatus above.
    persistenceState.ensureLoaded().catch(() => {})
    // keys and people: never fetched below admin -- both GET /api/tokens
    // and GET /api/auth/users 403 for anyone else (see the module doc
    // comment above), so fetching either as a lesser role would only
    // leave the group rendering an error it has no way to act on.
    if (isAdmin) {
      usersState.refresh().catch(() => {})
      tokensState.refresh().catch(() => {})
    }
    fetchDevices()
      .then((all) => {
        // Same de-dup/order rule the former tokens door applied -- an
        // id-less or duplicated device would crash the keyed {#each}
        // the mint form's device picker uses.
        const byID = new Map<string, Device>()
        for (const d of all) {
          if (d.id && !byID.has(d.id)) byID.set(d.id, d)
        }
        knownDevices = [...byID.values()].sort(
          (a, b) => Number(b.configured) - Number(a.configured) || a.id.localeCompare(b.id),
        )
      })
      .catch(() => {
        knownDevices = []
      })
  })

  const epsText = $derived(appState.stats ? formatEps(appState.stats.eventsPerSecond) : null)

  // --- account: sessions (#677) ----------------------------------------
  // "this device" is a plain fact, not a fabricated device name: nothing
  // in internal/auth tracks a hostname or user-agent per session (see
  // auth.Session -- just ID/UserID/IssuedAt/ExpiresAt), so the row states
  // only what's real -- that this is one signed-in session among however
  // many -- rather than inventing a label like the mockup's "tom-desktop".
  let soSaving = $state(false)
  let soError = $state<string | null>(null)
  let soDone = $state(false)

  async function doSignOutEverywhere() {
    soSaving = true
    soError = null
    soDone = false
    const err = await authState.signOutEverywhere()
    soSaving = false
    if (err) soError = err
    else soDone = true
  }

  // --- your deck -----------------------------------------------------------
  const cards = $derived(deckOrderState.apply(deckCards(authState.isAdmin, authState.canEdit)))

  let dragKey = $state<string | null>(null)

  function onDrop(targetKey: string) {
    if (dragKey && dragKey !== targetKey) deckOrderState.move(dragKey, targetKey)
    dragKey = null
  }

  // Keyboard route to the same reorder: a card's handle moves it one
  // place left or right, mirroring what a drag does.
  function onCardKey(e: KeyboardEvent, key: string) {
    const order = cards.map((c) => c.key)
    const i = order.indexOf(key)
    if (e.key === 'ArrowLeft' && i > 0) {
      e.preventDefault()
      deckOrderState.move(key, order[i - 1])
    } else if (e.key === 'ArrowRight' && i < order.length - 1) {
      e.preventDefault()
      deckOrderState.move(order[i + 1], key)
    }
  }

  // --- detection -----------------------------------------------------------
  // Short chip names, mockup-cased; the same deliberate duplication
  // convention TYPE_LABELS follows elsewhere (see Flags.svelte).
  const CHIP_LABELS: Record<FlagType, string> = {
    port_scan: 'port scan',
    activity_spike: 'activity spike',
    critical_port: 'critical port',
    global_spike: 'network surge',
    distributed_brute_force: 'brute force',
    outbound_anomaly: 'outbound',
    internal_recon: 'internal recon',
    rule_spike: 'rule spike',
    repeated_drops: 'repeated drops',
    low_slow_scan: 'low & slow scan',
    off_hours_activity: 'off hours',
    device_silence: 'gone quiet',
    new_device: 'new device',
    stale_rule: 'stale rule',
    unexpected_mail_sender: 'mail sender',
    known_bad_ip: 'known-bad IP',
  }

  // Newly-raised episodes this hour, by type -- flagsState.timeSeries is
  // exactly that window (see fetchFlags' comment). Only types that fired
  // wear a chip; the rest fold into one quiet count.
  const fired = $derived.by(() => {
    const counts = new Map<FlagType, number>()
    for (const b of flagsState.timeSeries) {
      for (const [t, n] of Object.entries(b.byType)) {
        if (n) counts.set(t as FlagType, (counts.get(t as FlagType) ?? 0) + n)
      }
    }
    return [...counts.entries()].sort((a, b) => b[1] - a[1])
  })
  const quietTypes = $derived(Math.max(0, Object.keys(CHIP_LABELS).length - fired.length))

  const watchersRunning = $derived(detectorSettingsState.list.filter((d) => d.enabled).length)
  const watchersTotal = $derived(detectorSettingsState.list.length)

  // --- detection: port-scan window (#677) -----------------------------
  // The detector's own numeric tuning -- threshold/window -- read and
  // written through the exact same GET/PUT /api/definitions the bench
  // above uses for enabled/scope (see detectorSettingsState.updateParams).
  // Deliberately not a second source of truth: this is
  // detectorSettingsState.list's port_scan entry, same list the bench
  // renders from. Distinct from that entry's *scope* (which restricts
  // which hosts/ports it watches) -- this is the count/window it fires
  // at, carried through Definition.params.
  const portScan = $derived(detectorSettingsState.list.find((d) => d.name === 'port_scan'))
  const portScanSummary = $derived.by(() => {
    const p = portScan?.params
    if (!p || typeof p.threshold !== 'number' || typeof p.window !== 'string') return null
    return `${p.threshold} ports / ${Math.round(parseGoDurationSeconds(p.window))} s`
  })

  let psEditing = $state(false)
  let psThreshold = $state(1)
  let psWindowSeconds = $state(1)
  let psSaving = $state(false)
  let psError = $state<string | null>(null)

  function openPortScanEdit() {
    const p = portScan?.params
    if (!p || typeof p.threshold !== 'number' || typeof p.window !== 'string') return
    psThreshold = p.threshold
    psWindowSeconds = Math.round(parseGoDurationSeconds(p.window))
    psError = null
    psEditing = true
  }

  async function savePortScanWindow() {
    psSaving = true
    const err = await detectorSettingsState.updateParams('port_scan', {
      threshold: psThreshold,
      window: `${psWindowSeconds}s`,
    })
    psSaving = false
    psError = err
    if (!err) psEditing = false
  }

  // --- memory --------------------------------------------------------------
  // The buffer's own time series, folded into at most 24 ribbon slices,
  // each shaded by how much of the buffer that stretch holds.
  const memSlices = $derived.by(() => {
    const series = appState.stats?.timeSeries ?? []
    if (series.length === 0) return []
    const SLICES = Math.min(24, series.length)
    const per = Math.ceil(series.length / SLICES)
    const sums: number[] = []
    for (let i = 0; i < series.length; i += per) {
      let v = 0
      for (const b of series.slice(i, i + per)) {
        v += Object.values(b.byAction).reduce((a, n) => a + (n ?? 0), 0)
      }
      sums.push(v)
    }
    const max = Math.max(...sums, 1)
    return sums.map((v) => v / max)
  })

  const oldestHeld = $derived.by(() => {
    const series = appState.stats?.timeSeries ?? []
    return series.length > 0 ? formatHM(series[0].time) : null
  })

  const retentionHours = $derived(
    appState.stats ? Math.max(1, Math.round(appState.stats.windowSeconds / 3600)) : null,
  )

  // --- memory: persistence (#677) -------------------------------------
  // Two halves, both live truth rather than the ratified copy's "JSON
  // store · 14 d" (no such event-retention feature exists -- see
  // internal/persist's own package doc): which backend the stores it
  // does cover (flags, definitions, watchlist entries, entities,
  // tokens/accounts) actually use, from persistenceState -- absent
  // entirely for a non-admin, the same absent-not-disabled grammar the
  // rest of Settings' admin-only facts already follow -- plus the one
  // fact that holds regardless of role or config: the event buffer
  // above is memory-only.
  const persistenceSummary = $derived.by(() => {
    const bufferFact = 'the event buffer above is memory-only and clears on restart'
    const info = persistenceState.info
    if (!info) return bufferFact
    const backend = info.backend === 'postgres' ? 'Postgres' : `file store · ${info.dir ?? '—'}`
    return `${backend} — holds flags, definitions, watchlist entries, entities and tokens; ${bufferFact}`
  })

  // --- ingest --------------------------------------------------------------
  const routers = $derived.by(() => {
    const list = [...appState.devices].sort(
      (a, b) => new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime(),
    )
    return list.slice(0, 2)
  })

  function quietFor(lastSeen: string): string | null {
    const days = Math.floor((Date.now() - new Date(lastSeen).getTime()) / 86400000)
    return days >= 1 ? `quiet ${days} d — quiet is a fact, not a fault` : null
  }

  // --- keys (#767, round 32) -------------------------------------------
  // "Which machines may speak", moved from the retired EngineRoomDoors
  // door into the ingest group's own row grammar -- the reveal, the
  // mint form and revoke's arm-then-confirm all follow
  // docs/design/concepts/round-32/settings-doors.html's #keys verbatim.
  let knownDevices = $state<Device[]>([])
  let showMintKey = $state(false)
  let newKeyName = $state('')
  let newKeyKind = $state<'api' | 'ingest'>('api')
  let newKeyDevice = $state('')
  let keyError = $state<string | null>(null)
  let minting = $state(false)
  let keyCopied = $state(false)
  let routerCopied = $state(false)
  let armedRevoke = $state<string | null>(null)

  function selectKeyKind(kind: 'api' | 'ingest') {
    newKeyKind = kind
    if (kind === 'ingest' && !newKeyDevice) newKeyDevice = knownDevices[0]?.id ?? ''
  }

  function openMintKey() {
    showMintKey = true
    keyError = null
  }

  function cancelMintKey() {
    showMintKey = false
    newKeyName = ''
    newKeyKind = 'api'
    newKeyDevice = ''
    keyError = null
  }

  async function submitMintKey() {
    keyError = null
    if (newKeyKind === 'ingest' && !newKeyDevice) {
      keyError = 'An ingest key needs a device -- pick the router it speaks for.'
      return
    }
    minting = true
    keyCopied = false
    routerCopied = false
    const err = await tokensState.create(
      newKeyName.trim(),
      newKeyKind,
      newKeyKind === 'ingest' ? newKeyDevice : undefined,
    )
    minting = false
    if (err) {
      keyError = err
      return
    }
    cancelMintKey()
  }

  async function copyKeyValue(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      keyCopied = true
    } catch {
      // The value stays selectable in the reveal regardless.
    }
  }

  // copyRouterLines hands back the same push script the setup wizard
  // would (setupsteps.ts's pushScript, keyed to instance() address and
  // status.pushKinds) with the freshly-minted key already embedded, so
  // rotating an ingest key never sends the operator back through setup
  // for a line-by-line diff.
  async function copyRouterLines(value: string) {
    if (!status) return
    const script = pushScript(window.location.host, value, status.pushKinds)
    try {
      await navigator.clipboard.writeText(script)
      routerCopied = true
    } catch {
      // Nothing else to fall back to -- the value itself stays copyable.
    }
  }

  function onRevokeClick(e: MouseEvent, id: string) {
    e.stopPropagation()
    if (armedRevoke === id) {
      armedRevoke = null
      revokeKey(id)
      return
    }
    // One armed verb at a time, as in the mockup: arming this disarms
    // whatever else was armed, key or person.
    disarmAll()
    armedRevoke = id
  }

  async function revokeKey(id: string) {
    const err = await tokensState.revoke(id)
    if (err) keyError = err
  }

  // --- people (#767, round 32) -------------------------------------------
  // "Who may look in", moved from the retired EngineRoomDoors door into
  // the account group's own row grammar. Admin-only end to end -- GET
  // /api/auth/users is gated, so the group is absent, not empty, for
  // anyone else.
  let showAddPerson = $state(false)
  let newPersonName = $state('')
  let newPersonPass = $state('')
  let newPersonRole = $state<'user' | 'viewer'>('user')
  let personError = $state<string | null>(null)
  let addingPerson = $state(false)
  let armedRemove = $state<string | null>(null)

  // Your own row leads the list, then everyone else's, matching the
  // drawing's "your account, then everyone else's" -- the server has no
  // opinion on the order.
  const people = $derived.by(() => {
    const mine = usersState.list.filter((u) => u.username === authState.username)
    const rest = usersState.list.filter((u) => u.username !== authState.username)
    return [...mine, ...rest]
  })

  function openAddPerson() {
    showAddPerson = true
    personError = null
  }

  function cancelAddPerson() {
    showAddPerson = false
    newPersonName = ''
    newPersonPass = ''
    newPersonRole = 'user'
    personError = null
  }

  async function submitAddPerson() {
    personError = null
    addingPerson = true
    const err = await usersState.create(newPersonName.trim(), newPersonPass, newPersonRole)
    addingPerson = false
    if (err) {
      personError = err
      return
    }
    cancelAddPerson()
  }

  function onRemoveClick(e: MouseEvent, id: string) {
    e.stopPropagation()
    if (armedRemove === id) {
      armedRemove = null
      removePerson(id)
      return
    }
    disarmAll()
    armedRemove = id
  }

  async function removePerson(id: string) {
    const err = await usersState.remove(id)
    if (err) personError = err
  }

  // Round 28's arm-then-confirm gesture (Docket.svelte's clear-all
  // bubble is the other example): a click anywhere that isn't the armed
  // button itself disarms it, so an armed revoke/remove can't be
  // triggered by a stray click elsewhere on the page.
  function disarmAll() {
    armedRevoke = null
    armedRemove = null
  }
</script>

<svelte:window onclick={disarmAll} />

<div class="page scrollbar">
  <!-- No page heading (#697/#700), which took the READ-ONLY chip off
       this page with it: the chip lived in the header, and round 30
       drew no replacement anywhere. That left #548's grammar --
       read-only declared once, in words, never by disabling every
       control -- with nowhere to be said, recorded here as a gap.
       Rounds 37-38 close it (#804): the sentence is now on the account
       chip, "anna (viewer) · read-only" (AccountMenu.svelte), which is
       the one place every screen already says who you are, so it is
       said once for the whole app rather than once per page. Nothing is
       owed here. keys and people stay gated on isAdmin either way. -->

  <div class="setlay">
    <div class="og deckcol">
      <h3>your deck</h3>
      <div class="stshelf">
        {#each cards as card, i (card.key)}
          <span
            class="stcard"
            class:first={i === 0}
            class:dragging={dragKey === card.key}
            draggable="true"
            role="button"
            tabindex="0"
            aria-label="{card.name}, position {i + 1} of {cards.length} — arrow keys reorder"
            ondragstart={() => (dragKey = card.key)}
            ondragend={() => (dragKey = null)}
            ondragover={(e) => e.preventDefault()}
            ondrop={() => onDrop(card.key)}
            onkeydown={(e) => onCardKey(e, card.key)}
          >
            <i aria-hidden="true">⠿</i>
            {#if card.key === 'fall'}
              <svg viewBox="0 0 76 40" aria-hidden="true">
                <line x1="16" y1="6" x2="16" y2="22" stroke="var(--fall-accept)" stroke-width="2" opacity="0.55" />
                <line x1="30" y1="12" x2="30" y2="24" stroke="var(--fall-drop)" stroke-width="2" opacity="0.4" />
                <line x1="44" y1="4" x2="44" y2="16" stroke="var(--fall-accept)" stroke-width="2" opacity="0.3" />
                <line x1="58" y1="10" x2="58" y2="28" stroke="var(--fall-accept)" stroke-width="2" opacity="0.5" />
                <line x1="8" y1="34" x2="68" y2="34" stroke="var(--now)" stroke-width="1.4" opacity="0.6" />
              </svg>
            {:else if card.key === 'topography'}
              <svg viewBox="0 0 76 40" aria-hidden="true">
                <circle cx="38" cy="10" r="5" fill="none" stroke="var(--accent)" stroke-width="1.3" />
                <circle cx="38" cy="10" r="1.4" fill="var(--accent)" />
                <path d="M35 14 C 26 22, 20 24, 16 30" fill="none" stroke="var(--lane-lan)" stroke-width="1.3" />
                <path d="M38 15 V 30" fill="none" stroke="var(--lane-srv)" stroke-width="1.3" />
                <path d="M41 14 C 50 22, 56 24, 60 30" fill="none" stroke="var(--lane-iot)" stroke-width="1.3" />
                <circle cx="16" cy="32" r="2.6" fill="var(--lane-lan)" />
                <circle cx="38" cy="32" r="2.6" fill="var(--lane-srv)" />
                <circle cx="60" cy="32" r="2.6" fill="var(--lane-iot)" />
              </svg>
            {:else if card.key === 'metrics'}
              <svg viewBox="0 0 76 40" aria-hidden="true">
                <polyline
                  points="6,26 16,22 24,28 32,12 40,24 48,18 56,27 70,20"
                  fill="none"
                  stroke="var(--accent)"
                  stroke-width="1.4"
                  opacity="0.8"
                />
                <line x1="6" y1="33" x2="70" y2="33" stroke="var(--border)" stroke-width="1" />
              </svg>
            {:else if card.key === 'live'}
              <svg viewBox="0 0 76 40" aria-hidden="true">
                <line x1="8" y1="9" x2="68" y2="9" stroke="var(--fg-dim)" stroke-width="1.2" opacity="0.7" />
                <line x1="8" y1="17" x2="56" y2="17" stroke="var(--fg-dim)" stroke-width="1.2" opacity="0.5" />
                <line x1="8" y1="25" x2="64" y2="25" stroke="var(--alarm)" stroke-width="1.2" opacity="0.6" />
                <line x1="8" y1="33" x2="50" y2="33" stroke="var(--fg-dim)" stroke-width="1.2" opacity="0.4" />
              </svg>
            {:else if card.key === 'docket'}
              <svg viewBox="0 0 76 40" aria-hidden="true">
                <rect x="8" y="6" width="3" height="8" fill="#ff5470" />
                <line x1="16" y1="10" x2="66" y2="10" stroke="var(--fg-dim)" stroke-width="1.2" opacity="0.6" />
                <rect x="8" y="18" width="3" height="8" fill="#ff9e64" />
                <line x1="16" y1="22" x2="58" y2="22" stroke="var(--fg-dim)" stroke-width="1.2" opacity="0.5" />
                <rect x="8" y="30" width="3" height="8" fill="var(--marked)" />
                <line x1="16" y1="34" x2="62" y2="34" stroke="var(--fg-dim)" stroke-width="1.2" opacity="0.5" />
              </svg>
            {:else if card.key === 'entities'}
              <svg viewBox="0 0 76 40" aria-hidden="true">
                <circle cx="14" cy="10" r="3.4" fill="none" stroke="var(--accent)" stroke-width="1.2" />
                <circle cx="14" cy="10" r="1" fill="var(--accent)" />
                <line x1="24" y1="10" x2="66" y2="10" stroke="var(--border)" stroke-width="1" />
                <circle cx="14" cy="22" r="2.4" fill="var(--lane-lan)" />
                <line x1="24" y1="22" x2="58" y2="22" stroke="var(--border)" stroke-width="1" />
                <circle cx="14" cy="33" r="2.4" fill="var(--lane-iot)" />
                <line x1="24" y1="33" x2="62" y2="33" stroke="var(--border)" stroke-width="1" />
              </svg>
            {:else}
              <svg viewBox="0 0 76 40" aria-hidden="true">
                <line x1="10" y1="10" x2="66" y2="10" stroke="var(--border)" stroke-width="1.2" />
                <circle cx="46" cy="10" r="3" fill="var(--accent)" />
                <line x1="10" y1="21" x2="66" y2="21" stroke="var(--border)" stroke-width="1.2" />
                <circle cx="24" cy="21" r="3" fill="var(--fg-dim)" />
                <line x1="10" y1="32" x2="66" y2="32" stroke="var(--border)" stroke-width="1.2" />
                <circle cx="56" cy="32" r="3" fill="var(--fg-dim)" />
              </svg>
            {/if}
            <span class="nm">{card.name}</span>
            {#if card.key === 'fall' && epsText}
              <span class="lv">{epsText} events/s now</span>
            {:else if card.key === 'docket'}
              <span class="lv">
                {#if flagsState.activeCount > 0}<b class="ct">⚑ {flagsState.activeCount}</b>{/if}
                {#if isAdmin && watchlistState.entries.length > 0}
                  <b class="wct">◉ {watchlistState.entries.length}</b>
                  {#if watchlistState.brokenCount > 0}<b class="ct">○{watchlistState.brokenCount}</b>{/if}
                {/if}
              </span>
            {/if}
            {#if i === 0}<b class="lands">SIGN-IN LANDS HERE</b>{/if}
          </span>
        {/each}
      </div>
    </div>

    <div class="og stgrid">
      <div class="stsection wide">
        <h3>ingest</h3>
        <div class="wleft">
          <svg
            class="stpath"
            viewBox="0 0 520 92"
            role="img"
            aria-label="Routers push their logs one way into mikroview's listening port; nothing travels back"
          >
            {#if routers[0]}
              <circle cx="52" cy="30" r="10" fill="none" stroke="var(--accent)" stroke-width="1.4" />
              <circle cx="52" cy="30" r="2" fill="var(--accent)" />
              <text x="52" y="54" text-anchor="middle" class="sp-n">{routers[0].name}</text>
              <path d="M66 30 C 180 30, 260 38, 340 42" fill="none" stroke="var(--border)" stroke-width="1.4" />
            {/if}
            {#if routers[1]}
              <circle cx="52" cy="74" r="6" fill="none" stroke="var(--fg-dim)" stroke-width="1.2" opacity="0.7" />
              <text x="66" y="78" class="sp-n" opacity="0.7">
                {routers[1].name}{quietFor(routers[1].lastSeen) ? ` · ${quietFor(routers[1].lastSeen)}` : ''}
              </text>
              <path
                d="M60 70 C 180 64, 260 54, 340 48"
                fill="none"
                stroke="var(--border)"
                stroke-width="1.2"
                opacity="0.6"
              />
            {/if}
            <path d="M334 37 L 345 44 L 333 50" fill="none" stroke="var(--fg-dim)" stroke-width="1.3" />
            {#if routers[0] && appState.stats && appState.stats.eventsPerSecond > 0}
              <!-- The arriving pulse travels the live router's line only
                   (rounds 25: honesty in motion) -- a quiet router gets
                   none. Hidden entirely under prefers-reduced-motion. -->
              <circle class="sp-pulse" r="2.4" fill="var(--accent)" />
            {/if}
            <circle cx="372" cy="45" r="15" fill="none" stroke="var(--accent)" stroke-width="1.5" />
            <circle cx="372" cy="45" r="4" fill="var(--accept)" />
            {#if status}
              <text x="396" y="41" class="sp-k">
                {portOf(status.instance.syslogPort)}{status.instance.tlsEnabled ? ' · TLS' : ''} · listening
              </text>
            {/if}
            {#if epsText}
              <text x="396" y="57" class="sp-n">{epsText} events/s arriving now</text>
            {/if}
          </svg>
          <p class="oghint">the logs travel one way — mikroview never connects to your router</p>
        </div>
        <div class="wrows">
          {#if status}
            <div class="orow">
              <span>syslog listener</span>
              <span class="ov">
                {portOf(status.instance.syslogPort)}{status.instance.tlsEnabled ? ' · TLS' : ''} ·
                <span class="yaml">set in config.yaml; the page shows what is, the file decides</span>
              </span>
            </div>
          {/if}
          <div class="orow">
            <span>who may speak</span>
            <span class="ov">holders of an ingest key — the keys group below</span>
          </div>
        </div>
      </div>

      {#if isAdmin}
        <div class="stsection" id="keys">
          <h3>keys</h3>
          <p class="oghint">
            which machines may speak — an ingest key lets one router push its state; a read-only key lets a reader
            ask
          </p>

          {#if tokensState.justCreated}
            {@const jc = tokensState.justCreated}
            <div class="reveal">
              <div class="rk">
                <span class="pn">{jc.name}</span>
                <span class="pr" class:ingest={jc.kind === 'ingest'}>
                  {jc.kind === 'ingest' ? `ingest · speaks for ${jc.device}` : 'read-only'}
                </span>
                <code>{jc.value}</code>
                <button type="button" class="olink" onclick={() => copyKeyValue(jc.value ?? '')}>
                  {keyCopied ? 'copied' : 'copy'}
                </button>
                <button type="button" class="olink quiet" onclick={() => tokensState.clearJustCreated()}>
                  done
                </button>
              </div>
              <div class="rnote">shown once — mikroview keeps only its fingerprint, so copy it now</div>
              {#if jc.kind === 'ingest'}
                <div class="rnote">
                  the router lines, with this key already in them:
                  <button type="button" class="olink" onclick={() => copyRouterLines(jc.value ?? '')}>
                    {routerCopied ? 'copied for RouterOS' : 'copy for RouterOS'}
                  </button>
                </div>
              {/if}
            </div>
          {/if}

          {#each tokensState.list.filter((t) => t.id !== tokensState.justCreated?.id) as tok (tok.id)}
            <div class="prow">
              <span class="pn">{tok.name}</span>
              <span class="pr" class:ingest={tok.kind === 'ingest'}>
                {tok.kind === 'ingest' ? `ingest · speaks for ${tok.device}` : 'read-only'}
              </span>
              <span class="pf">
                {tok.lastUsedAt ? `spoke ${formatRelative(tok.lastUsedAt, appState.now)}` : 'never spoke — yet'}
              </span>
              <button
                type="button"
                class="olink quiet revoke"
                class:armed={armedRevoke === tok.id}
                onclick={(e) => onRevokeClick(e, tok.id)}
              >
                {armedRevoke === tok.id ? 'confirm — it stops speaking now' : 'revoke'}
              </button>
            </div>
          {/each}

          {#if keyError}<p class="oghint err">{keyError}</p>{/if}

          {#if showMintKey}
            <div class="pform wf">
              <input
                type="text"
                placeholder="name it — birdcage, grafana, the-laptop"
                aria-label="key name"
                bind:value={newKeyName}
              />
              <span class="seg" role="group" aria-label="Key kind">
                <button type="button" class:on={newKeyKind === 'api'} onclick={() => selectKeyKind('api')}>
                  read-only
                </button>
                <button type="button" class:on={newKeyKind === 'ingest'} onclick={() => selectKeyKind('ingest')}>
                  ingest
                </button>
              </span>
              {#if newKeyKind === 'ingest'}
                <span class="lab">for</span>
                {#if knownDevices.length > 0}
                  <span class="seg" role="group" aria-label="which router">
                    {#each knownDevices as d (d.id)}
                      <button
                        type="button"
                        data-device-id={d.id}
                        class:on={newKeyDevice === d.id}
                        onclick={() => (newKeyDevice = d.id)}
                      >
                        {d.name && d.name !== d.id ? d.name : d.id}
                      </button>
                    {/each}
                  </span>
                {:else}
                  <span class="lab">no devices known yet — one appears here once it sends syslog</span>
                {/if}
              {/if}
              <span class="acts dwr-acts">
                <button type="button" class="quiet" onclick={cancelMintKey}>cancel</button>
                <button type="button" disabled={minting} onclick={submitMintKey}>
                  {minting ? 'minting…' : 'mint it'}
                </button>
              </span>
            </div>
          {:else}
            <div class="ogfoot"><button type="button" class="olink" onclick={openMintKey}>+ mint a key</button></div>
          {/if}
        </div>
      {/if}

      <div class="stsection wide">
        <h3>detection</h3>
        <div class="wleft">
          <div class="stflags">
            {#each fired as [type, n] (type)}
              <span class="stf" style="color: {familyOf(type).ink}">
                {familyOf(type).mark} {CHIP_LABELS[type] ?? type} · {n}
              </span>
            {/each}
            {#if quietTypes > 0}
              <span class="stf dim">
                {fired.length > 0 ? `+ ${quietTypes} more · quiet this hour` : 'all quiet this hour'}
              </span>
            {/if}
          </div>
        </div>
        <div class="wrows">
          <div class="orow">
            <span>detectors</span>
            <span class="ov">
              {watchersRunning} of {watchersTotal} on ·
              <button class="olink" onclick={() => (benchOpen = !benchOpen)}>
                {benchOpen ? 'close the bench' : 'tune…'}
              </button>
            </span>
          </div>
          {#if benchOpen}
            <div class="bench">
              <EngineRoomWatchers {canEdit} />
            </div>
          {/if}
          {#if portScan}
            <div class="orow">
              <span>port-scan window</span>
              <span class="ov">
                {#if psEditing}
                  <span class="pswform">
                    <input
                      type="number"
                      min="1"
                      step="1"
                      class="psn"
                      aria-label="distinct ports"
                      disabled={psSaving}
                      bind:value={psThreshold}
                    /> ports /
                    <input
                      type="number"
                      min="1"
                      step="1"
                      class="psn"
                      aria-label="window in seconds"
                      disabled={psSaving}
                      bind:value={psWindowSeconds}
                    /> s
                    <button class="olink" disabled={psSaving} onclick={savePortScanWindow}>
                      {psSaving ? 'saving…' : 'save'}
                    </button>
                    <button class="olink" disabled={psSaving} onclick={() => (psEditing = false)}>cancel</button>
                  </span>
                {:else if canEdit && portScanSummary}
                  <button class="pswknob" onclick={openPortScanEdit}>{portScanSummary}</button>
                {:else}
                  {portScanSummary ?? '—'}
                {/if}
              </span>
            </div>
            {#if psError}
              <p class="oghint err">{psError}</p>
            {/if}
          {/if}
        </div>
      </div>

      <div class="stsection wide">
        <h3>memory</h3>
        <div class="wleft">
          {#if memSlices.length > 0}
            <svg
              class="stmem"
              viewBox="0 0 520 40"
              role="img"
              aria-label="The event buffer, hour by hour; darker stretches held more, the oldest falls away as the newest arrives"
            >
              <rect x="8" y="14" width="500" height="10" rx="5" fill="var(--bg-hover)" />
              {#each memSlices as v, i (i)}
                <rect
                  x={8 + (500 / memSlices.length) * i}
                  y="14"
                  width={500 / memSlices.length}
                  height="10"
                  fill="var(--accent)"
                  opacity={0.05 + 0.25 * v}
                />
              {/each}
              <rect x="504" y="9" width="3" height="20" rx="1.5" fill="var(--now)" />
              {#if oldestHeld}
                <text x="8" y="38" class="sp-n">{oldestHeld} — the oldest event still held</text>
              {/if}
              <text x="508" y="38" text-anchor="end" class="sp-k">now</text>
            </svg>
          {/if}
          <p class="oghint">the oldest falls away as the newest arrives; darker stretches held more</p>
        </div>
        <div class="wrows">
          {#if appState.stats}
            <div class="orow">
              <span>event buffer</span>
              <span class="ov">
                {appState.stats.count.toLocaleString()} of {appState.stats.capacity.toLocaleString()} events
                {#if retentionHours !== null}
                  · ~{retentionHours} h window
                {/if}
              </span>
            </div>
          {/if}
          <div class="orow">
            <span>what reads it</span>
            <span class="ov">every scene below reads from here; nothing anywhere probes</span>
          </div>
          <!-- #677: "persistence — JSON store · 14 d" was the ratified
               copy, but no event store with a day-based retention exists
               -- internal/persist's own package doc calls the live event
               stream the one deliberate in-memory-only exception, with no
               config path that changes it. internal/persist IS real,
               though, and backs flags/definitions/watchlist/entities/
               tokens -- persistenceSummary states which backend it
               actually uses (see persistenceState) alongside the buffer
               fact, rather than only the negative half. -->
          <div class="orow">
            <span>persistence</span>
            <span class="ov dim">{persistenceSummary}</span>
          </div>
        </div>
      </div>

      <div class="stsection">
        <h3>account</h3>
        <div class="orow">
          <span>signed in</span>
          <span class="ov">{authState.username} ({authState.role})</span>
        </div>
        <div class="orow">
          <span>password</span>
          <span class="ov"><button class="olink" onclick={() => (authState.showChangePassword = true)}>change…</button></span>
        </div>
        <!-- #677: "tom-desktop" in the ratified copy was a mockup
             placeholder -- internal/auth's Session carries no device
             name or user-agent (just ID/UserID/IssuedAt/ExpiresAt), so
             "this device" states what's real (one session among
             however many) without inventing a label. -->
        <div class="orow">
          <span>sessions</span>
          <span class="ov">
            this device{authState.signedInSince ? `, signed in ${formatDaysSince(authState.signedInSince)}` : ''} ·
            <button class="olink" disabled={soSaving} onclick={doSignOutEverywhere}>
              {soSaving ? 'signing out…' : 'sign out everywhere'}
            </button>
          </span>
        </div>
        {#if soError}
          <p class="oghint err">{soError}</p>
        {/if}
        {#if soDone}
          <p class="oghint">done — every other session has been ended</p>
        {/if}
        <div class="orow">
          <span>version</span>
          <span class="ov dim">{versionState.version || '—'} · AGPL-3.0</span>
        </div>
      </div>

      {#if isAdmin}
        <div class="stsection" id="people">
          <h3>people</h3>
          <p class="oghint">who may look in, and what each may do — the admin is made at the console, never here</p>

          {#each people as user (user.id)}
            <div class="prow">
              <span class="pn">{user.username}</span>
              {#if user.role === 'admin'}<span class="pr admin">admin</span>{/if}
              {#if user.role === 'viewer'}<span class="pr look">can only look</span>{/if}
              {#if user.sso}<span class="pr">sso</span>{/if}
              <span class="pf">
                {user.username === authState.username ? 'this is you · ' : ''}{user.lastLogin
                  ? `signed in ${formatRelative(user.lastLogin, appState.now)}`
                  : 'never signed in — yet'}
              </span>
              {#if user.role === 'admin'}
                <span class="olink quiet dim" title="transfer the admin role from the command line first">
                  console-only
                </span>
              {:else}
                <button
                  type="button"
                  class="olink quiet remove"
                  class:armed={armedRemove === user.id}
                  onclick={(e) => onRemoveClick(e, user.id)}
                >
                  {armedRemove === user.id ? 'confirm — signs them out, revokes their keys' : 'remove'}
                </button>
              {/if}
            </div>
          {/each}

          {#if personError}<p class="oghint err">{personError}</p>{/if}

          {#if showAddPerson}
            <div class="pform wf">
              <input type="text" placeholder="their name" aria-label="username" autocomplete="off" bind:value={newPersonName} />
              <input
                type="password"
                placeholder="a first password — they change it"
                aria-label="password"
                autocomplete="new-password"
                bind:value={newPersonPass}
              />
              <span class="seg" role="group" aria-label="What they may do">
                <button type="button" class:on={newPersonRole === 'user'} onclick={() => (newPersonRole = 'user')}>
                  can change things
                </button>
                <button type="button" class:on={newPersonRole === 'viewer'} onclick={() => (newPersonRole = 'viewer')}>
                  can only look
                </button>
              </span>
              <span class="acts dwr-acts">
                <button type="button" class="quiet" onclick={cancelAddPerson}>cancel</button>
                <button type="button" disabled={addingPerson} onclick={submitAddPerson}>
                  {addingPerson ? 'saving…' : 'let them in'}
                </button>
              </span>
            </div>
          {:else}
            <div class="ogfoot">
              <button type="button" class="olink" onclick={openAddPerson}>+ let someone in</button>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px 24px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .og {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 12px 14px;
  }

  .og h3 {
    margin: 0 0 6px;
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  .oghint {
    margin: 2px 0 8px;
    font-size: 11.5px;
    font-style: italic;
    color: var(--fg-dim);
  }

  /* The settings card is one .og panel, same treatment as the deck
     band above it -- not four separately-bordered cards. ingest,
     detection, memory and account are .stsection children of it; a
     divider between them (not inside them -- see .orow below, #735)
     is what makes "same top edge, same border/radius/padding/
     background" actually hold, and stops either box drawing more than
     it holds (owner: the boxes didn't line up). */
  .stsection + .stsection {
    margin-top: 14px;
    padding-top: 14px;
    border-top: 1px solid var(--border);
  }

  /* ingest, detection and memory each split into a fixed 560px left
     column (the diagram and its caption) and a right column of rows,
     side by side (owner, 2026-08-31) -- see the round's own #set
     .og.wide rule. account has no diagram, so it stays a plain
     .stsection of stacked rows. */
  .stsection.wide {
    display: grid;
    grid-template-columns: 560px 1fr;
    column-gap: 40px;
    align-items: start;
  }

  .stsection.wide > h3 {
    grid-column: 1 / -1;
  }

  .stsection.wide > .wleft {
    grid-column: 1;
    min-width: 0;
  }

  .stsection.wide > .wrows {
    grid-column: 2;
    min-width: 0;
  }

  .stsection.wide .wleft .oghint {
    margin-bottom: 0;
  }

  @media (max-width: 1100px) {
    .stsection.wide {
      grid-template-columns: 1fr;
    }

    .stsection.wide > .wleft,
    .stsection.wide > .wrows {
      grid-column: 1;
    }
  }

  /* Round 30 (#735): the deck goes back to a full-width horizontal band
     across the top, the settings card below it (owner, 2026-08-31) --
     this partially reverses the narrow-left-column layout #725 landed;
     #725's own substance (one settings panel, not four) is untouched. */
  .setlay {
    display: flex;
    flex-direction: column;
    gap: 22px;
  }

  .stgrid {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  /* --- your deck: the shelf ---------------------------------------------- */
  .deckcol .stshelf {
    display: flex;
    flex-direction: row;
    flex-wrap: wrap;
    gap: 6px;
  }

  .stcard {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 3px;
    width: 108px;
    padding: 8px 10px;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 8px;
    cursor: grab;
    user-select: none;
  }

  .stcard.dragging {
    opacity: 0.4;
  }

  .stcard.first {
    border-color: var(--accent);
  }

  .stcard i {
    position: absolute;
    top: 6px;
    right: 8px;
    font-style: normal;
    font-size: 10px;
    color: var(--fg-dim);
  }

  .stcard svg {
    width: 76px;
    height: 40px;
  }

  .stcard .nm {
    font-size: 9.5px;
    font-weight: 650;
    letter-spacing: 0.1em;
    color: var(--fg-muted);
  }

  .stcard .lv {
    font-family: var(--font-mono);
    font-size: 9.5px;
    color: var(--fg-dim);
    min-height: 12px;
    display: flex;
    gap: 6px;
  }

  .stcard .lv .ct {
    color: var(--alarm);
  }

  .stcard .lv .wct {
    color: var(--marked);
  }

  .stcard .lands {
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.1em;
    color: var(--accent);
  }

  /* --- the shared row grammar -------------------------------------------- */
  /* Round 30 (#735): the row hairline made a settings page read as a
     ruled ledger (owner, 2026-08-31) -- rows separate on space and the
     label/value alignment alone now. Group headings keep their own
     divider (see .stsection + .stsection above); rows do not. */
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

  .yaml {
    font-style: italic;
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

  /* --- ingest ------------------------------------------------------------- */
  .stpath {
    width: 100%;
    height: auto;
    display: block;
  }

  .sp-n {
    font-family: var(--font-mono);
    font-size: 9.5px;
    fill: var(--fg-dim);
  }

  .sp-k {
    font-family: var(--font-mono);
    font-size: 10px;
    fill: var(--fg-muted);
  }

  .sp-pulse {
    offset-path: path('M66 30 C 180 30, 260 38, 340 42');
    animation: travel 3.2s linear infinite;
  }

  @keyframes travel {
    from {
      offset-distance: 0%;
      opacity: 0;
    }
    12% {
      opacity: 1;
    }
    88% {
      opacity: 1;
    }
    to {
      offset-distance: 100%;
      opacity: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .sp-pulse {
      display: none;
    }
  }

  /* --- detection ----------------------------------------------------------- */
  .stflags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 12px;
    margin-bottom: 8px;
  }

  .stf {
    font-family: var(--font-mono);
    font-size: 10.5px;
    font-weight: 600;
    white-space: nowrap;
  }

  .stf.dim {
    color: var(--fg-dim);
    font-weight: 400;
  }

  .bench {
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  /* The port-scan window's own dashed-underline handle -- same "a
     control is a fact wearing a handle" convention as
     EngineRoomWatchers' .scope-knob, so the value itself is what you
     click rather than a separate "edit" word next to it. */
  .pswknob {
    color: var(--fg);
    font-weight: 600;
    border: none;
    border-bottom: 1px dashed var(--accent);
    background: transparent;
    padding: 0;
    font: inherit;
  }

  .pswknob:hover {
    color: var(--accent);
  }

  .pswform {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex-wrap: wrap;
  }

  .psn {
    width: 3.4em;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 3px 5px;
    font-size: 12px;
    font-family: var(--font-mono);
  }

  .oghint.err {
    color: var(--reject);
    font-style: normal;
  }

  /* --- memory -------------------------------------------------------------- */
  .stmem {
    width: 100%;
    height: auto;
    display: block;
  }

  /* --- keys, people (#767, round 32): the two doors' own row grammar ------
     Ported from docs/design/concepts/round-32/settings-doors.html's #keys/
     #people -- name · chips · fact · quiet verb, a form row where the next
     row would go, and the once-only reveal. Mockup tokens translate onto
     this file's own custom properties the same way the rest of the card
     already does (--hair/--hair-2 -> --border, --ink/-2/-3 -> --fg/-muted/
     -dim, --raised -> --bg, --ok -> --accept, --sans dropped since the
     inherited default already is one). */
  .prow {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 0;
    border-top: 1px solid var(--border);
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .stsection .prow:first-of-type {
    border-top: 0;
  }

  .prow .pn,
  .reveal .pn {
    color: var(--fg);
    font-weight: 600;
    min-width: 92px;
  }

  .pr {
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.05em;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 2px 9px;
    color: var(--fg-dim);
    white-space: nowrap;
  }

  .pr.admin {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }

  .pr.look {
    color: var(--now);
    border-color: color-mix(in srgb, var(--now) 45%, transparent);
  }

  .pr.ingest {
    color: var(--accept);
    border-color: color-mix(in srgb, var(--accept) 40%, transparent);
  }

  .prow .pf {
    margin-left: auto;
    color: var(--fg-dim);
    text-align: right;
    font-size: 11px;
  }

  .prow .olink,
  .reveal .olink,
  .ogfoot .olink {
    font-size: 12.5px;
  }

  .olink.quiet {
    color: var(--fg-dim);
  }

  .olink.quiet:hover {
    color: var(--fg-muted);
  }

  .olink.quiet.dim {
    cursor: default;
  }

  .prow .remove.armed,
  .prow .revoke.armed {
    color: var(--alarm);
  }

  .ogfoot {
    margin-top: 4px;
    padding: 9px 0 2px;
    border-top: 1px solid var(--border);
  }

  .pform {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 9px 0;
    border-top: 1px solid var(--border);
    flex-wrap: wrap;
  }

  .pform input {
    width: 220px;
    flex: none;
  }

  .pform .acts {
    margin-left: auto;
    display: flex;
    gap: 8px;
    align-items: center;
  }

  /* The form is round 31's -- dashed-underline inputs, segmented choices,
     pill actions -- so it reads the same wherever it is met. */
  .wf input {
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg);
    padding: 3px 0;
    outline: none;
  }

  .wf input:focus {
    border-bottom-color: var(--accent);
  }

  .wf input::placeholder {
    color: var(--fg-dim);
    opacity: 0.7;
  }

  .wf .seg {
    display: inline-flex;
    border: 1px solid var(--border);
    border-radius: 999px;
    overflow: hidden;
    flex: none;
  }

  .wf .seg button {
    font-family: var(--font-mono);
    font-size: 10.5px;
    font-weight: 600;
    color: var(--fg-dim);
    padding: 2px 12px;
    background: none;
    border: none;
    cursor: pointer;
  }

  .wf .seg button.on {
    color: var(--fg);
    background: var(--bg-hover);
  }

  .dwr-acts button {
    font-size: 11px;
    font-weight: 600;
    color: var(--accent);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 4px 16px;
    cursor: pointer;
  }

  .dwr-acts button:hover {
    border-color: var(--accent);
  }

  .dwr-acts button.quiet {
    color: var(--fg-dim);
  }

  /* A freshly minted key: the one moment the secret exists on screen. */
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

  .reveal .olink + .olink {
    margin-left: 0;
  }

  .rnote {
    font-size: 11px;
    color: var(--fg-dim);
    margin-top: 6px;
  }

  .pform .lab {
    font-size: 11px;
    color: var(--fg-dim);
  }
</style>
