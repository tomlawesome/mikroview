<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  // Admin-only audit log (issue #112): a read-only, most-recent-first
  // table of every admin-privileged mutation mikroview has recorded --
  // who created a user, changed a detector setting, upserted/deleted an
  // entity, created/revoked an API token, or removed a permanent flag
  // exclusion. See internal/audit.Entry -- nothing here is editable from
  // the UI, mirroring Fleet.svelte's plain read-only table shape rather
  // than Entities.svelte's form-backed CRUD one, since there's nothing
  // to create/edit/delete about a historical log entry.
  //
  // Round 30 (docs/design/concepts/round-30/the-whole.html #s7) draws
  // three columns -- when | who | what -- where "what" is one
  // human-readable sentence, not the raw action/target/detail fields
  // side by side. describeEntry below composes that sentence from
  // exactly those fields (internal/audit.Entry's Action is deliberately
  // an open, extensible vocabulary -- see that type's doc comment -- so
  // unrecognized actions still get a plain, readable fallback rather
  // than a dumped field).
  import { onMount } from 'svelte'
  import { auditState } from '../lib/audit.svelte'
  import { appState } from '../lib/state.svelte'
  import { formatHM } from '../lib/format'
  import { compareText, matchesFilter } from '../lib/sortFilter'
  import type { SortDir } from '../lib/sortFilter'
  import type { AuditEntry } from '../lib/types'

  onMount(() => {
    auditState.refresh()
  })

  // Every column sorts and filters (#649): click a head to sort by it,
  // again to reverse; a quiet dashed row beneath the heads narrows the
  // list, per round-18/19's ratified idiom, matching Flags.svelte and
  // Watchlist.svelte's own tabs on this same card. Time defaults to
  // newest first, matching the fixed order this replaces. Only three
  // keys now, matching round 30's three columns.
  type SortKey = 'time' | 'actor' | 'what'
  let sortKey = $state<SortKey>('time')
  let sortDir = $state<SortDir>('desc')
  let filters = $state({ time: '', actor: '', what: '' })

  function toggleSort(key: SortKey) {
    if (sortKey === key) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc'
    } else {
      sortKey = key
      sortDir = key === 'time' ? 'desc' : 'asc'
    }
  }

  function dirGlyph(key: SortKey): string {
    if (sortKey !== key) return ''
    return sortDir === 'asc' ? '▲' : '▼'
  }

  // formatWhen renders round 30's absolute-with-relative-fallback time
  // (the mockup shows "13:47", "12:58" for today's entries and
  // "yesterday" for an older one) -- distinct from formatRelative's
  // uniform "6m ago", which round 30 does not use here. Calendar days,
  // not 24h windows: an entry from 23:59 read at 00:01 the next day is
  // "yesterday", not "0d ago". Older than yesterday falls back to a
  // short day count, then a bare date -- the mockup's fixture never
  // reaches either, but the log itself is not bounded to two days.
  function startOfDay(ms: number): number {
    const d = new Date(ms)
    return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  }

  function formatWhen(iso: string, nowMs: number): string {
    const t = new Date(iso).getTime()
    if (Number.isNaN(t)) return iso
    const dayDiff = Math.round((startOfDay(nowMs) - startOfDay(t)) / 86_400_000)
    if (dayDiff <= 0) return formatHM(iso)
    if (dayDiff === 1) return 'yesterday'
    if (dayDiff < 7) return `${dayDiff}d ago`
    return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  }

  // flagKey turns a flag/exclusion id -- always Type:Target, per
  // internal/flags.flagID -- into the mockup's "TYPE · target" reading,
  // e.g. "port_scan:198.51.100.77" -> "PORT SCAN · 198.51.100.77".
  function flagKey(id: string): string {
    const i = id.indexOf(':')
    if (i < 0) return id
    return `${id.slice(0, i).replace(/_/g, ' ').toUpperCase()} · ${id.slice(i + 1)}`
  }

  // describeEntry composes the "what" sentence from action/target/detail
  // -- the fields the app already has, nothing invented. lead + key +
  // tail concatenate to the full sentence; key is the phrase rendered
  // with emphasis (<span class="k">), matching the mockup's rows, and
  // is '' where no single field reads naturally as the highlighted
  // subject. Known actions get phrasing matching the mockup's verb +
  // subject + "·" idiom; anything else (a future action string -- the
  // vocabulary is deliberately open, see internal/audit.Entry.Action)
  // falls back to a humanized version of the action string itself
  // rather than showing it raw or dropping the record.
  type What = { lead: string; key: string; tail: string }

  function tailOf(detail: string | undefined): string {
    return detail ? ` · ${detail}` : ''
  }

  const KNOWN_ACTIONS: Record<string, (e: AuditEntry) => What> = {
    'flag.clear': (e) => ({
      lead: 'cleared flag ',
      key: flagKey(e.target),
      tail: e.detail ? ` — "${e.detail}"` : '',
    }),
    'flag.clear_all': (e) => ({ lead: 'cleared all flags', key: '', tail: tailOf(e.detail) }),
    'flag.clear_permanent': (e) => ({ lead: 'permanently cleared flag ', key: flagKey(e.target), tail: '' }),
    'flag.exclusion_remove': (e) => ({ lead: 'removed exclusion for ', key: flagKey(e.target), tail: '' }),
    'user.create': (e) => ({ lead: 'created user ', key: e.target, tail: tailOf(e.detail) }),
    'user.delete': (e) => ({ lead: 'deleted user ', key: e.target, tail: tailOf(e.detail) }),
    'account.password_changed': (e) => ({ lead: 'changed password for ', key: e.target, tail: tailOf(e.detail) }),
    'account.sessions_ended': (e) => ({ lead: 'ended all sessions for ', key: e.target, tail: '' }),
    'account.link_sso': (e) => ({ lead: 'linked SSO for ', key: e.target, tail: tailOf(e.detail) }),
    'token.create': (e) => ({ lead: 'created API token ', key: e.target, tail: tailOf(e.detail) }),
    'token.revoke': (e) => ({ lead: 'revoked API token ', key: e.target, tail: '' }),
    'entity.upsert': (e) => ({ lead: 'updated entity ', key: e.target, tail: tailOf(e.detail) }),
    'entity.delete': (e) => ({ lead: 'deleted entity ', key: e.target, tail: '' }),
    'coverage.declare': (e) => ({ lead: 'declared coverage for ', key: e.target, tail: tailOf(e.detail) }),
    'coverage.undeclare': (e) => ({ lead: 'undeclared coverage for ', key: e.target, tail: '' }),
    'definition.create': (e) => ({ lead: 'created definition ', key: e.target, tail: tailOf(e.detail) }),
    'definition.update': (e) => ({ lead: 'updated definition ', key: e.target, tail: tailOf(e.detail) }),
    'definition.delete': (e) => ({ lead: 'deleted definition ', key: e.target, tail: tailOf(e.detail) }),
    'definition.clone': (e) => ({ lead: 'cloned definition ', key: e.target, tail: tailOf(e.detail) }),
    'definition.reset': (e) => ({ lead: 'reset definition ', key: e.target, tail: '' }),
    'definition.promote': (e) => ({ lead: 'promoted definition ', key: e.target, tail: tailOf(e.detail) }),
    'definition.observing.start': (e) => ({ lead: 'started observing ', key: e.target, tail: '' }),
    'definition.observing.stop': (e) => ({ lead: 'stopped observing ', key: e.target, tail: '' }),
    'definition.suggestion.accept': (e) => ({ lead: 'accepted suggestion ', key: e.target, tail: tailOf(e.detail) }),
    'definition.suggestion.hide': (e) => ({ lead: 'hid suggestion ', key: e.target, tail: tailOf(e.detail) }),
    'definition.suggestion.unhide': (e) => ({ lead: 'unhid suggestion ', key: e.target, tail: tailOf(e.detail) }),
    'definition.suggestion.reset': (e) => ({ lead: 'reset suggestions', key: '', tail: tailOf(e.detail) }),
    'setup.step_skipped': (e) => ({ lead: 'skipped setup ', key: e.target, tail: tailOf(e.detail) }),
    'setup.step_forced': (e) => ({ lead: 'forced setup ', key: e.target, tail: tailOf(e.detail) }),
    'ingest.routeros': (e) => ({ lead: 'ingested from ', key: e.target, tail: tailOf(e.detail) }),
    'ingest.routeros.refused': (e) => ({ lead: 'refused ingest from ', key: e.target, tail: tailOf(e.detail) }),
  }

  function describeEntry(e: AuditEntry): What {
    const known = KNOWN_ACTIONS[e.action]
    if (known) return known(e)
    // Fallback for an action string this table doesn't recognize yet:
    // humanize the dotted/underscored action itself rather than
    // printing it raw, and keep target/detail attached so no field is
    // silently dropped.
    const words = e.action.replace(/[._]/g, ' ').trim()
    return { lead: `${words} `, key: e.target, tail: tailOf(e.detail) }
  }

  function whatText(e: AuditEntry): string {
    const w = describeEntry(e)
    return `${w.lead}${w.key}${w.tail}`
  }

  // #736 (corrected 2026-08-31): the backend's action set is almost
  // entirely mutations -- definition.*, detector.update, entity.*,
  // coverage.*, flag.clear, store.retention, token.*, user.*, plus
  // exactly ingest.routeros(.refused) -- so a mutation/refusal/routine
  // split left one bucket holding ~95% of rows and the log one colour
  // again. Classify by subject family instead, since that is what
  // actually varies down the page. Refusal outranks its family: a
  // refused push is a refusal first, checked before any family prefix.
  // ingest.routeros and anything this classifier has never seen both
  // fall through to 'routine' -- unstyled, quiet body ink -- rather
  // than guessing an unrecognised action into another family's meaning.
  // Colour is never the only channel: describeEntry's own lead verb
  // ("revoked", "refused ingest from", "ingested from") already states
  // the distinction in words, so a colourblind reader or a monochrome
  // screenshot loses nothing.
  type EventKind = 'refusal' | 'identity' | 'engine' | 'naming' | 'flag' | 'retention' | 'routine'

  function entryKind(action: string): EventKind {
    if (action.endsWith('.refused')) return 'refusal'
    if (action.startsWith('token.') || action.startsWith('user.')) return 'identity'
    if (action.startsWith('definition.') || action.startsWith('detector.')) return 'engine'
    if (action.startsWith('entity.') || action.startsWith('coverage.')) return 'naming'
    if (action === 'flag.clear') return 'flag'
    if (action === 'store.retention') return 'retention'
    return 'routine'
  }

  const filtered = $derived(
    auditState.list.filter(
      (e) =>
        matchesFilter(formatWhen(e.timestamp, appState.now), filters.time) &&
        matchesFilter(e.actor, filters.actor) &&
        matchesFilter(whatText(e), filters.what),
    ),
  )

  const rows = $derived.by((): AuditEntry[] => {
    const list = [...filtered]
    list.sort((a, b) => {
      switch (sortKey) {
        case 'time':
          return compareText(a.timestamp, b.timestamp, sortDir)
        case 'actor':
          return compareText(a.actor, b.actor, sortDir)
        case 'what':
          return compareText(whatText(a), whatText(b), sortDir)
      }
    })
    return list
  })

  // Off for round-30 fidelity: round 30's audit panel draws no
  // explanatory paragraph above the table -- see
  // docs/design/concepts/round-30/README.md, "No apparatus, anywhere"
  // ("a learned display explains itself once, in the docs"). Unmounted
  // rather than deleted, per the project's build-to-the-mockup-first
  // policy (#700, #691) -- same pattern as LiveTable.svelte's
  // RESIZE_HANDLES_ENABLED, MetricsRegister.svelte's LEDGER_ENABLED, and
  // Topography.svelte's DEGRADED_NOTE_ENABLED. Typed explicitly: a bare
  // `false` narrows to `never` and reports the guarded block below as
  // unreachable.
  const INTRO_ENABLED: boolean = false
