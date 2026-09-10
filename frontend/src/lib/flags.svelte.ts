// SPDX-License-Identifier: AGPL-3.0-only

import { clearAllFlags, deleteFlagVerdict, fetchFlags, setFlagVerdict } from './api'
import type { Flag, FlagTimeBucket, Verdict } from './types'

const IPV4_RE = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/

function isIpAddress(value: string): boolean {
  const m = value.match(IPV4_RE)
  if (m) return m.slice(1).every((octet) => Number(octet) <= 255)
  // Loose IPv6 check -- this only needs to decide "does this look like a
  // single IP" for grouping purposes, not fully validate address syntax
  // (unlike isPublicIp in format.ts, which is deliberately IPv4-only for
  // its own narrower purpose).
  return value.includes(':') && /^[0-9a-fA-F:]+$/.test(value)
}

// Extracts the leading source IP from a flag's `target` for campaign
// grouping (issue #106), or null if the target isn't a single-source-IP
// shape to correlate on. `target` varies by detector (see
// internal/flags.Flag's doc comment and internal/detect/*.go):
//   - bare source IP for most per-host detectors (port_scan,
//     activity_spike, critical_port, outbound_anomaly, internal_recon,
//     low_slow_scan)
//   - "<ip> -> port <N>" for repeated_drops -- the port suffix is
//     stripped here for grouping purposes only; the flag's own
//     detail/display still shows the full composite target
//   - "port <N>" for distributed_brute_force -- no single source IP
//   - a rule label for rule_spike -- no single source IP
//   - "global" for global_spike -- no single actor at all
// The latter three (and anything else that isn't IP-shaped after
// stripping the repeated_drops suffix) return null so they're excluded
// from grouping rather than mis-grouped under a bogus shared key.
export function extractSourceIp(target: string): string | null {
  const withoutDropsSuffix = target.replace(/ -> port \d+$/, '')
  return isIpAddress(withoutDropsSuffix) ? withoutDropsSuffix : null
}

// A campaign (#988, round 47): the flags one source IP raised inside one
// 30-minute window, whatever their types. Flags.svelte folds them into
// one row that opens to its members.
export interface Campaign {
  // Stable while the campaign's oldest flag is: the row's key, and what
  // Flags.svelte remembers as open.
  id: string
  ip: string
  // Oldest first, as they joined.
  flags: Flag[]
  // The span: the oldest member's firstSeen to the newest member's
  // lastSeen.
  firstSeen: string
  lastSeen: string
  // Member counts summed.
  count: number
}

export const CAMPAIGN_WINDOW_MS = 30 * 60 * 1000

// Folds flags into campaigns (#988): same source IP *and* each flag's
// active window (firstSeen..lastSeen) overlapping, or sitting within 30
// minutes of, the campaign so far. A flag from the same source outside
// that window starts a new campaign -- the owner's "separate worthy
// items", not one campaign per IP. Flags whose target is not a single
// IP (a rule label, a port, "global" -- see extractSourceIp) are never
// grouped. A campaign of one is just a flag: returned as-is, in the
// `singles` list, and never a campaign row.
//
// The window test is on time alone, never on type: a port scan, then a
// critical-port hit, then repeated drops from the same host inside half
// an hour is one actor doing one thing. This replaced groupedBySource
// (#106), which grouped by IP alone and so folded a morning's activity
// spike in with an afternoon's scan.
export function buildCampaigns(
  flags: Flag[],
  windowMs = CAMPAIGN_WINDOW_MS,
): { campaigns: Campaign[]; singles: Flag[] } {
  const byIp = new Map<string, Flag[]>()
  const singles: Flag[] = []
  for (const f of flags) {
    const ip = extractSourceIp(f.target)
    if (!ip) {
      singles.push(f)
      continue
    }
    const existing = byIp.get(ip)
    if (existing) existing.push(f)
    else byIp.set(ip, [f])
  }
  const campaigns: Campaign[] = []
  for (const [ip, list] of byIp) {
    const ordered = [...list].sort((a, b) => Date.parse(a.firstSeen) - Date.parse(b.firstSeen))
    let run: Flag[] = []
    // The newest lastSeen in the run so far, and the flag carrying it --
    // kept as the flag's own string rather than re-serialised, so the
    // span reads exactly as the server wrote it.
    let runEnd = Number.NEGATIVE_INFINITY
    let runLast = ''
    const flush = () => {
      if (run.length >= 2) {
        campaigns.push({
          id: `campaign:${run[0].id}`,
          ip,
          flags: run,
          firstSeen: run[0].firstSeen,
          lastSeen: runLast,
          count: run.reduce((n, f) => n + f.count, 0),
        })
      } else {
        singles.push(...run)
      }
      run = []
      runEnd = Number.NEGATIVE_INFINITY
      runLast = ''
    }
    for (const f of ordered) {
      const start = Date.parse(f.firstSeen)
      const end = Math.max(start, Date.parse(f.lastSeen))
      if (run.length > 0 && start - runEnd > windowMs) flush()
      run.push(f)
      if (end > runEnd) {
        runEnd = end
        runLast = end === start && end > Date.parse(f.lastSeen) ? f.firstSeen : f.lastSeen
      }
    }
    flush()
  }
  return { campaigns, singles }
}

