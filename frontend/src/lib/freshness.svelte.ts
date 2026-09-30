// SPDX-License-Identifier: AGPL-3.0-only
//
// #1363: after a server upgrade, an already-open tab keeps running the
// bundle it loaded with -- the first load after the upgrade is served
// the browser's old cache and only then finds the new service worker
// (main.go's staticCacheHeaders). This is the comparison and the
// reload/banner decision the ratified 2026-09-30 design calls for.
//
// Baseline is __MIKROVIEW_VERSION__, the version this bundle was built
// with (vite.config.ts's `define`, from the same VERSION the Dockerfile
// stamps into the Go binary) -- not lib/version.svelte.ts's cached first
// answer. That cache is "fetched once, since it only changes on
// restart" by design; a tab opened after the upgrade would load with
// the new bundle and cache the new version immediately, so comparing
// against it would never see a difference to notice.
import { fetchHealthz } from './api'
import { authState, pageReload } from './auth.svelte'
import { configEditorState } from './configEditor.svelte'
import { dossierState } from './dossier.svelte'
import { leaveGuard } from './leaveGuard.svelte'
import { activateWaitingWorker } from './serviceWorker'
import { appState } from './state.svelte'
import { wanDoorsState } from './wanDoors.svelte'
import { wizardState } from './wizard.svelte'
import { wizardJourney } from './wizardJourney.svelte'

const CHECK_INTERVAL_MS = 60_000

// Tab-scoped, like the wizard's own history key and auth's
// justSignedOut flag: a reload has to remember which version it already
// tried, and a fresh tab (a different one, or this one after being
// closed and reopened) gets its own one automatic attempt again.
const RELOADED_FOR_KEY = 'mikroview.freshnessReloadedFor'

/**
 * True while the field the document currently has focus in holds
 * something -- the one busy signal that is not a store anywhere, since
 * nothing about typing itself is worth making reactive. Checked only at
 * the moment a mismatch is found, not continuously.
 */
function focusedFieldHasContent(doc: Pick<Document, 'activeElement'> = document): boolean {
  const el = doc.activeElement
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) return el.value.length > 0
  if (el instanceof HTMLElement && el.isContentEditable) return (el.textContent ?? '').trim().length > 0
  return false
}

class FreshnessState {
  // The line, once shown, is the end state (owner, 2026-09-30, "7a"):
  // it waits for a click and never reloads on its own after that,
  // whatever becomes of the busy reasons that put it up. Every check
  // below returns immediately once this is true.
  banner = $state(false)

  /** Set from main.ts once registerServiceWorker's promise settles. */
  registration: ServiceWorkerRegistration | undefined

  private started = false
  private interval: ReturnType<typeof setInterval> | null = null
  private checking = false
  // The fetch-error trigger fires at most once per tab (build notes:
  // "once, without repeats") -- an outage's burst of failing requests
  // must not turn into one healthz call per request.
  private erroredCheckSpent = false

  setRegistration(registration: ServiceWorkerRegistration | undefined): void {
    this.registration = registration
  }

  /** Starts the 60-second poll and does one immediate check. Idempotent. */
  start(): void {
    if (this.started) return
    this.started = true
    void this.checkNow()
    this.interval = setInterval(() => void this.checkNow(), CHECK_INTERVAL_MS)
  }

  stop(): void {
    if (this.interval !== null) clearInterval(this.interval)
    this.interval = null
    this.started = false
  }

  /** The shared fetch path's hook (api.ts, wired from main.ts) for a
   * 401/403/404/5xx that could mean the server was upgraded mid-session. */
  checkOnErrorSignal(): void {
    if (this.erroredCheckSpent) return
    this.erroredCheckSpent = true
    void this.checkNow()
  }

  async checkNow(): Promise<void> {
    if (this.banner) return
    if (this.checking) return
    this.checking = true
    try {
      const healthz = await fetchHealthz()
      await this.handle(healthz.version)
    } catch {
      // healthz unreachable says nothing about freshness either way --
      // leave it for the next trigger (the 60s poll if nothing else).
    } finally {
      this.checking = false
    }
  }

  /** Every "busy" source the ratified design lists, ORed together. The
   * stream's own connection state is deliberately not one of them --
   * only the operator actively holding or reading it is. */
  private busy(): boolean {
    return (
      appState.streamHeld ||
      leaveGuard.held ||
      focusedFieldHasContent() ||
      dossierState.isOpen ||
      wanDoorsState.isOpen ||
      authState.showSSOLink ||
      authState.showChangePassword ||
      wizardState.open ||
      wizardJourney.active ||
      configEditorState.visible
    )
  }

  private async handle(serverVersion: string): Promise<void> {
    if (serverVersion === __MIKROVIEW_VERSION__) return

    if (this.busy()) {
      this.banner = true
      return
    }

    if (sessionStorage.getItem(RELOADED_FOR_KEY) === serverVersion) {
      // Already spent the one automatic reload for this version and
      // came back still mismatched -- banner only from here.
      this.banner = true
      return
    }

    sessionStorage.setItem(RELOADED_FOR_KEY, serverVersion)
    await this.reloadNow()
  }

  /** The safe-reload mechanics: let the new worker take over, then
   * reload -- shared by the automatic path above and the banner's own
   * "reload" click, so a reload is a reload everywhere it happens. */
  async reloadNow(): Promise<void> {
    await activateWaitingWorker(this.registration)
    pageReload.now()
  }
}

export const freshnessState = new FreshnessState()
