// SPDX-License-Identifier: AGPL-3.0-only

// The full-screen setup wizard's run model (#1381), ported from the
// ratified prototype's wizard.js (docs/design/screens/wizard/round-15:
// STEPS, routerDone, stageOf, reachedIdx, rowState, renderFoot,
// renderBar). Pure functions of two inputs -- the answers this run has
// given, and the evidence the server holds -- so the gating and the
// receipts stay testable without a browser: a row that unlocks a step
// early, or a receipt in the wrong ink, is the kind of mistake this
// wizard exists to avoid.
//
// Nothing here talks to the server, and nothing here connects to a
// router (the AGENTS.md invariant). Evidence is read; it is never
// fetched from this module.

export type StepId = 'router' | 'pass' | 'paste' | 'tune' | 'stand'

export const STEPS: readonly { id: StepId; title: string }[] = [
  { id: 'router', title: 'The router' },
  { id: 'pass', title: 'Mint the token' },
  { id: 'paste', title: 'Paste once' },
  { id: 'tune', title: 'Tag firewall rules' },
  { id: 'stand', title: 'Where setup stands' },
]

// The run's own stage. 'ask' covers the two question steps (q < 4 is
// the router form, q === 4 the password); 'paste' is the block shown
// and not yet copied; 'watch' is the router's turn; 'tune' the rule
// list; 'done' the ledger.
export type Stage = 'ask' | 'paste' | 'watch' | 'tune' | 'done'

// What this run has answered or done. Everything the operator can
// change without the server knowing.
export interface RunAnswers {
  stage: Stage
  q: number
  name: string
  addr: string
  pass: string
  push: boolean | null
  backup: boolean | null
  copied: boolean
  tuneCopied: boolean
  tuneSkipped: boolean
  chosenCount: number
  tunedAt: string
}

// What the server holds. Each timestamp is '' until that proof landed;
// a chip, a receipt and a station all read the same field.
export interface Evidence {
  cert: string
  enrol: string
  push: string
  backup: string
  // The address the enrol line arrived from -- the only address this
  // router's logs are accepted from afterwards.
  from: string
  lines: number
  // An address whose lines were refused while this run waited.
  refused: string
  version: string
  ahead: boolean
  // Rules: true once lines carrying a new prefix have landed after the
  // tagging block was copied.
  tagged: boolean
  tokenUntil: string
  // The strip's boundaries once the push names them: lane ink ('' when
  // dark) and whether the boundary is watched.
  boundaries: { lane: string; watched: boolean; dark: boolean }[]
}

export const NO_EVIDENCE: Evidence = {
  cert: '',
  enrol: '',
  push: '',
  backup: '',
  from: '',
  lines: 0,
  refused: '',
  version: '',
  ahead: false,
  tagged: false,
  tokenUntil: '',
  boundaries: [],
}

export function freshAnswers(): RunAnswers {
  return {
    stage: 'ask',
    q: 0,
    name: '',
    addr: '',
    pass: '',
    push: null,
    backup: null,
    copied: false,
    tuneCopied: false,
    tuneSkipped: false,
    chosenCount: 0,
    tunedAt: '',
  }
}

// A router address is a bare address and nothing else: no port, no
// name, no prefix length (owner, round 7: "the router ip block rejects
// incorrect formatting for IPs"). The wording is the record's own
// (#1380). The server's rule is netip.ParseAddr (internal/device/
// enrolment.go, validateExpectedAddress), so whatever passes here must
// parse there too: a dotted quad without leading zeros, or an IPv6
// literal. The check is a courtesy; the server keeps rejecting.
export function addrProblem(v: string): string {
  if (!v) return ''
  if (v.includes(':') && !v.includes('.') && isIPv6(v)) return ''
  if (/[:/]/.test(v)) {
    return v.includes(':')
      ? 'No port here — just the address. The port is MikroView’s side.'
      : 'No prefix length — the router’s own address, not its network.'
  }
  if (/[^0-9.]/.test(v)) return 'A name will not do: the enrolment window binds to an address.'
  const p = v.split('.')
  if (p.length !== 4 || p.some((x) => !/^(0|[1-9][0-9]{0,2})$/.test(x) || Number(x) > 255)) {
    return 'Four numbers, 0–255, separated by dots.'
  }
  return ''
}

