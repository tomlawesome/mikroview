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

import {
  createDevice,
  fetchDevices,
  fetchRefusedSenders,
  fetchRouterBackups,
  fetchSetupCommands,
  fetchSetupStatus,
  markSetupStep,
  mintEnrolment,
  saveSetupAddress,
  saveSetupBackupTransport,
} from './api'
import {
  buildLedger,
  firstOpenStep,
  refusedSince,
  ROUTER_STEPS,
  SETUP_STEPS,
  silenceExplanation,
  type StepKey,
} from './setupsteps'
import type {
  BackupTransport,
  Device,
  EnrolmentToken,
  RefusedSender,
  RouterBackupsResponse,
  SetupCommandsResponse,
  SetupMark,
  SetupStatus,
} from './types'

class WizardState {
  open = $state(false)
  // steps is which ledger is open (#1284): the full first-run set, or
  // the router-side five that Add a router and Re-enrol… walk. Held
  // here rather than passed to the component because the pane number
  // means nothing without it -- "step 2" is Name your router in one and
  // Send logs in the other.
  steps = $state<readonly StepKey[]>(SETUP_STEPS)
  // 1..steps.length for a step, finishPane for the finish.
  pane = $state(1)
  status = $state<SetupStatus | null>(null)
  devices = $state<Device[]>([])
  error = $state<string | null>(null)

  // ledgerDevice is the router this walk is about (#1284): the row the
  // name step created, or the one Re-enrol… named. Empty on a plain
  // first-run walk that has not named anything yet, and the ledger then
  // reads the fleet as a whole, exactly as it always did.
  ledgerDevice = $state('')

  // enrolment is the token the Send logs step minted for ledgerDevice
  // (#1281), and enrolmentMintedAt when it did. The value is shown once
  // -- the server keeps only its hash -- and lives here rather than in
  // the component for the same reason `token` below does: a step change
  // must not mint a second one.
  enrolment = $state<EnrolmentToken | null>(null)
  enrolmentMintedAt = $state('')
  enrolmentError = $state<string | null>(null)

  // refused is GET /api/devices/refused: addresses whose lines were
  // dropped for not being any router's enrolled address. Read on the
  // same 5s cadence as the status poll while the modal is open.
  refused = $state<RefusedSender[]>([])

  // finishTo is where the finish's primary leads out to -- the fall
  // when the walk was opened from setup, the fleet when it was opened
  // from there (the record's own rule).
  finishTo = $state<'fall' | 'fleet'>('fall')
  // The small-screen sheet's body <-> ledger flip. Held here rather than
  // in the component so it survives a step change, which is what makes
  // "Show setup steps" a place you can stay rather than a peek.
  showStepList = $state(false)

  // lostRouterDevice names the router step 6 (#394, round 45) should
  // treat as lost, once it can -- set only by openLostRouter below, the
  // Settings backups group's "is it gone?" link (owner decision, issue
  // note 10572: this is the only door into that shape; the wizard never
  // offers it on its own). Null in every ordinary launch.
  lostRouterDevice = $state<string | null>(null)

  // pickedVersion (#436) is the operator's choice from the "Your
  // RouterOS version" pick-list -- '' means the first option, "Not
  // sure", which omits `version` from the request entirely rather than
  // sending an empty string. Held here rather than in the component so
  // it survives a step change; a module-lifetime field, not persisted
  // anywhere, per the owner's "session only".
  pickedVersion = $state('')

  // token is the ingest token step 4 minted, and tokenDevice the router
  // it is scoped to (#1183). Here rather than in the component for a
  // stronger reason than pickedVersion's: minting is not free. Every
  // visit to the push step used to reach for a new key, so four walks
  // left four rows called "setup-172.23.0.1" in Settings, each with its
  // own revoke control and nothing to tell them apart. A key is shown
  // once and never again, so the one this session minted is the one
  // every later visit has to show -- and the only way to mint another
  // is to ask (step 6's "mint a new one").
  //
  // Module-lifetime and deliberately not persisted: this is a bearer
  // credential, and web storage is not where one goes. A later page
  // load therefore still mints afresh, because there is nothing left to
  // reuse -- the server keeps only the hash.
  token = $state('')
  tokenDevice = $state('')