// Behavioral flags (port scans, activity spikes, critical-port attempts,
// global volume spikes -- see internal/detect) raised server-side and
// reviewed/cleared by a human here. Kept as its own small module rather
// than folded into appState, matching how theme/colorway/retention/
// presets each get their own state module in this codebase.
class FlagsState {
  list = $state<Flag[]>([])
  // Last hour of newly-raised-episode counts by type at 1-minute
  // resolution (see internal/flags.Store.TimeSeries), for metrics --
  // fetched alongside list in the same GET /api/flags response.
  timeSeries = $state<FlagTimeBucket[]>([])
  // Whether refresh() has ever completed -- an empty list before this is
  // true is "not fetched yet", not "no flags exist". The city's watched
  // reading (#867) reads this rather than assuming an empty list means
  // nothing is flagged.
  loaded = $state(false)

  // The learning shelf's warming signal (#768), from the same GET
  // /api/flags response as list above -- one source for one surface, so
  // the shelf cannot disagree with the flags beside it the way a second
  // poll of GET /api/definitions could. undefined until the server says
  // (see api.ts's FlagsResponse): a claim it did not make.
  baselinesWarming = $state<boolean | undefined>(undefined)

  // Ids Flags.svelte's current visit judged, kept in the settled/shelf
  // tables in place -- dimmed, carrying their stamp -- rather than
  // dropped the instant the server marks them cleared (#780 item 2: "the
  // recently-cleared list, in place", staying until the tab is left).
  //
  // Lives here rather than as Flags.svelte's own component-local $state
  // (#961): "watch for this" (#641) sends the operator to the watchlist
  // tab and back, and Docket.svelte destroys and recreates Flags.svelte
  // on every switch between its own tabs
  // (`{#if tab === 'watchlist'}...{:else}<Flags/>`), so a component-local
  // list reset on that remount too, dropping the very row the operator
  // just judged before the round trip could bring them back to it. A
  // singleton survives the remount; Flags.svelte's own mount logic
  // decides whether to keep or clear it -- see its doc comment there for
  // how it tells that return apart from a genuinely fresh visit. Nothing
  // else about how long a pinned row stays changes: a plain tab switch
  // away and back (to `live`, say) still starts the list over.
  pinnedIds = $state<string[]>([])

  // Bumped by refresh() before its fetch and by every optimistic
  // mutation below, same requestId idiom as dossier.svelte.ts/
  // ipLookup.svelte.ts (#1074). refresh() replaces `this.list` wholesale
  // with new objects (see refresh() below), while judgeInvestigate/
  // judgeAndClear/undoVerdict/clearAll mutate an object captured from
  // the *old* list across an await. Without this, a refresh already in
  // flight when one of those mutations lands can resolve afterwards
  // with the pre-mutation snapshot it fetched and stomp the optimistic
  // change -- the operator's undo/verdict/clear appears to revert until
  // the next poll. refresh() only applies its result if this counter
  // still reads what it captured before the fetch, so a mutation (or a
  // newer refresh) landing in between makes it a no-op instead.
  private generation = 0

  pin(id: string) {
    if (!this.pinnedIds.includes(id)) this.pinnedIds = [...this.pinnedIds, id]
  }

  unpin(id: string) {
    this.pinnedIds = this.pinnedIds.filter((pid) => pid !== id)
  }

  clearPins() {
    this.pinnedIds = []
  }

  // The open-flags count is the *settled* ledger's count (#642): a
  // provisional flag -- raised while its baseline was still warming, so
  // a judgement mikroview does not yet trust -- is visible on the
  // learning shelf but never counted here. Everything that renders this
  // (the scene bar's flag mark, BottomBar's badge) therefore only ever
  // claims trusted judgements; the shelf's own heading carries
  // provisionalCount instead.
  activeCount = $derived(this.list.filter((f) => !f.cleared && !f.provisional).length)

  // Open provisional flags -- the learning shelf's number (#642).
  provisionalCount = $derived(this.list.filter((f) => !f.cleared && f.provisional).length)