// isIPv6 is the shape netip.ParseAddr accepts for a plain IPv6 literal:
// eight hex groups of up to four digits, or fewer with one "::". Zones
// and embedded IPv4 tails are left to the server -- the client accepting
// less than the server is the safe direction.
function isIPv6(v: string): boolean {
  if (!/^[0-9a-fA-F:]+$/.test(v)) return false
  const halves = v.split('::')
  if (halves.length > 2) return false
  const groups = halves.flatMap((h) => (h === '' ? [] : h.split(':')))
  if (groups.some((g) => g === '' || g.length > 4)) return false
  return halves.length === 2 ? groups.length <= 7 : groups.length === 8
}

export function routerDone(s: RunAnswers): boolean {
  return !!s.name && !!s.addr && !addrProblem(s.addr) && s.push !== null && s.backup !== null
}

// Everything chosen has arrived: the certificate, the enrol line, and
// the push and the backup where they were said yes to.
export function arrivedAll(s: RunAnswers, ev: Evidence): boolean {
  return !!ev.cert && !!ev.enrol && (s.push === false || !!ev.push) && (s.backup === false || !!ev.backup)
}

export function stageOf(s: RunAnswers): StepId {
  if (s.stage === 'ask') return s.q === 4 ? 'pass' : 'router'
  if (s.stage === 'watch') return 'paste'
  if (s.stage === 'done') return 'stand'
  return s.stage
}

// Which steps are reached: everything up to the furthest the run has got.
export function reachedIdx(s: RunAnswers): number {
  if (s.stage === 'done') return 4
  if (s.stage === 'tune') return 3
  if (s.stage === 'paste' || s.stage === 'watch') return 2
  return s.q === 4 ? 1 : 0
}

// hms formats an instant as the wall-clock time the receipts and chips
// show (14:03:04). '' stays ''; an unparseable value is shown as given
// rather than hidden.
export function hms(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleTimeString(undefined, { hour12: false })
}

export type RowClass = '' | 'done' | 'chosen'

// The ink a row's receipt wears: the ink of what it records (DESIGN.md,
// "One ink per thing"). '' is the rail's own muted colour.
export type Ink = '' | 'cert' | 'logs' | 'push' | 'backup' | 'token' | 'rules' | 'off'

export interface RowState {
  cls: RowClass
  receipt: string
  ink: Ink
}

export function rowState(s: RunAnswers, ev: Evidence, id: StepId): RowState {
  const a: RowState = { cls: '', receipt: '', ink: '' }
  switch (id) {
    case 'router':
      if (ev.enrol) {
        a.cls = 'done'
        a.ink = 'logs'
        a.receipt = `${s.name} · enrolled ${ev.from} · ${hms(ev.enrol)}`
      } else if (routerDone(s)) {
        a.cls = 'chosen'
        a.ink = 'token'
        a.receipt = `${s.name} · ${s.addr} · push ${s.push ? 'yes' : 'not now'} · backup ${s.backup ? 'yes' : 'not now'}`
      } else a.receipt = 'name, address, push, backup'
      break
    case 'pass':
      if (ev.tokenUntil) {
        a.cls = ev.cert ? 'done' : 'chosen'
        a.ink = 'token'
        a.receipt = `token good until ${ev.tokenUntil}`
      } else a.receipt = 'your password, once'
      break
    case 'paste':
      if (arrivedAll(s, ev)) {
        a.cls = 'done'
        a.ink = 'logs'
        a.receipt = `everything arrived · ${ev.lines.toLocaleString()} lines`
      } else if (ev.cert) {
        a.cls = 'chosen'
        a.ink = 'cert'
        a.receipt = 'certificate fetched · waiting'
      } else if (s.copied) a.receipt = 'copied · waiting for the certificate'
      else a.receipt = 'one block, into the terminal'
      break
    case 'tune':
      if (s.push === false) a.receipt = 'needs the push'
      else if (ev.tagged) {
        a.cls = 'done'
        a.ink = 'rules'
        a.receipt = `${s.chosenCount} rules tagged · ${s.tunedAt}`
      } else if (s.tuneSkipped) {
        a.cls = 'chosen'
        a.ink = 'off'
        a.receipt = 'left dark'
      } else a.receipt = 'proposed from the push'
      break
    case 'stand':
      a.cls = s.stage === 'done' ? 'done' : ''
      if (a.cls === 'done') a.ink = 'logs'
      break
  }
  return a
}

