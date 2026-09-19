// SPDX-License-Identifier: AGPL-3.0-only

// The rules for deciding whether each of the setup wizard's steps has
// landed (#320), plus the small address-handling helpers the wizard and
// a few other surfaces still need on the client.
//
// The RouterOS commands themselves moved server-side with #436 (see
// internal/routeros): the wizard now renders what POST
// /api/setup/commands sends back, selected by the row that covers the
// router's version, rather than generating RouterOS syntax here. Kept
// out of the component so the step-status rules stay testable without a
// browser: getting a claim wrong is the failure this whole feature
// exists to prevent, and "the wizard said step 3 was done when it
// wasn't" would be worse than no wizard at all.

import { formatSize } from './memory'
import type {
  BackupTransport,
  Device,
  RefusedSender,
  RouterBackupsResponse,
  SetupMark,
  SetupStatus,
  SetupWitness,
} from './types'

// 'quiet' is #487's fifth reading, and the only one that is not a claim
// about a router: a step with nothing to wait for (step 5's naming is
// config-file work) is neither done nor waiting, and calling it either
// would be a small lie in a feature whose whole point is not telling
// them.
export type StepState = 'done' | 'waiting' | 'blocked' | 'partial' | 'quiet'

export interface StepStatus {
  state: StepState
  // What to show the operator. For 'waiting' this says what is being
  // waited for; for 'blocked' it says what to fix, on MikroView's side.
  detail: string
  // What a 'partial' step is still short of, kept apart from what
  // arrived (#1132): the wizard renders it as its own warning box under
  // the green arrived line, so a shortfall is never coloured as an
  // arrival. Empty on every other state, and empty `detail` with a
  // shortfall set means there was no arrival to word -- one warning box
  // and nothing above it.
  shortfall?: string
}

// hostname strips a port. Certificate names never carry one, so this is
// what tls.hosts is compared against.
export function hostname(hostPort: string): string {
  // IPv6 literals arrive as [::1]:8080.
  if (hostPort.startsWith('[')) {
    const end = hostPort.indexOf(']')
    return end === -1 ? hostPort : hostPort.slice(1, end)
  }
  const colon = hostPort.lastIndexOf(':')
  return colon === -1 ? hostPort : hostPort.slice(0, colon)
}

// certificateCovers reports whether the running certificate claims the
// address the operator is using. This is step 0, and it exists because
// getting it wrong produces a failure three steps later
// ("name verification failed") whose cause is on MikroView's side, not
// the router's.
//
// tls.enabled only turns HTTPS off on the API port -- it does not stop
// a certificate from being loaded. main.go loads/generates one whenever
// cfg.TLS.Enabled || cfg.Listen.SyslogTLS != "", and hands that same
// certificate to the syslog TLS listener regardless of tls.enabled
// (#374). So the check only skips entirely when neither HTTP TLS nor
// syslog TLS is on -- matching main.go's own condition for when a
// certificate is even in play.
export function certificateCovers(status: SetupStatus, address: string): boolean {
  if (!status.instance.tlsEnabled && !status.instance.syslogEnabled) return true
  const host = hostname(address)
  const hosts = status.instance.hosts
  // An empty list means the generated certificate covers
  // localhost/127.0.0.1 only -- see internal/servertls.defaultHosts.
  const effective = hosts.length > 0 ? hosts : ['localhost', '127.0.0.1']
  return effective.includes(host)
}

// portOf takes the port out of a listen address like ":6514" or
// "0.0.0.0:6514" -- the router needs the port, not the bind address.
export function portOf(listenAddr: string): string {
  const colon = listenAddr.lastIndexOf(':')
  return colon === -1 ? listenAddr : listenAddr.slice(colon + 1)
}

// deviceStanza is what an operator pastes into config.yaml to give a
// router a name of their choosing. Emitted rather than written by
// MikroView: the sourceIp -> id mapping decides who an event stream is
// attributed to, and that stays under file control (owner decision on
// #320, 2026-08-13).
export function deviceStanza(sourceIp: string, name: string): string {
  return [`devices:`, `  - sourceIp: "${sourceIp}"`, `    name: "${name || 'my-router'}"`].join('\n')
}

// --- Step status --------------------------------------------------------

export function caStep(status: SetupStatus, address: string): StepStatus {
  // An empty address (#1213: the header field has not been answered
  // yet) is not a certificate mismatch to report -- there is nothing to
  // check yet, not a wrong answer. It falls through to the ordinary
  // waiting/done read below, the same as it would before any address
  // existed to check at all.
  if (address && !certificateCovers(status, address)) {
    const shown = hostname(address)
    return {
      state: 'blocked',
      detail:
        `MikroView's certificate does not cover ${shown}, so the router will refuse it ` +
        `("name verification failed"). Add ${shown} to tls.hosts in config.yaml and restart, ` +
        `then come back — the router needs no change, the same CA signs the new certificate.`,
    }
  }
  if (status.sources.some((s) => s.caFetchedAt)) {
    return { state: 'done', detail: 'A router downloaded the certificate authority.' }
  }
  return { state: 'waiting', detail: 'Waiting for a router to download /ca.crt.' }
}

export function syslogStep(
  status: SetupStatus,
  devices: Device[] = [],
  device = '',
  reEnrolSince = '',
): StepStatus {
  if (!status.instance.syslogEnabled) {
    return {
      state: 'blocked',
      detail:
        'Syslog is switched off (listen.syslogTls is empty in config.yaml), so no router-side ' +
        'configuration can work until it is set.',
    }
  }
  // Enrolment (#1281) is this step's own arrival once the ledger is
  // about one router: the enrol line at the end of the block names the
  // address, and that address is the only one this router's logs are
  // accepted from afterwards. Read ahead of the fleet-wide sources
  // below, because it is a reading of the router in front of the
  // operator rather than of whatever else is streaming.
  const enrolling = device ? devices.find((d) => d.id === device) : undefined
  if (enrolling) {
    const accepted = enrolling.acceptedIp ?? ''
    const at = enrolling.enrolledAt ?? ''
    // Re-enrol (#1284): the row already carries an accepted address, so
    // the honest line names it and says what is still outstanding --
    // the new line. An arrival at or after this walk's own mint is that
    // new line, and it replaces the address.
    //
    // Instants, not strings (#1291): the server stamps enrolledAt in its
    // own zone, the browser mints reEnrolSince in UTC, and the two only
    // sort alike by luck -- a string compare reads an old enrolment as
    // new whenever the server's offset pushes its clock digits ahead of
    // Z. Guard unparseable values the same way: no instant, no "new".
    const atInstant = at ? Date.parse(at) : NaN
    const sinceInstant = reEnrolSince ? Date.parse(reEnrolSince) : NaN
    if (accepted && (!reEnrolSince || (!Number.isNaN(atInstant) && atInstant >= sinceInstant))) {
      return { state: 'done', detail: `Enrolled at ${accepted} · ${when(at)}` }
    }
    if (accepted) {
      return { state: 'waiting', detail: `Enrolled at ${accepted} · waiting for the new line` }
    }
    return { state: 'waiting', detail: 'Waiting for the enrol line at the end of the block.' }
  }
  // The source-address split (#442) reads as partial, in the voice step
  // 3 uses when events arrive without an action: evidence has arrived,
  // but composed wrongly. Not blocked -- everything on mikroview's side
  // works, which is the whole problem.
  const splits = sourceSplits(devices)
  if (splits.length > 0) {
    return {
      state: 'partial',
      detail: sourceSplitObservation(splits),
      shortfall: sourceSplitShortfall(splits),
    }
  }
  if (status.sources.some((s) => s.syslogFirstSeenAt)) {
    return { state: 'done', detail: 'A router has an open syslog connection.' }
  }
  return { state: 'waiting', detail: 'Waiting for a router to connect.' }
}