  async refresh() {
    const gen = ++this.generation
    const res = await fetchFlags()
    // A mutation (or a newer refresh) landed while this fetch was in
    // flight -- its snapshot predates that change, so applying it now
    // would revert it. Drop it; the next poll re-fetches current state.
    if (gen !== this.generation) return
    this.list = res.flags
    this.timeSeries = res.timeSeries
    this.baselinesWarming = res.baselinesWarming
    this.loaded = true
  }

  // "Clear all" (issue #198) -- same optimistic-update reasoning as
  // clear() above, applied to every currently-active flag at once. The
  // server reports how many it actually cleared, which can differ from
  // the count marked here if a flag raised between the click and the
  // response landing; that's an acceptable margin App.svelte's existing
  // 5s poll reconciles, same as clear()'s own gap.
  //
  // Snapshotting every touched flag's prior state (not just a single
  // one) is what makes the revert-on-failure path correct here: a
  // partial optimistic update left in place after a failed request would
  // show flags as cleared that the server never actually cleared.
  async clearAll() {
    const touched = this.list.filter((f) => !f.cleared)
    if (touched.length === 0) return

    const snapshot = touched.map((f) => ({ flag: f, clearedAt: f.clearedAt }))
    const now = new Date().toISOString()
    this.generation++
    for (const f of touched) {
      f.cleared = true
      f.clearedAt = now
    }

    try {
      await clearAllFlags()
      // A refresh that started after the bump above (so it passed its own
      // gen check) can still resolve while this request is in flight and
      // replace this.list wholesale -- see the `generation` field's doc
      // comment. The objects `touched` points at would then be detached
      // from this.list, so re-find each by id in the *current* list
      // before writing the confirmed state (a no-op if it's gone), and
      // bump generation again so a refresh already past its gen check
      // cannot land on top of this write afterwards.
      this.generation++
      for (const { flag } of snapshot) {
        const current = this.list.find((f) => f.id === flag.id)
        if (current) {
          current.cleared = flag.cleared
          current.clearedAt = flag.clearedAt
        }
      }
    } catch (err) {
      this.generation++
      for (const { flag, clearedAt } of snapshot) {
        const current = this.list.find((f) => f.id === flag.id)
        if (current) {
          current.cleared = false
          current.clearedAt = clearedAt
        }
      }
      throw err
    }
  }

  // Whether id currently carries an undoable verdict (issue #638; #780
  // moved its one consumer onto the flag row itself, offered for as
  // long as the row stays pinned in place rather than for a fixed
  // window -- see Flags.svelte's own doc comment on why the row, not a
  // timer, now owns that lifetime). Read straight off the flag's own
  // Verdict field rather than a separate client-side list: the server
  // is the same source of truth undoVerdict() below calls, and
  // UndoVerdict's own doc comment (store.go) already treats "undo an
  // unjudged flag" as a no-op, so there is nothing this needs to track
  // beyond what the flag itself says.
  isUndoable(id: string): boolean {
    return !!this.list.find((f) => f.id === id)?.verdict
  }

  // Re-finds `id` in the *current* this.list and applies `fn` to it, or
  // no-ops if it's gone. Every mutation below calls this right after its
  // own network call resolves (both on success and on revert), because a
  // refresh that starts after the mutation's pre-await generation bump
  // (so it passes refresh()'s own gen check) can still resolve *while
  // that network call is in flight* and replace this.list wholesale --
  // see the `generation` field's doc comment. That detaches the object
  // the mutation captured before its await, so writing straight onto it
  // afterwards lands on an object the UI no longer shows. Bumping
  // generation again here also stops a refresh that raced past its own
  // gen check from landing on top of this write afterwards.
  private applyToCurrent(id: string, fn: (flag: Flag) => void) {
    this.generation++
    const current = this.list.find((f) => f.id === id)
    if (current) fn(current)
  }