export interface RailRow {
  id: StepId
  title: string
  // The number in the disc; '✓' for the ledger row.
  n: string
  state: RowState
  current: boolean
  // A step ahead of the furthest reached: dashed, dim, not clickable.
  locked: boolean
  // An earlier completed step, while the run is still yours to change --
  // before the paste lands. Once the router is answering nothing goes
  // back.
  can: boolean
}

export function railRows(s: RunAnswers, ev: Evidence): RailRow[] {
  const cur = stageOf(s)
  const reached = reachedIdx(s)
  return STEPS.map((d, i) => {
    const locked = i > reached
    const can = !locked && i < reached && s.stage !== 'watch' && s.stage !== 'tune' && s.stage !== 'done'
    return {
      id: d.id,
      title: d.title,
      n: d.id === 'stand' ? '✓' : String(i + 1),
      state: rowState(s, ev, d.id),
      current: cur === d.id,
      locked,
      can,
    }
  })
}

// The footer, as renderFoot draws it: a left control, a hint, and the
// right-hand controls, with Next's enabled state.
export type FootAction =
  | 'router-next'
  | 'back-router'
  | 'back-pass'
  | 'mint'
  | 'to-tune'
  | 'tune-skip'
  | 'to-done'
  | 'another'
  | 'finish'
  | 'none'

export interface FootButton {
  label: string
  action: FootAction
  primary: boolean
  disabled: boolean
}

export interface FootSpec {
  left: FootButton | null
  hint: string
  right: FootButton[]
}

export function footSpec(s: RunAnswers, ev: Evidence): FootSpec {
  const st = stageOf(s)
  if (st === 'router') {
    const ok = routerDone(s)
    return {
      left: null,
      hint: ok ? '' : 'All four, then Next',
      right: [{ label: 'Next', action: 'router-next', primary: true, disabled: !ok }],
    }
  }
  if (st === 'pass') {
    return {
      left: { label: 'Back', action: 'back-router', primary: false, disabled: false },
      hint: '',
      right: [{ label: 'Mint the token', action: 'mint', primary: true, disabled: !s.pass }],
    }
  }
  if (st === 'paste' && s.stage === 'paste') {
    return {
      left: { label: 'Back', action: 'back-pass', primary: false, disabled: false },
      hint: 'Next checks what has arrived',
      right: [{ label: 'Next', action: 'none', primary: true, disabled: true }],
    }
  }
  if (st === 'paste') {
    const ok = arrivedAll(s, ev)
    return {
      left: null,
      hint: ok ? '' : 'Next checks what has arrived',
      right: [{ label: 'Next', action: 'to-tune', primary: true, disabled: !ok }],
    }
  }
  if (st === 'tune') {
    return {
      left: null,
      hint: ev.tagged ? '' : 'Next checks what has arrived',
      right: [
        { label: 'Skip this step', action: 'tune-skip', primary: false, disabled: false },
        { label: 'Next', action: 'to-done', primary: true, disabled: !ev.tagged },
      ],
    }
  }
  return {
    left: { label: 'Add another router', action: 'another', primary: false, disabled: false },
    hint: '',
    right: [{ label: 'Finish', action: 'finish', primary: true, disabled: false }],
  }
}