export function rulesStep(status: SetupStatus): StepStatus {
  const withEvents = status.devices.filter((d) => d.events > 0)
  if (withEvents.length === 0) {
    return {
      state: 'waiting',
      detail:
        'Connected, but no events yet — that means no firewall rule has log=yes, ' +
        'or no traffic has matched one.',
    }
  }
  const undecoded = withEvents.filter((d) => d.decodedActions === 0)
  if (undecoded.length === withEvents.length) {
    return {
      state: 'partial',
      detail: 'Events are arriving.',
      shortfall:
        'None carry an action from a log-prefix. The rules log without one, so rows show ' +
        '"unknown". Add the prefixes below.',
    }
  }
  const total = withEvents.reduce((n, d) => n + d.events, 0)
  const decoded = withEvents.reduce((n, d) => n + d.decodedActions, 0)
  if (decoded < total) {
    return {
      state: 'partial',
      detail: `${decoded} of ${total} events carry an action.`,
      shortfall: `The other ${total - decoded} do not — some rules are still untagged.`,
    }
  }
  return { state: 'done', detail: `${total} events, all with a decoded action.` }
}

export function pushStep(status: SetupStatus): StepStatus {
  const pushed = new Set<string>()
  for (const d of status.devices) {
    for (const kind of Object.keys(d.pushedKinds ?? {})) pushed.add(kind)
  }
  if (pushed.size === 0) {
    return { state: 'waiting', detail: 'Waiting for the first push. Run the script by hand to test it.' }
  }
  const missing = status.pushKinds.filter((k) => !pushed.has(k))
  if (missing.length > 0) {
    return {
      state: 'partial',
      detail: `Arrived: ${[...pushed].sort().join(', ')}.`,
      shortfall: `Still missing: ${missing.join(', ')}.`,
    }
  }
  return { state: 'done', detail: 'Every table has been pushed.' }
}

// backupStep is step 6 (#394, round 45): whether any router's config
// backup has ever arrived. Aggregate across every router, the same
// "any evidence at all" reading pushStep gives step 4's tables, rather
// than tied to whichever single router the operator happens to be
// minting a token for here -- the step is answering "does this feature
// work at all", not "has this one router done it yet".
//
// backups is null before the first read of GET /api/router-backups (or
// on a session this modal would not otherwise be open on) -- read the
// same way as "nothing has arrived", never as a claim about the key,
// so this never states "no key" without having actually asked.
export function backupStep(
  backups: RouterBackupsResponse | null,
  transport: BackupTransport = 'sftp',
): StepStatus {
  if (backups && !backups.enabled) {
    return {
      state: 'blocked',
      detail:
        'No key file is mounted, so the drop box is closed and a push would be refused. Put the key ' +
        'above in place and restart, and this step prints the script.',
    }
  }
  const routers = backups?.routers ?? []
  if (routers.length === 0) {
    // #1220: a router that cannot reach the drop box's port looks
    // identical to one that just hasn't run the script yet -- nothing
    // here distinguishes "give it a minute" from "it will never
    // arrive". Naming the port is the cheap half-measure available
    // without a live connection-attempt signal (see backupReceipt/the
    // module doc above): the operator has something to check instead
    // of only waiting.
    // ...and only when the drop box is the way in at all: an HTTPS-only
    // install (#955) pushes over the address the router already reaches,
    // so naming a drop box port here would send the operator to check
    // something this deployment does not use.
    const port = transport === 'sftp' && backups?.port ? portOf(backups.port) : null
    return {
      state: 'waiting',
      detail: port
        ? `Waiting for the first push — the script below runs once at the end; give it a minute, and make sure the router can reach this host on port ${port}.`
        : 'Waiting for the first push — the script below runs once at the end; give it a minute.',
    }
  }
  const receipt = backupReceipt(backups)
  return { state: 'done', detail: receipt ? `arrived ${receipt}` : 'A router has pushed a backup.' }
}

// --- The address-not-answered no-command state (#1213) ------------------
//
// Every RouterOS command block that embeds the operator's address --
// caTrust, syslog, push/schedule, and backup/backupSchedule beside its
// own preconditions below -- comes back blank with this one
// commandStep.blocked key when nothing has been answered yet in the
// wizard header field above the numbered steps. Reuses #1217's own
// mechanism (a machine-readable key the server states, worded here)
// rather than a second "why is this blank" shape.
export const NO_ADDRESS_KEY = 'no-address'

// NO_COMMAND_HEADING is caTrust/syslog/push's own no-command state --
// simpler than backup's below, since a missing address is the only
// reason any of those three ever come back blank.
export const NO_COMMAND_HEADING = 'no commands yet'
export const NO_ADDRESS_LINE =
  'no address has been given yet — answer "What address can your router reach MikroView on?" above, at the top of this wizard, and this fills in.'

// --- The backup step's no-script state (#1217) --------------------------
//
// commandStep.blocked (internal/api/setupcommands.go's handleSetupCommands)
// names every precondition the backup block came back blank for, as
// machine-readable keys. The server states which keys apply; every
// operator-facing sentence lives here instead, same split #436 already
// draws for the RouterOS commands themselves.
//
// Order matters (owner's ruling, 2026-09-14): config problems first --
// the operator edits config.yaml and restarts -- then the ones the
// wizard itself can still fix, in the order it asks them (the header
// field before either step-4/6 pick).
export const BACKUP_BLOCKED_ORDER = [
  'backups-off',
  'retention-key-unreadable',
  'no-retention-key',
  NO_ADDRESS_KEY,
  'no-device',
  'no-token',
] as const

