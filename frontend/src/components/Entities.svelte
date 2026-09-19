<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // Entities, built to round 29's ratified scene (#675, superseding
  // #647's Fleet-folded-in page and the older #489/DESIGN.md survey
  // concept, which was ratified before round 29 restyled every "operate"
  // page onto the deck's own clothes and was never built against it).
  // The scene is two things under the strap "the routers that push
  // here, and the named things behind them":
  //
  //  1. Router cards -- one per device that pushes here (Fleet's old
  //     table, folded into cards: lib/fleet.ts's sort/status logic still
  //     backs it, so this and the standalone Fleet.svelte can't drift).
  //     A live card carries its current push rate; a quiet one states
  //     that fact without alarm. Adding a router used to be a further
  //     dashed card printed in the row, carrying the standing promise
  //     (mikroview only ever receives, never connects out) and the real
  //     RouterOS lines to paste as instructional prose on the page. The
  //     owner's round-30 review (#718) called that apparatus, not
  //     content -- it became a single "add router" pill button that
  //     opened a small dialog with the same port, lines
  //     (lib/setupsteps.ts, the same generator the setup wizard uses) and
  //     assurance, on demand rather than always printed. A further
  //     review ("can we do something cooler for this button?") called
  //     the pill itself "a conventional web button on a screen built to
  //     look like an instrument" -- it is now the empty berth: one more
  //     card at the end of the router row, the same size and shape as a
  //     real one but empty, that unfolds in place into those same
  //     commands (Design: the add-router control, Option 1, #718). The
  //     row's own bordered panel went the same way (#718's "boxes in
  //     boxes"): the label stays, the frame around already-bordered
  //     cards does not.
  //  2. Three views -- hosts / rules / ports (#681, drawn in rounds
  //     37-38 and built by #804) -- over one table. Not tabs: the
  //     metrics page's own idiom, three view names carrying their
  //     counts, one underlined, because rules and ports are names too
  //     and so are views of one set of named things rather than three
  //     destinations. hosts is the ratified table exactly as #675 built
  //     it and stays the default. rules and ports exist because naming
  //     in context has nowhere to happen: a rule that is in a router's
  //     pushed rule table but has never fired has no row anywhere else
  //     to click (#681's owner decision), so it needs its own surface,
  //     whether or not it has ever fired. The old page's separate
  //     add-entity form is still gone -- these views name what has
  //     actually arrived (fired) or been pushed (a rule), never a blank
  //     invented row.
  //
  //     No descriptor line sits under a view. Round 37 drew one under
  //     each explaining where that kind of name comes from; round 38
  //     removed all four on the owner's word (2026-09-02: "Remove all
  //     these little descriptor lines on the entities views, I don't
  //     want them"). Where a name comes from is documentation.
  //
  // mac/first-seen/last-seen have no existing source (the entity store
  // only ever held label/tags, and the client event buffer is far too
  // short-lived for "412 d"): #675 added the minimal backend piece
  // (device.MACRegistry.NoteIP + GET /api/devices/macs) rather than
  // inventing the numbers, reusing the MAC registry that already existed
  // for the new-device detector. Lane reuses zones.svelte.ts unchanged
  // (the same boundary-derived zones the topography map draws).
  //
  // The rules tab's chain/action/last-fired columns are its own join,
  // not GET /api/rules alone: that endpoint (internal/rules.Store) only
  // ever holds a rule label once it has fired, so a never-fired rule is
  // simply absent from it, not present with a zero count -- the exact
  // case #681 exists for. The full pushed-rule table (fetchRouterRules,
  // already loaded per router below for the router cards' rule count)
  // carries chain/action/log-prefix for every rule regardless of firing,
  // keyed by lib/routerLookup.svelte.ts's own "<ACTION>|<slug>|"
  // log-prefix convention (ruleLabelFromLogPrefix, added for this tab as
  // that file's prefixMatchesLabel's inverse) -- the same convention
  // #445's router-lookup popup already decodes the other direction. A
  // rule with no log-prefix, or one that doesn't follow the convention,
  // can never produce a firing event carrying a label either (payload.go
  // -- "an unlogged rule must stay unnameable"). It used to be left off
  // this view for that reason; rounds 37-38 draw it present instead,
  // showing its number -- "#17 — no comment on the router" -- because
  // the rule is on the router and fires on the router, so a rules view
  // that omits it is not the rule table. It stays unnameable: the row
  // is plain dim text with no rename affordance, for every tier.
  import { onMount } from 'svelte'
  import { entitiesState } from '../lib/entities.svelte'
  import { appState } from '../lib/state.svelte'
  import { authState } from '../lib/auth.svelte'
  import { dossierState } from '../lib/dossier.svelte'
  import { flagsState } from '../lib/flags.svelte'
  import { watchlistState } from '../lib/watchlist.svelte'
  import { zonesState } from '../lib/zones.svelte'
  import { familyOf } from '../lib/flagPalette'
  import {
    fetchDeviceMACs,
    fetchRouterRules,
    fetchRouterAddresses,
    fetchRules,
    fetchRefusedSenders,
    fetchSetupStatus,
    fetchUnattributedSources,
    type RouterFilterRule,
  } from '../lib/api'
  import { discoverHosts, discoverPorts } from '../lib/discoveredEntities'
  import { ruleLabelFromLogPrefix } from '../lib/routerLookup.svelte'
  import { formatLastHeard, formatSpacedAge, formatHM } from '../lib/format'
  import {
    deviceState,
    multihomedEcho,
    setupEcho,
    sortedDevices,
    ratePerSecond,
    unattributedLabel,
    UNATTRIBUTED_FIX,
  } from '../lib/fleet'
  import { REFUSED_STRIP_LEAD } from '../lib/setupsteps'
  import { wizardState } from '../lib/wizard.svelte'
  import type {
    EntityType,
    MACRegistryEntry,
    RefusedSender,
    RuleUsage,
    SetupStatus,
    UnattributedSource,
  } from '../lib/types'

  // --- routers (folded in from Fleet, #647; cards since #675) ---------
  const routerRows = $derived(sortedDevices(appState.devices))

  // A router that pushes but is not in the devices config is drawn as a
  // state of the dashed third slot (rounds 37-38, `#ent.unreg`), not as
  // one more card in the fleet: it gets its own card, in `.fcard.unreg`
  // form, ahead of the berth in `.fcards`.
  //
  // The berth used to give way to that card -- one dashed slot saying
  // either "point a router here" or "something is pointed here and has
  // no name yet", never both. #828 overturned that (owner, 2026-09-03):
  // an operator with one unregistered router already pushing still needs
  // to see how to connect a second one, and those instructions live only
  // in the berth. The berth is now always the last card in the row,
  // collapsed once anything is pushing, alongside any unregistered
  // card(s) rather than replaced by them.
  //
  // This is the one fact the round-30 device-status strip carried that
  // had no home once that strip became the router cards (round-37
  // README). `configured` is the registry's own word for it, the same
  // field Fleet and Topography already read.
  const registeredRouters = $derived(routerRows.filter((d) => d.configured))
  const unregisteredRouters = $derived(routerRows.filter((d) => !d.configured))

  // The registry's other list (#1170): syslog sources no router has
  // claimed -- no configured sourceIp matches them, and no single
  // router's pushed address table carries them. The server stopped
  // inventing a device row for one, so they arrive alongside the
  // devices rather than among them, and they are drawn as sources here,
  // never as routers.
  //
  // Re-read whenever the fleet moves: a source stops being
  // unattributed the moment config.yaml names it or exactly one
  // router's address table claims it. A failed read just leaves the
  // list empty -- these cards explain something, and an explanation is
  // not worth an error state on this page.
  //
  // "moves" is judged on this signature, not on appState.devices
  // itself (#1269). App.svelte's global 5s poll reassigns
  // appState.devices to a brand-new array every tick regardless of
  // whether anything in it changed -- a live router's rate/lastSeen
  // update every cycle -- so watching the array directly re-asked GET
  // /api/devices a second time, every 5 seconds, for the very payload
  // that poll had just downloaded. id/sourceIp/configured are the only
  // fields that can turn a source from unattributed to attributed;
  // collapsing them into one string means Svelte reruns this effect on
  // that string's *value* changing, not on the array's identity, so a
  // poll tick that alters none of them costs nothing here.
  const deviceSignature = $derived(routerRows.map((d) => `${d.id}:${d.sourceIp}:${d.configured}`).join('|'))
  let unattributed = $state<UnattributedSource[]>([])
  $effect(() => {
    void deviceSignature
    fetchUnattributedSources()
      .then((list) => {
        unattributed = list
      })
      .catch(() => {})
  })

  // The refused senders (#1281): addresses whose syslog lines were
  // dropped for belonging to no enrolled router. They live here, beside
  // the routers, because this is the screen an admin's deck actually
  // draws for the fleet view (deckCards.ts, #785) and GET
  // /api/devices/refused is admin-only -- the strip Fleet.svelte
  // originally carried could be reached by nobody. Read on the fleet's
  // own signature like the unattributed sources above; a failed read
  // leaves the list empty, since these cards explain a silence rather
  // than being one.
  const isAdmin = $derived(authState.isAdmin)
  let refused = $state<RefusedSender[]>([])
  $effect(() => {
    void deviceSignature
    if (!isAdmin) {
      refused = []
      return
    }
    fetchRefusedSenders()
      .then((list) => {
        refused = list
      })
      .catch(() => {
        refused = []
      })
  })

  // Re-enrol… opens the router ledger at Send logs for one router with
  // a fresh token: a replaced or re-addressed router needs the enrol
  // line again and nothing else.
  function reEnrol(deviceId: string) {
    wizardState.openReEnrol(deviceId)
  }

  // Finish registering… (#1291) opens the router ledger straight at
  // Register, for a router whose only gap is that one step -- its
  // enrolment already stands, so nothing here mints a fresh token.
  function finishRegistering(deviceId: string) {
    wizardState.openRegister(deviceId)
  }

  // Renaming is an edit, so the viewer tier does not get the affordance
  // and its names stop looking clickable. Nothing on this page says why:
  // the read-only fact is declared once, on the account chip
  // (AccountMenu.svelte), which is #548's ratified grammar and where
  // rounds 37-38 put the sentence. Repeating it per row -- or per page,
  // or as a lock on every control -- is what that grammar rules out.
  const canRename = $derived(authState.canEdit)

  let status = $state<SetupStatus | null>(null)

  // The empty berth (#718): one more card at the end of the router row,
  // the same size and shape as a real one but empty -- an outline with
  // nothing in it. Activating it (click, Enter or Space -- a native
  // <button>, so both keys work for free) used to unfold it in place
  // into the syslog paste lines; since #1284 it opens the router ledger
  // instead -- the setup wizard's own router steps, which name the
  // router, enrol it and print the same block with an enrol line at the
  // end. Two surfaces printing the same commands in different words is
  // exactly what the ledger being one component removes.
  function openBerth() {
    wizardState.openAddRouter()
  }

  // Per-router enrichment beyond what GET /api/devices already carries
  // (rule/zone counts, last push): the same pushed tables Topography's
  // zones and the setup wizard already read, fetched once per device
  // seen so far. A device that has never pushed either table simply
  // leaves these null -- absence is a fact, not a loading state to fake.
  interface RouterDetail {
    ruleCount: number | null
    zoneCount: number | null
    lastPush: string | null
  }
  let routerDetail = $state<Record<string, RouterDetail>>({})

  // The rules tab's source table (#681): kept alongside routerDetail's
  // ruleCount rather than re-fetched, since fetchRouterRules(deviceId)
  // already runs once per router below -- the count and the rows it is
  // counted from come from the same response.
  let routerRulesRaw = $state<Record<string, RouterFilterRule[]>>({})

  async function loadRouterDetail(deviceId: string) {
    const [rules, addrs] = await Promise.all([
      fetchRouterRules(deviceId).catch(() => null),
      fetchRouterAddresses(deviceId).catch(() => null),
    ])
    routerDetail[deviceId] = {
      ruleCount: rules?.available ? rules.rules.length : null,
      zoneCount: addrs?.available ? new Set(addrs.rules.map((a) => a.interface)).size : null,
      lastPush: rules?.updatedAt ?? addrs?.updatedAt ?? null,
    }
    routerRulesRaw[deviceId] = rules?.available ? rules.rules : []
  }

  // Fetches detail for any router this page hasn't asked about yet --
  // runs again whenever the device list gains one, e.g. a third router
  // just pointed its syslog here.
  $effect(() => {
    for (const d of routerRows) {
      if (!(d.id in routerDetail)) loadRouterDetail(d.id)
    }
  })

  // fstate/routerRate moved to lib/fleet.ts (deviceState/ratePerSecond)
  // when #706 put the viewer's Fleet card back on the deck: the two
  // surfaces draw the same router cards, so the status vocabulary and
  // the fixed-one-decimal rate (#718) have exactly one home.

  // --- named things: entity hosts + discovered-but-unnamed hosts, one
  // table (#675) ---------------------------------------------------------
  let macs = $state<MACRegistryEntry[]>([])
  const macByIp = $derived.by(() => {
    const m = new Map<string, MACRegistryEntry>()
    for (const e of macs) if (e.lastIp) m.set(e.lastIp, e)
    return m
  })

  const hostEntities = $derived(entitiesState.list.filter((e) => e.type === 'host'))
  const discoveredHosts = $derived(discoverHosts(appState.events, entitiesState.list))

  interface ThingRow {
    key: string // the IP address -- the entity key
    label: string // '' means not yet named
    fallbackLastSeen: string | null // from the client buffer, for a row with no MAC-registry entry
  }

  const thingRows = $derived.by((): ThingRow[] => {
    const rows: ThingRow[] = hostEntities.map((e) => ({ key: e.key, label: e.label ?? '', fallbackLastSeen: null }))
    for (const d of discoveredHosts) rows.push({ key: d.key, label: '', fallbackLastSeen: d.lastSeen })
    return rows
  })

  // The lanes: the same busiest-first order zones.svelte.ts already
  // ranks Topography's map by, reused here as a rank (not just a name)
  // so the table groups by lane the way the ratified scene's mockup
  // data does, and the dot wears the same colour Topography would give
  // that lane.
  const LANE_INKS = ['var(--lane-lan)', 'var(--lane-srv)', 'var(--lane-iot)', 'var(--lane-guest)', 'var(--marked)']
  const laneOf = $derived.by(() => {
    const m = new Map<string, { name: string; rank: number; ink: string }>()
    zonesState.zones.forEach((z, rank) => {
      for (const h of z.hosts) m.set(h.ip, { name: z.name, rank, ink: LANE_INKS[rank % LANE_INKS.length] })
    })
    return m
  })

  function elideMac(mac: string): string {
    const parts = mac.split(':')
    if (parts.length !== 6) return mac
    return `${parts[0]}:${parts[1]}:${parts[2]}:…:${parts[5]}`
  }

  // Marks, in the docket's own vocabulary (lib/flagPalette.ts,
  // watchlistState) -- never a new vocabulary invented for this page.
  interface Marks {
    newTalker: boolean
    watched: boolean
    ringBroken: boolean
    alarmCount: number
  }

  // The marks index -- built once per change of the flag and watchlist
  // state, not once per row. marksFor below used to walk all of
  // flagsState.list and all of watchlistState.entries for every row it
  // was asked about, so naming N things cost N x (flags + entries).
  // #690's 2026-09-09 profile caught it at 11.9% of self-time on a roll
  // to the *docket*, where nobody has asked to see this page at all --
  // Deck.svelte's neighbour-mount policy premounts Entities beside it,
  // and a flag arriving re-ran the whole scan. The keys are exactly the
  // ones marksFor already matched on, so the marks are unchanged; only
  // the scanning moved out of the loop.
  const markIndex = $derived.by(() => {
    // new_device names a MAC, every other family names an address --
    // which a flag target may carry a port suffix on, stripped here once
    // rather than per row.
    const newTalkerMacs = new Set<string>()
    const alarmsByAddress = new Map<string, number>()
    for (const f of flagsState.list) {
      if (f.cleared) continue
      if (f.type === 'new_device') {
        newTalkerMacs.add(f.target.toLowerCase())
        continue
      }
      if (familyOf(f.type).mark !== '✱') continue
      const addr = f.target.replace(/ -> port \d+$/, '')
      alarmsByAddress.set(addr, (alarmsByAddress.get(addr) ?? 0) + 1)
    }
    // One watch can be reached by three keys -- its destination IP, its
    // source IP, its source MAC -- and any one of them counts as a hit,
    // so each key carries that watch's own broken-ring answer and two
    // watches landing on the same key keep the broken one's.
    const ringByWatchKey = new Map<string, boolean>()
    const watch = (key: string | undefined, ringBroken: boolean) => {
      if (!key) return
      ringByWatchKey.set(key, (ringByWatchKey.get(key) ?? false) || ringBroken)
    }
    for (const e of watchlistState.entries) {
      if (!e.enabled) continue
      const ringBroken = watchlistState.coverage[e.id] === 'no-logging'
      watch(e.destIp, ringBroken)
      watch(e.source?.ip, ringBroken)
      watch(e.source?.mac?.toLowerCase(), ringBroken)
    }
    return { newTalkerMacs, alarmsByAddress, ringByWatchKey }
  })

  function marksFor(address: string, mac: string | undefined): Marks {
    const macLower = mac?.toLowerCase()
    let watched = false
    let ringBroken = false
    for (const key of macLower ? [address, macLower] : [address]) {
      const broken = markIndex.ringByWatchKey.get(key)
      if (broken === undefined) continue
      watched = true
      ringBroken ||= broken
    }
    return {
      newTalker: macLower !== undefined && markIndex.newTalkerMacs.has(macLower),
      watched,
      ringBroken,
      alarmCount: markIndex.alarmsByAddress.get(address) ?? 0,
    }
  }

  const NEW_TALKER_INK = familyOf('new_device').ink

  interface Row {
    key: string
    label: string
    lane: { name: string; ink: string } | null
    mac: MACRegistryEntry | null
    marks: Marks
  }

  const rows = $derived.by((): Row[] => {
    return thingRows
      .map((t): Row => {
        const mac = macByIp.get(t.key) ?? null
        const lane = laneOf.get(t.key) ?? null
        return { key: t.key, label: t.label, lane, mac, marks: marksFor(t.key, mac?.mac) }
      })
      .sort((a, b) => {
        const la = a.lane?.name ?? '￿'
        const lb = b.lane?.name ?? '￿'
        if (la !== lb) return la.localeCompare(lb)
        return (a.label || a.key).localeCompare(b.label || b.key)
      })
  })

  // formatSpacedAge, not formatRelative: round 38's `#ent` writes these
  // columns as a bare "412 d" / "2 m" / "now", with no "ago" suffix
  // anywhere in the table (lib/format.ts carries the idiom).
  function lastSeenOf(row: Row): string {
    if (row.mac) return formatSpacedAge(row.mac.lastSeen, appState.now)
    const t = thingRows.find((t) => t.key === row.key)
    return t?.fallbackLastSeen ? formatSpacedAge(t.fallbackLastSeen, appState.now) : '—'
  }

  function firstSeenOf(row: Row): string {
    return row.mac ? formatSpacedAge(row.mac.firstSeen, appState.now) : '—'
  }

  // --- rules tab: every pushed rule, named or not, fired or not (#681)
  // ---------------------------------------------------------------------
  let rulesUsage = $state<RuleUsage[]>([])
  const usageByRule = $derived.by(() => new Map(rulesUsage.map((u) => [u.rule, u])))
  const ruleEntities = $derived(entitiesState.list.filter((e) => e.type === 'rule'))

  // One row per distinct rule slug pushed by any router, deduped on the
  // slug itself: if two routers push a same-named rule (the operator's
  // own convention, not something mikroview enforces -- see
  // RulesForLogPrefix's doc), the later device in routerRows order wins
  // the displayed chain/action. A single-router fleet, by far the common
  // case, never hits this.
  const pushedRules = $derived.by(() => {
    const m = new Map<string, { chain: string; action: string }>()
    for (const d of routerRows) {
      for (const r of routerRulesRaw[d.id] ?? []) {
        const slug = ruleLabelFromLogPrefix(r.logPrefix)
        if (slug) m.set(slug, { chain: r.chain, action: r.action })
      }
    }
    return m
  })

  // A pushed rule that yields no slug still gets a row (rounds 37-38:
  // "a rule with no comment on the router shows its number"). Silence
  // would be the worse answer -- the rule exists on the router and fires
  // on the router, so a rules view that omits it is not the rule table.
  //
  // It cannot be named here, and that is not a limitation to work
  // around: the slug is how an event names the rule it fired, so a rule
  // with no usable log-prefix has no stable key to hang a name on (see
  // ruleLabelFromLogPrefix's own note, and payload.go's FilterRule doc
  // for the same rule on the push side). These rows therefore render as
  // plain dim text with no rename affordance, for every tier.
  //
  // `comment` is read directly rather than inferred from the missing
  // slug: a rule can have a comment and still not log, and telling that
  // operator "no comment on the router" would be a plain untruth.
  interface UncommentedRule {
    key: string
    display: string
    chain: string
    action: string
  }

  const unnamedPushedRules = $derived.by((): UncommentedRule[] => {
    const out: UncommentedRule[] = []
    for (const d of routerRows) {
      for (const r of routerRulesRaw[d.id] ?? []) {
        if (ruleLabelFromLogPrefix(r.logPrefix)) continue
        out.push({
          // Scoped by device: `ordinal` counts from the top of one
          // router's table, so two routers both have a rule #17.
          key: `${d.id}:#${r.ordinal}`,
          display: r.comment.trim() || `#${r.ordinal} — no comment on the router`,
          chain: r.chain,
          action: r.action,
        })
      }
    }
    return out
  })

  interface RuleRow {
    key: string // the rule slug -- the entity key
    label: string
    chain: string | null
    action: string | null
    lastFired: string | null // usage.lastSeen -- null means never fired
    // Set only on a pushed rule with no usable log-prefix: what to show
    // in the name column, and the flag that it can never be renamed.
    unnameable: string | null
  }

  const ruleRows = $derived.by((): RuleRow[] => {
    const keys = new Set<string>([...pushedRules.keys(), ...ruleEntities.map((e) => e.key), ...rulesUsage.map((u) => u.rule)])
    const named = [...keys].map((key): RuleRow => {
      const pushed = pushedRules.get(key) ?? null
      const usage = usageByRule.get(key) ?? null
      const entity = ruleEntities.find((e) => e.key === key)
      return {
        key,
        label: entity?.label ?? '',
        chain: pushed?.chain ?? null,
        action: pushed?.action ?? null,
        lastFired: usage?.lastSeen ?? null,
        unnameable: null,
      }
    })
    const unnamed = unnamedPushedRules.map(
      (r): RuleRow => ({
        key: r.key,
        label: '',
        chain: r.chain,
        action: r.action,
        // No slug means no event can ever name this rule, so there is no
        // usage record to read a firing time from.
        lastFired: null,
        unnameable: r.display,
      }),
    )
    return [...named, ...unnamed].sort((a, b) =>
      (a.unnameable || a.label || a.key).localeCompare(b.unnameable || b.label || b.key),
    )
  })

  // --- ports tab: every port named, plus every port seen in traffic
  // that isn't yet (#681) --------------------------------------------
  const portEntities = $derived(entitiesState.list.filter((e) => e.type === 'port'))
  const discoveredPorts = $derived(discoverPorts(appState.events, entitiesState.list))

  interface PortRow {
    key: string
    label: string
    lastSeen: string | null
  }

  const portRows = $derived.by((): PortRow[] => {
    const rows: PortRow[] = portEntities.map((e) => ({ key: e.key, label: e.label ?? '', lastSeen: null }))
    for (const d of discoveredPorts) rows.push({ key: d.key, label: '', lastSeen: d.lastSeen })
    return rows.sort((a, b) => Number(a.key) - Number(b.key))
  })

  onMount(() => {
    entitiesState.refresh().catch(() => {
      // The table simply shows fewer named rows until this resolves.
    })
    fetchDeviceMACs()
      .then((m) => (macs = m))
      .catch(() => {
        // mac/first-seen/last-seen fall back to '—' until this resolves.
      })
    zonesState.refresh().catch(() => {
      // Lane names fall back to the raw boundary id until this resolves.
    })
    fetchSetupStatus()
      .then((s) => (status = s))
      .catch(() => {
        // Nothing on this page depends on it beyond the unattributed
        // card's own wording; it simply reads less until this resolves.
      })
    fetchRules()
      .then((r) => (rulesUsage = r))
      .catch(() => {
        // The rules tab simply shows every pushed rule as never-fired
        // until this resolves -- true of what's loaded, not a guess.
      })
  })

  // --- the three views (#804, rounds 37-38): hosts / rules / ports.
  //
  // Not tabs. #681 built a tab strip here and round 30 drew none, so it
  // sat behind a TABS_ENABLED gate; rounds 37-38 resolve that by drawing
  // the metrics page's own idiom instead -- three view names carrying
  // their counts, "hosts 23 · rules 41 · ports 12", one underlined, over
  // the same table. Rules and ports are names too, so they are views of
  // one set of named things rather than three destinations, and the
  // table under them keeps its look and feel across a switch. The gate
  // is gone with the furniture it was hiding.
  //
  // The switch is the shipped `.sw` pattern from the metrics view
  // switcher (SceneBar.svelte), so the two read as one idiom rather than
  // as a lookalike -- buttons, not the drawing's bare spans, so the
  // views are reachable and operable from the keyboard.
  type EntityView = 'hosts' | 'rules' | 'ports'
  let activeView = $state<EntityView>('hosts')

  // ---- inline rename (issue #675: rename lives in the table, not a
  // separate form; #681 generalizes it across all three tabs -- same
  // store, same EntityType, same Enter-saves/Esc-cancels/blur-saves
  // behaviour, one rename path rather than three) ----------------------
  let renamingKey = $state<{ type: EntityType; key: string } | null>(null)
  let renameDraft = $state('')
  let renameSaving = $state(false)
  let renameError = $state<string | null>(null)

  function isRenaming(type: EntityType, key: string): boolean {
    return renamingKey?.type === type && renamingKey.key === key
  }

  function startRename(type: EntityType, key: string, label: string) {
    renamingKey = { type, key }
    renameDraft = label
    renameError = null
  }

  function cancelRename() {
    renamingKey = null
    renameError = null
  }

  async function saveRename(type: EntityType, key: string) {
    renameError = null
    renameSaving = true
    const existing = entitiesState.list.find((e) => e.type === type && e.key === key)
    const wasRenaming = renamingKey
    renamingKey = null
    const err = await entitiesState.upsert({
      type,
      key,
      label: renameDraft.trim(),
      tags: existing?.tags ?? [],
    })
    renameSaving = false
    if (err) {
      renamingKey = wasRenaming
      renameError = err
    }
  }

  function onRenameKeydown(e: KeyboardEvent, type: EntityType, key: string) {
    if (e.key === 'Enter') {
      e.preventDefault()
      saveRename(type, key)
    } else if (e.key === 'Escape') {
      cancelRename()
    }
  }

  // Removing the input on Enter/Escape can itself fire a blur in a real
  // browser (losing focus because its element vanished, not because the
  // operator tabbed away) -- guarded on renamingKey still being this row
  // so that trailing blur never re-saves (Enter) or overrides a cancel
  // with the stale draft (Escape). jsdom doesn't reproduce this blur, so
  // nothing in the test suite would have caught it without the guard.
  function onRenameBlur(type: EntityType, key: string) {
    if (isRenaming(type, key)) saveRename(type, key)
  }

  function focusOnMount(node: HTMLInputElement) {
    node.focus()
    node.select()
  }
