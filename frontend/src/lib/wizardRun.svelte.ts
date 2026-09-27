// SPDX-License-Identifier: AGPL-3.0-only

// The full-screen wizard's run (#1381): the answers this walk has given,
// and the evidence the server holds, read into the shape wizardRun.ts's
// pure functions take. wizardState (wizard.svelte.ts) stays the owner
// of everything server-side -- the ledger, the devices, the minted
// token, the refused senders, the backups -- and is what every door
// (Run setup…, Add a router, Re-enrol…) already drives; this module is
// the run's own memory on top of it, and is reset every time the
// wizard opens.
//
// Nothing here connects to a router (the AGENTS.md invariant): every
// field of `evidence` is a reading of what has arrived.

import { appState } from './state.svelte'
import { fallState, laneColors } from './fall.svelte'
import { wizardState } from './wizard.svelte'
import {
  addrProblem,
  arrivedAll,
  freshAnswers,
  routerDone,
  type Evidence,
  type RunAnswers,
  type Stage,
} from './wizardRun'

// The token's expiry as the paste step's line shows it (14:17).
function hm(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit' })
}

class WizardRun {
  stage = $state<Stage>('ask')
  q = $state(0)
  name = $state('')
  addr = $state('')
  // The password, held only between typing and Mint: wizardState takes
  // it for the one call and clears its own copy either way, and this
  // copy goes with it.
  pass = $state('')
  push = $state<boolean | null>(null)
  backup = $state<boolean | null>(null)
  copied = $state(false)
  tuneCopied = $state(false)
  tuneSkipped = $state(false)
  chosenCount = $state(0)
  tunedAt = $state('')
  undoOpen = $state<string | null>(null)
  showUndoAll = $state(false)

  // decodedAtCopy is how many lines carried a decoded action when the
  // tagging block was copied: the rules step counts upward from there,
  // so "tagged" is the first new prefix landing, not the old ones.
  private decodedAtCopy = 0

  // The live rate on the bar's right ("live · 7/s"): lines per second
  // between two polls, never a guess.
  rate = $state(0)
  private rateLines = -1
  private rateAt = 0

  get answers(): RunAnswers {
    return {
      stage: this.stage,
      q: this.q,
      name: this.name,
      addr: this.addr,
      pass: this.pass,
      push: this.push,
      backup: this.backup,
      copied: this.copied,
      tuneCopied: this.tuneCopied,
      tuneSkipped: this.tuneSkipped,
      chosenCount: this.chosenCount,
      tunedAt: this.tunedAt,
    }
  }

  get addrProblem(): string {
    return addrProblem(this.addr)
  }

  get routerDone(): boolean {
    return routerDone(this.answers)
  }

  // decoded is the count the rules step reads: events whose action came
  // from a log-prefix, across the router this run is about.
  private get decoded(): number {
    const st = wizardState.status
    if (!st) return 0
    const id = wizardState.ledgerDevice
    return st.devices.filter((d) => !id || d.device === id).reduce((n, d) => n + d.decodedActions, 0)
  }

  get evidence(): Evidence {
    const st = wizardState.status
    const id = wizardState.ledgerDevice
    const dev = id ? wizardState.devices.find((x) => x.id === id) : undefined
    // The enrol line is this router's own arrival (#1281); a walk that
    // has no router record yet reads the fleet's first open syslog
    // source, as the ledger always did.
    let enrol = dev?.acceptedIp ? (dev.enrolledAt ?? '') : ''
    let from = dev?.acceptedIp ?? ''
    if (!enrol && !id && st) {
      const seen = st.sources.find((s) => s.syslogFirstSeenAt)
      if (seen) {
        enrol = seen.syslogFirstSeenAt ?? ''
        from = seen.source
      }
    }
    const sources = st?.sources ?? []
    const certSrc =
      sources.find((s) => s.caFetchedAt && (s.source === from || s.source === this.addr)) ??
      sources.find((s) => s.caFetchedAt)
    let push = ''
    let lines = 0
    for (const d of st?.devices ?? []) {
      if (id && d.device !== id) continue
      lines += d.events
      for (const at of Object.values(d.pushedKinds ?? {})) if (at > push) push = at
    }
    let backup = ''
    for (const r of wizardState.backups?.routers ?? []) {
      if (id && r.device !== id) continue
      const g = r.generations[r.generations.length - 1]
      const at = r.lastArrival || g?.backupArrivedAt || g?.rscArrivedAt || ''
      if (at > backup) backup = at
    }
    const lanes = laneColors(fallState.boundaries)
    return {
      cert: certSrc?.caFetchedAt ?? '',
      enrol,
      push,
      backup,
      from,
      lines,
      refused: wizardState.refusedForThisWalk[0]?.ip ?? '',
      version: dev?.routerosVersion ?? '',
      ahead: dev?.routerosStanding === 'ahead-of-review',
      tagged: this.tuneCopied && this.decoded > this.decodedAtCopy,
      tokenUntil: hm(wizardState.enrolment?.expiresAt ?? ''),
      boundaries: fallState.boundaries.map((b) => ({
        lane: lanes.get(b.key) ?? '',
        watched: b.coverage === 'observed',
        dark: b.coverage === 'dark',
      })),
    }
  }