const BACKUP_BLOCKED_COPY: Record<string, string> = {
  'backups-off': 'backups are switched off. Set backup.enabled: true in config.yaml and restart mikroview.',
  // #1264 finding 5: a configured retention key that could not be read
  // is not the same fact as no-retention-key below, and must never read
  // like it -- "set history.keyFile" tells the operator to mint a fresh
  // one, and a fresh key cannot decrypt what the old, now-unreadable one
  // already wrote. This line says what actually happened and warns off
  // the one action that would make it permanent.
  'retention-key-unreadable':
    'the retention key at history.keyFile is set but could not be read (missing, unreadable, or too short) — ' +
    'check the server logs and fix that file in place. Do not replace it with a new one: every backup already ' +
    'stored under the old key would become unrecoverable.',
  'no-retention-key':
    'no retention key is mounted, so there is nowhere safe to keep a backup. Set history.keyFile in ' +
    'config.yaml and restart mikroview.',
  [NO_ADDRESS_KEY]: NO_ADDRESS_LINE,
  'no-device': 'this router has no name yet. Name it in the step above; the script files each backup under that name.',
  'no-token': 'no token has been minted for this router yet. The step above mints it.',
}

// BACKUP_NO_SCRIPT_HEADING is the heading over the lines above -- the
// no-script state entirely replaces the input boxes and Copy buttons
// the step would otherwise show (#1217).
export const BACKUP_NO_SCRIPT_HEADING = 'no script yet'

// backupBlockedLines turns commandStep.blocked's keys into the sentences
// the wizard shows, in the ratified order, regardless of what order the
// server happened to list them in.
export function backupBlockedLines(blocked: string[] | undefined): string[] {
  if (!blocked || blocked.length === 0) return []
  const set = new Set(blocked)
  return BACKUP_BLOCKED_ORDER.filter((key) => set.has(key)).map((key) => BACKUP_BLOCKED_COPY[key])
}

// backupReceipt is the newest pair to have arrived, across every
// router -- "today 03:00 · rb5009.backup 412 KiB + rb5009.rsc 38 KiB ·
// kept under the key" (round 45's observation line). Empty when
// nothing has arrived yet.
export function backupReceipt(backups: RouterBackupsResponse | null): string {
  const routers = backups?.routers ?? []
  let newestAt = ''
  let newestDevice = ''
  let newestBackup: number | undefined
  let newestRsc: number | undefined
  let newestHasBackup = false
  let newestHasRsc = false
  for (const r of routers) {
    const g = r.generations[r.generations.length - 1]
    if (!g) continue
    const at = g.backupArrivedAt && g.rscArrivedAt
      ? g.backupArrivedAt > g.rscArrivedAt ? g.backupArrivedAt : g.rscArrivedAt
      : g.backupArrivedAt || g.rscArrivedAt || ''
    if (!at || at <= newestAt) continue
    newestAt = at
    newestDevice = r.device
    newestBackup = g.backupBytes
    newestRsc = g.rscBytes
    newestHasBackup = !!g.backupArrivedAt
    newestHasRsc = !!g.rscArrivedAt
  }
  if (!newestAt) return ''
  const parts: string[] = []
  if (newestHasBackup) parts.push(`${newestDevice}.backup ${formatSize(newestBackup ?? 0)}`)
  if (newestHasRsc) parts.push(`${newestDevice}.rsc ${formatSize(newestRsc ?? 0)}`)
  return `${when(newestAt)} · ${parts.join(' + ')} · kept under the key`
}

// backupReceiptForDevice is round 45's lost-router receipt: not the
// newest across every router, but how much this one router's own
// history holds -- "10 pairs kept · the newest today 03:00" -- since a
// replacement's own step is about what it inherits, not the fleet.
export function backupReceiptForDevice(backups: RouterBackupsResponse | null, device: string): string {
  const router = backups?.routers.find((r) => r.device === device)
  if (!router || router.generations.length === 0) return ''
  const newest = router.generations[router.generations.length - 1]
  const at = newest.backupArrivedAt || newest.rscArrivedAt
  const n = router.generations.length
  return `${n} ${n === 1 ? 'pair' : 'pairs'} kept · the newest ${at ? when(at) : 'unknown'}`
}

// undeclaredDevices are routers sending syslog that config.yaml does not
// name. They work as they are; declaring one only swaps its address for
// a name of the operator's choosing.
export function undeclaredDevices(devices: Device[]): Device[] {
  return devices.filter((d) => !d.configured)
}

// --- The source-address split (#442) -----------------------------------
//
// A router holds an address on every network it routes, and its logs
// arrive stamped with whichever one faces this instance -- frequently
// not the one declared as sourceIp. The declared device then sits
// silent while the real stream auto-discovers under another address,
// and a token minted for the declared identity enriches nothing.
//
// The server pairs the two (Registry.MultihomedCandidates, #499) and
// this module only words it. The wording states both facts and hands
// the operator the one fact only they hold -- whether the two addresses
// are one box. Nothing here claims they are.

export interface SourceSplit {
  // The declared identity, as config.yaml names it: sourceIp, or the id
  // when a declaration carries no address.
  declared: string
  // Every undeclared address logs arrive from, in id order. All of them,
  // never a pick: the server returns candidates, not a diagnosis.
  arriving: string[]
}

// sourceSplits is one entry per declared device the server has paired
// with arriving undeclared addresses.
export function sourceSplits(devices: Device[]): SourceSplit[] {
  return devices
    .filter((d) => d.configured && (d.multihomedCandidates?.length ?? 0) > 0)
    .map((d) => ({ declared: d.sourceIp || d.id, arriving: d.multihomedCandidates ?? [] }))
}

// srcAddressCommand is the recommended remedy: the router keeps the
// address it was declared under, so the token step 4 mints and the
// tables it pushes need no reissuing. Assumes the logging action is
// named mikroview -- step 2's own `add` created it under that name, the
// same assumption every wizard command already makes.
export function srcAddressCommand(declared: string): string {
  return `/system logging action set mikroview src-address=${declared}`
}

// arrivingAddresses is every undeclared address across the splits, in
// first-seen order and without repeats. The server pairs each silent
// declared device with the same discovered set, so with two declared
// devices silent this is the set once, not twice.
export function arrivingAddresses(splits: SourceSplit[]): string[] {
  const seen = new Set<string>()
  for (const s of splits) for (const a of s.arriving) seen.add(a)
  return [...seen]
}

// prose joins addresses the way a sentence does: "a", "a and b",
// "a, b and c". Exported for the wizard body, which words the same
// addresses in the same voice.
export function prose(items: string[], joiner: 'and' | 'or' = 'and'): string {
  if (items.length <= 1) return items.join('')
  return `${items.slice(0, -1).join(', ')} ${joiner} ${items[items.length - 1]}`
}

// sourceSplitObservation is step 2's arrived line when the split is on:
// what the router shows. The other half of #442's sentence -- what you
// told mikroview, and that it has sent nothing -- is the shortfall
// below, in its own warning box (#1132). Still no diagnosis in either.
export function sourceSplitObservation(splits: SourceSplit[]): string {
  const arriving = arrivingAddresses(splits)
  const from = `${prose(arriving)}, ${arriving.length === 1 ? 'an address' : 'addresses'} you haven't declared`
  return `Connected — but from ${from}.`
}