</script>

<div class="page scrollbar">
  {#if INTRO_ENABLED}
    <p class="intro">
      Every admin-privileged mutation mikroview has recorded -- who created a user, changed a detector setting,
      upserted/deleted an entity, created or revoked an API token, or removed a permanent flag exclusion. Read-only
      actions (viewing pages, listing users) are never logged here, only mutations.
      {#if auditState.hasMore}
        <span class="truncated">Showing the most recent entries only.</span>
      {/if}
    </p>
  {/if}

  {#if auditState.list.length === 0}
    <p class="empty">No admin actions recorded yet.</p>
  {:else}
    <table>
      <thead>
        <tr>
          <th onclick={() => toggleSort('time')} aria-sort={sortKey === 'time' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
            When <span class="dir">{dirGlyph('time')}</span>
          </th>
          <th onclick={() => toggleSort('actor')} aria-sort={sortKey === 'actor' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
            Who <span class="dir">{dirGlyph('actor')}</span>
          </th>
          <th onclick={() => toggleSort('what')} aria-sort={sortKey === 'what' ? (sortDir === 'asc' ? 'ascending' : 'descending') : 'none'}>
            What <span class="dir">{dirGlyph('what')}</span>
          </th>
        </tr>
        <tr class="filters">
          <td><input bind:value={filters.time} placeholder="filter…" aria-label="Filter by time" /></td>
          <td><input bind:value={filters.actor} placeholder="filter…" aria-label="Filter by actor" /></td>
          <td><input bind:value={filters.what} placeholder="filter…" aria-label="Filter by what" /></td>
        </tr>
      </thead>
      <tbody>
        {#if rows.length === 0}
          <tr><td colspan="3" class="empty-filtered">No entries match these filters.</td></tr>
        {:else}
          {#each rows as e (e.id)}
            {@const w = describeEntry(e)}
            <tr class="row-{entryKind(e.action)}">
              <td class="mono when" title={formatHM(e.timestamp)}>{formatWhen(e.timestamp, appState.now)}</td>
              <td class="actor">{e.actor}</td>
              <td class="what"><span class="action">{w.lead}{#if w.key}<span class="k">{w.key}</span>{/if}</span>{#if w.tail}<span class="detail">{w.tail}</span>{/if}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  {/if}
</div>

<style>
  .page {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .intro {
    margin: 0;
    max-width: 80ch;
    font-size: 13px;
    color: var(--fg-muted);
    line-height: 1.5;
  }

  .truncated {
    display: block;
    margin-top: 4px;
    color: var(--fg-dim, var(--fg-muted));
    font-style: italic;
  }

  .empty {
    margin: 0;
    color: var(--fg-dim);
    font-size: 13px;
    padding: 10px 0;
  }

  /* Flags.svelte's `.ftable` and Watchlist.svelte's `.watch-table`
     treatment (both round 29/30, `#s7`): a bare table, no elevated
     outer card -- the "heavy card, rounded corners" look the owner
     called out (#719) came from wrapping this same table in one. Mono
     throughout, matching the density both siblings draw. */
  table {
    width: 100%;
    border-collapse: collapse;
    font-family: var(--font-mono);
    font-size: 12px;
  }

  th,
  td {
    padding: 8px 12px;
    text-align: left;
    white-space: nowrap;
  }

  th {
    font-size: 9.5px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.1em;
    color: var(--fg-dim);
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    user-select: none;
  }

  th:hover {
    color: var(--fg-muted);
  }

  th .dir {
    display: inline-block;
    min-width: 8px;
    color: var(--accent);
    font-size: 9px;
  }

  /* The quiet dashed filter row (#649), beneath the heads -- matches
     round-18's idiom (docs/design/concepts/round-18/the-docket-opened.html):
     no border of its own, a dashed underline per input, dim until focused. */
  tr.filters td {
    padding: 2px 12px 8px;
    border-bottom: 1px solid var(--border);
  }

  tr.filters input {
    width: 100%;
    background: transparent;
    border: 0;
    border-bottom: 1px dashed var(--border);
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--fg-muted);
    padding: 2px 0;
    outline: none;
  }

  tr.filters input::placeholder {
    color: var(--fg-dim);
    opacity: 0.7;
  }

  tr.filters input:focus {
    border-bottom-color: var(--accent);
  }

  /* Matches Watchlist's `.empty-row`: a colspan message reads as plain
     prose, not a dense-mono log line, and carries no border of its own. */
  .empty-filtered {
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-size: 13px;
    padding: 14px;
    white-space: normal;
    border-bottom: none;
  }

  tbody tr {
    border-bottom: 1px solid var(--border);
  }

  /* Same hover as `.frow`/`.wt-row`: a plain highlight, not zebra
     striping -- neither sibling stripes its rows. */
  tbody tr:hover td {
    background: var(--bg-hover);
  }

  .mono {
    font-family: var(--font-mono);
    color: var(--fg);
  }

  .actor {
    color: var(--fg);
    font-weight: 600;
  }

  /* When and Who hug their content so What -- the only column with a
     sentence in it -- takes the slack. Without this the auto layout
     spreads three columns evenly and the log reads as mostly gutter,
     which is half of why the owner called it "dropped in from another
     project" (#719). */
  th:nth-child(1),
  th:nth-child(2),
  .when,
  .actor {
    width: 1%;
  }

  .when {
    white-space: nowrap;
  }

  /* "what" is round 30's single composed sentence (#s7's when|who|what
     table) -- muted body text with the sentence's subject (a flag,
     entity, user, definition…) picked out via .k, matching the
     mockup's rows. */
  .what {
    color: var(--fg-muted);
    white-space: normal;
    max-width: 480px;
  }

  .what .k {
    color: var(--fg);
    font-weight: 600;
  }

  /* #736 (corrected 2026-08-31): the detail/quote a describeEntry tail
     carries (e.g. "· expected, speed test") stays body ink regardless of
     the row's family -- only the action (lead verb + subject) takes the
     ink below, so the log doesn't trade one uniform page for a
     rainbow-coloured one. A plain rule directly on .detail always wins
     over the inherited family colour from .what, no matter the family
     selector's own specificity. */
  .what .detail {
    color: var(--fg-muted);
  }

  /* Colour the action by its subject family, not by mutation/refusal/
     routine -- the backend's action set is almost entirely mutations
     (definition.*, detector.update, entity.*, coverage.*, flag.clear,
     store.retention, token.*, user.*), so that split left one bucket
     holding ~95% of rows and the log one colour again (see entryKind's
     comment). Reuses the app's existing action-badge inks, no audit-only
     palette. Refusal outranks its family and is checked first in
     entryKind. ingest.routeros and any action this classifier has never
     seen fall through to unstyled routine -- quiet body ink, per the
     .what/.what .k rules above -- rather than borrowing another
     family's meaning. */
  tr.row-refusal .what .action,
  tr.row-refusal .what .action .k {
    color: var(--reject);
  }

  tr.row-identity .what .action,
  tr.row-identity .what .action .k {
    color: var(--marked);
  }

  tr.row-engine .what .action,
  tr.row-engine .what .action .k {
    color: var(--log);
  }

  tr.row-naming .what .action,
  tr.row-naming .what .action .k {
    color: var(--natted);
  }

  tr.row-flag .what .action,
  tr.row-flag .what .action .k {
    color: var(--now);
  }

  tr.row-retention .what .action,
  tr.row-retention .what .action .k {
    color: var(--accent);
  }

  @media (max-width: 700px) {
    th,
    td {
      padding: 8px 10px;
    }
  }
</style>