  // 'investigate' (#640): records the verdict without clearing the flag,
  // so it stays open while someone works on it -- optimistic like every
  // other mutation here, but setting the verdict fields instead of
  // `cleared`. judgedBy is the calling account's own username, shown
  // immediately; the server's response then replaces it (and verdictAt)
  // with its own canonical values in case the two ever diverge, same
  // reasoning setFlagVerdict's doc comment gives for returning the
  // updated flag at all.
  async judgeInvestigate(id: string, judgedBy: string) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.verdict) return

    const prev = { verdict: flag.verdict, verdictBy: flag.verdictBy, verdictAt: flag.verdictAt }
    this.generation++
    flag.verdict = 'investigate'
    flag.verdictBy = judgedBy
    flag.verdictAt = new Date().toISOString()

    try {
      const updated = await setFlagVerdict(id, 'investigate')
      this.applyToCurrent(id, (current) => {
        current.verdict = updated.verdict
        current.verdictBy = updated.verdictBy
        current.verdictAt = updated.verdictAt
      })
    } catch (err) {
      this.applyToCurrent(id, (current) => {
        current.verdict = prev.verdict
        current.verdictBy = prev.verdictBy
        current.verdictAt = prev.verdictAt
      })
      throw err
    }
  }

  // The three verdicts that clear (#640: expected, checked, resolved):
  // posts the verdict immediately, optimistically marks the flag
  // cleared, then reconciles against the server's response -- same shape
  // as judgeInvestigate above. Undo (below) is offered for as long as
  // the flag still carries this verdict -- see isUndoable's own doc
  // comment for why that is no longer a timed window.
  //
  // For 'expected' the same request also records the expectation
  // server-side; there is no separate client-visible state for it to
  // revert beyond the clear, and undoVerdict withdraws it the same way
  // the server does.
  //
  // This replaced a version that deferred the POST itself behind the
  // undo window and sent it only once the window lapsed, cancelling the
  // timer instead of the request for a same-window undo. That looked
  // right and was not: the PWA's own service worker re-issues every
  // fetch through itself (vite.config.ts's registerType: 'autoUpdate'
  // sets clientsClaim), which strips the keepalive guarantee a
  // page-teardown request depends on -- a verdict judged just before a
  // reload reached the server 0 times out of 6 in testing, silently,
  // and only on a properly-certificated deployment (a self-signed one
  // masked it). Posting at once has no equivalent gap: there is no
  // window in which the click has happened but the request has not been
  // sent, so there is nothing left for a page teardown to lose.
  async judgeAndClear(id: string, verdict: Extract<Verdict, 'expected' | 'checked' | 'resolved'>) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.cleared) return

    const prev = {
      cleared: flag.cleared,
      clearedAt: flag.clearedAt,
      verdict: flag.verdict,
      verdictBy: flag.verdictBy,
      verdictAt: flag.verdictAt,
    }
    this.generation++
    flag.cleared = true
    flag.clearedAt = new Date().toISOString()

    try {
      const updated = await setFlagVerdict(id, verdict)
      this.applyToCurrent(id, (current) => {
        current.verdict = updated.verdict
        current.verdictBy = updated.verdictBy
        current.verdictAt = updated.verdictAt
        current.cleared = updated.cleared
        current.clearedAt = updated.clearedAt
      })
    } catch (err) {
      this.applyToCurrent(id, (current) => {
        current.cleared = prev.cleared
        current.clearedAt = prev.clearedAt
        current.verdict = prev.verdict
        current.verdictBy = prev.verdictBy
        current.verdictAt = prev.verdictAt
      })
      throw err
    }
  }

  // Undoes a still-undoable verdict (issue #638) -- now a real
  // DELETE /api/flags/verdict/{id}, since judgeAndClear above no longer
  // defers the POST for this to cancel before it happens. Optimistic
  // like every other mutation here: the flag reopens and its verdict
  // clears immediately, reverted on failure the same way every other
  // optimistic write here reverts. A no-op (like the server's own
  // UndoVerdict) if id
  // carries no verdict to undo -- the row that offers this button is
  // gone by then anyway, but a stale click racing that is harmless
  // rather than an error.
  async undoVerdict(id: string) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || !flag.verdict) return

    const prev = {
      cleared: flag.cleared,
      clearedAt: flag.clearedAt,
      verdict: flag.verdict,
      verdictBy: flag.verdictBy,
      verdictAt: flag.verdictAt,
    }
    this.generation++
    flag.cleared = false
    flag.clearedAt = undefined
    flag.verdict = undefined
    flag.verdictBy = undefined
    flag.verdictAt = undefined

    try {
      const updated = await deleteFlagVerdict(id)
      this.applyToCurrent(id, (current) => {
        current.cleared = updated.cleared
        current.clearedAt = updated.clearedAt
        current.verdict = updated.verdict
        current.verdictBy = updated.verdictBy
        current.verdictAt = updated.verdictAt
      })
    } catch (err) {
      this.applyToCurrent(id, (current) => {
        current.cleared = prev.cleared
        current.clearedAt = prev.clearedAt
        current.verdict = prev.verdict
        current.verdictBy = prev.verdictBy
        current.verdictAt = prev.verdictAt
      })
      throw err
    }
  }
}

export const flagsState = new FlagsState()
