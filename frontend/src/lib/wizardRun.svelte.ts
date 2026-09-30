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

import { createToken } from './api'
import { appState } from './state.svelte'
import { fallState, laneColors } from './fall.svelte'
import { TITLES } from './setupsteps'
import { wizardState } from './wizard.svelte'
import { wizardJourney } from './wizardJourney.svelte'
import {
  addrProblem,
  arrivedAll,
  freshAnswers,
  latestArrivalHeadline,
  refusedFixBlock,
  routerDone,
  trackStations,
  type Evidence,
  type LatestArrival,
  type RunAnswers,
  type Stage,
  type TrackStation,
} from './wizardRun'
import type { TuneRule } from './wizardTune'

// blockSection is one numbered, titled part of the paste-once block
// (DESIGN.md, "3 · Paste once"): a title from TITLES (the same
// vocabulary the rail and the fleet ledger already use) and the
// commands the server rendered for it. Built, never hand-written --
// see WizardRun.blockSections' own comment.
export interface BlockSection {
  id: 'ca' | 'push' | 'backup' | 'syslog'
  title: string
  commands: string
}

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
  // copiedAt is the track's own "copied" station timestamp (#1383): a
  // client-side moment, not a server receipt, because Copy itself is
  // never something the server witnesses.
  copiedAt = $state('')
  tuneCopied = $state(false)
  tuneSkipped = $state(false)
  chosenCount = $state(0)
  tunedAt = $state('')
  // The rules the tagging block was copied for (#1384): what the rail's
  // receipt counts, and what the ledger's Undo for tagged rules lists.
  tuneChosen = $state<TuneRule[]>([])
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

  // blockSections is the paste-once block (DESIGN.md, "3 · Paste
  // once"): the certificate, the push and the backup where they were
  // said yes to, and the logging step last -- last because its own
  // commands (SyslogCommands, internal/routeros/commands.go) are the
  // ones ending in the enrol line, so keeping it last is what makes
  // "the enrol line last" true without slicing the server's own text
  // apart. push's section is the schedule step's commands, not push's
  // own -- schedule has carried the whole hand-over (script, scheduler,
  // one run now) as a single block since #1131; push stays the bare
  // script body for the Engine Room's re-key affordance, which nothing
  // here needs. backup's section joins backup and backupSchedule, the
  // one place this build concatenates two of the server's blocks
  // itself -- both are rendered text already, so this is not writing
  // RouterOS, only laying two blocks under one heading.
  //
  // A section with no commands yet (still loading, or its own
  // precondition unmet) is left out rather than shown half-formed,
  // the same contract every blank CommandStep already keeps.
  get blockSections(): BlockSection[] {
    const steps = wizardState.commands?.steps
    if (!steps) return []
    const sections: BlockSection[] = [{ id: 'ca', title: TITLES.ca, commands: steps.caTrust.commands }]
    if (this.push === true) sections.push({ id: 'push', title: TITLES.push, commands: steps.schedule.commands })
    if (this.backup === true) {
      const commands = [steps.backup.commands, steps.backupSchedule.commands].filter(Boolean).join('\n')
      sections.push({ id: 'backup', title: TITLES.backup, commands })
    }
    sections.push({ id: 'syslog', title: TITLES.syslog, commands: steps.syslog.commands })
    return sections.filter((s) => s.commands)
  }

  // blockText is what Copy puts on the clipboard and the folded `pre`
  // shows: every section numbered and titled, in order.
  get blockText(): string {
    return this.blockSections.map((s, i) => `# ${i + 1} · ${s.title}\n${s.commands}`).join('\n')
  }

  get trackStations(): TrackStation[] {
    return trackStations(this.answers, this.evidence, this.copiedAt)
  }

  get latestArrival(): LatestArrival | null {
    return latestArrivalHeadline(this.trackStations, this.evidence, this.name)
  }

  // reviewedVersion is the dialect table's own "last checked against"
  // bound (SetupCommandsResponse.routeros.newest) -- the (a.b) the
  // ahead-of-review caution box quotes.
  get reviewedVersion(): string {
    return wizardState.commands?.routeros.newest ?? ''
  }

  // refusedFixWithEnrol is the refused-sender box's copyable fix: the
  // syslog step's own guarded action line plus the enrol line, read
  // from whatever the server most recently rendered.
  get refusedFixWithEnrol(): string {
    return refusedFixBlock(wizardState.commands?.steps.syslog.commands ?? '')
  }

  reset() {
    Object.assign(this, freshAnswers())
    this.copiedAt = ''
    this.tuneChosen = []
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
  // every time). A token is minted for a named router, so a walk that
  // has no router record yet makes one here first (POST /api/devices,
  // as the retired modal's name step did); a walk opened for a router
  // that exists (Re-enrol…) already has one. The record's refusal
  // reads where the mint's would.
  private minting = false
  async mint() {
    if (!this.pass || this.minting) return
    this.minting = true
    try {
      if (!wizardState.ledgerDevice) {
        const refused = await wizardState.createRouter(this.name.trim())
        if (refused !== null) {
          this.pass = ''
          wizardState.enrolmentError = refused
          return
        }
      }
      wizardState.enrolExpectedAddress = this.addr
      wizardState.enrolPassword = this.pass
      this.pass = ''
      await wizardState.mintEnrolmentToken()
      if (wizardState.enrolment) {
        await this.ensureIngestToken()
        this.stage = 'paste'
      }
    } finally {
      this.minting = false
    }
  }

  // ensureIngestToken mints the persistent ingest token
  // (internal/api/ingest.go's bearer, distinct from the one-shot
  // enrolment marker mintEnrolmentToken makes) that the paste block's
  // push and backup sections need to authenticate the router's own
  // posts -- see routeros.PushScript/BackupScript, embedded in
  // steps.schedule/steps.backup by POST /api/setup/commands.
  //
  // Called only from inside Mint and Reroll -- the password-checked
  // act -- never from a form toggle on The router: an API credential
  // must not be created by ticking Yes before the password step even
  // runs. If push and backup were both No at the moment of minting and
  // the operator later goes Back and answers one Yes, the rail lets
  // The router be revisited until the paste lands (railRows' `can`),
  // and reaching Mint again calls this again -- so no separate path is
  // needed for that case, only that Back-and-forward keep landing on
  // Mint rather than skipping it (StepMint's own Enter/click always
  // call mint() afresh, whatever wizardState.enrolment already holds).
  // A no-op once a token already stands for this device; POST
  // /api/tokens writes its own audit line, so nothing else is recorded
  // here. Best-effort: a failure here leaves the push/backup sections
  // blank rather than failing the mint that already succeeded.
  private async ensureIngestToken(): Promise<void> {
    if (this.push !== true && this.backup !== true) return
    const device = wizardState.ledgerDevice
    if (!device) return
    if (wizardState.token && wizardState.tokenDevice === device) return
    try {
      const result = await createToken(`setup-${device}`, 'ingest', device)
      if (typeof result === 'string' || !result.value) return
      wizardState.token = result.value
      wizardState.tokenDevice = device
      await wizardState.refreshCommands({ device, token: result.value })
    } catch {
      // See the doc comment above: best-effort.
    }
  }

  // Copy on the paste step: the router's turn begins.
  markCopied() {
    this.copied = true
    this.copiedAt = new Date().toISOString()
    this.stage = 'watch'
  }

  // Reroll is Mint again for the same router and address (#1291: it
  // asks for the password every time, because minting is what opens
  // the port). Only reachable before Copy -- the paste step's own
  // copyrow is what offers it -- so this never touches `stage`. Also
  // ensures the ingest token, the same as Mint: a first attempt that
  // failed or was skipped (push/backup answered after the fact is not
  // reachable here, but a transient failure is) gets another chance.
  async reroll(password: string): Promise<void> {
    wizardState.enrolExpectedAddress = this.addr
    wizardState.enrolPassword = password
    await wizardState.mintEnrolmentToken()
    if (wizardState.enrolment) await this.ensureIngestToken()
  }

  // "enrol at <other> instead" (DESIGN.md, "Refused sender"; #1370,
  // #1373) is the same act as Mint and Reroll above, pointed at the
  // address that actually sent -- not a new endpoint: mintEnrolment
  // (POST /api/devices/{id}/enrolment) re-checks the password every
  // time, writes the one audit line every mint does, and never sets an
  // accepted address itself -- that still only happens when a router
  // presents the fresh token it mints here. Once it succeeds,
  // evidence.refused reads empty on its own: refusedSince keeps only
  // addresses first seen after enrolmentMintedAt, which this call just
  // moved to now.
  async enrolAtOther(password: string): Promise<void> {
    const other = this.evidence.refused
    if (!other) return
    wizardState.enrolExpectedAddress = other
    wizardState.enrolPassword = password
    await wizardState.mintEnrolmentToken()
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
    // The way out (#1386): Finish plays the journey onto the fall, and
    // the hand-over -- show the landing, close the wizard -- happens under
    // its cover. Where it cannot play (reduced motion, no canvas), the
    // hand-over happens outright.
    const swap = () => {
      appState.view = wizardState.finishTo === 'fleet' ? 'fleet' : 'fall'
      wizardState.close()
    }
    if (!wizardJourney.wayOut(swap)) swap()
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
