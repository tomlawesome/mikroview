// SPDX-License-Identifier: AGPL-3.0-only

// The setup wizard's modal state (#487), built from
// docs/design/screens/wizard/DESIGN.md -- the ratified record; where it
// and a mockup disagree, the record wins.
//
// Its own module rather than a corner of appState, for the same reason
// the wizard stopped being a view: it is no longer a page the app
// navigates to, it is a modal over whatever page is already there. A
// flag on the view state would have made it look like navigation again.
//
// What lives here is only what outlives the modal being mounted: whether
// it is open, which step it is on, and the ledger it reads. Everything
// the modal fetches while open belongs to the component and stops with
// it.

import { fetchDevices, fetchSetupCommands, fetchSetupStatus, markSetupStep } from './api'
import { buildLedger, firstOpenStep, silenceExplanation, STEP_COUNT } from './setupsteps'
import type { Device, SetupCommandsResponse, SetupMark, SetupStatus } from './types'

// FINISH_PANE is the pane after the last step -- the ledger read back.
// One past the count rather than a separate flag, so "which pane" stays
// a single number and Back from the finish lands on step 5.
export const FINISH_PANE = STEP_COUNT + 1

class WizardState {
  open = $state(false)
  // 1..STEP_COUNT for a step, FINISH_PANE for the finish.
  pane = $state(1)
  status = $state<SetupStatus | null>(null)
  devices = $state<Device[]>([])
  error = $state<string | null>(null)
  // The small-screen sheet's body <-> ledger flip. Held here rather than
  // in the component so it survives a step change, which is what makes
  // "Show setup steps" a place you can stay rather than a peek.
  showStepList = $state(false)

  // pickedVersion (#436) is the operator's choice from the "Your
  // RouterOS version" pick-list -- '' means the first option, "Not
  // sure", which omits `version` from the request entirely rather than
  // sending an empty string. Held here rather than in the component so
  // it survives a step change; a module-lifetime field, not persisted
  // anywhere, per the owner's "session only".
  pickedVersion = $state('')

  // commands is the last response from POST /api/setup/commands: the
  // rendered command blocks, the dialect table the pick-list lists, and
  // the router-standing warning data. null until the first fetch lands.
  commands = $state<SetupCommandsResponse | null>(null)
  commandsError = $state<string | null>(null)

  // autoLaunched guards the record's "auto-launch, once". Deliberately a
  // module-lifetime flag and not persisted anywhere: the record says the
  // wizard is stateless beyond the evidence and that "finished" is not
  // stored, so the only honest client-side memory is "this app instance
  // has already offered it". Reopening after that is the operator's own
  // choice, through Run setup….
  private autoLaunched = false

  get address(): string {
    return window.location.host
  }

  // ledger is the five steps as they currently stand. Empty until the
  // first status arrives, so callers can render a loading state without
  // a second flag.
  get ledger() {
    if (!this.status) return []
    return buildLedger(this.status, this.devices, this.address)
  }

  get marks(): SetupMark[] {
    return this.status?.marks ?? []
  }

  // silence is what a surface with nothing to show says about why -- the
  // reach of "the record is the feature" past the modal itself. Null
  // when the ledger explains nothing.
  get silence(): string | null {
    return silenceExplanation(this.marks)
  }

  // refresh reads the ledger back from the server. Called on a timer
  // while the modal is open, and once on sign-in so the surfaces that
  // explain their own silence have something to explain it with.
  async refresh(): Promise<void> {
    try {
      const [s, d] = await Promise.all([fetchSetupStatus(), fetchDevices()])
      this.status = s
      this.devices = d
      this.error = null
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e)
    }
  }

  // refreshCommands re-renders the wizard's RouterOS command blocks from
  // the server (#436), keyed to whatever this session currently knows:
  // the instance address and syslog port, the push kinds, the operator's
  // picked version if any, and -- once step 4 has minted one -- the
  // token. Callers pass the token explicitly rather than this holding
  // it, since it lives in the component (created on step 4 entry, never
  // stored here).
  async refreshCommands(opts: { token?: string } = {}): Promise<void> {
    if (!this.status) return
    const result = await fetchSetupCommands({
      address: this.address,
      syslogPort: this.status.instance.syslogPort,
      kinds: this.status.pushKinds,
      token: opts.token || undefined,
      version: this.pickedVersion || undefined,
    })
    if (typeof result === 'string') {
      this.commandsError = result
      return
    }
    this.commandsError = null
    this.commands = result
  }

  // launch opens the ledger at the first step still waiting. Evidence
  // that arrived while it was closed is already green, because the
  // ledger is rebuilt from the server's observations every time.
  launch() {
    this.pane = firstOpenStep(this.ledger)
    this.showStepList = false
    this.open = true
  }

  close() {
    this.open = false
  }

  // maybeAutoLaunch is the record's first-run rule: first admin sign-in
  // with no router sending, after the shell has painted. The caller owns
  // "after the shell has painted" and the role check; what is decided
  // here is the once-ness and the "nothing has arrived and nothing has
  // been decided" test.
  //
  // A ledger carrying any mark suppresses it. Someone who has already
  // skipped or forced a step has been through this modal, and reopening
  // it unasked would be the interface disagreeing with a decision it
  // asked them to record.
  //
  // The once-ness is spent on the first *answerable* call, not on the
  // first launch. That distinction is the whole of it: gating on "is it
  // open" instead would re-arm the moment the operator closed the modal,
  // and reopen it under them -- an explicit close that undoes itself is
  // worse than no close at all.
  maybeAutoLaunch(hasDevices: boolean) {
    if (this.autoLaunched) return
    // No ledger yet means no answer yet, not an answer of "no".
    if (!this.status) return
    this.autoLaunched = true
    if (hasDevices || this.marks.length > 0) return
    this.launch()
  }

  // markAutoLaunchSpent lets a caller that opens the wizard through its
  // own path (the journey, #646) spend this once-only slot itself,
  // without also launching from here. Without it, the ordinary
  // maybeAutoLaunch check would still be unspent the next time its
  // conditions are re-evaluated and would reopen a wizard something else
  // just opened and handed off.
  markAutoLaunchSpent() {
    this.autoLaunched = true
  }

  goTo(pane: number) {
    if (pane < 1 || pane > FINISH_PANE) return
    this.pane = pane
  }

  back() {
    this.goTo(this.pane - 1)
  }

  next() {
    this.goTo(this.pane + 1)
  }

  // record writes one step decision and refreshes the ledger from the
  // server rather than patching it locally: the mark that matters is the
  // one the server actually holds, and a client that drew its own would
  // be reporting its intention rather than the record.
  async record(step: number, outcome: 'skipped' | 'forced', note: string): Promise<string | null> {
    const result = await markSetupStep(step, outcome, note)
    if (typeof result === 'string') {
      this.error = result
      return result
    }
    await this.refresh()
    return null
  }
}

export const wizardState = new WizardState()