  get arrivedAll(): boolean {
    return arrivedAll(this.answers, this.evidence)
  }

  reset() {
    Object.assign(this, freshAnswers())
    this.undoOpen = null
    this.showUndoAll = false
    this.decodedAtCopy = 0
    this.rate = 0
    this.rateLines = -1
    this.rateAt = 0
  }

  // begin is the run's start, every time the wizard opens: the answers
  // go back to fresh, then what the server already holds is read in so
  // reopening shows the ledger as it stands. A router the door named
  // (Re-enrol…, Finish registering…) fills the name and the address; a
  // router already sending lands on the router's turn, or on the
  // ledger once everything that stands has arrived.
  begin() {
    this.reset()
    const dev = wizardState.ledgerDevice
      ? wizardState.devices.find((x) => x.id === wizardState.ledgerDevice)
      : undefined
    if (dev) {
      this.name = dev.name || dev.id
      this.addr = dev.acceptedIp ?? ''
    }
    const ev = this.evidence
    if (ev.enrol) {
      // What stands is what was chosen: a push or a backup that never
      // arrived is not waited for on a reopen.
      this.push = !!ev.push
      this.backup = !!ev.backup
      this.copied = true
      this.stage = this.arrivedAll ? 'done' : 'watch'
      return
    }
    if (wizardState.enrolment) {
      this.stage = 'paste'
      return
    }
    if (this.routerDone) this.q = 4
  }

  // ---- the footer's acts, as wizard.js's click handler has them ----

  routerNext() {
    if (this.routerDone) this.q = 4
  }

  // An earlier completed row, while the run is still yours to change.
  gotoStep(i: number) {
    if (this.stage !== 'ask' && this.stage !== 'paste') return
    this.stage = 'ask'
    this.q = i === 0 ? 0 : 4
    if (i === 2) this.stage = 'paste'
  }

  // Mint the token: the password is spent on the one call, whatever
  // the answer (#1291: minting is what opens the port, so it asks
  // every time). The router record itself is the router step's act.
  async mint() {
    if (!this.pass) return
    wizardState.enrolExpectedAddress = this.addr
    wizardState.enrolPassword = this.pass
    this.pass = ''
    await wizardState.mintEnrolmentToken()
    if (wizardState.enrolment) this.stage = 'paste'
  }

  // Copy on the paste step: the router's turn begins.
  markCopied() {
    this.copied = true
    this.stage = 'watch'
  }

  toTune() {
    if (this.arrivedAll) this.stage = 'tune'
  }

  // Copy on the rules step: the count starts from here.
  markTuneCopied(chosen: number) {
    this.chosenCount = chosen
    this.decodedAtCopy = this.decoded
    this.tuneCopied = true
  }

  tuneSkip() {
    this.tuneSkipped = true
    this.stage = 'done'
  }

  toDone() {
    if (this.evidence.tagged) this.stage = 'done'
  }

  // Add another router: the same wizard at The router, for a new one.
  another() {
    wizardState.openAddRouter()
    this.begin()
  }

  // Finish leads out: to the fleet when the walk was opened from
  // there, to the fall when it was opened from setup (the record's rule).
  finish() {
    appState.view = wizardState.finishTo === 'fleet' ? 'fleet' : 'fall'
    wizardState.close()
  }

  // poll is the per-tick bookkeeping: the live rate, and the moment the
  // first tagged line lands.
  poll() {
    const ev = this.evidence
    const now = Date.now()
    if (this.rateLines >= 0 && now > this.rateAt) {
      this.rate = Math.max(0, Math.round(((ev.lines - this.rateLines) * 1000) / (now - this.rateAt)))
    }
    this.rateLines = ev.lines
    this.rateAt = now
    if (ev.tagged && !this.tunedAt) {
      this.tunedAt = new Date().toLocaleTimeString(undefined, { hour12: false })
    }
  }
}

export const wizardRun = new WizardRun()