// The bar's chips, as proofs arrive, each in the ink of what it records
// (renderBar). `kind` is the .att modifier class.
export interface Chip {
  kind: 'dec' | 'alarm' | 'cert' | 'logs' | 'push' | 'dark' | 'now' | 'backup' | 'rules'
  text: string
}

export function chipsFor(s: RunAnswers, ev: Evidence): Chip[] {
  const chips: Chip[] = []
  if (s.name) chips.push({ kind: 'dec', text: s.name })
  if (ev.refused) chips.push({ kind: 'alarm', text: `refused · ${ev.refused}` })
  if (ev.cert) chips.push({ kind: 'cert', text: `cert · ${hms(ev.cert)}` })
  if (ev.enrol) chips.push({ kind: 'logs', text: `logs · ${hms(ev.enrol)}` })
  if (ev.push) {
    chips.push({ kind: 'push', text: 'push · 20 min' })
    const dark = ev.boundaries.filter((b) => b.dark).length
    chips.push(dark ? { kind: 'dark', text: `${dark} dark` } : { kind: 'logs', text: 'every boundary watched' })
    if (ev.ahead) chips.push({ kind: 'now', text: `RouterOS ${ev.version} — ahead of review` })
  }
  if (ev.backup) chips.push({ kind: 'backup', text: 'backup · 03:00' })
  if (ev.tagged) chips.push({ kind: 'rules', text: `${s.chosenCount} rules` })
  return chips
}

// The strip under the bar: one tick until the push names boundaries,
// grey until the router speaks and green once logs flow; then one tick
// per boundary in its lane.
export interface StripTick {
  lane: string
  on: boolean
  dark: boolean
}

export function stripFor(ev: Evidence): StripTick[] {
  if (ev.push && ev.boundaries.length > 0) {
    return ev.boundaries.map((b) => ({ lane: b.watched ? b.lane : '', on: b.watched, dark: b.dark }))
  }
  return [{ lane: ev.enrol ? 'var(--accept)' : '', on: !!ev.enrol, dark: false }]
}

// What a step change announces (DESIGN.md, "Keyboard, motion,
// announcements"): the step, its title, and its receipt.
export function announce(rows: RailRow[]): string {
  const row = rows.find((r) => r.current)
  if (!row) return ''
  const n = row.id === 'stand' ? 'Where setup stands' : `Step ${row.n} of ${STEPS.length - 1}`
  const tail = row.state.receipt ? ` — ${row.state.receipt}` : ''
  return row.id === 'stand' ? `${n}${tail}` : `${n} — ${row.title}${tail}`
}

// --- The router's turn: the track (ported from track.js's stations) ---
//
// One wire, a station per proof. The track "stays green" (DESIGN.md,
// "One ink per thing", round 9's note) -- every station's ink is the
// same accept green once it lands, so a station only needs its state,
// not its own ink. Set-aside stations (push/backup answered No) are
// 'skip': dashed and struck. A station this run has not reached yet
// is 'later' rather than 'wait' -- the same distinction track.js's own
// stations() makes, just without a per-station ink lookup.
//
// The rules station (tuneBody's tagging, and the compact track on
// ✓ Where setup stands, #1385) only ever appears once push was said
// yes to and the run has reached the rules step or beyond -- never
// while stage is still 'watch', so the paste step's own track
// (StepPaste.svelte) is unaffected by its addition below.
export type TrackState = 'wait' | 'done' | 'skip' | 'alarm' | 'later'

export interface TrackStation {
  id: 'copy' | 'cert' | 'enrol' | 'push' | 'backup' | 'rules'
  lab: string
  st: string
  state: TrackState
}