</script>

{#snippet reEnrolButton(deviceId: string, label: string)}
  <!-- Re-enrol… (#1284): the ledger at Send logs with a fresh token,
       for a router that has been replaced or has moved address. Absent
       rather than disabled for anyone who cannot use it (#657's
       grammar). One snippet for both cards below it renders on -- a
       registered router and one only pushing -- so the two can never
       read the same button differently (#1291 audit, stage 5). -->
  <button
    type="button"
    class="row-action"
    onclick={() => reEnrol(deviceId)}
    aria-label="Re-enrol {label} — mint a fresh enrolment token for it"
  >
    Re-enrol…
  </button>
{/snippet}

{#snippet finishRegisteringButton(deviceId: string, label: string)}
  <!-- Finish registering… (#1291): the ledger at Register, keeping the
       router's device and history, with no fresh token minted -- for a
       router whose enrolment already stands and is only short of the
       ledger's own confirmation. Re-enrol… stays offered beside it for
       when the enrolment itself is the problem; this is for the one
       step nearly finished, not the whole walk again. -->
  <button
    type="button"
    class="row-action"
    onclick={() => finishRegistering(deviceId)}
    aria-label="Finish registering {label} — resume the Register step for it"
  >
    Finish registering…
  </button>
{/snippet}

<div class="page scrollbar op-page">
  <div class="opwrap"><div class="opanel">
    <div class="og">
      <h3>routers — every one that pushes here</h3>
        <div class="fcards">
          {#each registeredRouters as d (d.id)}
            {@const st = deviceState(d, appState.now)}
            {@const detail = routerDetail[d.id]}
            <div class="fcard" class:live={d.status === 'live'}>
              <div class="fhead"><b>{d.name}</b><span class="fstate {st.cls}">{st.mark} {st.text}</span></div>
              <div class="frow">
                {d.routerosVersion ? `RouterOS ${d.routerosVersion}` : 'RouterOS version not yet reported'}
                {#if detail?.ruleCount !== null && detail?.ruleCount !== undefined}
                  · {detail.ruleCount} rule{detail.ruleCount === 1 ? '' : 's'}
                {/if}
                {#if detail?.zoneCount !== null && detail?.zoneCount !== undefined}
                  · {detail.zoneCount} zone{detail.zoneCount === 1 ? '' : 's'}
                {/if}
              </div>
              {#if d.status === 'live'}
                <div class="frow">
                  {#if detail?.lastPush}last push {formatHM(detail.lastPush)} ·{/if} {ratePerSecond(appState.events, d.id, appState.now)} events/s now
                </div>
              {:else if d.status === 'never_seen'}
                <div class="frow dim">never heard from yet</div>
              {:else}
                <div class="frow dim">last heard {formatLastHeard(d.lastSeen, appState.now)} — quiet is a fact, not a fault</div>
              {/if}
              {#if multihomedEcho(d)}
                <!-- The source-address split's echo (#442), the same
                     sentence Fleet.svelte carries: Send logs owns the
                     diagnosis and the command (named, not numbered --
                     it read "step 2" until #1284 moved Send logs to
                     third; see fleet.ts's multihomedEcho comment). -->
                <div class="frow dim">{multihomedEcho(d)}</div>
              {/if}
              {#if setupEcho(d)}
                <!-- The same #1241 line Fleet.svelte carries: what this
                     router reports of the wizard's own logging setup. -->
                <div class="frow dim">{setupEcho(d)}</div>
              {/if}
              <div class="frow dim">syslog{status?.instance.tlsEnabled ? ' TLS' : ''} · state pushed every 20 min</div>
              {#if isAdmin}
                {@render reEnrolButton(d.id, d.name)}
              {/if}
            </div>
          {/each}
          {#each unregisteredRouters as d (d.id)}
            {@const detail = routerDetail[d.id]}
            <div class="fcard unreg">
              <div class="fhead">
                <b>{d.name || d.sourceIp}</b><span class="fstate warn">● PUSHING · UNREGISTERED</span>
              </div>
              <div class="frow">
                {d.routerosVersion ? `RouterOS ${d.routerosVersion}` : 'RouterOS version not yet reported'}
                · pushing since {formatHM(d.firstSeen)} · {ratePerSecond(appState.events, d.id, appState.now)} events/s now
              </div>
              <div class="frow dim">its lines are kept; it has no name and no zones until it is registered</div>
              {#if setupEcho(d)}
                <!-- #1241's line belongs here too: a router discovered by
                     its own push and never declared is the common case,
                     and Fleet.svelte shows this on every card. Leaving it
                     off here hid the one card most likely to need it. -->
                <div class="frow dim">{setupEcho(d)}</div>
              {/if}
              {#if detail?.ruleCount !== null && detail?.ruleCount !== undefined}
                <div class="frow dim">{detail.ruleCount} rule{detail.ruleCount === 1 ? '' : 's'} pushed</div>
              {/if}
              <!-- #1291: enrolling and the ledger's Register step are
                   independent, so the pair says how far the operator
                   actually got. This card is where it has to be said --
                   every router the wizard adds is drawn here, and the
                   config.yaml-declared cards above can never carry a
                   registeredAt at all (the server refuses to register
                   one, since config.yaml rebuilds it every boot).
                   Worded as "the Register step" rather than
                   "registered": on this card that word already means
                   declared in config.yaml, which is the chip in the
                   header, and the two senses must not be read as one. -->
              {#if d.acceptedIp && !d.registeredAt}
                <div class="frow">its logs are accepted, but the Register step was never finished</div>
              {:else if d.registeredAt && !d.acceptedIp}
                <div class="frow dim">
                  the Register step is done; still waiting for its enrolment token to arrive
                </div>
              {/if}
              {#if isAdmin}
                {#if d.acceptedIp && !d.registeredAt}
                  <!-- #1291: this router's only gap is the Register
                       step -- offer to finish that directly rather than
                       sending it through Re-enrol's full mint-a-fresh-
                       token walk for one step it nearly completed. -->
                  {@render finishRegisteringButton(d.id, d.name || d.sourceIp)}
                {/if}
                <!-- Re-enrol… belongs on this card most of all: since
                     #1281 a router earns its place by presenting a
                     token, and a router the ledger declared is not in
                     config.yaml, so every router the wizard itself adds
                     is drawn here rather than above. -->
                {@render reEnrolButton(d.id, d.name || d.sourceIp)}
              {/if}
            </div>
          {/each}
          {#each unattributed as s (s.address)}
            <!-- #1170: a source, not a router. Its own card and its own
                 quiet vocabulary -- deliberately not .unreg, which means
                 a router that pushes without being registered. Nothing
                 here may count it as a router. -->
            <div class="fcard unattr" role="group" aria-label={unattributedLabel(s)}>
              <div class="fhead">
                <b>unattributed · {s.address}</b><span class="fstate quiet">◌ NOT A ROUTER</span>
              </div>
              <div class="frow">syslog from an address no router has claimed</div>
              <div class="frow dim">
                {s.lines} line{s.lines === 1 ? '' : 's'} seen · first seen {formatHM(s.firstSeen)}
              </div>
              {#if s.explanation}
                <!-- Present only where two routers have both pushed this
                     address as their own, so nothing can say which of
                     them sent the lines. -->
                <div class="frow dim">{s.explanation}</div>
              {/if}
              <div class="frow dim">{UNATTRIBUTED_FIX}</div>
            </div>
          {/each}
          {#each refused as r (r.ip)}
            <!-- The refused senders (#1281), in the unattributed card's
                 own quiet vocabulary: an address whose lines were
                 dropped because no router is enrolled at it. There is no
                 accept control here, by ruling -- an address is accepted
                 only by a router presenting a one-time token, so the
                 only ways on are Re-enrol… on a router above and the
                 berth's + add a router. -->
            <div
              class="fcard unattr refused"
              role="group"
              aria-label="refused · {r.ip} — syslog from an address no router is enrolled at"
            >
              <div class="fhead">
                <b>refused · {r.ip}</b><span class="fstate quiet">◌ REFUSED</span>
              </div>
              <div class="frow">syslog from an address no router is enrolled at</div>
              <div class="frow dim">
                {r.lines} line{r.lines === 1 ? '' : 's'} · first seen {formatHM(r.firstSeen)} · last seen {formatHM(r.lastSeen)}
              </div>
              <div class="frow dim">{REFUSED_STRIP_LEAD} Re-enrol the router it belongs to, or add it as a new one.</div>
            </div>
          {/each}
          {#if isAdmin}
          <!-- Adding a router is admin-only: POST /api/devices refuses
               anyone else, and #657's grammar is absent rather than
               disabled, so a user tier does not meet a berth that would
               only fail at the end of the walk. -->
          <div class="fcard berth">
            <!-- #1168: the resting state says what it is. #718 asked for
                 no words at all, on the reading that an empty shape in a
                 row of full cards is affordance enough -- with no routers
                 registered there is no row of full cards, and what the
                 operator met on first run was one blank dashed box. -->
            <button
              type="button"
              class="berth-trigger"
              onclick={openBerth}
              aria-label="Add a router"
            ><span class="berth-label">+ add a router</span></button>
          </div>
          {/if}
        </div>
    </div>

    <!-- The three views: names with their counts, one underlined, over
         one table. No descriptor line under them (round 38). -->
    <div class="eviews" id="eviews" role="group" aria-label="Which names: hosts, rules or ports">
      <button
        class="ev"
        class:on={activeView === 'hosts'}
        data-v="hosts"
        aria-pressed={activeView === 'hosts'}
        onclick={() => (activeView = 'hosts')}>hosts <em>{rows.length}</em></button
      ><button
        class="ev"
        class:on={activeView === 'rules'}
        data-v="rules"
        aria-pressed={activeView === 'rules'}
        onclick={() => (activeView = 'rules')}>rules <em>{ruleRows.length}</em></button
      ><button
        class="ev"
        class:on={activeView === 'ports'}
        data-v="ports"
        aria-pressed={activeView === 'ports'}
        onclick={() => (activeView = 'ports')}>ports <em>{portRows.length}</em></button
      >
    </div>

    {#if activeView === 'hosts'}
      <table class="etable">
        <thead>
          <tr>
            <th>name</th>
            <th>zone</th>
            <th>address</th>
            <th>mac</th>
            <th>first seen</th>
            <th>last seen</th>
            <th>marks</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as row (row.key)}
            <tr class:warn={row.marks.alarmCount > 0}>
              <td class="k">
                {#if isRenaming('host', row.key)}
                  <input
                    class="rename-input"
                    type="text"
                    placeholder="friendly name"
                    bind:value={renameDraft}
                    use:focusOnMount
                    onkeydown={(e) => onRenameKeydown(e, 'host', row.key)}
                    onblur={() => onRenameBlur('host', row.key)}
                    disabled={renameSaving}
                  />
                {:else if canRename}
                  <button
                    type="button"
                    class="rename-btn"
                    class:unnamed={!row.label}
                    onclick={() => startRename('host', row.key, row.label)}
                    title="Click to rename"
                  >
                    {row.label || '— click to name —'}
                  </button>
                {:else}
                  <span class="static-name" class:dim={!row.label}>{row.label || '—'}</span>
                {/if}
              </td>
              <td>
                {#if row.lane}<i class="lz" style="background:{row.lane.ink}"></i>{row.lane.name}{:else}<span class="dim">—</span>{/if}
              </td>
              <!-- #410: the dossier, from the row that names the host.
                   The design puts this on the row's Edit; #675 replaced
                   that Edit with the inline rename in the name cell,
                   which cannot also open a card without taking the
                   rename away, so the door sits on the address instead
                   -- the one token on the row that *is* the host. -->
              <td
                ><button
                  type="button"
                  class="dossier-btn"
                  title="Dossier for {row.key}"
                  onclick={(e) => dossierState.open(row.key, e.currentTarget)}>{row.key}</button
                ></td
              >
              <!-- #1158: an em dash, the marker every other column here
                   uses for a value nothing knows. It read "private"
                   before, which on a row for 1.1.1.1 or 8.8.8.8 looked
                   like a claim about the address rather than a MAC the
                   router never told us. -->
              <td class="dim">{row.mac ? elideMac(row.mac.mac) : '—'}</td>
              <td class="dim">{firstSeenOf(row)}</td>
              <td>{lastSeenOf(row)}</td>
              <td>
                {#if row.marks.newTalker}<span class="mk" style="color:{NEW_TALKER_INK}">▲ new talker</span>{/if}
                {#if row.marks.watched}<span class="mk mk-watched"
                  >◉ watched{row.marks.ringBroken ? ' · ○ ring broken' : ''}</span
                >{/if}
                {#if row.marks.alarmCount > 0}<span class="mk mk-flagged">✱ flagged</span>{/if}
              </td>
            </tr>
            {#if isRenaming('host', row.key) && renameError}
              <tr><td colspan="7" class="rename-error">{renameError}</td></tr>
            {/if}
          {/each}
          {#if rows.length === 0}
            <tr><td colspan="7" class="dim">Nothing seen yet.</td></tr>
          {/if}
        </tbody>
      </table>
    {:else if activeView === 'rules'}
      <table class="etable">
        <thead>
          <tr>
            <th>name</th>
            <th>chain</th>
            <th>action</th>
            <th>last fired</th>
          </tr>
        </thead>
        <tbody>
          {#each ruleRows as row (row.key)}
            <tr>
              <td class="k">
                {#if row.unnameable}
                  <span class="static-name dim">{row.unnameable}</span>
                {:else if isRenaming('rule', row.key)}
                  <input
                    class="rename-input"
                    type="text"
                    placeholder="friendly name"
                    bind:value={renameDraft}
                    use:focusOnMount
                    onkeydown={(e) => onRenameKeydown(e, 'rule', row.key)}
                    onblur={() => onRenameBlur('rule', row.key)}
                    disabled={renameSaving}
                  />
                {:else if canRename}
                  <button type="button" class="rename-btn" onclick={() => startRename('rule', row.key, row.label)} title="Click to rename">
                    {row.label || row.key}
                  </button>
                {:else}
                  <span class="static-name">{row.label || row.key}</span>
                {/if}
              </td>
              <td class="dim">{row.chain ?? '—'}</td>
              <td class="dim">{row.action ?? '—'}</td>
              <td class={row.lastFired ? '' : 'dim'}>
                {row.lastFired ? formatSpacedAge(row.lastFired, appState.now) : 'has not fired'}
              </td>
            </tr>
            {#if isRenaming('rule', row.key) && renameError}
              <tr><td colspan="4" class="rename-error">{renameError}</td></tr>
            {/if}
          {/each}
          {#if ruleRows.length === 0}
            <tr><td colspan="4" class="dim">No router has pushed a rule table yet — once one does, every rule it carries appears here, fired or not.</td></tr>
          {/if}
        </tbody>
      </table>
    {:else}
      <table class="etable">
        <thead>
          <tr>
            <th>name</th>
            <th>port</th>
            <th>last seen</th>
          </tr>
        </thead>
        <tbody>
          {#each portRows as row (row.key)}
            <tr>
              <td class="k">
                {#if isRenaming('port', row.key)}
                  <input
                    class="rename-input"
                    type="text"
                    placeholder="friendly name"
                    bind:value={renameDraft}
                    use:focusOnMount
                    onkeydown={(e) => onRenameKeydown(e, 'port', row.key)}
                    onblur={() => onRenameBlur('port', row.key)}
                    disabled={renameSaving}
                  />
                {:else if canRename}
                  <button type="button" class="rename-btn" onclick={() => startRename('port', row.key, row.label)} title="Click to rename">
                    {row.label || '— · unnamed'}
                  </button>
                {:else}
                  <span class="static-name" class:dim={!row.label}>{row.label || '— · unnamed'}</span>
                {/if}
              </td>
              <td>{row.key}</td>
              <td class={row.lastSeen ? '' : 'dim'}>{row.lastSeen ? formatSpacedAge(row.lastSeen, appState.now) : '—'}</td>
            </tr>
            {#if isRenaming('port', row.key) && renameError}
              <tr><td colspan="3" class="rename-error">{renameError}</td></tr>
            {/if}
          {/each}
          {#if portRows.length === 0}
            <tr><td colspan="3" class="dim">No port has shown up in traffic yet, and none has been named ahead of time.</td></tr>
          {/if}
        </tbody>
      </table>
    {/if}
  </div></div>
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px 24px;
  }

  .op-page .opwrap {
    display: flex;
    justify-content: center;
  }

  .op-page .opanel {
    width: 100%;
    max-width: 1500px;
  }

  /* No border/background here (#718): a bordered panel around a row of
     already-bordered router cards was a box inside a box. The label
     stays -- round 30 still names the row -- the frame around it does
     not. */
  .og {
    margin-bottom: 20px;
  }

  .og h3 {
    margin: 0 0 6px;
    font-size: 10px;
    font-weight: 650;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }

  /* --- the three views (rounds 37-38's `.eviews`): quiet sans names
     carrying their counts, the active one in full ink over an accent
     rule. The same idiom as the metrics view switcher (SceneBar's
     `.sw`), which is the point -- one way of choosing a view of one
     data set, not a second kind of furniture. --- */
  .eviews {
    display: flex;
    gap: 16px;
    align-items: baseline;
    margin: 14px 0 8px;
  }

  .ev {
    background: transparent;
    border: none;
    border-bottom: 1px solid transparent;
    padding: 0 0 2px;
    font: 500 11px var(--font-sans);
    color: var(--fg-dim);
    cursor: pointer;
  }

  .ev:hover {
    color: var(--fg);
  }

  .ev.on {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }

  /* The count rides its name in the tables' own mono, a size down and
     dim, so the name stays the thing being read. */
  .ev em {
    font-style: normal;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--fg-dim);
    margin-left: 3px;
  }

  .dim {
    color: var(--fg-dim);
  }

  /* --- router cards -------------------------------------------------- */
  .fcards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
    gap: 14px;
  }

  .fcard {
    background: var(--glass);
    border: 1px solid var(--border);
    border-radius: 12px;
    padding: 16px 20px;
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .fcard.live {
    border-color: var(--hair-2);
  }

  /* The unregistered router, standing in the berth's slot (rounds
     37-38). Its border is --now -- the time/attention ink, not --alarm:
     a router that pushes without being registered is something to look
     at, not something going wrong, and its lines are being kept either
     way. No new colour is introduced for it. */
  .fcard.unreg {
    border-color: color-mix(in srgb, var(--now) 45%, transparent);
  }

  /* An unattributed source (#1170) is not a router, so it does not wear
     .unreg's --now border: nothing about it is asking to be looked at
     now. It takes the dim ink its own state chip uses (.fstate.quiet),
     dashed like the berth to say the slot is not a real router either.
     No new colour. */
  .fcard.unattr {
    border-style: dashed;
    border-color: color-mix(in srgb, var(--fg-dim) 40%, transparent);
  }

  /* Re-enrol… on a router card (#1284), ported from Fleet.svelte with
     the fields it had there: a quiet outline button, not a primary. */
  .row-action {
    margin-top: 8px;
    align-self: flex-start;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 6px;
    padding: 4px 10px;
    font-size: 11.5px;
    font-weight: 600;
    cursor: pointer;
  }

  .row-action:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  .row-action:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  /* The empty berth (#718): a further grid cell in .fcards, same
     minmax(280px, 1fr) track as a real router card, so with no routers
     at all it alone fills the row -- the correct first-run read. Closed,
     it borrows .fcard's radius and (transparent, dashed) its border
     rather than inventing a second card style; its min-height is a
     plain layout number, not a colour, chosen to read like a real card's
     footprint rather than a sliver when it's the only cell in its row. */
  .fcard.berth {
    position: relative;
    padding: 0;
    min-height: 96px;
    background: transparent;
    border-style: dashed;
    border-color: var(--border);
  }

  .fcard.berth:hover,
  .fcard.berth:focus-within {
    border-color: var(--accent);
  }

  /* The whole card is the trigger (#718's "Design: the add-router
     control", Option 1), now carrying its own resting label (#1168):
     the empty shape reads as an affordance beside full cards, and on
     first run there are none to read it against. */
  .berth-trigger {
    all: unset;
    box-sizing: border-box;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
    min-height: inherit;
    cursor: pointer;
    border-radius: inherit;
  }

  .berth-label {
    color: var(--fg-dim);
    font-size: 13px;
  }

  .fcard.berth:hover .berth-label,
  .fcard.berth:focus-within .berth-label {
    color: var(--accent);
  }

  .berth-trigger:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .fhead {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 8px;
    gap: 10px;
  }

  .fhead b {
    font-size: 15px;
    color: var(--fg);
  }

  .fstate {
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.08em;
    white-space: nowrap;
  }

  .fstate.ok {
    color: var(--accept);
  }

  .fstate.quiet {
    color: var(--fg-dim);
  }

  /* PUSHING · UNREGISTERED: the time/attention ink, not the alarm one
     (see .fcard.unreg). */
  .fstate.warn {
    color: var(--now);
  }

  .fcard .frow {
    padding: 3px 0;
  }

  /* --- the named-things table ------------------------------------------ */
  .etable {
    border-collapse: collapse;
    font-family: var(--font-mono);
    font-size: 12px;
    width: 100%;
  }

  .etable th {
    text-align: left;
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--fg-dim);
    padding: 6px 14px;
    border-bottom: 1px solid var(--hair-2);
  }

  .etable td {
    padding: 8px 14px;
    border-bottom: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .etable tr.warn td {
    background: rgba(255, 84, 112, 0.04);
  }

  .etable td.k {
    color: var(--fg);
  }

  /* A name nobody here can change: the read-only viewer's rows, and any
     tier's view of a rule with no comment on the router. Plain text --
     no hover, no dashed edge, no text cursor -- so it does not look
     clickable, which is the whole of what rounds 37-38 ask of it. The
     row says nothing about why; the account chip already did. */
  .static-name {
    font: inherit;
    color: inherit;
  }

  .rename-btn {
    background: none;
    border: 1px dashed transparent;
    border-radius: 4px;
    padding: 2px 4px;
    margin: -2px -4px;
    font: inherit;
    color: inherit;
    cursor: text;
    text-align: left;
  }

  .rename-btn:hover {
    border-color: var(--border);
    color: var(--accent);
  }

  /* #1152: the "— click to name —" placeholder is one phrase, and at
     1100px wide it broke after "name" and left its closing dash alone on
     a second line. Only the placeholder -- a real host name may be long
     enough to want the wrap. */
  .rename-btn.unnamed {
    white-space: nowrap;
  }

  .dossier-btn {
    padding: 0;
    background: transparent;
    border: none;
    border-bottom: 1px dotted var(--border);
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .dossier-btn:hover {
    color: var(--accent);
    border-bottom-color: var(--accent);
  }

  .rename-input {
    font: inherit;
    background: var(--bg);
    border: 1px solid var(--accent);
    border-radius: 4px;
    padding: 2px 6px;
    color: var(--fg);
    width: 100%;
    min-width: 120px;
  }

  .rename-error {
    color: var(--reject);
    font-family: var(--font-sans, inherit);
    font-size: 12px;
  }

  .lz {
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    margin-right: 7px;
    vertical-align: 0;
  }

  .mk {
    white-space: nowrap;
    margin-right: 8px;
  }

  .mk:last-child {
    margin-right: 0;
  }

  .mk-watched {
    color: var(--marked);
  }

  .mk-flagged {
    color: var(--alarm);
  }
</style>