  // commands is the last response from POST /api/setup/commands: the
  // rendered command blocks, the dialect table the pick-list lists, and
  // the router-standing warning data. null until the first fetch lands.
  commands = $state<SetupCommandsResponse | null>(null)
  commandsError = $state<string | null>(null)
  // commandsRequestSeq guards refreshCommands against an out-of-order
  // response: the effect that calls it re-fires on the operator's own
  // pick (commandsKey in SetupWizard.svelte), and nothing stops a
  // still-in-flight earlier request's response landing after a later
  // one's, which would overwrite the freshly-picked version's commands
  // with the previous pick's. Not hypothetical: this is what "picking a
  // version re-requests the commands, and today's single dialect
  // renders the same text back" saw fail intermittently on a loaded gate
  // host, where request latency varies enough for responses to arrive
  // out of the order they were sent.
  private commandsRequestSeq = 0

  // backups is step 6's own read (#394, round 45): what has arrived per
  // router, from the same admin-only GET /api/router-backups the
  // Settings group reads (fetchRouterBackups). Null until the first
  // fetch lands or on a non-admin session -- see refreshBackups, which
  // this deliberately does not fold into refresh() above: that call
  // runs for any signed-in user (so surfaces outside the wizard can
  // explain their own silence), and this endpoint 403s below admin.
  backups = $state<RouterBackupsResponse | null>(null)

  // autoLaunched guards the record's "auto-launch, once". Deliberately a
  // module-lifetime flag and not persisted anywhere: the record says the
  // wizard is stateless beyond the evidence and that "finished" is not
  // stored, so the only honest client-side memory is "this app instance
  // has already offered it". Reopening after that is the operator's own
  // choice, through Run setup….
  private autoLaunched = false

  // address is the operator's own answer (#1213) to "what address can
  // your router reach mikroview on?" -- a required field in the
  // wizard's header, above the numbered steps, not a numbered step
  // itself (a step number is persisted in internal/setup's marks, and
  // inserting one here would silently renumber every stored mark).
  // Bound directly to the header field, so typing there is what
  // SyslogCommands, CaTrustCommands, PushScript, BackupScript and the
  // certificate check (setupsteps.ts's caStep) all read live. Nothing
  // else reads window.location.host except the last line of the
  // default refresh() applies below.
  address = $state('')

  // addressInitialized guards that one-time default: refresh() polls
  // every 5s while the modal is open, and must not overwrite an edit
  // already in flight -- or a value already saved -- on every tick.
  private addressInitialized = false

  // addressSaveError surfaces a save the server refused (validSetupAddress's
  // charset check, #1095), read by the header field beside the input.
  addressSaveError = $state<string | null>(null)

  // backupTransport is step 6's one choice (#955): how the router hands
  // its backup over -- 'sftp' through the drop box, or 'https' in
  // slices over the ingest channel, for an install whose only open way
  // in is its reverse proxy. Mirrors what the server has stored rather
  // than being this browser's own setting: the server is what
  // /api/setup/commands renders from, so a value held only here could
  // draw one choice above the other one's script.
  backupTransport = $state<BackupTransport>('sftp')

  // backupTransportInitialized guards the read-back against the 5s
  // status poll, exactly as addressInitialized does above: a switch is
  // applied here only once the server has accepted it, and a tick
  // landing in between must not put the old answer back.
  private backupTransportInitialized = false

  // backupTransportError surfaces a switch the server refused, read by
  // step 6 beside the pair.
  backupTransportError = $state<string | null>(null)

