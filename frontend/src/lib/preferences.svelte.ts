// SPDX-License-Identifier: AGPL-3.0-only
//
// #1283: the one place every per-user preference lives now. Before this,
// each of presets/topTalkers/altitudeStop/columns/groupMode/
// retention/metrics/deckOrder read and wrote its own localStorage key
// directly -- which meant a shared machine handed the next person to
// sign in the previous operator's saved filters and top-talker widgets
// (the two keys that carry account content), and every layout choice
// besides. This module is the one thing that talks to
// GET/PATCH /api/me/preferences; the eight modules above keep their own
// shape and public API, but read/write their slice of the shared record
// through get()/set() below instead of localStorage.
//
// The server stores and returns `prefs` whole and never looks inside it
// -- the key names (one per module, named after the module) are a
// frontend-only contract, listed above and nowhere else.

import { fetchMyPreferences, saveMyPreferences } from './api'
import type { PreferencesRecord } from './types'

const VERSION = 1
const DEBOUNCE_MS = 500

type Hydrator = (value: unknown) => void

class PreferencesState {
  private prefs: Record<string, unknown> = {}
  private loaded = false
  // True only once a fetchMyPreferences() has actually succeeded --
  // `loaded` above flips on a fallback too (see load()'s catch), so it
  // alone can't tell a real baseline from "sign-in couldn't reach the
  // server, defaults are standing in for this session". flush() below
  // refuses to send anything until this is true, and ensureLoaded()
  // keeps retrying the fetch until it is.
  private loadSucceeded = false
  private loading: Promise<void> | null = null
  // Keys changed (via set()) since the last successful flush -- what a
  // save actually sends, so two tabs saving different keys around the
  // same time both survive on the server (#1283's "save only what
  // changed" ruling; the server's merge side is prefs.Store.Merge).
  private changedKeys = new Set<string>()
  private timer: ReturnType<typeof setTimeout> | undefined
  // One entry per preference module -- registered at each module's own
  // import time (its `new XState()` singleton), long before sign-in has
  // happened or ensureLoaded() has anything to hand it. A module
  // registering after the record has already loaded (the ordinary case:
  // every module singleton is constructed once, at app start, well
  // before ensureLoaded() ever resolves) is still covered: register()
  // hydrates immediately when that's true.
  private hydrators = new Map<string, Hydrator>()

  register(key: string, hydrate: Hydrator) {
    this.hydrators.set(key, hydrate)
    if (this.loaded) hydrate(this.prefs[key])
  }

  /** Called once after sign-in (see auth.svelte.ts's apply()). A second
   * call while already loaded, or while a first call is still in
   * flight, is a no-op/joins the same promise -- check() can run more
   * than once per session (e.g. #677's sessions row) and must not
   * re-fetch or re-run the migration each time. If the previous attempt
   * failed, though, this one tries the fetch again rather than standing
   * by the earlier failure forever -- #677's periodic re-check is what
   * actually drives that retry in the running app. */
  async ensureLoaded(): Promise<void> {
    if (this.loadSucceeded) return
    if (!this.loading) this.loading = this.load()
    try {
      await this.loading
    } finally {
      this.loading = null
    }
  }

  private async load(): Promise<void> {
    let record: PreferencesRecord
    try {
      record = await fetchMyPreferences()
    } catch {
      // Unreachable API: fall back to defaults for this session rather
      // than blocking sign-in on it, but loadSucceeded stays false --
      // flush() (below) then refuses to send anything, and the next
      // ensureLoaded() retries the fetch, rather than either wiping the
      // rest of the record with a save built from these defaults or
      // caching this one failure forever. `this.loaded` itself is only
      // set (and the hydrators only run) the first time through, so a
      // later failed retry doesn't re-clear whatever this session has
      // built up meanwhile. Migration is skipped here for the same
      // reason it always was: with no honest answer for "is the server
      // record empty", upload-then-delete could throw away the only
      // copy of a browser's presets against a record that turns out not
      // to have been empty at all.
      if (!this.loaded) {
        this.prefs = {}
        this.loaded = true
        for (const [key, hydrate] of this.hydrators) hydrate(undefined)
      }
      return
    }
    let prefs = record.prefs ?? {}
    if (Object.keys(prefs).length === 0) {
      const migrated = migrateLegacyLocalPreferences(record.userId)
      if (migrated) {
        prefs = migrated
        try {
          // saveMyPreferences resolves to an error string rather than
          // throwing on a non-2xx response (see api.ts's send()), so a
          // failed PUT has to be read from the return value, not a
          // catch -- checking only for a thrown exception would clear
          // the legacy keys after a refused write too.
          const err = await saveMyPreferences({ version: VERSION, prefs })
          if (!err) clearLegacyLocalPreferences()
        } catch {
          // A genuinely thrown failure (e.g. the request never reached
          // send() at all). Best effort either way: the legacy keys are
          // left in place and migration is retried next sign-in rather
          // than losing the only copy. The migrated values still apply
          // to this session in memory (below) regardless.
        }
      }
    }
    // Any key already changed locally (set() calls made while a prior
    // attempt was still failing) takes priority over what the server
    // just returned -- it hasn't reached the server yet, and this being
    // the first successful load must not silently drop it.
    for (const key of this.changedKeys) prefs[key] = this.prefs[key]
    this.prefs = prefs
    this.loaded = true
    this.loadSucceeded = true
    for (const [key, hydrate] of this.hydrators) hydrate(this.prefs[key])
    // Now that there is a real baseline, send anything that was only
    // sitting in memory because flush() was refusing to run without one.
    if (this.changedKeys.size > 0) void this.flush()
  }