// sourceSplitShortfall is the silent half: the declared identity that
// the logs are not arriving under.
export function sourceSplitShortfall(splits: SourceSplit[]): string {
  const declared = splits.map((s) => s.declared)
  return `${prose(declared)}, which you declared in config.yaml, ${declared.length === 1 ? 'has' : 'have'} sent nothing.`
}

// sourceSplitReceipt is the step list's sub-line for the same reading.
export function sourceSplitReceipt(splits: SourceSplit[]): string {
  return `syslog from ${arrivingAddresses(splits).join(', ')} · declared ${splits.map((s) => s.declared).join(', ')} silent`
}

// --- The claim ledger ---------------------------------------------------
//
// #487 turns the wizard page into a modal, and the modal into a claim
// ledger: every check above is an observation -- mikroview never
// connects to the router -- so each step is a claim about what has
// arrived, and ends in exactly one of done, skipped, or forced past.
// See docs/design/screens/wizard/DESIGN.md, the ratified record this
// implements; where it and a mockup disagree, the record wins.
//
// Everything here is pure: the component renders it and the modal's
// state module drives it, but neither owns the wording or the rules.
// Getting a claim wrong is the failure this whole feature exists to
// prevent, so the claims stay testable without a browser.

// Flavour is how an observation line reads. The record names four and
// calls them the complete set -- waiting, arrived, counting, quiet.
//
// 'attention' is not a fifth flavour of observation: it is the
// mikroview-side check logic this design inherits from #371/#374, where
// nothing is being waited for because nothing router-side can work yet
// (the certificate cannot cover the address; the syslog listener is
// off). Kept distinct precisely so it never borrows the patient,
// nothing-is-wrong voice the waiting flavour is required to use.
//
// 'partial' is not a fifth flavour either (owner, #1132): it stays the
// arrived (or counting) line, saying what did arrive, with the
// shortfall carried beside it in its own warning box -- warning
// colour, never the reject red 'attention' uses, because nothing is
// wrong on mikroview's side.
export type Flavour = 'waiting' | 'arrived' | 'counting' | 'quiet' | 'attention'

// Outcome is where a step stands in the ledger. 'open' is a step that
// has neither evidence nor a decision yet -- it is not a fourth
// outcome, it is the absence of one.
export type Outcome = 'done' | 'skipped' | 'forced' | 'open'

// StepKey names a step by what it is rather than by where it sits
// (#1284). Adding a router is the wizard's router-side steps opened on
// their own, so a step's position is no longer fixed: first-run setup
// shows all six, the router ledger shows the five from `name` down.
// Every rule that used to be written against a step number -- which
// flavour it reads in, whether Next runs a check, what skipping it
// costs -- is written against the key instead, so the same step behaves
// the same wherever it is drawn.
export type StepKey = 'ca' | 'name' | 'syslog' | 'rules' | 'push' | 'backup' | 'register'

// SETUP_STEPS is the first-run ledger in the order it is walked. The
// router ledger below is a slice of this list, not a copy of it -- "the
// ledger is embedded, not copied", the record's own words.
export const SETUP_STEPS: readonly StepKey[] = [
  'ca',
  'name',
  'syslog',
  'rules',
  'push',
  'backup',
  'register',
]

// RECORD_NUMBERS is the number each step's marks and witnesses are
// recorded under in internal/setup's ledger. It is the v0.5 walking
// order, frozen: the server witnesses steps by these numbers
// (internal/api/setup.go notes step 2 when the first syslog line lands,
// step 4 when the first push arrives), and ledgers written before
// #1284 moved "Name your router" forward hold marks under them. Walking
// order can change; this cannot, or every stored mark changes meaning.
const RECORD_NUMBERS: Readonly<Record<StepKey, number>> = {
  ca: 1,
  syslog: 2,
  rules: 3,
  push: 4,
  name: 5,
  backup: 6,
  // 7 is new with #1291 and has no history to preserve; it is last
  // because nothing was ever recorded under it before.
  register: 7,
}

// ROUTER_STEPS is the router ledger (#1284): the same five router-side
// steps without the certificate step in front, which is an instance
// question and is asked once.
export const ROUTER_STEPS: readonly StepKey[] = ['name', 'syslog', 'rules', 'push', 'backup', 'register']

// canonicalStep is the number a step's decisions are recorded under,
// whichever ledger it is being walked in. A mark is persisted server
// side, so it cannot mean "second row of whatever list was open".
export function canonicalStep(key: StepKey): number {
  return RECORD_NUMBERS[key]
}

export interface LedgerStep {
  // n is where this step sits in the ledger being walked -- "Step 2 of
  // 5" on the router ledger, "Step 3 of 6" on first-run setup.
  n: number
  // canonical is the number the same step's marks are recorded under
  // (RECORD_NUMBERS), which never moves whatever order it is walked in.
  canonical: number
  key: StepKey
  title: string
  // The step body's lead sentence -- one anatomy for every step:
  // lead sentence, the router-side command (with Copy), the observation
  // line.
  lead: string
  status: StepStatus
  flavour: Flavour
  outcome: Outcome
  // The step list's sub-line, carried for the wizard's life: the receipt
  // when evidence arrived, the decision when one was recorded, and the
  // honest gap when neither.
  receipt: string
  // Whether Next runs a check here. Tagging rules counts and can only
  // count upward, and naming has nothing to wait for, so on both Next
  // is always free -- there is no waiting check to force past.
  hasCheck: boolean
  // enrolling is true when this walk has a router with a token minted
  // and unspent (#1281). It only changes how Send logs words what was
  // not observed: a router that was never enrolled has its logs
  // refused, which is a different sentence from never having connected.
  enrolling: boolean
  // witnessed is true when this step's 'done' outcome rests on the
  // server's own witness (#1221) rather than evidence it can see right
  // now -- the moment that happens, status.detail is a stale reading
  // (whatever the live check falls back to with nothing to look at,
  // usually 'waiting') and must not be shown as if it were current.
  // receipt already carries the honestly past-tense line to use
  // instead; see witnessReceipt.
  witnessed: boolean
}

// TITLES is every step's name, by key. The step list, the header and
// the spoken announcement all read from here, so they cannot drift.
// Exported because the fleet's own sentences point at a step by name:
// a number would be wrong the next time the walking order moves, which
// #1284 is exactly what happened to (the source-split echo sent an
// operator to "step 2" long after Send logs stopped being second).
export const TITLES: Record<StepKey, string> = {
  ca: 'Trust the certificate',
  name: 'Name your router',
  syslog: 'Send logs',
  rules: 'Tag firewall rules',
  push: 'Push router state',
  backup: 'Back up the router',
  register: 'Register the router',
}

// STEP_TITLES is the six in RECORD_NUMBERS' order, not the walking
// order: it is indexed by a stored mark's step number, wherever that
// mark is read back (silenceExplanation's empty-state sentence, most of
// all), so it has to name the step the server meant rather than
// whichever row happens to sit there in the ledger being walked.
// Sorted from RECORD_NUMBERS rather than written out again, so the two
// cannot drift apart.
export const STEP_TITLES: readonly string[] = [...SETUP_STEPS]
  .sort((a, b) => RECORD_NUMBERS[a] - RECORD_NUMBERS[b])
  .map((k) => TITLES[k])