export function trackStations(s: RunAnswers, ev: Evidence, copiedAt: string): TrackStation[] {
  const stations: TrackStation[] = []
  stations.push({
    id: 'copy',
    lab: 'copied',
    st: s.copied ? hms(copiedAt) : 'not yet',
    state: s.copied ? 'done' : 'wait',
  })
  stations.push({
    id: 'cert',
    lab: 'certificate',
    st: ev.cert ? hms(ev.cert) : 'fetches /ca.crt',
    state: ev.cert ? 'done' : s.copied ? 'wait' : 'later',
  })
  if (ev.refused) {
    stations.push({ id: 'enrol', lab: 'refused', st: `lines from ${ev.refused}`, state: 'alarm' })
  } else {
    stations.push({
      id: 'enrol',
      lab: 'logs',
      st: ev.enrol ? `${hms(ev.enrol)} · ${ev.lines.toLocaleString()} lines` : `enrol line from ${s.addr}`,
      state: ev.enrol ? 'done' : ev.cert ? 'wait' : 'later',
    })
  }
  if (s.push === false) {
    stations.push({ id: 'push', lab: 'push', st: 'not now · address-only', state: 'skip' })
  } else {
    stations.push({
      id: 'push',
      lab: 'push',
      st: ev.push ? `${hms(ev.push)} · v${ev.version}` : 'end of the block',
      state: ev.push ? 'done' : ev.enrol ? 'wait' : 'later',
    })
  }
  if (s.backup === false) {
    stations.push({ id: 'backup', lab: 'backup', st: 'not now · none kept here', state: 'skip' })
  } else {
    stations.push({
      id: 'backup',
      lab: 'backup',
      st: ev.backup ? `${hms(ev.backup)} · 03:00` : 'after the push',
      state: ev.backup ? 'done' : ev.push || (s.push === false && !!ev.enrol) ? 'wait' : 'later',
    })
  }
  // The rules station: only once push was said yes to, and only once
  // there is something to say about it -- tagged, skipped, or the run
  // has actually reached the rules step (track.js's own stations()
  // gate, ported: `s.push !== false && (stage tune/done || tagged ||
  // skipped)`). Never present during the paste step's own 'watch'
  // stage, since none of tagged/tuneSkipped/stage-tune-or-done can hold
  // yet there.
  if (s.push !== false && (s.stage === 'tune' || s.stage === 'done' || ev.tagged || s.tuneSkipped)) {
    if (ev.tagged) {
      stations.push({ id: 'rules', lab: `${s.chosenCount} rules`, st: `lit since ${s.tunedAt}`, state: 'done' })
    } else if (s.tuneSkipped) {
      stations.push({ id: 'rules', lab: 'rules', st: 'left dark', state: 'skip' })
    } else {
      stations.push({
        id: 'rules',
        lab: 'rules',
        st: s.tuneCopied ? 'waiting for the first new line' : 'touch a dark column',
        state: s.tuneCopied ? 'wait' : 'later',
      })
    }
  }
  return stations
}

// --- ✓ · Where setup stands: the ledger (#1385) -----------------------
//
// A row per thing the wizard sets up -- certificate, logs, router state,
// backup, rules -- ported from the prototype's doneBody. Certificate and
// logs are always 'done': the ledger only ever shows once arrivedAll has
// been true (Wizard.svelte's own stageOf/footSpec gating), so there is
// nothing left to be dashed about for either of them. Router state,
// backup and rules are each either done with a receipt, or dashed and
// struck with "not now · <consequence>" in the design's own words
// (DESIGN.md, "✓ · Where setup stands").
export type LedgerRowId = 'cert' | 'logs' | 'push' | 'backup' | 'tune'

export interface LedgerRow {
  done: boolean
  // The ledger's own row title, distinct from the step's title.
  t: string
  // The receipt or the "not now" line underneath it.
  r: string
  ink: Ink
  // u names which Undo this row has, '' for a row with none -- a
  // dashed, set-aside row is never offered Undo, since there is nothing
  // on the router to undo.
  u: LedgerRowId | ''
}