  get<T>(key: string): T | undefined {
    return this.prefs[key] as T | undefined
  }

  /** Every module's one write path. Updates the in-memory record at
   * once (a caller reading get() straight back sees its own write) and
   * schedules a debounced PATCH of the keys changed since the last
   * flush. */
  set(key: string, value: unknown): void {
    this.prefs = { ...this.prefs, [key]: value }
    this.changedKeys.add(key)
    if (this.timer) clearTimeout(this.timer)
    this.timer = setTimeout(() => {
      void this.flush()
    }, DEBOUNCE_MS)
  }

  /** Sends whatever is pending right now, skipping the debounce --
   * called on sign-out (before the server call that ends the session,
   * see auth.svelte.ts's logout()) and from the pagehide handler below,
   * both places where waiting another 500ms may mean never. A no-op
   * when nothing has changed since the last flush, and also a no-op
   * before ensureLoaded() has ever actually succeeded -- a change made
   * during a fallback session (load() unreachable) is kept in memory
   * and sent once a retry lands (see load()'s success path), not sent
   * blind against a baseline this session never actually saw. */
  async flush(opts: { keepalive?: boolean } = {}): Promise<void> {
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = undefined
    }
    if (!this.loadSucceeded || this.changedKeys.size === 0) return
    // Cleared before sending, not after -- matching every other
    // storage-write catch below: best effort, applied optimistically,
    // not retried forever if the server refuses it.
    const keys = [...this.changedKeys]
    this.changedKeys.clear()
    const patch: Record<string, unknown> = {}
    for (const key of keys) patch[key] = this.prefs[key]
    try {
      await saveMyPreferences({ version: VERSION, prefs: patch }, opts)
    } catch {
      // Best effort, matching every other storage-write catch in this
      // codebase -- the change still applies to this session in memory,
      // it just may not survive a sign-out/sign-in on this shape.
    }
  }

  /** Test seam: behaves like a completed ensureLoaded() using `prefs`
   * directly -- no network call, no migration attempt -- and hydrates
   * every module already registered. Lets a module's own test seed "a
   * fresh reader with X already saved" the same way it used to seed
   * localStorage ahead of a vi.resetModules() + re-import, since that
   * re-import's constructor calls register(), which (see register()
   * above) hydrates immediately once `loaded` is already true. */
  seedForTest(prefs: Record<string, unknown>): void {
    this.prefs = { ...prefs }
    this.loaded = true
    this.loadSucceeded = true
    this.loading = null
    this.changedKeys.clear()
    for (const [key, hydrate] of this.hydrators) hydrate(this.prefs[key])
  }

  /** Drops the record from memory on sign-out (#1283's own "cleared from
   * memory" requirement) -- called from auth.svelte.ts's
   * clearSessionState(), after logout() has already flushed. Does not
   * itself flush: a caller that needs the pending write sent first
   * (sign-out) does so explicitly, since reset() alone cannot know
   * whether the session is still good for one more request. */
  reset(): void {
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = undefined
    }
    this.prefs = {}
    this.loaded = false
    this.loadSucceeded = false
    this.loading = null
    this.changedKeys.clear()
  }
}

export const preferencesState = new PreferencesState()

// Every legacy key this issue moves off localStorage. All eight start
// with 'mikroview' (as either 'mikroview-' or 'mikroview:'), which is
// also the sweep clearLegacyLocalPreferences() below uses -- listed here
// individually only because migrateLegacyLocalPreferences needs to read
// each one by its own name and reshape it into the new record's keys.
const LEGACY_KEYS = {
  presets: 'mikroview-filter-presets',
  topTalkers: 'mikroview-top-talker-widgets',
  altitudeStop: 'mikroview:topography-altitude',
  columnWidths: 'mikroview-column-widths-v8',
  columnVisibility: 'mikroview-column-visibility-v1',
  groupMode: 'mikroview:group',
  retention: 'mikroview-max-age-seconds',
  metrics: 'mikroview-metrics-view',
  deckOrder: 'mikroview-deck-order',
} as const

// Records which account's sign-in first attempted the migration below,
// on a shared browser where a v0.6.0 install left the nine legacy keys
// behind for whoever signs in next (#1283 round 2): without this, a
// second account signing in after the first account's migration failed
// -- or even after it succeeded but before its own clear ran -- could
// see the same "server record empty, legacy keys present" state and
// walk off with the first account's presets. This key is itself
// mikroview*-prefixed, so clearLegacyLocalPreferences()'s blanket sweep
// removes it along with everything else once a migration actually lands.
const MIGRATION_OWNER_KEY = 'mikroview-prefs-migration-owner'