export const STEP_COUNT = SETUP_STEPS.length

// arrived reports whether a step's evidence has landed. 'partial' counts:
// every step's check is "waiting → arrived", and a partial reading means
// the first push (or the first tagged rule) has already arrived -- it is
// a growing receipt, not a half-failure.
function arrived(state: StepState): boolean {
  return state === 'done' || state === 'partial'
}

// when renders a receipt's timestamp. Local time, no date for something
// that happened today: a receipt is read next to the thing it describes,
// and "14:02:11" says more at a glance than a full ISO string.
function when(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const today = new Date()
  const sameDay =
    d.getFullYear() === today.getFullYear() &&
    d.getMonth() === today.getMonth() &&
    d.getDate() === today.getDate()
  const time = d.toLocaleTimeString(undefined, { hour12: false })
  return sameDay ? time : `${d.toLocaleDateString()} ${time}`
}

// caReceipt: what arrived, when, from where. The source address is the
// "from where" -- on a first run it is also the first proof the router
// can reach mikroview at all.
export function caReceipt(status: SetupStatus): string {
  const fetched = status.sources.filter((s) => s.caFetchedAt)
  if (fetched.length === 0) return ''
  const first = fetched[0]
  const more = fetched.length > 1 ? ` (+${fetched.length - 1} more)` : ''
  return `ca.crt fetched by ${first.source} · ${when(first.caFetchedAt ?? '')}${more}`
}

export function syslogReceipt(status: SetupStatus, devices: Device[] = [], device = ''): string {
  // The enrolled address is this step's receipt once the ledger is
  // about one router (#1281) -- what arrived, when, and from where, in
  // the same three parts every other receipt carries.
  const enrolled = device ? devices.find((d) => d.id === device) : undefined
  if (enrolled?.acceptedIp) {
    return `enrolled at ${enrolled.acceptedIp} · ${when(enrolled.enrolledAt ?? '')}`
  }
  const splits = sourceSplits(devices)
  if (splits.length > 0) return sourceSplitReceipt(splits)
  const seen = status.sources.filter((s) => s.syslogFirstSeenAt)
  if (seen.length === 0) return ''
  const first = seen[0]
  const more = seen.length > 1 ? ` (+${seen.length - 1} more)` : ''
  return `syslog connected from ${first.source} · ${when(first.syslogFirstSeenAt ?? '')}${more}`
}

export function rulesReceipt(status: SetupStatus): string {
  const withEvents = status.devices.filter((d) => d.events > 0)
  if (withEvents.length === 0) return ''
  const total = withEvents.reduce((n, d) => n + d.events, 0)
  const decoded = withEvents.reduce((n, d) => n + d.decodedActions, 0)
  return `${decoded} of ${total} events carry an action`
}

export function pushReceipt(status: SetupStatus): string {
  let newest = ''
  const kinds = new Set<string>()
  for (const d of status.devices) {
    for (const [kind, at] of Object.entries(d.pushedKinds ?? {})) {
      kinds.add(kind)
      if (!newest || at > newest) newest = at
    }
  }
  if (kinds.size === 0) return ''
  return `${[...kinds].sort().join(', ')} · ${when(newest)}`
}

// nameStep is the ledger's first router step (#1284). Naming moved from
// last to first, and it acts rather than describing: the name is the
// only field, and Next creates the router, because the enrolment token
// the next step mints belongs to a named router. The old "nothing to
// name" row -- which pointed at a config.yaml edit and waited for
// nothing -- is retired with it.
//
// Still quiet, in the record's sense: there is nothing router-side to
// wait for here, so Next is always free. Once the router exists the row
// reads done, because the row itself is the evidence.
export function nameStep(devices: Device[], device = ''): StepStatus {
  const named = device ? devices.find((d) => d.id === device) : undefined
  if (named) {
    return { state: 'done', detail: `${named.name || named.id} is on the fleet — nothing to wait for.` }
  }
  return {
    state: 'quiet',
    detail: 'Nothing to wait for — the name is the only field, and Next creates the router.',
  }
}

// nameReceipt is the step list's sub-line for a router this walk has
// named: the fact, no timestamp, because the row was created by the
// operator in front of it rather than observed arriving.
export function nameReceipt(devices: Device[], device = ''): string {
  const named = device ? devices.find((d) => d.id === device) : undefined
  return named ? `named ${named.name || named.id}` : ''
}

// registerStep is the ledger's final step (#1291): whether the
// operator has confirmed this router on the device itself. There is
// nothing to wait for -- no router-side command, no arriving evidence
// -- because registering is the operator's own statement of intent,
// not something observed. It is 'done' once the server holds a
// registeredAt for the router, and 'quiet' until then.
//
// Deliberately says nothing about acceptedIp. Registering grants the
// router nothing (the server never sets an accepted address from it),
// so a step that read as done because a router had enrolled would be
// claiming the operator confirmed something they never did.
export function registerStep(devices: Device[], device = ''): StepStatus {
  const row = device ? devices.find((d) => d.id === device) : undefined
  if (row?.registeredAt) {
    return {
      state: 'done',
      detail: `${row.name || row.id} is registered — nothing to wait for.`,
    }
  }
  return {
    state: 'quiet',
    // Ruling 24 (#1291, owner, 2026-09-19): Next does not act here --
    // CHECKED.register stays false, and the only thing that registers
    // is the "Register this router" button above. An earlier wording
    // claimed Next itself recorded the confirmation, which was false:
    // it just moves on, the same as every other unchecked step.
    detail: 'Nothing to wait for — Next moves on without registering; the Register button above is what confirms this router.',
  }
}

// registerReceipt is the step list's sub-line once the router is
// registered: the fact, no timestamp, for nameReceipt's reason -- the
// operator did it in front of us rather than us observing it arrive.
export function registerReceipt(devices: Device[], device = ''): string {
  const row = device ? devices.find((d) => d.id === device) : undefined
  return row?.registeredAt ? `registered ${row.name || row.id}` : ''
}

// BACKUP_LEAD_INTRO is step 6's lead sentence with no script promised --
// what the step is for, said whether or not a script can be printed
// right now. BACKUP_LEAD_SCRIPT_NOTE is only true once one can: #1217's
// bug was this second sentence surviving into the no-script state,
// describing a token and a script that were not on the screen.
export const BACKUP_LEAD_INTRO =
  'Every night the router saves itself twice — the binary backup that restores it whole, and the plain ' +
  'export you can read — and drops both into MikroView. Nothing is sent back, and nothing is left on ' +
  'the router.'
const BACKUP_LEAD_SCRIPT_NOTE = ' The token below is minted for this one router and is already in the script.'