  // setBackupTransport switches the deployment over. The local value
  // moves only after the server has taken it, so the pair and the
  // script block below it never disagree: the block is re-requested off
  // the back of this change (commandsKey in SetupWizard.svelte), and
  // the server renders whichever transport it has stored.
  async setBackupTransport(transport: BackupTransport): Promise<void> {
    if (transport === this.backupTransport) return
    // saveSetupBackupTransport only ever resolves to an error string for
    // a refusal the server actually answered (postJSON/putJSON's own
    // fetch throws instead on a dropped connection) -- caught here so a
    // network failure surfaces the same way a refusal does, rather than
    // as an unhandled rejection that leaves backupTransportError exactly
    // as it was (most likely null), with nothing beside the pair saying
    // the switch never took.
    let error: string | null
    try {
      error = await saveSetupBackupTransport(transport)
    } catch (err) {
      error = err instanceof Error ? err.message : String(err)
    }
    this.backupTransportError = error
    if (error) return
    this.backupTransport = transport
    this.backupTransportInitialized = true
  }

  // saveAddress persists the header field's current value. Called on
  // blur/Enter rather than every keystroke, so typing stays purely
  // local (and every command block re-renders from it immediately,
  // through commandsKey) until the operator is actually done. An empty
  // value is not sent -- there is nothing to store for "not answered
  // yet", and every command block already renders its own no-command
  // state from that on the server side (commandStep.blocked's
  // "no-address" key, the same mechanism #1217 gave the backup block).
  //
  // saveSetupAddress only ever resolves to an error string for a
  // refusal the server actually answered (postJSON's own fetch throws
  // instead on a dropped connection) -- caught here so a network
  // failure surfaces the same way a refusal does, rather than as an
  // unhandled rejection that leaves addressSaveError exactly as it was
  // (most likely null), with the field looking saved when nothing was.
  async saveAddress(): Promise<void> {
    if (!this.address) {
      this.addressSaveError = null
      return
    }
    try {
      this.addressSaveError = await saveSetupAddress(this.address)
    } catch (err) {
      this.addressSaveError = err instanceof Error ? err.message : String(err)
    }
  }

  // finishPane is the pane after the last step -- the ledger read back.
  // One past the open ledger's own count rather than a separate flag,
  // so "which pane" stays a single number and Back from the finish
  // lands on the last step of whichever ledger is open.
  get finishPane(): number {
    return this.steps.length + 1
  }

  // ledger is the open ledger's steps as they currently stand. Empty
  // until the first status arrives, so callers can render a loading
  // state without a second flag.
  get ledger() {
    if (!this.status) return []
    return buildLedger(this.status, this.devices, this.address, this.backups, this.backupTransport, {
      steps: this.steps,
      device: this.ledgerDevice,
      enrolling: !!this.enrolment,
      reEnrolSince: this.enrolmentMintedAt,
    })
  }

  // refusedForThisWalk is what the Send logs step's warning box reads:
  // only addresses first seen after this walk's token was minted, so
  // the box speaks about the block the operator has just pasted rather
  // than about the fleet's history.
  get refusedForThisWalk(): RefusedSender[] {
    return refusedSince(this.refused, this.enrolmentMintedAt)
  }