export function ledgerRows(s: RunAnswers, ev: Evidence): LedgerRow[] {
  const rows: LedgerRow[] = [
    { done: true, t: 'Certificate trusted', r: `fetched by ${ev.from} · ${hms(ev.cert)}`, ink: 'cert', u: 'cert' },
    {
      done: true,
      t: 'Logs flowing',
      r: `enrolled at ${ev.from} · ${hms(ev.enrol)} · ${ev.lines.toLocaleString()} lines`,
      ink: 'logs',
      u: 'logs',
    },
  ]
  if (s.push) {
    rows.push({
      done: true,
      t: 'Router state pushed',
      r: `every 20 minutes · first ${hms(ev.push)} · RouterOS ${ev.version}`,
      ink: 'push',
      u: 'push',
    })
  } else {
    rows.push({ done: false, t: 'Router state', r: 'not now · the fall stays address-only', ink: '', u: '' })
  }
  if (s.backup) {
    rows.push({ done: true, t: 'Nightly backup', r: `03:00 · scheduled ${hms(ev.backup)}`, ink: 'backup', u: 'backup' })
  } else {
    rows.push({ done: false, t: 'Backup', r: 'not now · no backups kept here', ink: '', u: '' })
  }
  if (s.push) {
    if (ev.tagged) {
      rows.push({
        done: true,
        t: 'Rules tagged',
        r: `${s.chosenCount} rules log · since ${s.tunedAt}`,
        ink: 'rules',
        u: 'tune',
      })
    } else {
      const dark = ev.boundaries.filter((b) => b.dark).length
      rows.push({ done: false, t: 'Rules', r: `left dark · ${dark} boundaries log nothing`, ink: '', u: '' })
    }
  }
  return rows
}

// undoOrder is "undo everything on the router first" (DESIGN.md): every
// green row's Undo id, in the same order the ledger lists them -- cert,
// logs, then push/backup/tune only where each one stands.
export function undoOrder(rows: readonly LedgerRow[]): LedgerRowId[] {
  return rows.filter((r) => r.done && r.u).map((r) => r.u as LedgerRowId)
}

// latestArrivalHeadline is the observation line's own sentence once
// something beyond Copy has landed (watchBody's `latest`): the last
// station in wire order whose state is 'done', in the full sentence its
// own arrival earns -- not just the track's short label.
export interface LatestArrival {
  text: string
  small: string
}

export function latestArrivalHeadline(stations: TrackStation[], ev: Evidence, name: string): LatestArrival | null {
  const done = stations.filter((x) => x.state === 'done')
  const last = done[done.length - 1]
  if (!last || last.id === 'copy') return null
  const text =
    last.id === 'cert'
      ? `Certificate fetched by ${ev.from}`
      : last.id === 'enrol'
        ? `Enrol line from ${ev.from}`
        : last.id === 'push'
          ? `First push from ${name}`
          : 'Nightly backup scheduled'
  return { text, small: last.st.split(' · ')[0] }
}

// refusedFixBlock is the refused-sender warning box's copyable fix
// (DESIGN.md, "Refused sender"): the syslog step's own guarded action
// line -- which sets src-address back to 0.0.0.0, "let the router
// pick" (internal/routeros/commands.go's SyslogCommands, #1370) -- and
// the enrol line last, skipping the topics guard in between since that
// line never needed re-pasting. Built from the server's own rendered
// syslog block rather than a hand-written RouterOS line (AGENTS.md);
// falls back to the whole block if it is not the shape this expects
// (e.g. no token minted yet, so there is no enrol line to keep last).
export function refusedFixBlock(syslogCommands: string): string {
  const lines = syslogCommands.split('\n').filter(Boolean)
  if (lines.length === 0) return ''
  const first = lines[0]
  const last = lines[lines.length - 1]
  if (lines.length > 1 && last.startsWith('/log info "mikroview-enrol')) return `${first}\n${last}`
  return first
}