// backupLead is step 6's lead, gated on whether a script actually
// exists to describe (#1217).
export function backupLead(scriptExists: boolean): string {
  return scriptExists ? BACKUP_LEAD_INTRO + BACKUP_LEAD_SCRIPT_NOTE : BACKUP_LEAD_INTRO
}

// BACKUP_TRANSPORTS is step 6's one choice (#955), in the order it is
// drawn: how the router hands its backup over. 'sftp' is the default --
// the drop box #394 built -- and 'https' is for an install reachable
// only through its reverse proxy, where the router reads its own backup
// in slices and posts them through the channel that is already open.
export const BACKUP_TRANSPORTS: { value: BackupTransport; label: string }[] = [
  { value: 'sftp', label: 'sftp' },
  { value: 'https', label: 'https' },
]

// BACKUP_PORT_NOTE_HTTPS replaces the drop box's "publish this port"
// note when the router is pushing over HTTPS (#955): there is no second
// port to open, which is the whole reason that transport exists.
export const BACKUP_PORT_NOTE_HTTPS =
  'Nothing new has to be opened: the script posts the backup to the same HTTPS address the router ' +
  'already reaches, in slices, over the connection it already uses for its pushes.'

// BACKUP_WAITING_NO_SCRIPT replaces backupStep's ordinary waiting line
// when the backup block is blocked (#1217): "the script below runs once
// at the end" is false with no script below, so the promise is dropped
// rather than carried into a state that cannot make it true.
export const BACKUP_WAITING_NO_SCRIPT = 'Waiting for the first push.'

// SYSLOG_LEAD_ENROL is the enrolment half of the Send logs lead
// (#1281), said once and correctly: the last line of the block carries
// a token, and the address that line arrives from is the only address
// this router's logs are accepted from afterwards.
const SYSLOG_LEAD_ENROL =
  ' The last line carries this router’s enrolment token, and the address it arrives from becomes the ' +
  'only address MikroView accepts this router’s logs from.'

// syslogLead is the Send logs lead, with the enrolment sentence only
// where an enrol line is actually in the block below it -- a promise
// about a line that is not on the screen is #1217's bug in another
// step's clothes.
export function syslogLead(enrolling: boolean): string {
  const base =
    'Point the router at this instance. The handshake itself is the evidence — a failed one never ' +
    'counts as arrived.'
  const tail =
    ' This block is safe to paste again — a second run updates the existing rule rather than adding another.'
  return enrolling ? base + SYSLOG_LEAD_ENROL + tail : base + tail
}

// LEADS are the step bodies' lead sentences, by key. Wording is design,
// so it lives with the step it belongs to rather than being assembled
// in the component.
const LEADS: Record<StepKey, string> = {
  ca: "The router has to trust MikroView's certificate authority before it will open a TLS connection. Run this on the router; it fetches the certificate and imports it.",
  name: 'Give the router a name. MikroView creates it here, and the enrolment token the next step mints belongs to it — a token is minted for a named router, never for an address.',
  syslog: syslogLead(false),
  rules: 'The letter in the log-prefix is how MikroView knows what a rule did. This tags every existing filter rule by its action, in one pass.',
  push: 'A push turns addresses into names, fills the rule lookups, and gives suggestions something to suggest from. It authenticates with the token below.',
  backup: backupLead(true),
  register:
    'Confirm this router is one you meant to add. MikroView records that you did — the name, and that it is ' +
    'here to stay. Registering grants the router nothing on its own: its logs are accepted because its ' +
    'enrolment token arrived from its address, and that does not change here.',
}

// CHECKED says where Next runs a check. Tagging rules can only count
// upward and naming has nothing to wait for, so Next is always free on
// both -- there is no waiting check to force past.
const CHECKED: Record<StepKey, boolean> = {
  ca: true,
  name: false,
  syslog: true,
  rules: false,
  push: true,
  backup: true,
  // Nothing to wait for: the operator is confirming something they
  // already know, the same as naming.
  register: false,
}

// stepMarks indexes marks by step, so building the ledger stays one pass.
function markFor(marks: SetupMark[], step: number): SetupMark | undefined {
  return marks.find((m) => m.step === step)
}

// witnessFor is markFor's own twin for the server's witnesses (#1221).
function witnessFor(witnesses: SetupWitness[], step: number): SetupWitness | undefined {
  return witnesses.find((w) => w.step === step)
}

// witnessedWhen renders a witness's timestamp always dated, unlike
// `when` above, which drops the date for something read the same day it
// happened. A witness is read back after evidence has already gone
// (that is the only time it is used at all -- see buildLedger), so "just
// now" is never true of it, and the date has to say so rather than
// leaving a bare time that reads as today's.
function witnessedWhen(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const day = d.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
  const time = d.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit' })
  return `${day} at ${time}`
}

// witnessReceipt words a step read back from the server's own witness:
// the fact it recorded, plus plainly that this is a memory and not a
// reading -- "seen on 13 Sep at 10:27" -- so it can never be mistaken
// for a current observation the way a bare fact-plus-time could be.
export function witnessReceipt(witness: SetupWitness): string {
  return `${witness.receipt} — seen on ${witnessedWhen(witness.at)}`
}

// decisionReceipt words a recorded decision for the step list. Skip is
// quiet and force is loud, and the two must stay tellable apart at a
// glance, so they are worded as differently as they are coloured.
function decisionReceipt(mark: SetupMark): string {
  if (mark.outcome === 'skipped') return `skipped by ${mark.actor} · ${when(mark.at)}`
  return `forced past by ${mark.actor} · ${when(mark.at)}`
}

// flavourFor maps a check's state onto how its observation line reads.
// Tagging rules is the counting one -- it can only count upward, so any
// arrival there reads as counting rather than as a single arrival.
function flavourFor(key: StepKey, state: StepState): Flavour {
  if (state === 'blocked') return 'attention'
  if (state === 'quiet') return 'quiet'
  if (!arrived(state)) return 'waiting'
  return key === 'rules' ? 'counting' : 'arrived'
}

// buildLedger is the whole ledger in one pure function: the five steps,
// what each one has observed, and where each one stands.
//
// Evidence outranks a mark, always. That is the record's "forced is not
// failed": a step forced past that later receives its evidence turns
// green and stops explaining anybody's silence, while the audit entry
// stays as history rather than as a scar the interface keeps pointing
// at.
// LedgerOptions is what tells buildLedger which ledger is being walked
// (#1284) and which router it is about (#1281). Every field is
// optional, so the first-run call is exactly what it always was.
export interface LedgerOptions {
  // steps is the step set: SETUP_STEPS for first-run setup,
  // ROUTER_STEPS for Add a router and Re-enrol.
  steps?: readonly StepKey[]
  // device is the router this walk is about -- the row the name step
  // created, or the one Re-enrol… named. Empty on a plain setup walk,
  // which reads the fleet as a whole the way it always has.
  device?: string
  // enrolling is true while a token is minted and unspent for `device`.
  enrolling?: boolean
  // reEnrolSince is when this walk's token was minted, so an address
  // accepted before it reads as the old one, still waiting for the new
  // line.
  reEnrolSince?: string
}

