// SPDX-License-Identifier: AGPL-3.0-only

import { clearAllFlags, clearFlag, clearFlagPermanent, deleteFlagVerdict, fetchFlags, setFlagVerdict } from './api'
import type { Flag, FlagTimeBucket } from './types'

// How long Undo stays offered after a successful Expected/Noise
// judgement (#638) -- purely a UI affordance now (see judgeAndClear's
// own doc comment): the verdict has already reached the server by the
// time this timer starts, so it gates nothing but whether the Undo
// button is still shown.
const VERDICT_UNDO_MS = 5000

interface UndoableVerdict {
  id: string
  verdict: 'expected' | 'noise'
  timer: ReturnType<typeof setTimeout>
}

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

  // Groups *active* flags by normalized source IP (see extractSourceIp)
  // so "one actor, several signals" -- a port scan, then a critical-port
  // hit, then a reputation-triggered flag, all from the same host --
  // reads as one correlated unit in Flags.svelte instead of N unrelated
  // entries (issue #106). Cleared flags are left out: they've already
  // been reviewed, so there's nothing left to correlate them toward.
  // Only IPs with more than one active flag are kept -- a group of one
  // is just a normal flag, not a campaign worth calling out.
  groupedBySource = $derived.by(() => {
    const groups = new Map<string, Flag[]>()
    for (const f of this.list) {
      if (f.cleared) continue
      const ip = extractSourceIp(f.target)
      if (!ip) continue
      const existing = groups.get(ip)
      if (existing) existing.push(f)
      else groups.set(ip, [f])
    }
    for (const [ip, flags] of groups) {
      if (flags.length < 2) groups.delete(ip)
    }
    return groups
  })

  async refresh() {
    const res = await fetchFlags()
    this.list = res.flags
    this.timeSeries = res.timeSeries
  }

  // Updates the flag locally the instant the user clicks Clear, rather
  // than waiting on a network round-trip before anything visible
  // happens (the old code awaited clearFlag then a second full refresh()
  // -- two serial round-trips per click, whose completion queued behind
  // whatever else the main thread was doing under a live-traffic flood).
  // App.svelte's existing 5s poll reconciles this against the server
  // regardless, so there's no correctness gap from skipping the extra
  // refetch here -- only a failed clearFlag call needs an explicit
  // revert, since otherwise the flag would sit incorrectly "cleared"
  // until that poll ran.
  // note (#678): the operator's reason for clearing, if they gave one --
  // threaded straight through to clearFlag, which records it server-side
  // on the same admin-mutation audit entry the clear itself now writes.
  async clear(id: string, note?: string) {
    return this.optimisticallyClear(id, (flagId) => clearFlag(flagId, note))
  }

  // The shared body of clear() and clearPermanent(), which differed only
  // in which call they awaited. Kept as one so a change to the revert
  // logic cannot land in one and not the other -- the two had already
  // been maintained in parallel by hand (#267 finding 25).
  private async optimisticallyClear(id: string, call: (id: string) => Promise<unknown>) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.cleared) return

    const wasCleared = flag.cleared
    const previousClearedAt = flag.clearedAt
    flag.cleared = true
    flag.clearedAt = new Date().toISOString()

    try {
      await call(id)
    } catch (err) {
      flag.cleared = wasCleared
      flag.clearedAt = previousClearedAt
      throw err
    }
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
    for (const f of touched) {
      f.cleared = true
      f.clearedAt = now
    }

    try {
      await clearAllFlags()
    } catch (err) {
      for (const { flag, clearedAt } of snapshot) {
        flag.cleared = false
        flag.clearedAt = clearedAt
      }
      throw err
    }
  }

  // "Clear and never flag this again" -- same optimistic-update
  // reasoning as clear() above (instant feedback, revert on failure),
  // plus the permanent exclusion this adds server-side. There's no
  // client-visible "excluded" flag state to revert beyond the clear
  // itself; a caller that also renders an exclusions list (see
  // exclusions.svelte.ts) is expected to refresh() that separately.
  async clearPermanent(id: string) {
    return this.optimisticallyClear(id, clearFlagPermanent)
  }

  // Flags with an active, still-undoable verdict (issue #638) -- Undo
  // stays offered on the card/toast for VERDICT_UNDO_MS after a
  // successful Expected/Noise judgement, per this list. Purely a UI
  // affordance: by the time an entry lands here the verdict has already
  // reached the server (see judgeAndClear below), so this gates nothing
  // but whether the Undo button is still shown, not whether undoing
  // still works -- undoVerdict() below sends a real request regardless
  // of this list, and only consults it to know whether there is
  // anything left to undo at all.
  undoableVerdicts = $state<UndoableVerdict[]>([])

  isUndoable(id: string): boolean {
    return this.undoableVerdicts.some((u) => u.id === id)
  }

  // 'real' (issue #638): records the verdict without clearing the flag,
  // so it stays visible -- optimistic like clear() above, but setting
  // the verdict fields instead of `cleared`. judgedBy is the calling
  // account's own username, shown immediately; the server's response
  // then replaces it (and verdictAt) with its own canonical values in
  // case the two ever diverge, same reasoning setFlagVerdict's doc
  // comment gives for returning the updated flag at all.
  async judgeReal(id: string, judgedBy: string) {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.verdict) return

    const prev = { verdict: flag.verdict, verdictBy: flag.verdictBy, verdictAt: flag.verdictAt }
    flag.verdict = 'real'
    flag.verdictBy = judgedBy
    flag.verdictAt = new Date().toISOString()

    try {
      const updated = await setFlagVerdict(id, 'real')
      flag.verdict = updated.verdict
      flag.verdictBy = updated.verdictBy
      flag.verdictAt = updated.verdictAt
    } catch (err) {
      flag.verdict = prev.verdict
      flag.verdictBy = prev.verdictBy
      flag.verdictAt = prev.verdictAt
      throw err
    }
  }

  // 'expected'/'noise' (issue #638): posts the verdict immediately --
  // optimistic clear like clear() above, then the real request, then
  // reconciled against the server's response, same shape as judgeReal
  // above. Undo (below) is offered for VERDICT_UNDO_MS afterward, but
  // that is UI only now: the verdict is already recorded server-side by
  // the time this call returns.
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
  async judgeAndClear(id: string, verdict: 'expected' | 'noise') {
    const flag = this.list.find((f) => f.id === id)
    if (!flag || flag.cleared) return

    const prev = {
      cleared: flag.cleared,
      clearedAt: flag.clearedAt,
      verdict: flag.verdict,
      verdictBy: flag.verdictBy,
      verdictAt: flag.verdictAt,
    }
    flag.cleared = true
    flag.clearedAt = new Date().toISOString()

    try {
      const updated = await setFlagVerdict(id, verdict)
      flag.verdict = updated.verdict
      flag.verdictBy = updated.verdictBy
      flag.verdictAt = updated.verdictAt
      flag.cleared = updated.cleared
      flag.clearedAt = updated.clearedAt
    } catch (err) {
      flag.cleared = prev.cleared
      flag.clearedAt = prev.clearedAt
      flag.verdict = prev.verdict
      flag.verdictBy = prev.verdictBy
      flag.verdictAt = prev.verdictAt
      throw err
    }

    const timer = setTimeout(() => {
      this.undoableVerdicts = this.undoableVerdicts.filter((u) => u.id !== id)
    }, VERDICT_UNDO_MS)
    this.undoableVerdicts = [...this.undoableVerdicts, { id, verdict, timer }]
  }

  // Undoes a still-undoable verdict (issue #638) -- now a real
  // DELETE /api/flags/verdict/{id}, since judgeAndClear above no longer
  // defers the POST for this to cancel before it happens. Optimistic
  // like every other mutation here: the flag reopens and its verdict
  // clears immediately, reverted on failure the same way clear()'s own
  // revert works.
  async undoVerdict(id: string) {
    const pending = this.undoableVerdicts.find((u) => u.id === id)
    if (!pending) return
    clearTimeout(pending.timer)
    this.undoableVerdicts = this.undoableVerdicts.filter((u) => u.id !== id)

    const flag = this.list.find((f) => f.id === id)
    if (!flag) return

    const prev = {
      cleared: flag.cleared,
      clearedAt: flag.clearedAt,
      verdict: flag.verdict,
      verdictBy: flag.verdictBy,
      verdictAt: flag.verdictAt,
    }
    flag.cleared = false
    flag.clearedAt = undefined
    flag.verdict = undefined
    flag.verdictBy = undefined
    flag.verdictAt = undefined

    try {
      const updated = await deleteFlagVerdict(id)
      flag.cleared = updated.cleared
      flag.clearedAt = updated.clearedAt
      flag.verdict = updated.verdict
      flag.verdictBy = updated.verdictBy
      flag.verdictAt = updated.verdictAt
    } catch (err) {
      flag.cleared = prev.cleared
      flag.clearedAt = prev.clearedAt
      flag.verdict = prev.verdict
      flag.verdictBy = prev.verdictBy
      flag.verdictAt = prev.verdictAt
      throw err
    }
  }
}

export const flagsState = new FlagsState()