function migrationOwner(): string | undefined {
  return readRaw(MIGRATION_OWNER_KEY)
}

function claimMigrationFor(userID: string): void {
  try {
    localStorage.setItem(MIGRATION_OWNER_KEY, userID)
  } catch {
    // storage unavailable -- nothing to claim, and migrateLegacyLocalPreferences
    // already returned null for the same reason before reaching here.
  }
}

function readRaw(key: string): string | undefined {
  try {
    return localStorage.getItem(key) ?? undefined
  } catch {
    return undefined
  }
}

function readJSON(key: string): unknown {
  const raw = readRaw(key)
  if (raw === undefined) return undefined
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

/**
 * One-time migration, kept for one release (v0.6.1) and then removed:
 * if this browser still has any of the nine old localStorage keys and
 * the server's own record came back empty, build a record from
 * whatever is present and hand it to load() above to upload. Each
 * module's own hydrate() (registered via register()) still does the
 * real validation/defaulting on the way in -- exactly the sanitising
 * loadInitial() used to do reading straight from localStorage -- so
 * this only has to reshape raw storage values into the record's key
 * names, not validate them.
 *
 * Bound to userID, the account this load() is running for (its own
 * server-assigned id, off the GET response): the first account to reach
 * here with legacy keys present claims them (claimMigrationFor), and
 * only that same account's own later retries may still use them. Any
 * other account finds the keys present but MIGRATION_OWNER_KEY already
 * naming someone else, and leaves them alone -- a shared browser must
 * not hand the first operator's presets to the next one who signs in.
 *
 * Returns null when nothing legacy is present at all (a fresh install,
 * or a browser that has already migrated and had its keys cleared), or
 * when this account isn't the one the keys are bound to.
 */
function migrateLegacyLocalPreferences(userID: string | undefined): Record<string, unknown> | null {
  let anyPresent: boolean
  try {
    anyPresent = Object.values(LEGACY_KEYS).some((k) => localStorage.getItem(k) != null)
  } catch {
    return null
  }
  if (!anyPresent) return null

  // No id to bind to (shouldn't happen -- the server always sets it) is
  // treated the same as "claimed by someone else": safer to leave the
  // keys alone than to guess who they belong to.
  if (!userID) return null
  const owner = migrationOwner()
  if (owner !== undefined && owner !== userID) return null
  claimMigrationFor(userID)

  const prefs: Record<string, unknown> = {}

  const presets = readJSON(LEGACY_KEYS.presets)
  if (presets !== undefined) prefs.presets = presets

  const topTalkers = readJSON(LEGACY_KEYS.topTalkers)
  if (topTalkers !== undefined) prefs.topTalkers = topTalkers

  const altitudeStop = readRaw(LEGACY_KEYS.altitudeStop)
  if (altitudeStop !== undefined) prefs.altitudeStop = altitudeStop

  const widths = readJSON(LEGACY_KEYS.columnWidths)
  const visible = readJSON(LEGACY_KEYS.columnVisibility)
  if (widths !== undefined || visible !== undefined) prefs.columns = { widths, visible }

  const groupMode = readRaw(LEGACY_KEYS.groupMode)
  if (groupMode !== undefined) prefs.groupMode = groupMode === '1'

  const retention = readRaw(LEGACY_KEYS.retention)
  if (retention !== undefined) prefs.retention = retention === 'null' ? null : Number(retention)

  const metrics = readRaw(LEGACY_KEYS.metrics)
  if (metrics !== undefined) prefs.metrics = metrics

  const deckOrder = readJSON(LEGACY_KEYS.deckOrder)
  if (deckOrder !== undefined) prefs.deckOrder = deckOrder

  return prefs
}

/** Deletes every mikroview*-prefixed localStorage key, not just the nine
 * named above -- a blanket sweep is simpler than keeping this list in
 * step with LEGACY_KEYS and is what the issue itself asks for ("delete
 * every mikroview* key"). Only ever localStorage: the wizard's minted
 * history key and the sign-out beat flag both live in sessionStorage,
 * a different object entirely, and are untouched by this. */
function clearLegacyLocalPreferences(): void {
  try {
    for (const k of Object.keys(localStorage)) {
      if (k.startsWith('mikroview')) localStorage.removeItem(k)
    }
  } catch {
    // storage unavailable -- nothing to clear
  }
}

// Fired when the tab is closing or being backgrounded for good (unlike
// 'beforeunload', 'pagehide' also covers mobile Safari's app-switch
// suspend, which never fires the former) -- the one moment a debounced
// write sitting inside its 500ms window would otherwise be lost for
// good. keepalive is what lets the request outlive the page that sent
// it, the same guarantee sendBeacon gives (see api.ts's putJSON).
if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', () => {
    void preferencesState.flush({ keepalive: true })
  })
}