export function buildLedger(
  status: SetupStatus,
  devices: Device[],
  address: string,
  backups: RouterBackupsResponse | null = null,
  backupTransport: BackupTransport = 'sftp',
  opts: LedgerOptions = {},
): LedgerStep[] {
  const keys = opts.steps ?? SETUP_STEPS
  const device = opts.device ?? ''
  const checks: Record<StepKey, StepStatus> = {
    ca: caStep(status, address),
    name: nameStep(devices, device),
    syslog: syslogStep(status, devices, device, opts.reEnrolSince ?? ''),
    rules: rulesStep(status),
    push: pushStep(status),
    backup: backupStep(backups, backupTransport),
    register: registerStep(devices, device),
  }
  const receipts: Record<StepKey, string> = {
    ca: caReceipt(status),
    name: nameReceipt(devices, device),
    syslog: syslogReceipt(status, devices, device),
    rules: rulesReceipt(status),
    push: pushReceipt(status),
    backup: backupReceipt(backups),
    register: registerReceipt(devices, device),
  }

  return keys.map((key, i) => {
    const n = i + 1
    const canonical = canonicalStep(key)
    const check = checks[key]
    const mark = markFor(status.marks, canonical)
    // A witness is fleet-wide: some router once satisfied this step. On
    // a router ledger the Send logs step is this router's enrolment
    // (#1281), which another router's connection says nothing about, so
    // that one step reads live evidence only -- otherwise a second
    // router would show Send logs done before it had enrolled, and the
    // wrong-sender box (rendered only while the step waits) never could.
    const witness = device && key === 'syslog' ? undefined : witnessFor(status.witnesses, canonical)
    const hasEvidence = arrived(check.state)
    // A witness only ever speaks when there is nothing better to go on:
    // live evidence outranks it (the record's "forced is not failed"
    // reasoning applies here too -- a witness that later gets its own
    // live evidence back is simply done, the ordinary way), and an
    // operator's own mark for the same step outranks it as well, per
    // #1221's architecture call -- witnessing must never overwrite a
    // skip or force, so reading one back must not either.
    const witnessedOnly = !hasEvidence && !mark && !!witness
    let outcome: Outcome = 'open'
    if (hasEvidence) outcome = 'done'
    else if (mark) outcome = mark.outcome
    else if (witness) outcome = 'done'
    return {
      n,
      canonical,
      key,
      title: TITLES[key],
      lead: key === 'syslog' ? syslogLead(!!opts.enrolling) : LEADS[key],
      status: check,
      // A witnessed-only step reads the same as arrived evidence would
      // -- no waiting dot, no "counting" -- since as far as the operator
      // is concerned it is done; only the receipt says it is a memory.
      flavour: witnessedOnly ? 'arrived' : flavourFor(key, check.state),
      outcome,
      receipt: hasEvidence ? receipts[key] : mark ? decisionReceipt(mark) : witness ? witnessReceipt(witness) : '',
      hasCheck: CHECKED[key],
      enrolling: !!opts.enrolling,
      witnessed: witnessedOnly,
    }
  })
}

// firstOpenStep is where Run setup… reopens the ledger: the first step
// still waiting. A step already decided is not still waiting, so
// reopening does not drop the operator back onto a question they have
// answered -- and if nothing is left open, the ledger opens on its
// first step rather than on the finish, since reopening deliberately
// shows the ledger as it stands.
export function firstOpenStep(ledger: LedgerStep[]): number {
  // A quiet step has nothing to wait for, so reopening does not land on
  // one -- with two exceptions, both steps where quiet does not mean
  // answered. Naming (#1284) stopped being informational when it
  // started creating the router: there is still nothing to wait for,
  // but there is something being asked, and walking past an unanswered
  // question leaves the step after it with no router to mint a token
  // for. Register (#1291, Ruling 24) is the same shape: Next never
  // checks it, so it stays quiet until the operator presses the
  // Register button, and reopening a walk that never got pressed must
  // land back on it rather than reporting nothing left to do.
  const open = ledger.find(
    (s) => s.outcome === 'open' && (s.status.state !== 'quiet' || s.key === 'name' || s.key === 'register'),
  )
  return open?.n ?? 1
}

// forcedPastRecord is the exact line the amber button writes, quoted on
// the button itself before it is pressed. The operator sees the record
// they are about to create, which is the point: the record is the
// feature, so it is never a surprise produced after the fact.
//
// `actor` is what the button quotes; the server resolves the real one
// from the session when it writes, so a client that lied here would be
// caught by its own audit entry disagreeing.
// The step number quoted is the canonical one -- what the server
// actually stores -- and not the row's position in whichever ledger is
// open, because the button's whole job is to quote the record without
// editing it.
export function forcedPastRecord(step: LedgerStep, actor: string, now: Date): string {
  return `setup · step ${step.canonical} forced past · ${notObserved(step)} · ${actor || 'you'} · ${when(now.toISOString())}`
}

// notObserved is the "what was not observed" clause, in mikroview's own
// words rather than a generic "check failed". A check that could not be
// run is a different sentence from one that ran and saw nothing.
export function notObserved(step: LedgerStep): string {
  if (step.status.state === 'blocked') return 'the check could not run on MikroView’s side'
  switch (step.key) {
    case 'ca':
      return 'no router has fetched /ca.crt'
    case 'name':
      return 'no router was named here'
    case 'syslog':
      // With a token minted and unspent, what has not happened is the
      // enrolment, and its consequence is the sentence worth recording
      // (#1281) -- an un-enrolled router's logs do not merely fail to
      // arrive, they arrive and are dropped.
      return step.enrolling
        ? 'router not enrolled; its logs are refused until it is'
        : 'no router has opened a syslog connection'
    case 'rules':
      return 'no events carrying a decoded action have arrived'
    case 'push':
      return 'no pushed table has arrived'
    case 'backup':
      return 'no pushed backup has arrived'
    case 'register':
      return 'this router was not registered'
    default:
      return 'nothing has arrived'
  }
}

// finishHeadline reads the ledger back in one sentence, as the record
// asks: what is true now, then how the five steps stand.
export function finishHeadline(ledger: LedgerStep[]): string {
  // Read by key, not by row: the two steps this sentence is about sit
  // at different numbers in the two ledgers (#1284).
  const rules = ledger.find((s) => s.key === 'rules')
  const syslog = ledger.find((s) => s.key === 'syslog')
  const opening = rules && arrived(rules.status.state)
    ? 'Logs are flowing.'
    : syslog && arrived(syslog.status.state)
      ? 'The router is connected.'
      : 'Nothing has arrived from a router yet.'

  const evidence = ledger.filter((s) => s.outcome === 'done').length
  const skipped = ledger.filter((s) => s.outcome === 'skipped').length
  const forced = ledger.filter((s) => s.outcome === 'forced').length

  const clauses: string[] = []
  clauses.push(
    evidence === 0
      ? 'No step stands on evidence yet'
      : `${count(evidence)} ${evidence === 1 ? 'step stands' : 'steps stand'} on evidence`,
  )
  if (skipped > 0) clauses.push(`${count(skipped)} ${skipped === 1 ? 'was' : 'were'} skipped`)
  if (forced > 0) clauses.push(`${count(forced)} ${forced === 1 ? 'was' : 'were'} forced past`)
  // count() spells its numbers in lower case, which read as a broken
  // sentence directly after the opening one: "Logs are flowing. two
  // steps stand on evidence." (#1166). It is a sentence, so it starts
  // like one.
  const tally = clauses.join('; ')
  return `${opening} ${tally.charAt(0).toUpperCase()}${tally.slice(1)}.`
}