  // refreshBackups reads step 6's own evidence (#394): admin-only, so
  // called only while the modal is open (SetupWizard's own effect,
  // mirroring the poll it already runs for refresh()) rather than
  // alongside every ordinary refresh() above.
  async refreshBackups(): Promise<void> {
    try {
      this.backups = await fetchRouterBackups()
    } catch {
      // A non-admin session, or the server not answering: step 6 reads
      // this the same way it reads "nothing has arrived yet" -- never a
      // page-wide error for a group this modal does not itself gate on.
      this.backups = null
    }
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
      // The field's default order (#1213): the operator's own stored
      // answer first -- it survived whatever restart brought this
      // session here -- then the browser's own host, which is the
      // owner's ruling on the issue ("with the browser's host offered
      // as the default to accept or replace"), then an address the
      // server finds itself bound to.
      //
      // The browser's host outranks the server's own list because
      // neither is more than a guess and this one is at least a guess
      // the operator can recognise: it is what they typed. A host with
      // several interfaces offers several candidates and picking one of
      // them is a different guess, not a better one -- so they are
      // offered beside the field (addressCandidates) rather than
      // silently chosen. The field exists because every guess here can
      // be wrong; the line under it says why.
      if (!this.addressInitialized) {
        this.address = s.instance.address || window.location.host || s.instance.addressCandidates[0] || ''
        this.addressInitialized = true
      }
      // The stored transport (#955), read back the same guarded way:
      // the deployment's answer, taken once so a poll tick cannot undo
      // a switch the operator has just made.
      if (!this.backupTransportInitialized) {
        this.backupTransport = s.instance.backupTransport === 'https' ? 'https' : 'sftp'
        this.backupTransportInitialized = true
      }
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e)
    }
  }

  // refreshCommands re-renders the wizard's RouterOS command blocks from
  // the server (#436), keyed to whatever this session currently knows:
  // the instance address and syslog port, the push kinds, the operator's
  // picked version if any, and -- once step 4 has minted one -- the
  // token. Callers still pass the token explicitly, even though #1183
  // moved it onto this object: the component reads it in the same
  // derived key that decides when to re-request at all, and a call that
  // took its own copy from here could disagree with that key.
  //
  // Deliberately not debounced here (#1218 audit finding 11): the only
  // caller this actually spams is commandsKey's own effect in
  // SetupWizard.svelte, driven by wizardState.address on every
  // keystroke -- every other trigger (a token just minted, a version
  // picked, a transport switch) is a discrete event that this component
  // and its tests both expect to answer promptly. SetupWizard.svelte
  // debounces the address component of that key instead of delaying
  // every call here regardless of what triggered it.
  //
  // opts.address lets that caller pass its own debounced value rather
  // than this reading wizardState.address itself: this function reads
  // its address argument before its own first await, so a caller that
  // left it to read `this.address` here would pick up wizardState.address
  // as a dependency too, transitively -- Svelte's reactive tracking
  // follows any reactive read during an effect's synchronous execution,
  // including ones inside a function the effect calls, which is exactly
  // the "per keystroke" behaviour the debounce above exists to stop.
  // Every other caller omits it and gets the live value, same as before.
  async refreshCommands(opts: { token?: string; device?: string; address?: string } = {}): Promise<void> {
    if (!this.status) return
    const seq = ++this.commandsRequestSeq
    const result = await fetchSetupCommands({
      address: opts.address ?? this.address,
      syslogPort: this.status.instance.syslogPort,
      kinds: this.status.pushKinds,
      token: opts.token || undefined,
      device: opts.device || undefined,
      version: this.pickedVersion || undefined,
      // The minted token travels with every command request, not only
      // the one that follows a mint: the block is re-rendered whenever
      // the address or the version changes, and a re-render that
      // dropped the enrol line would quietly hand the operator a block
      // that configures logging and enrols nothing.
      enrolToken: this.enrolment?.token || undefined,
    })
    // A newer call started (and may already have answered) while this
    // one was in flight -- its result is the stale one now, whichever
    // order the two responses actually arrived in.
    if (seq !== this.commandsRequestSeq) return
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
    this.steps = SETUP_STEPS
    this.finishTo = 'fall'
    this.pane = firstOpenStep(this.ledger)
    this.showStepList = false
    this.lostRouterDevice = null
    this.open = true
  }

  // openAddRouter is the fleet's Add a router action and the Entities
  // berth (#1284): the same ledger, opened at Name your router with no
  // router yet in hand. The certificate step is not in front of it --
  // that is an instance question, asked once.
  openAddRouter() {
    this.steps = ROUTER_STEPS
    this.finishTo = 'fleet'
    this.ledgerDevice = ''
    this.tokenDevice = ''
    this.enrolment = null
    this.enrolmentMintedAt = ''
    this.enrolmentError = null
    this.pane = 1
    this.showStepList = false
    this.lostRouterDevice = null
    this.open = true
  }

  // openReEnrol is a router row's Re-enrol… (#1284): the same ledger,
  // opened at Send logs for a router that already exists, with a fresh
  // token. The router is already named, so the step before it has
  // nothing left to ask.
  openReEnrol(device: string) {
    this.openAddRouter()
    this.ledgerDevice = device
    this.tokenDevice = device
    this.pane = this.steps.indexOf('syslog') + 1
  }

  // createRouter is the name step's own act: naming a router is what
  // creates it, so there is one call and not a field plus a save. The
  // new row becomes this walk's router, and the token the next step
  // mints belongs to it.
  async createRouter(name: string): Promise<string | null> {
    const result = await createDevice(name)
    if (typeof result === 'string') return result
    this.ledgerDevice = result.id
    this.tokenDevice = result.id
    // The new row has to be in `devices` before the ledger is rebuilt,
    // or the name step reads "no router yet" until the next poll tick
    // and the operator watches their own act not happen.
    await this.refresh()
    return null
  }

  // mintEnrolmentToken is the Send logs step acting before it waits
  // (#1281), and the same call is Reroll -- re-minting is what the
  // control does, which is why there is no second endpoint for it. The
  // value comes back once and is written into the last line of the
  // block the server renders.
  async mintEnrolmentToken(): Promise<void> {
    if (!this.ledgerDevice) {
      this.enrolmentError = 'Name the router first — a token is minted for a named router.'
      return
    }
    this.enrolmentError = null
    let result: EnrolmentToken | string
    try {
      result = await mintEnrolment(this.ledgerDevice)
    } catch (err) {
      result = err instanceof Error ? err.message : String(err)
    }
    if (typeof result === 'string') {
      this.enrolmentError = result
      return
    }
    this.enrolment = result
    this.enrolmentMintedAt = new Date().toISOString()
    // Re-render the block so its last line carries the token just
    // minted -- the server writes that line, and this is the only call
    // that tells it which token to write.
    await this.refreshCommands({ device: this.ledgerDevice })
  }

  // refreshRefused reads the dropped-line addresses (#1281), polled
  // beside the status while the modal is open. A failure reads as
  // "nothing refused" rather than as a page-wide error: this list
  // explains a silence, it is not the silence itself.
  async refreshRefused(): Promise<void> {
    try {
      this.refused = await fetchRefusedSenders()
    } catch {
      this.refused = []
    }
  }

  // openLostRouter is the Settings backups group's "is it gone?" link
  // (#394, round 44's amended receipt): it jumps straight to step 6,
  // marked for device, rather than the ordinary "first step still
  // waiting" the ledger would otherwise pick -- an admin who has just
  // asked "is it gone?" already knows steps 1-5 are done; showing them
  // the ledger again would bury the one thing they came for.
  //
  // The backup step's own position, looked up rather than written as a
  // number: it is the sixth of the first-run ledger and the fifth of
  // the router one (#1284), so a literal would be wrong in one of them.
  openLostRouter(device: string) {
    this.steps = SETUP_STEPS
    this.finishTo = 'fall'
    this.lostRouterDevice = device
    this.pane = this.steps.indexOf('backup') + 1
    this.showStepList = false
    this.open = true
  }

  close() {
    this.open = false
    this.lostRouterDevice = null
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
    if (pane < 1 || pane > this.finishPane) return
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

  // reset puts every field back to what its initialiser holds, for
  // #1083's rule: signing out must not leave one account's state for
  // whoever signs in next on this tab. wizardState was missed from that
  // batch (v0.6.0 pre-release audit, Security stage) and it carries
  // more than a view position -- `token` is a router ingest token, and
  // `backups` is an admin-only read that decided, among other things,
  // whether the minted history key was still needed.
  //
  // commandsRequestSeq is bumped rather than zeroed: a reply still in
  // flight from the previous session must be discarded when it lands,
  // and zeroing would let it pass the sequence check instead.
  reset() {
    this.open = false
    this.steps = SETUP_STEPS
    this.finishTo = 'fall'
    this.ledgerDevice = ''
    this.enrolment = null
    this.enrolmentMintedAt = ''
    this.enrolmentError = null
    this.refused = []
    this.pane = 1
    this.status = null
    this.devices = []
    this.error = null
    this.showStepList = false
    this.lostRouterDevice = null
    this.pickedVersion = ''
    this.token = ''
    this.tokenDevice = ''
    this.commands = null
    this.commandsError = null
    this.commandsRequestSeq++
    this.backups = null
    this.autoLaunched = false
    this.address = ''
    this.addressInitialized = false
    this.addressSaveError = null
    this.backupTransport = 'sftp'
    this.backupTransportInitialized = false
    this.backupTransportError = null
  }
}

export const wizardState = new WizardState()
