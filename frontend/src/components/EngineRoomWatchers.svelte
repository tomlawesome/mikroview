<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The watchers station, opened (#490) -- the detector bench. Successor
  // to the former Detectors.svelte page (see git history): same data
  // (detectorSettingsState, lib/detectorCopy.ts's hand-written copy) and
  // the same on/off behaviour, reshaped into one row per detector rather
  // than a card grid, since it now unfolds inside the station instead of
  // occupying a whole page.
  //
  // Mounted only while the watchers station is the one open (see
  // EngineRoom.svelte) -- there is no standalone route for this any
  // more.
  //
  // #787 turned the row into a full editor. Before it, the only thing
  // editable anywhere in the UI was a detector's *scope*, typed as
  // comma-separated text; a detector whose threshold or window was wrong
  // for this network could not be corrected at all. A row now expands
  // downward in place (decision B on #787 -- one open at a time, no side
  // drawer, the bench stays a bench) into:
  //
  //   - typed tuning fields built from GET /api/definitions/schema, the
  //     server's own declaration of each param's type, bounds and unit.
  //     Not from the paramSchema copy riding on the row: one source, so
  //     a control cannot disagree with the validation behind it.
  //   - scope as removable chips with an add box that suggests what the
  //     app already knows -- hosts from Entities, rule labels from the
  //     router-pushed filter tables.
  //   - reset, clone, save and cancel at the foot.
  //
  // #786 added Try beside Save: the candidate numbers as typed are
  // replayed over the traffic mikroview still holds, and the answer --
  // a receipt or an honest decline -- lands in one slot under the
  // fields. Changing a threshold used to be a guess with no shown
  // workings; the receipt is what turns it into a checked change.
  //
  // #640's expectations ledger -- the station's second section, under
  // the bench. Its own component, so this file stays the bench.
  //
  // #829 gave a custom detector its own drawer, drawn as settings round 8
  // (docs/design/screens/settings/round-8/): the family picker whose ink
  // runs through everything below it, the conditions bar, the counting
  // line, the says line and the receipt. A shipped row keeps the #787
  // panel above -- the design draws a shipped detector's drawer nowhere,
  // because there is nothing structural in one to draw.
  import ExpectationsLedger from './ExpectationsLedger.svelte'
  import ConditionsBar from './ConditionsBar.svelte'
  import { detectorSettingsState } from '../lib/detectorSettings.svelte'
  import { FAMILY_NAMES, NAMED_FAMILIES, familyOf } from '../lib/flagPalette'
  import {
    DISTINCT_FIELDS,
    KEY_MODES,
    countingPhrase,
    goDuration,
    keyPhrase,
    placeholdersFor,
    saysParts,
    windowSeconds,
  } from '../lib/detectionCopy'
  import type { DefinitionCondition, DefinitionDetection, DetectorScope } from '../lib/types'
  import { scopeSuggestionsState, CLASSIFICATIONS } from '../lib/scopeSuggestions.svelte'
  import { appState } from '../lib/state.svelte'
  import {
    DETECTORS,
    SCOPE_FIELDS,
    learningSummary,
    scopeSummary,
  } from '../lib/detectorCopy'
  import {
    addChip,
    cloneName,
    paramFields,
    parsePortEntry,
    paramsFromFields,
    removeChip,
    scopeDraftFrom,
    scopeFromDraft,
    type ChipAxis,
    type ParamField,
    type ScopeDraft,
  } from '../lib/definitionEditor'
  import { formatDurationShort, parseGoDurationSeconds } from '../lib/format'
  import type { ReplayReceipt, ReplayResult } from '../lib/types'

  // canEdit (#653's middle tier): running the detector bench -- enabling
  // or pausing a detector, editing its scope, tuning its thresholds -- is
  // a normal operational action open to user and admin alike, not an
  // owner-level one. A viewer gets the same read-only facts admin/user
  // see, just no checkbox, no expander and no panel (hide, never
  // disable) -- the same grammar the run/pause tick already used.
  let { canEdit }: { canEdit: boolean } = $props()

  // Which detector's panel is open, if any. One at a time by design
  // (#787 decision B): the bench is a list of rows to compare, and two
  // open panels turn it into two forms stacked on top of each other.
  let openRow = $state<string | null>(null)

  // The open panel's working copy. Held as one object rather than a map
  // keyed by detector, because only one panel exists at a time and a map
  // would keep stale drafts alive for rows nobody is editing.
  let fields = $state<ParamField[]>([])
  let scope = $state<ScopeDraft>(scopeDraftFrom(undefined))
  let draftName = $state('')
  // What is half-typed in each add box, and what the last add refused.
  let adding = $state<Record<ChipAxis, string>>({ hosts: '', ports: '', rules: '' })
  let addError = $state<Partial<Record<ChipAxis, string>>>({})

  let errors = $state<Partial<Record<string, string>>>({})
  let saving = $state<Partial<Record<string, boolean>>>({})
  let busy = $state(false)

  // What the last Try answered for the open panel, and whether one is in
  // flight (#786). One value rather than a map keyed by detector, for the
  // same reason the draft above is one object: only one panel exists at a
  // time, and a map would keep a stale receipt alive for a row nobody is
  // looking at -- a receipt read against the wrong row's numbers is worse
  // than no receipt.
  //
  // Deliberately outside `busy`: Try never blocks Save. Pressing it
  // disables Try alone, so the numbers as typed can still be saved while
  // a replay is in flight, or after one declined.
  let replay = $state<ReplayResult | null>(null)
  let trying = $state(false)

  // Set when a row is opened by cloning, so the copy's name field takes
  // focus the moment it renders (#787 decision C). Cleared once used, so
  // reopening the same row later does not steal focus again.
  let focusName = $state(false)
  let nameInput = $state<HTMLInputElement | null>(null)

  // --- the custom detector's drawer (#829) --------------------------------

  // The open panel's working copy of the things only a custom detector
  // has: what it matches on, how it counts, what its flags say, and which
  // family it is filed under. Held beside the params draft above rather
  // than inside it, because they are saved through different doors -- the
  // structure through PUT's detection block, the numbers through its
  // params -- and mixing them would make one refusal look like the other.
  let draftConditions = $state<DefinitionCondition[]>([])
  // The mounted bar's instance, so Save can ask it to commit a
  // still-typed token before reading `draftConditions` back out (#1075).
  // Only one of the two ConditionsBar tags below is ever mounted at a
  // time: openPanel() discards a pending draft the same way
  // startDraftFrom() closes an open row's panel, so the two never
  // coexist and one ref is always the right one.
  let conditionsBarRef = $state<ReturnType<typeof ConditionsBar> | null>(null)
  let draftKey = $state('perSource')
  let draftCounting = $state('total')
  let draftDistinct = $state('destinationAddress')
  let draftTemplate = $state('')
  let draftFamily = $state('')
  // Set while a half-written condition is in the bar -- the sentence
  // beside the greyed try and save pills, naming the line to finish.
  let unfinishedLine = $state('')
  // Which of the counting line's numbers is being typed into, if any. The
  // numbers are the bench's own dashed click-to-edit, not form fields.
  let editingCount = $state<'threshold' | 'window' | null>(null)
  let editingSays = $state(false)
  // Where the copy came from, for the one dim mono line the drawer shows
  // after a clone. Cleared when the panel is opened any other way, so it
  // never claims a provenance for a detector that was simply opened.
  let copiedFrom = $state<{ name: string; carried: string } | null>(null)

  // The ink the whole drawer wears: the chosen family's, or -- for a
  // detector nobody has filed -- whatever the palette already gives this
  // detector, which is the operator-authored accent.
  function drawerInk(name: string): string {
    const fam = draftFamily ? NAMED_FAMILIES[draftFamily] : undefined
    return (fam ?? familyOf(name)).ink
  }

  function drawerMark(name: string): string {
    const fam = draftFamily ? NAMED_FAMILIES[draftFamily] : undefined
    return (fam ?? familyOf(name)).mark
  }

  // The threshold and window as the counting line reads them. Both are
  // ordinary params behind the same schema every detector's numbers use,
  // so they are read out of the params draft rather than held twice.
  function numberField(name: string): ParamField | undefined {
    return fields.find((f) => f.schema.name === name)
  }

  function thresholdValue(): number {
    const raw = numberField('threshold')?.value
    const n = Number(raw)
    return Number.isFinite(n) && n > 0 ? n : 1
  }

  function windowValue(): number {
    const f = numberField('window')
    if (!f) return 60
    const n = Number(f.value)
    if (Number.isFinite(n) && n > 0) return n
    return windowSeconds(String(f.value ?? ''), 60)
  }

  function setNumber(name: string, next: number) {
    const f = numberField(name)
    if (!f) return
    f.value = name === 'window' && f.control !== 'seconds' ? goDuration(next) : next
  }

  // A placeholder click inserts at the end of the sentence rather than at
  // a caret position: the says line is one short line, and tracking a
  // caret through a swap between rendered text and an input would be a
  // lot of machinery for a token that is nearly always appended anyway.
  function insertPlaceholder(token: string) {
    draftTemplate = draftTemplate.trimEnd() + (draftTemplate.trim() === '' ? '' : ' ') + token
    editingSays = true
  }

  // The structure as it would be saved, or null when the drawer has
  // nothing to save -- an unfinished line in the bar, or no conditions at
  // all, which the engine refuses because a detector matching nothing
  // meaningfully is almost certainly a mistake rather than a valid
  // "watch everything".
  function draftStructure(): DefinitionDetection | null {
    if (unfinishedLine !== '') return null
    if (draftConditions.length === 0) return null
    if (draftTemplate.trim() === '') return null
    return {
      conditions: draftConditions,
      key: draftKey,
      counting: draftCounting,
      ...(draftCounting === 'distinct' ? { distinctField: draftDistinct } : {}),
      detailTemplate: draftTemplate,
    }
  }

  // What the drawer says instead of letting Save be pressed. One line,
  // amber, beside the waiting pills -- never a disabled button with no
  // explanation, and never red, because a half-written detector is a line
  // someone is in the middle of rather than a mistake they have made.
  const waiting = $derived.by(() => {
    if (unfinishedLine !== '') return unfinishedLine
    if (draftConditions.length === 0) return 'add a condition'
    if (draftTemplate.trim() === '') return 'write what a flag says'
    return ''
  })

  $effect(() => {
    if (focusName && nameInput) {
      nameInput.focus()
      nameInput.select()
      focusName = false
    }
  })

  // The schema and the add boxes' suggestions are fetched once the bench
  // is mounted for someone who can edit. A viewer gets neither: the
  // schema endpoint and Entities are both user-tier server-side, so
  // asking would only produce two 403s for a surface with no controls on
  // it.
  $effect(() => {
    if (!canEdit) return
    void detectorSettingsState.refreshSchema()
    void scopeSuggestionsState.refresh(appState.devices)
  })

  function schemaFor(name: string) {
    return detectorSettingsState.schema[name] ?? []
  }

  // A row can be opened before the schema has arrived -- clicking one the
  // instant the bench appears is entirely ordinary -- and the panel would
  // then sit there with no tuning group for a definition that does
  // declare params. Fill it in when the schema lands. Only ever when the
  // panel has no fields yet, so this can never overwrite something the
  // operator has already typed.
  $effect(() => {
    const name = openRow
    if (!name || fields.length > 0) return
    const declared = detectorSettingsState.schema[name] ?? []
    if (declared.length === 0) return
    const d = detectorSettingsState.list.find((x) => x.name === name)
    fields = paramFields(declared, d?.params)
  })

  function openPanel(name: string, from: { name: string; carried: string } | null = null) {
    const d = detectorSettingsState.list.find((x) => x.name === name)
    if (!d) return
    // The reverse of startDraftFrom()'s closePanel(): a row panel and an
    // in-progress clone-draft must never both be mounted, or the one
    // conditionsBarRef above would point at whichever mounted last while
    // draftConditions and friends still belonged to the other (#1075).
    discardDraft()
    openRow = name
    draftName = d.label
    fields = paramFields(schemaFor(name), d.params)
    scope = scopeDraftFrom(d.scope)
    adding = { hosts: '', ports: '', rules: '' }
    addError = {}
    // The structure half (#829). A shipped detector has none -- its
    // structure is Go in this binary -- so the drawer's conditions bar,
    // counting line and says line are simply absent on those rows rather
    // than present and empty.
    const det = d.detection
    draftConditions = det ? det.conditions.map((c) => ({ ...c, values: [...c.values] })) : []
    draftKey = det?.key ?? 'perSource'
    draftCounting = det?.counting ?? 'total'
    draftDistinct = det?.distinctField ?? 'destinationAddress'
    draftTemplate = det?.detailTemplate ?? ''
    draftFamily = d.family ?? ''
    unfinishedLine = ''
    editingCount = null
    editingSays = false
    copiedFrom = from
    // A receipt is an answer about the numbers that were in the fields
    // when Try was pressed, so it cannot outlive them -- reopening a
    // panel, or opening a different row, starts with an empty slot rather
    // than a receipt the operator would reasonably read as being about
    // what is now on screen.
    replay = null
  }

  function closePanel() {
    openRow = null
    replay = null
    copiedFrom = null
  }

  function togglePanel(name: string) {
    if (openRow === name) {
      closePanel()
      return
    }
    openPanel(name)
  }

  async function toggleEnabled(name: string, enabled: boolean) {
    const d = detectorSettingsState.list.find((x) => x.name === name)
    saving[name] = true
    const err = await detectorSettingsState.update(name, enabled, d?.scope ?? {})
    saving[name] = false
    errors[name] = err ?? undefined
  }

  // --- chips -----------------------------------------------------------

  // A port entry may be a range ("8000-8010"), which expands into one
  // chip per port -- DetectorScope.ports is a number list on the wire, so
  // there is nowhere to store a range the engine would read back. Hosts
  // and rules are taken as typed: a CIDR and a rule label are already
  // single values.
  function commitChip(axis: ChipAxis) {
    const typed = adding[axis]
    if (typed.trim().length === 0) return
    if (axis === 'ports') {
      const { ports, error } = parsePortEntry(typed)
      if (error) {
        addError[axis] = error
        return
      }
      let next = scope.ports
      for (const p of ports) next = addChip(next, String(p))
      scope.ports = next
    } else {
      scope[axis] = addChip(scope[axis], typed)
    }
    adding[axis] = ''
    addError[axis] = undefined
  }

  function onAddKey(e: KeyboardEvent, axis: ChipAxis) {
    if (e.key !== 'Enter') return
    // The panel is not a <form>, but Enter inside a text input still
    // reads as "add this", and letting it bubble would look like a page
    // action to anyone driving by keyboard.
    e.preventDefault()
    commitChip(axis)
  }

  function dropChip(axis: ChipAxis, value: string) {
    scope[axis] = removeChip(scope[axis], value)
  }

  // --- try: the candidate numbers, replayed (#786) ------------------------

  // Which of the panel's fields a replay candidate may carry. The engine's
  // replay candidate is a closed set of two params -- window and threshold
  // (engine's replayParamSchema, internal/engine/replay_declarative.go) --
  // and the server refuses any name outside it outright, so sending the
  // whole panel would turn Try into a guaranteed refusal for every
  // detector that declares a third param. Nothing serves that set to the
  // client the way GET /api/definitions/schema serves the full one, which
  // is why it is named here; recorded as a gap on #786 rather than left
  // as a silent constant.
  const REPLAYABLE_PARAMS = ['window', 'threshold']

  // Try replays the candidate numbers as typed over the traffic still
  // held, and puts the answer in the slot under the fields. It writes
  // nothing -- the definition the engine is evaluating is exactly as it
  // was, whether the receipt is encouraging or not.
  //
  // A decline is not an error and is not stored as one: it goes into the
  // same slot as a receipt, in the panel, in the quiet ink. Only a
  // refusal -- no corpus, a definition this binary cannot replay at all
  // -- reaches the row's error line, in the server's own words, the way
  // the clone refusal already does.
  async function tryRow(name: string) {
    trying = true
    replay = null
    const candidate = paramsFromFields(
      fields.filter((f) => REPLAYABLE_PARAMS.includes(f.schema.name)),
    )
    const result = await detectorSettingsState.replay(name, candidate)
    trying = false
    if (typeof result === 'string') {
      errors[name] = result
      return
    }
    errors[name] = undefined
    replay = result
  }

  // A receipt's sample holds one entry per emission, so a host that would
  // have been flagged three times appears three times. The slot names the
  // hosts that would have been flagged, not how many entries the sample
  // has, so each one is listed once.
  // Where each firing sits along the strip, in the SVG's own 0-260
  // coordinates. Placed by when it happened within the window the receipt
  // covers, not spread evenly: two firings a minute apart look like two
  // firings a minute apart, which is the whole reason the strip is a
  // strip rather than a number.
  //
  // A window with no span to speak of, or a sample the server bounded,
  // still draws its marks -- clustered at the left rather than dividing
  // by zero. The count above the strip is the exact figure; the strip is
  // about shape.
  function tickPositions(receipt: ReplayReceipt): number[] {
    const from = Date.parse(receipt.window.start)
    const to = Date.parse(receipt.window.end)
    const span = to - from
    return receipt.sample.map((entry) => {
      const at = Date.parse(entry.at)
      if (!Number.isFinite(span) || span <= 0 || !Number.isFinite(at)) return 4
      const frac = Math.min(1, Math.max(0, (at - from) / span))
      return 4 + frac * 252
    })
  }

  function flaggedHosts(receipt: ReplayReceipt): string[] {
    return [...new Set(receipt.sample.map((s) => s.target))]
  }

  // Durations arrive as Go duration strings ("4h12m0s") throughout the
  // definitions surface; this is the same read-then-render pair the
  // panel's own duration fields use, so a replay and a window field never
  // say the same length two different ways.
  function asDuration(goDuration: string): string {
    return formatDurationShort(parseGoDurationSeconds(goDuration))
  }

  // --- the foot's actions -------------------------------------------------

  async function save(name: string) {
    const d = detectorSettingsState.list.find((x) => x.name === name)
    // A token still only typed -- never finished with Enter or a list
    // pick -- commits itself here, the same path Enter takes, so Save
    // never sends the structure without it (#1075). One that still fails
    // to parse is left for the bar's own amber line to name; commitPending
    // says so, and nothing is sent.
    if (d?.detection && conditionsBarRef && !conditionsBarRef.commitPending()) return
    busy = true
    saving[name] = true
    // A shipped definition's name is a property of the binary that ships
    // its logic and the server refuses to change it, so the name is sent
    // only for a custom definition -- and only when it actually changed,
    // since an unchanged name is not an edit worth risking a refusal on.
    const renameable = d?.origin === 'custom'
    // The structure goes up in the same PUT as the numbers and the scope,
    // because they are one Save press and one detector (#787's own
    // reasoning for the single request). Sent only where there is a whole
    // structure to send: a half-written condition never reaches the
    // server, because the pills that would send it are already waiting.
    const structure = d?.detection ? draftStructure() : null
    const err = await detectorSettingsState.edit(name, {
      ...(renameable && draftName !== d?.label ? { name: draftName } : {}),
      ...(fields.length > 0 ? { params: paramsFromFields(fields) } : {}),
      ...(structure ? { detection: structure } : {}),
      ...(d?.origin === 'custom' && draftFamily !== (d?.family ?? '') ? { family: draftFamily } : {}),
      scope: scopeFromDraft(scope),
    })
    saving[name] = false
    busy = false
    errors[name] = err ?? undefined
    if (!err) closePanel()
  }

  // Reset puts the detector's params back to exactly what it shipped
  // with. It leaves scope alone, because the server's reset does
  // (handleDefinitionsReset resets params only) -- a button that also
  // silently cleared an operator's host exclusions would be doing
  // something nobody pressed it for. The panel stays open on the freshly
  // stock values, so the operator can see what reset actually did.
  async function resetRow(name: string) {
    busy = true
    saving[name] = true
    const err = await detectorSettingsState.reset(name)
    saving[name] = false
    busy = false
    errors[name] = err ?? undefined
    if (!err) openPanel(name)
  }

  // Clone, with no prompt between the press and the work (#787 decision
  // C): the copy is created, paused so a half-edited detector never runs,
  // and its own panel opens with the name selected.
  //
  // The pause is the server's (#810): POST .../clone stores a custom
  // detection's copy disabled, so there is no second request here that
  // could fail on its own and leave a running duplicate behind.
  //
  // The button only appears on a custom row, so a refusal is no longer
  // the expected outcome -- but one is still shown verbatim if the
  // server ever declines, rather than reworded into something that says
  // less than the reason it gave.
  async function cloneRow(name: string) {
    const d = detectorSettingsState.list.find((x) => x.name === name)
    if (!d) return
    // Two clones, and which one this is depends on whether the original's
    // matching is data or Go. A custom detector or a shipped declarative
    // one has conditions the server can copy; a shipped code detector has
    // none, and its copy is written here instead (#829) -- carrying the
    // scope and the numbers, with the bar left for the operator.
    if (d.origin !== 'custom' && !d.structure) {
      startDraftFrom(d)
      return
    }
    busy = true
    saving[name] = true
    const result = await detectorSettingsState.clone(name, cloneName(d.label))
    saving[name] = false
    busy = false
    if (typeof result === 'string') {
      errors[name] = result
      return
    }
    errors[name] = undefined
    openPanel(result.id, { name: d.label, carried: 'conditions, scope and numbers · paused' })
    focusName = true
  }

  // --- the draft copy of a code detector (#829) ---------------------------

  // A detector being written rather than edited. It exists only in this
  // component until the first Save: the server refuses to store a
  // detection with no conditions -- a detector matching nothing
  // meaningfully is almost certainly a mistake -- so a copy that arrives
  // with an empty bar cannot be a stored definition yet.
  let draft = $state<{
    fromLabel: string
    label: string
    scope: DetectorScope
    threshold: number
    window: string
  } | null>(null)

  function startDraftFrom(d: (typeof detectorSettingsState.list)[number]) {
    closePanel()
    draft = {
      fromLabel: d.label,
      label: cloneName(d.label),
      scope: d.scope ?? {},
      threshold: Number(d.params?.threshold ?? 5) || 5,
      window: String(d.params?.window ?? '60s'),
    }
    draftName = draft.label
    scope = scopeDraftFrom(draft.scope)
    adding = { hosts: '', ports: '', rules: '' }
    addError = {}
    fields = []
    draftConditions = []
    draftKey = 'perSource'
    draftCounting = 'total'
    draftDistinct = 'destinationAddress'
    draftTemplate = '{Count} matching events from {SourceAddress}'
    draftFamily = ''
    draftThreshold = draft.threshold
    draftWindow = windowSeconds(draft.window, 60)
    draftError = ''
    unfinishedLine = ''
    editingCount = null
    editingSays = false
    replay = null
    copiedFrom = {
      name: d.label,
      // Round 7's own words. A copy that quietly arrived with an empty
      // bar would look like a clone that half-failed; this says what came
      // across and what is being asked for.
      carried: 'scope and numbers · its conditions are built in — write them here',
    }
    focusName = true
  }

  function discardDraft() {
    draft = null
    copiedFrom = null
    replay = null
  }

  async function saveDraft() {
    if (!draft) return
    // See save()'s own comment (#1075): commit a still-typed token before
    // reading the conditions back out, rather than dropping it.
    if (conditionsBarRef && !conditionsBarRef.commitPending()) return
    const structure = draftStructure()
    if (!structure) return
    busy = true
    const result = await detectorSettingsState.create({
      name: draftName.trim() || draft.label,
      family: draftFamily,
      detection: {
        ...structure,
        threshold: draftThreshold,
        window: goDuration(draftWindow),
      },
    })
    busy = false
    if (typeof result === 'string') {
      draftError = result
      return
    }
    draftError = ''
    const made = result.id
    draft = null
    // The new detector's scope is not part of creation -- the create
    // endpoint takes structure and numbers only -- so it goes up as the
    // ordinary edit it is, immediately after, and its failure is reported
    // on the row it belongs to rather than on a draft that no longer
    // exists.
    const scopeErr = await detectorSettingsState.edit(made, { scope: scopeFromDraft(scope) })
    errors[made] = scopeErr ?? undefined
    openPanel(made)
  }

  // A draft's own numbers, which have no param schema behind them until
  // the detector exists. Kept apart from `fields` for exactly that
  // reason: paramFields builds from a schema, and there is not one yet.
  let draftThreshold = $state(5)
  let draftWindow = $state(60)
  let draftError = $state('')

  // Remove, offered only on a custom row: a shipped definition is never
  // deleted, only paused (the engine's own invariant), so the server
  // refuses one and a button whose only outcome is that refusal would be
  // worse than no button.
  async function removeRow(name: string) {
    busy = true
    saving[name] = true
    const err = await detectorSettingsState.remove(name)
    saving[name] = false
    busy = false
    if (err) {
      errors[name] = err
      return
    }
    errors[name] = undefined
    closePanel()
  }

</script>

{#snippet scopeGroup(fieldsFor: string[])}
          <div class="group">
            <h4>What it watches</h4>

            {#if fieldsFor.includes('hosts')}
              <div class="field">
                <span>Hosts</span>
                <div class="field-row">
                  <select bind:value={scope.hostsMode} aria-label="Hosts restriction">
                    <option value="">no restriction</option>
                    <option value="allow">allow only</option>
                    <option value="deny">deny</option>
                  </select>
                  <div class="chipbox">
                    {#each scope.hosts as h (h)}
                      <span class="chip">
                        {h}
                        <button
                          type="button"
                          class="chip-x"
                          aria-label="remove host {h}"
                          onclick={() => dropChip('hosts', h)}>×</button
                        >
                      </span>
                    {/each}
                    <input
                      type="text"
                      class="chip-add"
                      list="watchers-hosts"
                      aria-label="add a host"
                      placeholder="192.168.1.50 or 203.0.113.0/24"
                      bind:value={adding.hosts}
                      onkeydown={(e) => onAddKey(e, 'hosts')}
                    />
                    <button type="button" class="chip-plus" onclick={() => commitChip('hosts')}
                      >add</button
                    >
                  </div>
                </div>
                {#if addError.hosts}<span class="adderr">{addError.hosts}</span>{/if}
              </div>
            {/if}

            {#if fieldsFor.includes('classification')}
              <!-- One value, not a list (internal/store.Scope), so this
                   stays the select it has always been rather than
                   becoming a chip row that could only ever hold one
                   chip. The fixed set is the select's own options. -->
              <label class="field">
                <span>Source classification</span>
                <select bind:value={scope.classification}>
                  <option value="">any</option>
                  {#each CLASSIFICATIONS as c (c)}
                    <option value={c}>{c} only</option>
                  {/each}
                </select>
              </label>
            {/if}

            {#if fieldsFor.includes('ports')}
              <div class="field">
                <span>Ports</span>
                <div class="field-row">
                  <select bind:value={scope.portsMode} aria-label="Ports restriction">
                    <option value="">no restriction</option>
                    <option value="allow">allow only</option>
                    <option value="deny">deny</option>
                  </select>
                  <div class="chipbox">
                    {#each scope.ports as p (p)}
                      <span class="chip">
                        {p}
                        <button
                          type="button"
                          class="chip-x"
                          aria-label="remove port {p}"
                          onclick={() => dropChip('ports', p)}>×</button
                        >
                      </span>
                    {/each}
                    <input
                      type="text"
                      class="chip-add"
                      inputmode="numeric"
                      aria-label="add a port"
                      placeholder="22, or a range like 8000-8010"
                      bind:value={adding.ports}
                      onkeydown={(e) => onAddKey(e, 'ports')}
                    />
                    <button type="button" class="chip-plus" onclick={() => commitChip('ports')}
                      >add</button
                    >
                  </div>
                </div>
                {#if addError.ports}<span class="adderr">{addError.ports}</span>{/if}
              </div>
            {/if}

            {#if fieldsFor.includes('rules')}
              <div class="field">
                <span>Rule labels</span>
                <div class="field-row">
                  <select bind:value={scope.rulesMode} aria-label="Rules restriction">
                    <option value="">no restriction</option>
                    <option value="allow">allow only</option>
                    <option value="deny">deny</option>
                  </select>
                  <div class="chipbox">
                    {#each scope.rules as r (r)}
                      <span class="chip">
                        {r}
                        <button
                          type="button"
                          class="chip-x"
                          aria-label="remove rule {r}"
                          onclick={() => dropChip('rules', r)}>×</button
                        >
                      </span>
                    {/each}
                    <input
                      type="text"
                      class="chip-add"
                      list="watchers-rules"
                      aria-label="add a rule label"
                      placeholder="r13"
                      bind:value={adding.rules}
                      onkeydown={(e) => onAddKey(e, 'rules')}
                    />
                    <button type="button" class="chip-plus" onclick={() => commitChip('rules')}
                      >add</button
                    >
                  </div>
                </div>
                {#if addError.rules}<span class="adderr">{addError.rules}</span>{/if}
              </div>
            {/if}
          </div>
{/snippet}

{#if canEdit && draft}
  <!-- A detector being written rather than edited (#829). It exists only
       here until the first Save: a shipped code detector carries no
       conditions to copy, and the server refuses to store a detection
       with none, so the copy is written in the drawer and created once it
       is a whole detector. Drawn exactly as a stored one's drawer,
       because it is the same drawer -- what is different is only that
       nothing has been created yet, which the copied-from line says. -->
  <div class="row open draft">
    <div class="line" style="--ft: {drawerInk('')}">
      <span class="name">{draftName || draft.label}</span>
      <span class="dash">—</span>
      <span class="scope-fact">{scopeSummary(scopeFromDraft(scope))}</span>
      <span class="state paused"><span class="dot"></span>not created yet</span>
    </div>
    {#if draftError}
      <p class="error">{draftError}</p>
    {/if}
    <div class="drawer" style="--ft: {drawerInk('')}">
      {#if copiedFrom}
        <p class="copied">copied from <em>{copiedFrom.name}</em> · {copiedFrom.carried}</p>
      {/if}

      <div class="famline">
        <div class="famtop">
          <span class="famname"><i>{drawerMark('')}</i>{draftName || draft.label}</span>
          <div class="picker" role="group" aria-label="file this detector under a flag family">
            {#each FAMILY_NAMES as fam (fam)}
              <button
                type="button"
                class:on={draftFamily === fam}
                style="--pk: {NAMED_FAMILIES[fam].ink}"
                aria-label={fam}
                aria-pressed={draftFamily === fam}
                onclick={() => (draftFamily = draftFamily === fam ? '' : fam)}
                >{NAMED_FAMILIES[fam].mark}</button
              >
            {/each}
          </div>
        </div>
        <span class="btbar"></span>
        <span class="lab famcap">
          {#if draftFamily}
            filed as · <em>{draftFamily}</em> — its flags wear this ink on the docket, the fall and
            the map
          {:else}
            not filed yet · its flags wear the operator's accent
          {/if}
        </span>
      </div>

      <ConditionsBar bind:this={conditionsBarRef} bind:conditions={draftConditions} bind:unfinishedLine />

      <p class="stat">
        <input
          class="kin"
          type="number"
          min="1"
          bind:value={draftThreshold}
          aria-label="how many before it fires"
        />
        <select class="k sel" bind:value={draftCounting} aria-label="what it counts">
          <option value="total">matching events</option>
          <option value="distinct">distinct…</option>
        </select>
        {#if draftCounting === 'distinct'}
          <select class="k sel" bind:value={draftDistinct} aria-label="what it counts distinctly">
            {#each DISTINCT_FIELDS as f (f)}
              <option value={f}>{countingPhrase('distinct', f).replace('distinct ', '')}</option>
            {/each}
          </select>
        {/if}
        <span class="mid">·</span> per
        <select class="k sel" bind:value={draftKey} aria-label="what the window is counted per">
          {#each KEY_MODES as k (k)}
            <option value={k}>{keyPhrase(k)}</option>
          {/each}
        </select>
        <span class="mid">·</span> within
        <input
          class="kin"
          type="number"
          min="1"
          bind:value={draftWindow}
          aria-label="the window it counts over, in seconds"
        />
        <span class="u">s</span>
      </p>

      <p class="says">
        <span class="lab">says</span>
        <input
          class="saysin"
          type="text"
          bind:value={draftTemplate}
          aria-label="what a raised flag says"
        />
        <span class="phoffer">
          {#each placeholdersFor(draftKey) as token (token)}
            <button
              type="button"
              class="ph add"
              onclick={() => insertPlaceholder(token)}
              aria-label="add {token} to what a flag says">{token}</button
            >
          {/each}
        </span>
      </p>

      <!-- Try is absent here, not disabled: a replay asks the server what
           a *stored* definition would have done, and this one is not
           stored yet. The app's own rule is absent rather than disabled,
           so the pill is simply not drawn until Save has made it a
           detector -- at which point its drawer has one. -->
      {@render scopeGroup(['hosts', 'classification', 'ports', 'rules'])}

      <div class="acts">
        <button
          type="button"
          class="p"
          class:wait={waiting !== ''}
          class:live={waiting === ''}
          disabled={busy || waiting !== ''}
          onclick={saveDraft}><i>✓</i>{busy ? 'saving…' : 'save'}</button
        >
        {#if waiting}
          <span class="waiting">{waiting}</span>
        {/if}
        <span class="mid">·</span>
        <button type="button" class="olink quiet" disabled={busy} onclick={discardDraft}>
          discard
        </button>
      </div>

      <label class="field rename">
        <span class="lab">name</span>
        <input type="text" bind:this={nameInput} bind:value={draftName} />
      </label>
    </div>
  </div>
{/if}

<ul class="bench">
  {#each detectorSettingsState.list as d (d.name)}
    <!-- Only what this bench renders. DetectorInfo.explanation -- 16
         hand-written sentences saying what each detector actually
         watches -- is deliberately not computed here: nothing on any
         surface renders it, and synthesising it from the server's
         description built a value that went nowhere. The copy stays in
         detectorCopy.ts; that it has no home is a gap on #691, and the
         module's own comment says why it matters: "a detector that is
         evaluating but invisible on the station that exists to say what
         is being watched is the worst failure the bench can have". -->
    {@const copy = DETECTORS[d.name]}
    {@const label = copy?.label ?? d.label}
    {@const fieldsFor = SCOPE_FIELDS[d.name] ?? []}
    {@const learning = learningSummary(d.learning)}
    {@const open = openRow === d.name}
    <li class="row" class:open>
      <div class="line">
        {#if canEdit}
          <input
            type="checkbox"
            class="cbx"
            aria-label="{label} runs"
            checked={d.enabled}
            disabled={saving[d.name]}
            onchange={(e) => toggleEnabled(d.name, e.currentTarget.checked)}
          />
        {/if}
        {#if canEdit}
          <!-- The row itself is the expander (#787 decision B). It keeps
               the scope knob's ink -- a dashed underline under a value,
               not a button -- because that is what the station already
               taught an operator to look for, and the thing it opens is
               a superset of what the knob opened. -->
          <button
            type="button"
            class="row-knob"
            aria-expanded={open}
            onclick={() => togglePanel(d.name)}
          >
            <span class="name">{label}</span>
            <span class="id">{d.name}</span>
            <span class="dash">—</span>
            <span class="scope-fact">{scopeSummary(d.scope)}</span>
          </button>
        {:else}
          <span class="name">{label}</span>
          <span class="id">{d.name}</span>
          <span class="dash">—</span>
          <span class="scope-fact">{scopeSummary(d.scope)}</span>
        {/if}
        {#if d.overridden}
          <!-- Stated in words, not by colour: this row's numbers are not
               the ones it shipped with, which is a fact worth reading
               without opening the panel. -->
          <span class="tuned">tuned</span>
        {/if}
        <span class="state" class:paused={!d.enabled}>
          <span class="dot"></span>
          {saving[d.name] ? 'saving…' : d.enabled ? 'running' : 'paused'}
        </span>
      </div>

      {#if learning}
        <p class="learning">{learning}</p>
      {/if}

      {#if errors[d.name]}
        <p class="error">{errors[d.name]}</p>
      {/if}

      {#if canEdit && open && d.detection}
        <!-- The custom detector's drawer, settings round 8. One unbroken
             family stripe from the row into the drawer, the way a flag
             opens on the docket, and every ink below it is the chosen
             family's. -->
        <div class="drawer" style="--ft: {drawerInk(d.name)}">
          {#if copiedFrom}
            <p class="copied">copied from <em>{copiedFrom.name}</em> · {copiedFrom.carried}</p>
          {/if}

          <div class="famline">
            <div class="famtop">
              <span class="famname"><i>{drawerMark(d.name)}</i>{draftName || label}</span>
              <div class="picker" role="group" aria-label="file this detector under a flag family">
                {#each FAMILY_NAMES as fam (fam)}
                  <button
                    type="button"
                    class:on={draftFamily === fam}
                    style="--pk: {NAMED_FAMILIES[fam].ink}"
                    aria-label={fam}
                    aria-pressed={draftFamily === fam}
                    onclick={() => (draftFamily = draftFamily === fam ? '' : fam)}
                    >{NAMED_FAMILIES[fam].mark}</button
                  >
                {/each}
              </div>
            </div>
            <span class="btbar"></span>
            <span class="lab famcap">
              {#if draftFamily}
                filed as · <em>{draftFamily}</em> — its flags wear this ink on the docket, the fall
                and the map
              {:else}
                not filed yet · its flags wear the operator's accent
              {/if}
            </span>
          </div>

          <ConditionsBar bind:this={conditionsBarRef} bind:conditions={draftConditions} bind:unfinishedLine />

          <p class="stat">
            {#if editingCount === 'threshold'}
              <input
                class="kin"
                type="number"
                min="1"
                value={thresholdValue()}
                oninput={(e) => setNumber('threshold', Number(e.currentTarget.value))}
                onblur={() => (editingCount = null)}
                onkeydown={(e) => e.key === 'Enter' && (editingCount = null)}
                aria-label="how many before it fires"
              />
            {:else}
              <button type="button" class="k" onclick={() => (editingCount = 'threshold')}>
                <b>{thresholdValue()}</b>
              </button>
            {/if}
            <!-- What is being counted. Total counts events; distinct
                 counts distinct values of one field, which is the thing
                 the noun after the number has to name. -->
            <select class="k sel" bind:value={draftCounting} aria-label="what it counts">
              <option value="total">matching events</option>
              <option value="distinct">distinct…</option>
            </select>
            {#if draftCounting === 'distinct'}
              <select class="k sel" bind:value={draftDistinct} aria-label="what it counts distinctly">
                {#each DISTINCT_FIELDS as f (f)}
                  <option value={f}>{countingPhrase('distinct', f).replace('distinct ', '')}</option>
                {/each}
              </select>
            {/if}
            <span class="mid">·</span> per
            <select class="k sel" bind:value={draftKey} aria-label="what the window is counted per">
              {#each KEY_MODES as k (k)}
                <option value={k}>{keyPhrase(k)}</option>
              {/each}
            </select>
            <span class="mid">·</span> within
            {#if editingCount === 'window'}
              <input
                class="kin"
                type="number"
                min="1"
                value={windowValue()}
                oninput={(e) => setNumber('window', Number(e.currentTarget.value))}
                onblur={() => (editingCount = null)}
                onkeydown={(e) => e.key === 'Enter' && (editingCount = null)}
                aria-label="the window it counts over, in seconds"
              />
            {:else}
              <button type="button" class="k" onclick={() => (editingCount = 'window')}>
                <b>{windowValue()}</b> <span class="u">s</span>
              </button>
            {/if}
          </p>

          <p class="says">
            <span class="lab">says</span>
            {#if editingSays}
              <input
                class="saysin"
                type="text"
                bind:value={draftTemplate}
                onblur={() => (editingSays = false)}
                onkeydown={(e) => e.key === 'Enter' && (editingSays = false)}
                aria-label="what a raised flag says"
              />
            {:else}
              <button type="button" class="saying" onclick={() => (editingSays = true)}>
                {#each saysParts(draftTemplate) as part, i (i)}
                  {#if part.placeholder}<span class="ph">{part.text}</span>{:else}{part.text}{/if}
                {/each}
              </button>
            {/if}
            <span class="phoffer">
              {#each placeholdersFor(draftKey) as token (token)}
                <button
                  type="button"
                  class="ph add"
                  onclick={() => insertPlaceholder(token)}
                  aria-label="add {token} to what a flag says">{token}</button
                >
              {/each}
            </span>
          </p>

          {#if replay?.receipt}
            {@const receipt = replay.receipt}
            <div class="side">
              <span class="lab">tried · 24 h</span>
              <p class="fired">
                would have fired {receipt.corpusTruncated ? 'at least ' : ''}<b
                  >{receipt.emissionCount}</b
                >
                {receipt.emissionCount === 1 ? 'time' : 'times'}
              </p>
              <!-- The docket's own tick strip, in the family ink: one mark
                   per firing, placed where in the window it happened.
                   Same shape as THE EPISODE, because it answers the same
                   question about a different subject. -->
              <svg viewBox="0 0 260 34" preserveAspectRatio="none" role="img"
                aria-label="{receipt.emissionCount} firings across the window">
                {#each tickPositions(receipt) as x, i (i)}
                  <line x1={x} y1="6" x2={x} y2="28" stroke="var(--ft)" stroke-width="2.5"
                    stroke-linecap="round" />
                {/each}
              </svg>
              {#if receipt.sample.length > 0}
                <p class="hosts">
                  {#each flaggedHosts(receipt) as host (host)}
                    <span>{host}</span><br />
                  {/each}
                </p>
              {/if}
            </div>
          {:else if replay?.decline}
            {@const decline = replay.decline}
            <div class="side">
              <span class="lab">tried · 24 h</span>
              <p class="none">
                needs a {asDuration(decline.definitionWindow)} window, only
                {asDuration(decline.corpusSpan)} held
              </p>
            </div>
          {:else}
            <div class="side">
              <span class="lab">tried · 24 h</span>
              <p class="none">{trying ? 'trying…' : 'not tried yet'}</p>
            </div>
          {/if}

          <!-- Scope has no home in the drawing, and taking it off the
               surface would leave a custom detector unable to say what it
               watches at all -- so it stays, under the drawer, in the
               bench's own chips. Recorded as a gap on #829 rather than
               given a home nobody drew. -->
          {@render scopeGroup(fieldsFor)}

          <div class="acts">
            <button
              type="button"
              class="p"
              class:wait={waiting !== ''}
              disabled={trying || waiting !== ''}
              onclick={() => tryRow(d.name)}><i>▸</i>{replay ? 'try again' : 'try'}</button
            >
            <button
              type="button"
              class="p"
              class:wait={waiting !== ''}
              class:live={waiting === '' && replay !== null}
              disabled={busy || waiting !== ''}
              onclick={() => save(d.name)}><i>✓</i>{saving[d.name] ? 'saving…' : 'save'}</button
            >
            {#if waiting}
              <span class="waiting">{waiting}</span>
            {/if}
            <span class="mid">·</span>
            <button type="button" class="olink" disabled={busy} onclick={() => cloneRow(d.name)}>
              clone
            </button>
            <span class="mid">·</span>
            <button type="button" class="olink quiet" disabled={busy} onclick={() => removeRow(d.name)}>
              remove
            </button>
            <span class="mid">·</span>
            <button type="button" class="olink quiet" disabled={busy} onclick={closePanel}>
              close
            </button>
          </div>

          <label class="field rename">
            <span class="lab">name</span>
            <input type="text" bind:this={nameInput} bind:value={draftName} />
          </label>
        </div>
      {:else if canEdit && open}
        <div class="panel">
          {#if copy?.scopeNote}
            <p class="note"><strong>What this restricts:</strong> {copy.scopeNote}</p>
          {/if}
          {#if copy?.example}
            <p class="example"><strong>Example:</strong> {copy.example}</p>
          {/if}

          {#if d.origin === 'custom'}
            <!-- Only a custom definition carries a name field. A shipped
                 one's name belongs to the binary that ships its logic and
                 the server refuses to change it, so an input for it would
                 be a control that can only ever produce a refusal. -->
            <label class="field">
              <span>Name</span>
              <input type="text" bind:this={nameInput} bind:value={draftName} />
            </label>
          {/if}

          {#if fields.length > 0}
            <div class="group">
              <h4>When it fires</h4>
              {#each fields as f (f.schema.name)}
                <label class="field">
                  <span>
                    {f.label}
                    {#if f.control === 'seconds'}
                      (seconds)
                    {:else if f.schema.unit}
                      ({f.schema.unit})
                    {/if}
                  </span>
                  <!-- Read/write handlers rather than bind: ParamField.value
                       is one union across every control, and a bound
                       expression cannot be narrowed to the type each input
                       wants. Assigning in the handler keeps the narrowing
                       visible instead of casting it away. -->
                  {#if f.control === 'number' || f.control === 'seconds'}
                    <input
                      type="number"
                      step={f.schema.type === 'float' ? 'any' : '1'}
                      min={f.min}
                      max={f.max}
                      value={String(f.value)}
                      oninput={(e) => (f.value = e.currentTarget.valueAsNumber)}
                    />
                  {:else if f.control === 'bool'}
                    <input
                      type="checkbox"
                      checked={f.value === true}
                      onchange={(e) => (f.value = e.currentTarget.checked)}
                    />
                  {:else if f.control === 'enum'}
                    <select
                      value={String(f.value)}
                      onchange={(e) => (f.value = e.currentTarget.value)}
                    >
                      {#each f.schema.enumValues ?? [] as v (v)}
                        <option value={v}>{v}</option>
                      {/each}
                    </select>
                  {:else}
                    <!-- A list param is edited as text and split on save
                         by the same chip rules the scope axes use; kept
                         as one box because these are long curated lists
                         (a critical-port set, an interface glob list),
                         where a chip per entry is a wall rather than a
                         control. -->
                    <input
                      type="text"
                      value={(f.value as string[]).join(', ')}
                      oninput={(e) => {
                        f.value = e.currentTarget.value
                          .split(',')
                          .map((s) => s.trim())
                          .filter((s) => s.length > 0)
                      }}
                    />
                  {/if}
                  <span class="hint">
                    {f.schema.description}
                    {#if f.min !== undefined || f.max !== undefined}
                      <span class="bounds">
                        ({f.min !== undefined ? `min ${f.min}` : ''}{f.min !== undefined &&
                        f.max !== undefined
                          ? ', '
                          : ''}{f.max !== undefined ? `max ${f.max}` : ''})
                      </span>
                    {/if}
                  </span>
                </label>
              {/each}
            </div>
          {/if}

          {@render scopeGroup(fieldsFor)}

          <!-- One slot under the fields for whatever the last Try
               answered (#786): a receipt or a decline, never both, which
               is how the server answers too. -->
          {#if replay?.receipt}
            {@const receipt = replay.receipt}
            <div class="tried">
              <p class="tried-count">
                <!-- "at least" where the corpus read was cut short: the
                     count is then a floor, not a total. sampleTruncated
                     does not touch this line -- a bounded sample leaves
                     the count exact. -->
                Would have fired {receipt.corpusTruncated ? 'at least ' : ''}{receipt.emissionCount}
                {receipt.emissionCount === 1 ? 'time' : 'times'} in the last
                {asDuration(receipt.window.duration)}
                <!-- What the detector counts as it stands, over the same
                     traffic in the same request (#786) -- the candidate's
                     number says nothing on its own. Quieter than the
                     count it sits beside: it is the comparison, not the
                     answer. Absent entirely when nothing was changed,
                     because the count is then already the current one. -->
                {#if replay.current?.receipt}
                  {@const now = replay.current.receipt}
                  <span class="tried-current"
                    >currently: {now.corpusTruncated ? 'at least ' : ''}{now.emissionCount}</span
                  >
                {:else if replay.current?.decline}
                  <!-- Same grey the decline itself uses: the live window
                       being longer than the traffic held is an honest
                       limit, not a failure, and it is only the comparison
                       that is missing -- the receipt above still stands. -->
                  <span class="tried-current"
                    >currently: not replayable over the traffic held ({replay.current.decline
                      .reason})</span
                  >
                {/if}
              </p>
              {#if receipt.sample.length > 0}
                <ul class="tried-hosts">
                  {#each flaggedHosts(receipt) as host (host)}
                    <li>{host}</li>
                  {/each}
                </ul>
                {#if receipt.sampleTruncated}
                  <!-- The sample is bounded server-side, so the hosts
                       above are some of them, not all of them -- the same
                       "at least" the count line uses for a truncated
                       corpus, said about the list instead. -->
                  <p class="tried-more">at least these</p>
                {/if}
              {/if}
            </div>
          {:else if replay?.decline}
            {@const decline = replay.decline}
            <!-- Grey, not red: the corpus being shorter than the window
                 this definition needs is an honest limit of the traffic
                 held, not a failure of anything the operator did. -->
            <p class="tried declined">
              Can't replay: needs a {asDuration(decline.definitionWindow)} window, only
              {asDuration(decline.corpusSpan)} held
            </p>
          {/if}

          <div class="actions">
            <span class="actions-left">
              <button type="button" class="quiet" disabled={busy} onclick={() => resetRow(d.name)}>
                Reset to stock
              </button>
              <!-- Offered on every row since #829. It was custom-only
                   under #810, when a shipped detector's logic was Go the
                   server could not copy; now a shipped declarative one's
                   conditions come across with the copy, and a shipped
                   code one's copy is written in the drawer instead. Both
                   are things this button can do, so both get it. -->
              <button type="button" class="quiet" disabled={busy} onclick={() => cloneRow(d.name)}>
                Clone
              </button>
            </span>
            <!-- Try (#786) sits between the destructive-ish pair on the
                 left and the commit pair on the right: trying a candidate
                 threshold against the stored corpus is what an operator
                 does *before* Save, so it sits immediately beside it.
                 Disabled only while its own replay is in flight -- never
                 on `busy`, because Try must never block Save. -->
            <span class="actions-right">
              <button type="button" class="cancel" disabled={busy} onclick={closePanel}>
                Cancel
              </button>
              <button type="button" class="try" disabled={trying} onclick={() => tryRow(d.name)}>
                {trying ? 'trying…' : 'Try'}
              </button>
              <button type="button" class="save" disabled={busy} onclick={() => save(d.name)}>
                {saving[d.name] ? 'saving…' : 'Save'}
              </button>
            </span>
          </div>
        </div>
      {/if}
    </li>
  {/each}
</ul>

<ExpectationsLedger {canEdit} />

{#if canEdit}
  <!-- One suggestion list per axis for the whole bench rather than per
       row: the sources are deployment-wide (Entities, the pushed filter
       tables), and only one panel is ever open, so repeating them per row
       would duplicate the same options a dozen times over. -->
  <datalist id="watchers-hosts">
    {#each scopeSuggestionsState.hosts as h (h.key)}
      <option value={h.key}>{h.label || h.key}</option>
    {/each}
  </datalist>
  <datalist id="watchers-rules">
    {#each scopeSuggestionsState.rules as r (r)}
      <option value={r}></option>
    {/each}
  </datalist>
{/if}

<style>
  /* ---- the custom detector's drawer (#829, settings round 8) ----------
     Ported from docs/design/screens/settings/round-8/conditions-editor.
     html: the docket's one unbroken family stripe running row into
     drawer, the .btbar gradient rule, .lab for every label, the .stamp
     shrunk to a placeholder, the .v pills for try and save, .olink for
     the text actions and the bench's own dashed click-to-edit for the
     counting numbers. No bordered panel anywhere -- the conditions bar is
     the one boxed input on the surface. */
  .drawer {
    box-shadow: inset 3px 0 0 var(--ft);
    padding: 4px 10px 16px 13px;
  }
  .copied {
    margin: 0 0 12px;
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }
  .copied em {
    font-style: normal;
    color: var(--ft);
  }
  .lab {
    font-family: var(--font-mono);
    font-size: 9px;
    font-weight: 600;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--fg-dim);
  }
  .famline {
    margin-bottom: 16px;
  }
  .famtop {
    display: flex;
    align-items: baseline;
    gap: 18px;
    flex-wrap: wrap;
  }
  .famname {
    font-family: var(--font-mono);
    font-size: 13px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--ft);
    white-space: nowrap;
  }
  .famname i {
    font-style: normal;
    margin-right: 7px;
  }
  .picker {
    display: flex;
    gap: 10px;
    align-items: baseline;
  }
  .picker button {
    background: none;
    border: none;
    padding: 0 1px;
    cursor: pointer;
    font-family: var(--font-mono);
    font-size: 13px;
    font-weight: 700;
    color: var(--pk);
    opacity: 0.34;
    line-height: 1;
  }
  .picker button.on {
    opacity: 1;
  }
  .picker button:hover {
    opacity: 0.8;
  }
  /* The docket's BY TYPE bar: a 16% tint of the ink washing up to it. */
  .btbar {
    display: block;
    height: 3px;
    margin: 7px 0;
    border-radius: 2px;
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--ft) 16%, transparent),
      var(--ft)
    );
  }
  .famcap {
    display: block;
  }
  .famcap em {
    font-style: normal;
    color: var(--ft);
  }

  /* the counting line: the bench's own dashed click-to-edit numbers */
  .stat {
    margin: 16px 0 0;
    font-size: 13px;
    line-height: 2.05;
    color: var(--fg-muted);
  }
  .k {
    font: inherit;
    color: var(--fg);
    font-weight: 600;
    background: transparent;
    border: none;
    border-bottom: 1px dashed var(--accent);
    padding: 0;
    cursor: pointer;
    line-height: 1.3;
  }
  .k:hover {
    color: var(--accent);
  }
  .k b {
    font-weight: 700;
  }
  .k .u {
    color: var(--fg-muted);
    font-weight: 400;
  }
  .k.sel {
    appearance: none;
    -webkit-appearance: none;
  }
  .kin {
    font: inherit;
    color: var(--fg);
    font-weight: 700;
    width: 5ch;
    background: transparent;
    border: none;
    border-bottom: 1px dashed var(--accent);
    padding: 0;
  }
  .mid {
    color: var(--fg-dim);
    margin: 0 5px;
  }

  /* the says line, and the placeholders offered beside it */
  .says {
    margin: 12px 0 0;
    display: flex;
    gap: 10px;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .saying {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--fg-muted);
    background: none;
    border: none;
    padding: 0;
    text-align: left;
    cursor: pointer;
  }
  .saysin {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--fg);
    background: transparent;
    border: none;
    border-bottom: 1px dashed var(--accent);
    min-width: 32ch;
    flex: 1;
    padding: 0;
  }
  /* the docket's .stamp, shrunk: 1.5px of the family ink around a word */
  .ph {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 9.5px;
    font-weight: 800;
    letter-spacing: 0.08em;
    color: var(--ft);
    border: 1.5px solid var(--ft);
    border-radius: 3px;
    padding: 0 5px;
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ft) 30%, transparent);
    vertical-align: 1px;
    background: none;
  }
  .phoffer {
    display: inline-flex;
    gap: 6px;
    align-items: baseline;
  }
  .phoffer .add {
    opacity: 0.55;
    cursor: pointer;
  }
  .phoffer .add:hover {
    opacity: 1;
  }

  /* the receipt, where THE EPISODE sits */
  .side {
    margin-top: 16px;
  }
  .side .fired {
    margin: 6px 0 0;
    font-size: 12.5px;
    color: var(--fg-muted);
  }
  .side .fired b {
    color: var(--fg);
    font-weight: 700;
    font-size: 14px;
  }
  .side svg {
    display: block;
    width: 100%;
    max-width: 420px;
    height: 34px;
    margin: 6px 0 2px;
  }
  .side .hosts {
    margin: 4px 0 0;
    font-family: var(--font-mono);
    font-size: 10.5px;
    line-height: 1.7;
    color: var(--fg-muted);
  }
  .side .none {
    margin: 6px 0 0;
    font-size: 12px;
    color: var(--fg-dim);
  }

  /* the actions: the docket's .v pills, and .olink for the text ones */
  .acts {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 18px;
    flex-wrap: wrap;
  }
  .p {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--fg-muted);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 2px 11px 2px 9px;
    cursor: pointer;
    line-height: 16px;
  }
  .p i {
    font-style: normal;
    color: var(--ft);
    margin-right: 6px;
    font-weight: 700;
  }
  .p:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--ft);
    background: color-mix(in srgb, var(--ft) 12%, transparent);
  }
  .p.live {
    color: var(--fg);
    border-color: color-mix(in srgb, var(--ft) 55%, transparent);
  }
  /* Waiting, not disabled-looking: amber, because a half-written detector
     is a line someone is in the middle of, not a mistake. */
  .p.wait {
    color: var(--fg-dim);
    border-color: color-mix(in srgb, var(--now) 30%, transparent);
    cursor: default;
  }
  .p.wait i {
    color: var(--now);
    opacity: 0.55;
  }
  .waiting {
    color: var(--now);
    font-size: 11.5px;
  }
  .olink {
    background: none;
    border: none;
    padding: 0;
    font-size: 12px;
    font-family: inherit;
    color: var(--accent);
    cursor: pointer;
    text-decoration: underline;
    text-decoration-color: transparent;
  }
  .olink:hover:not(:disabled) {
    text-decoration-color: currentColor;
  }
  .olink.quiet {
    color: var(--fg-dim);
  }
  .rename {
    margin-top: 16px;
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .rename input {
    font: 12px var(--font-mono);
  }

  .bench {
    list-style: none;
    margin: 8px 0 0;
    padding-top: 8px;
    border-top: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .row {
    font-size: 12px;
  }

  .line {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
  }

  .cbx {
    align-self: center;
    /* Never a flex item that grows: the checkbox is a fixed mark at the
       head of the row, and the row's alignment depends on it. */
    flex: none;
  }

  .name {
    color: var(--fg);
    font-weight: 600;
  }

  .id {
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    font-size: 10.5px;
    color: var(--fg-dim);
  }

  .dash {
    color: var(--fg-dim);
  }

  .scope-fact {
    color: var(--fg-muted);
  }

  /* The whole row line as the expander, carrying the scope knob's ink:
     border: none first -- app.css resets a button's font but not its
     border, so setting only the dashed underline left Chromium's default
     button border boxing in what the record calls the admin's ink. */
  .row-knob {
    display: inline-flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
    text-align: left;
    border: none;
    background: transparent;
    padding: 0;
  }

  .row-knob .scope-fact {
    border-bottom: 1px dashed var(--accent);
  }

  .row-knob:hover .scope-fact,
  .row-knob:hover .name {
    color: var(--accent);
  }

  .tuned {
    font-size: 10.5px;
    color: var(--fg-muted);
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0 6px;
  }

  .state {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    color: var(--fg-muted);
    white-space: nowrap;
  }

  .state .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accept);
  }

  .state.paused {
    color: var(--fg-dim);
  }

  .state.paused .dot {
    background: transparent;
    border: 1px solid var(--fg-dim);
  }

  .error {
    margin: 4px 0 0;
    color: var(--reject);
    font-size: 11.5px;
  }

  /* No colour carries meaning here on its own (#639) -- the wording
     already states which of the five states this is, so this is styled
     identically to any other secondary fact line rather than given a
     status colour that would just repeat the words. */
  .learning {
    margin: 4px 0 0;
    color: var(--fg-muted);
    font-size: 11.5px;
  }

  .panel {
    margin-top: 6px;
    padding: 10px;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .note,
  .example {
    margin: 0;
    font-size: 11.5px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .example {
    color: var(--fg);
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  .group h4 {
    margin: 0;
    font-size: 11px;
    font-weight: 600;
    text-transform: lowercase;
    letter-spacing: 0.04em;
    color: var(--fg-dim);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 11.5px;
    color: var(--fg-muted);
  }

  .field-row {
    display: flex;
    gap: 6px;
    align-items: flex-start;
  }

  .hint {
    color: var(--fg-dim);
    font-size: 11px;
    line-height: 1.45;
  }

  .bounds {
    white-space: nowrap;
  }

  .adderr {
    color: var(--reject);
    font-size: 11px;
  }

  /* Scoped to the panel. As bare element selectors these also hit the
     bench's run/pause checkbox -- which is an <input> on the same row --
     and `flex: 1` made it absorb the row's free space, so every detector
     name started at a different x depending on how long it was. The bench
     read as ragged for that reason alone. */
  .panel input,
  .panel select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 5px 7px;
    font-size: 12px;
  }

  .panel input[type='text'],
  .panel input[type='number'] {
    flex: 1;
    min-width: 0;
  }

  .panel input[type='checkbox'] {
    flex: none;
    align-self: flex-start;
  }

  .chipbox {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
    padding: 4px;
    border: 1px solid var(--border);
    border-radius: 5px;
    background: var(--bg-elevated);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 4px 1px 7px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--bg);
    color: var(--fg);
    font-size: 11.5px;
    white-space: nowrap;
  }

  .chip-x {
    border: none;
    background: transparent;
    color: var(--fg-dim);
    padding: 0 2px;
    font-size: 13px;
    line-height: 1;
  }

  .chip-x:hover {
    color: var(--reject);
  }

  /* The add box sits inside the chip well and carries no border of its
     own, so a half-typed value reads as the next chip rather than as a
     second control beside them. */
  .panel .chip-add {
    flex: 1;
    min-width: 8ch;
    border: none;
    background: transparent;
    padding: 2px 4px;
  }

  .chip-plus {
    border: none;
    background: transparent;
    color: var(--accent);
    font-size: 11.5px;
    padding: 0 4px;
  }

  .actions {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  .actions-left,
  .actions-right {
    display: inline-flex;
    gap: 8px;
  }

  .quiet,
  .cancel,
  .try,
  .save {
    border-radius: 5px;
    padding: 5px 10px;
    font-size: 12px;
  }

  .quiet {
    background: transparent;
    border: 1px solid transparent;
    color: var(--fg-muted);
  }

  .quiet:hover:not(:disabled) {
    color: var(--accent);
  }

  /* Try carries Cancel's ink, not Save's: it is a real button rather than
     one of the left pair's quiet ones, but it commits nothing, and giving
     it the accent fill would put two primary-looking actions side by side
     at the foot of the panel. */
  .cancel,
  .try {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .save {
    background: var(--accent);
    border: 1px solid var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  .quiet:disabled,
  .cancel:disabled,
  .try:disabled,
  .save:disabled {
    opacity: 0.6;
    cursor: default;
  }

  /* The replay slot, in the panel's own secondary-fact ink (see .note and
     .learning above) rather than a status colour of its own: the receipt
     states its count in words, and the decline is an honest limit rather
     than an error, so neither has anything for colour to add. */
  .tried {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 11.5px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .tried-count {
    margin: 0;
    color: var(--fg);
  }

  .declined {
    color: var(--fg-muted);
  }

  /* The current count sits inside the count line, in the decline's own
     quiet ink rather than the count's: it is the thing the candidate is
     measured against, not a second answer. */
  .tried-current {
    color: var(--fg-muted);
  }

  .tried-hosts {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
    font-size: 11px;
    color: var(--fg-dim);
  }

  .tried-more {
    margin: 0;
    color: var(--fg-dim);
  }
</style>