// count words small numbers, because "Four steps stand on evidence"
// reads as a sentence and "4 steps" reads as a readout.
function count(n: number): string {
  return ['zero', 'one', 'two', 'three', 'four', 'five', 'six'][n] ?? String(n)
}

// silenceExplanation is what a surface with nothing to show says about
// why. It is the reach of "the record is the feature": the Stream's
// empty state does not merely say it is empty, it names the step that
// accounts for the silence and who decided it.
//
// Returns null when the ledger explains nothing -- an empty surface with
// no decision behind it is simply empty, and inventing a cause for it
// would be the opposite of this feature.
export function silenceExplanation(marks: SetupMark[]): string | null {
  const forced = marks.filter((m) => m.outcome === 'forced')
  const skipped = marks.filter((m) => m.outcome === 'skipped')
  const first = forced[0] ?? skipped[0]
  if (!first) return null
  const verb = first.outcome === 'forced' ? 'forced past' : 'skipped'
  const note = first.note ? ` — ${first.note}` : ''
  return `Setup step ${first.step} (${STEP_TITLES[first.step - 1] ?? 'unknown'}) was ${verb} by ${first.actor} on ${when(first.at)}${note}.`
}

// SKIP_CONSEQUENCES is what a skipped step costs, stated plainly in the
// ledger's dashed row. The record is explicit that a skipped step is
// never a reproach: it states its consequence, so the operator can see
// what they chose rather than being told off for choosing it.
export const SKIP_CONSEQUENCES: Record<StepKey, string> = {
  ca: 'the router will not trust this certificate, so its TLS connection will fail',
  name: 'no router is created here, so there is nothing to enrol',
  syslog: 'no logs arrive, so the stream stays empty',
  rules: 'events arrive without an action, so rows read "unknown"',
  push: 'the stream stays address-only — no names, no rule lookups, nothing to suggest from',
  backup: 'no backups are kept until the script runs',
  register:
    'the router stays an enrolment nobody finished, and the ledger reopens here next time',
}

// announceStep is what a screen reader is told when the step changes:
// which step, its title, and where it stands. The record asks for
// exactly this sentence -- "Step 4 of 5 — Push router state — waiting
// for the first push" -- rather than the step title alone, which would
// announce a move without announcing what was moved to.
// total is how many steps the open ledger holds -- five on the router
// ledger, six on first-run setup -- so the announcement counts the list
// in front of the operator rather than a list they are not walking.
export function announceStep(step: LedgerStep, total: number = STEP_COUNT): string {
  // A witnessed step has nothing current to speak (#1221): status.detail
  // is whatever the live check falls back to with no evidence in front
  // of it, and reading that aloud would announce a step as waiting that
  // the disc already shows done. The receipt says what actually happened.
  if (step.witnessed) {
    return `Step ${step.n} of ${total} — ${step.title} — ${step.receipt}`
  }
  // A partial step's shortfall is spoken with its arrival, in the order
  // the two boxes are read on screen: a screen reader told only what
  // arrived would hear the step as finished (#1132).
  const observed = [step.status.detail, step.status.shortfall].filter(Boolean).join(' ')
  return `Step ${step.n} of ${total} — ${step.title} — ${observed}`
}

// --- Enrolment wording (#1281) ------------------------------------------
//
// The token line under the Send logs command block, the expired reading
// of it, and the refused-senders warning box. Wording is design, so it
// lives here with the step's own rules rather than in the component.

// TOKEN_LIFETIME_NOTE is what the plain line says a token is good for.
// The minutes are counted from now rather than fixed at fifteen: the
// line is read some time after the mint, and a step whose whole point
// is not making small claims it cannot stand behind cannot print a
// number that quietly stops being true.
export function tokenLine(expiresAt: string, now: Date = new Date()): string {
  const expiry = new Date(expiresAt)
  if (Number.isNaN(expiry.getTime())) return ''
  const minutes = Math.max(0, Math.ceil((expiry.getTime() - now.getTime()) / 60000))
  const at = expiry.toLocaleTimeString(undefined, { hour12: false, hour: '2-digit', minute: '2-digit' })
  return `Token good until ${at} (${minutes} minute${minutes === 1 ? '' : 's'})`
}

// TOKEN_EXPIRED_LINE is the same line once the token has lapsed. The
// command block above it dims: what it prints can no longer be pasted.
export const TOKEN_EXPIRED_LINE = 'Token expired ·'
export const TOKEN_REROLL_EXPIRED_LABEL = 'Reroll to mint another'
export const TOKEN_REROLL_LABEL = 'Reroll'

export function tokenExpired(expiresAt: string, now: Date = new Date()): boolean {
  const expiry = new Date(expiresAt)
  if (Number.isNaN(expiry.getTime())) return false
  return expiry.getTime() <= now.getTime()
}

// refusedWarning is the partial-step warning box (#1132's shape) the
// Send logs step raises while it waits: lines did arrive, from an
// address that is not enrolled, and were dropped. It never claims the
// address is this router -- only the operator knows that, which is the
// same "MikroView can't tell X. You can." shape #442 uses.
export function refusedWarning(refused: RefusedSender[]): string {
  if (refused.length === 0) return ''
  const which = prose(refused.map((r) => r.ip))
  return (
    `Lines from ${which} arrived without the enrol line and were refused — if that is this ` +
    'router, paste the whole block, last line included.'
  )
}

// REFUSED_STRIP_LEAD heads the fleet's own quiet strip, present only
// when something has actually been refused. There is no accept control
// under it, by ruling: an address is accepted only by a router
// presenting a token.
export const REFUSED_STRIP_LEAD =
  'Refused senders — logs from an address that is not enrolled are dropped.'

// refusedSince keeps only the addresses first seen after this walk's
// token was minted: an address refused last week is the fleet strip's
// business, not evidence about the block the operator just pasted.
export function refusedSince(refused: RefusedSender[], since: string): RefusedSender[] {
  if (!since) return []
  // Instants, not strings: the server stamps in its own zone, the
  // browser mints in UTC, and the two only sort alike by luck.
  const from = Date.parse(since)
  return refused.filter((r) => Date.parse(r.firstSeen) >= from)
}
