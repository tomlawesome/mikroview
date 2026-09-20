<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The setup wizard as a modal (#487), replacing the wizard page
  // wholesale. Built from docs/design/screens/wizard/DESIGN.md, the
  // ratified record -- where it and a mockup disagree, the record wins.
  //
  // The model is a claim ledger. Mikroview never connects to a router
  // (the AGENTS.md invariant), so every check here is an observation of
  // what arrived, and each step ends in exactly one of: done, in green;
  // skipped, in the same solid style but --log's bright blue, since a
  // step seen and declined is a decision, not a gap (#1216); or forced
  // past, in --caution's yellow -- pushed through without evidence, so
  // it reads as a caution rather than either choice. The record is the
  // feature -- a forced-past line reaches the step list, the audit log,
  // and every empty state whose silence it explains.
  //
  // A step done from evidence mikroview no longer has -- typically
  // because a restart emptied the memory-backed store that saw it --
  // still shows done's own green disc, from the server's own witness
  // (#1221); only its receipt changes, to a past-tense, dated line that
  // never claims to be a current reading. See setupsteps.ts's
  // witnessReceipt and LedgerStep.witnessed.
  //
  // Mounted once, beside the other overlays in App.svelte rather than
  // inside the rail that opens it: the rail unmounts when it is docked
  // and does not exist at all on a phone, and the modal outlives both.

  import { createToken, routerBackupDownloadUrl } from '../lib/api'
  import { authState } from '../lib/auth.svelte'
  import { appState } from '../lib/state.svelte'
  import { viewportState } from '../lib/viewport.svelte'
  import { trapFocus } from '../lib/focusTrap'
  import { wizardState } from '../lib/wizard.svelte'
  import { journeyState } from '../lib/journey.svelte'
  import { newestGeneration, vaultGated } from '../lib/backups'
  import { downloadFromUrl } from '../lib/export'
  import {
    HOW_TO_MOUNT_URL,
    KEY_DIR,
    KEY_FILE_CONTAINER_PATH,
    KEY_FILE_PATH,
    forgetHistoryKeyForSession,
    loadOrMintHistoryKey,
    newHistoryKey,
    saveHistoryKeyForSession,
  } from '../lib/history'
  import {
    announceStep,
    backupBlockedLines,
    backupLead,
    backupReceiptForDevice,
    BACKUP_NO_SCRIPT_HEADING,
    BACKUP_PORT_NOTE_HTTPS,
    BACKUP_TRANSPORTS,
    BACKUP_WAITING_NO_SCRIPT,
    finishHeadline,
    forcedPastRecord,
    notObserved,
    NO_COMMAND_HEADING,
    NO_ADDRESS_LINE,
    portOf,
    prose,
    sourceSplits,
    arrivingAddresses,
    srcAddressCommand,
    refusedWarning,
    SKIP_CONSEQUENCES,
    tokenExpired,
    tokenLine,
    TOKEN_EXPIRED_LINE,
    TOKEN_REROLL_EXPIRED_LABEL,
    TOKEN_REROLL_LABEL,
    TITLES,
    type LedgerStep,
    type StepKey,
  } from '../lib/setupsteps'
  import type { RouterosStanding } from '../lib/types'
  import CopyButton from './CopyButton.svelte'
  import MemoryControl from './MemoryControl.svelte'

  // Steps land seconds to minutes apart (the documented push scheduler
  // runs every 20 minutes), so this polls rather than streaming -- and
  // only while the modal is open, which is why the interval is set up
  // and torn down by the same effect that watches `open`.
  const POLL_MS = 5000

  const isAdmin = $derived(authState.state === 'authenticated' && authState.role === 'admin')

  // One read of the ledger on sign-in, whether or not the modal is ever
  // opened: the surfaces that explain their own silence (the Stream's
  // empty state) need the marks, and #490 makes setup status readable by
  // any signed-in user for exactly that kind of reason.
  $effect(() => {
    if (authState.state !== 'authenticated') return
    wizardState.refresh()
  })

  // The record's auto-launch: first admin sign-in with no router
  // sending, after the shell has painted. appState.initialLoadDone is
  // that paint -- the shell has its rail, its ghost rows and its honest
  // empty states before anything is layered over them.
  $effect(() => {
    if (!isAdmin) return
    if (!appState.initialLoadDone) return
    // The journey (#646) owns first-run launch timing on the path it
    // covers -- right after a brand-new instance's admin account is
    // made -- walking Attach, Connecting and the glass before it opens
    // this itself. Deferring here keeps this check exactly as it was
    // for every other path (a returning admin whose router still has
    // nothing sent).
    if (journeyState.active) return
    wizardState.maybeAutoLaunch(appState.devices.length > 0)
  })

  $effect(() => {
    if (!wizardState.open) return
    const timer = setInterval(() => wizardState.refresh(), POLL_MS)
    return () => clearInterval(timer)
  })

  // Step 6's own evidence (#394): the same admin-only endpoint the
  // Settings backups group reads, polled the same way status/devices
  // above are -- and, like them, only while the modal is actually open.
  $effect(() => {
    if (!wizardState.open) return
    wizardState.refreshBackups()
    const timer = setInterval(() => wizardState.refreshBackups(), POLL_MS)
    return () => clearInterval(timer)
  })

  // The refused senders (#1281), on the same cadence and for the same
  // reason as the two polls above: the Send logs step's warning box is
  // a reading of what has arrived while it waits.
  $effect(() => {
    if (!wizardState.open) return
    wizardState.refreshRefused()
    const timer = setInterval(() => wizardState.refreshRefused(), POLL_MS)
    return () => clearInterval(timer)
  })

  // openLostRouter (#394) always names the router step 6 is about, so
  // the token picker below is pre-set to it rather than left on
  // whichever device step 4 last touched.
  $effect(() => {
    const device = wizardState.lostRouterDevice
    if (device) wizardState.tokenDevice = device
  })

  const ledger = $derived(wizardState.ledger)
  // stepCount is the open ledger's own length (#1284): six on first-run
  // setup, five on the router ledger. Everything that used to read the
  // module's fixed STEP_COUNT reads this instead, so "Step 2 of 5" is
  // true of the list in front of the operator.
  const stepCount = $derived(ledger.length)
  const step = $derived<LedgerStep | undefined>(
    wizardState.pane <= stepCount ? ledger[wizardState.pane - 1] : undefined,
  )
  // paneKey is which step the pane is on without reading the ledger at
  // all -- the ledger is rebuilt on every 5s poll, and an effect that
  // depended on it would re-run on every tick for no reason.
  const paneKey = $derived<StepKey | undefined>(wizardState.steps[wizardState.pane - 1])
  const onFinish = $derived(wizardState.pane === wizardState.finishPane)

  // The source-address split (#442): each declared device the server has
  // paired with undeclared addresses that are streaming, and those
  // addresses once. Step 2's body below the observation line states
  // both facts and prints the remedy; it never claims the two are one
  // router, because only the operator knows.
  const splits = $derived(sourceSplits(wizardState.devices))
  const arriving = $derived(arrivingAddresses(splits))

  // The Send logs step used to mint its token on entry (#1281). It
  // cannot any more: since #1291 minting needs the router's own address
  // -- the enrolment window opens for that one address and nothing else
  // -- and the admin's password re-typed at that moment. Neither is
  // something the wizard can supply on the operator's behalf, so the
  // step asks first and mints when they say so.
  //
  // rerollAsked reopens the same form for a Reroll, which goes through
  // the same endpoint and so asks again every time.
  let rerollAsked = $state(false)
  const mintFormOpen = $derived(!wizardState.enrolment || rerollAsked)
  $effect(() => {
    if (!wizardState.open) {
      // The name field goes with the walk it was typed in: this
      // component outlives the modal, and a name left in the box would
      // be offered to whoever opens the next ledger. The same is true
      // of anything typed into the mint form.
      routerName = ''
      rerollAsked = false
      return
    }
  })

  // mintNow runs the mint the operator asked for and closes the form
  // again on success. The password is cleared by the state object
  // itself, whatever the outcome.
  async function mintNow() {
    await wizardState.mintEnrolmentToken()
    if (wizardState.enrolment && !wizardState.enrolmentError) rerollAsked = false
  }

  // The Register step's name field (#1291), seeded from the router the
  // ledger is about so confirming is one click for the common case
  // where the name has not changed since the name step. Local to the
  // component for routerName's reason below.
  let registerName = $state('')
  let registerSeededFor = ''
  $effect(() => {
    const device = wizardState.ledgerDevice
    if (!device) {
      registerName = ''
      registerSeededFor = ''
      return
    }
    if (registerSeededFor === device) return
    const row = wizardState.devices.find((d) => d.id === device)
    registerName = row?.name || device
    registerSeededFor = device
  })

  // The name step's one field (#1284). Local to the component: what
  // outlives it is the router the name created, which is
  // wizardState.ledgerDevice.
  let routerName = $state('')
  let nameError = $state<string | null>(null)
  let naming = $state(false)

  // createRouter is what Next does on the name step: naming a router is
  // what creates it, so there is one act and not a field plus a save.
  async function createRouter(): Promise<boolean> {
    const name = routerName.trim()
    if (!name) {
      nameError = 'Give the router a name.'
      return false
    }
    naming = true
    nameError = await wizardState.createRouter(name)
    naming = false
    return nameError === null
  }

  // The token line under the Send logs block is read against a clock
  // that moves, not against the moment the step was opened: "good until
  // 14:17 (3 minutes)" has to count down, and an expired token has to
  // say so without waiting for the operator to do something. Ticked by
  // the same poll that reads everything else while the modal is open.
  let clock = $state(Date.now())
  $effect(() => {
    if (!wizardState.open) return
    clock = Date.now()
    const timer = setInterval(() => (clock = Date.now()), POLL_MS)
    return () => clearInterval(timer)
  })
  const enrolExpired = $derived(
    !!wizardState.enrolment && tokenExpired(wizardState.enrolment.expiresAt, new Date(clock)),
  )
  const enrolLine = $derived(
    wizardState.enrolment ? tokenLine(wizardState.enrolment.expiresAt, new Date(clock)) : '',
  )

  // The refused senders that arrived since this walk's token was minted
  // (#1281) -- the partial-step warning box's own reading, and only
  // while the step is still waiting for its enrol line.
  const refusedLine = $derived(refusedWarning(wizardState.refusedForThisWalk))

  // The heavy warning takes the step body in place, rather than stacking
  // a second dialog on the first. Cleared on every pane change: a
  // warning is about the step it was raised on and nothing else.
  let warning = $state(false)
  let busy = $state(false)
  let copied = $state('')

  // The reader (#1219): one paste block opened at the full height of
  // the step column, the modal grown to the veil's edge, the step list
  // untouched on the left. The owner's shape -- the small box stays as
  // the summary; its drawer handle opens this. The text is captured at
  // the click: nothing regenerates a script while its reader is open
  // (the version picker and the mint form live in the body this
  // replaces), and a stale capture misquoting a live script would be
  // worse than a missed refresh.
  let expandedPaste = $state<{ key: string; label: string; text: string } | null>(null)

  // Step 4 acts before it waits: the token is created on entry, with its
  // own audit line, and the script below is written with it already in
  // place. Minting on entry is only unambiguous when mikroview knows
  // exactly one router -- with several, which router the token is scoped
  // to is the operator's call, so the picker stands in for "entry".
  //
  // The token and the router it is for live on wizardState since #1183:
  // they have to outlive this component, or a second visit to the push
  // step mints a second key with the same name. See that field's own
  // comment.
  const token = $derived(wizardState.token)
  const tokenDevice = $derived(wizardState.tokenDevice)
  let tokenError = $state<string | null>(null)
  let minting = $state(false)
  // Whether entering step 4 has already reached for a token. Without
  // it, a mint that fails leaves the effect's conditions exactly as it
  // found them, and the effect tries again immediately -- a failing
  // token endpoint would be called in a tight loop rather than reported
  // once.
  let mintAttempted = false
  // #1009: whether this visit to step 4/6 has already read a non-empty
  // device list. The single-device convenience below is only honest at
  // the moment the picker would first appear -- devices is polled every
  // POLL_MS regardless, and without this the effect re-applies "skip the
  // picker" every time the count merely passes through one on its way
  // to settling. That already yanked the form (and its <select>) out
  // from under an operator -- or Playwright -- mid-pick, the moment a
  // late-arriving device made the count read 1 for one poll. The first
  // non-empty read decides, and later polls no longer move the form:
  // an empty list (the normal first-run case -- no router has reported
  // yet) does not count as a look, or the operator who opens the wizard
  // before the first router shows up would be stuck with the picker,
  // one-router shortcut and all, forever.
  let deviceCountSeen = false

  $effect(() => {
    // Reading pane is what re-runs this on every move.
    void wizardState.pane
    warning = false
    nameError = null
    copied = ''
    // A reader is about the step it was opened on and nothing else --
    // so a step-list click while one is open lands on that step's
    // ordinary body, never on another step's script.
    expandedPaste = null
  })

  $effect(() => {
    if ((paneKey !== 'push' && paneKey !== 'backup') || !wizardState.open) {
      deviceCountSeen = false
      return
    }
    if (token || minting || mintAttempted || deviceCountSeen) return
    // A walk that already has a router (#1284: the name step created
    // one, or Re-enrol… named it) has no picker to skip -- the token is
    // for that router, and the device list cannot change the answer.
    if (wizardState.ledgerDevice) {
      deviceCountSeen = true
      mintAttempted = true
      wizardState.tokenDevice = wizardState.ledgerDevice
      mintToken()
      return
    }
    const known = wizardState.devices
    if (known.length === 0) return
    deviceCountSeen = true
    if (known.length === 1) {
      mintAttempted = true
      wizardState.tokenDevice = known[0].id
      mintToken()
    }
  })

  // Step 4 and step 6 share this one token: the ingest token step 4
  // mints is the same credential internal/backupsftp checks against
  // (#394 -- "no new credential type"), so a router that has already
  // been pushed to has everything step 6's script needs, and minting
  // again here is only ever a fresh one for a router that skipped step
  // 4 or is being replaced (mintNewBackupToken below).
  async function mintToken() {
    tokenError = null
    if (!tokenDevice) {
      tokenError = 'Choose which router this token is for.'
      return
    }
    // #1183: one key per wizard session. The key this session already
    // minted is the one to show again -- a second mint would give the
    // operator a second row with the same name in Settings, and only
    // the newer value in hand. The only way past this is to clear the
    // token first, which is what "mint a new one" below does.
    if (wizardState.token) return
    minting = true
    const result = await createToken(`setup-${tokenDevice}`, 'ingest', tokenDevice)
    minting = false
    if (typeof result === 'string') {
      tokenError = result
      return
    }
    wizardState.token = result.value ?? ''
  }

  // mintNewBackupToken is round 45's "mint a new one", offered only in
  // the lost-router shape: the old token still opens the drop box (it
  // is never revoked here -- Settings ▸ keys already offers that,
  // deliberately not duplicated), this just gives the replacement a
  // fresh one of its own to use instead. The operator asking is the one
  // thing that gets past the reuse rule above (#1183).
  function mintNewBackupToken() {
    wizardState.token = ''
    mintToken()
  }

  // The RouterOS command blocks (#436) come from the server now, keyed
  // to a plain string rather than to wizardState.status directly -- the
  // status poll (POLL_MS above) reassigns that object every 5s whether
  // or not anything relevant changed, and re-requesting commands on
  // every poll tick would be wasted work. This key only changes when
  // something the request actually carries changes.
  // The addresses the server reports itself bound to, minus whatever is
  // already in the field: offering the operator the value they are
  // looking at is noise.
  const addressCandidates = $derived(
    (wizardState.status?.instance.addressCandidates ?? []).filter((a) => a !== wizardState.address),
  )

  // debouncedAddress (#1218 audit finding 11): wizardState.address is
  // bound to the header field per keystroke, on purpose -- see its own
  // doc comment, other command blocks read it live. commandsKey below
  // used to include it directly, so every keystroke re-ran the effect
  // and fired a fresh POST /api/setup/commands. Only the address needs
  // this: every other commandsKey input (a token just minted, a version
  // picked, a transport switch) is a discrete event this component and
  // its tests both expect to answer promptly, not one that fires on
  // every keystroke -- so the delay belongs here, on the one input that
  // does, rather than in refreshCommands itself.
  //
  // Passed to refreshCommands explicitly below (opts.address), rather
  // than left for it to read wizardState.address itself: refreshCommands
  // reads that field synchronously, before its own first await, which
  // means the effect *calling* it picks up wizardState.address as a
  // dependency too, transitively, the same as if commandsKey had
  // included it directly -- Svelte's dependency tracking follows any
  // reactive read that happens during an effect's synchronous execution,
  // including ones inside a function it calls. Passing debouncedAddress
  // by value keeps the effect's only address dependency the debounced
  // one.
  const ADDRESS_DEBOUNCE_MS = 300
  let debouncedAddress = $state(wizardState.address)
  let addressDebounce: ReturnType<typeof setTimeout> | null = null
  $effect(() => {
    const address = wizardState.address
    if (addressDebounce) clearTimeout(addressDebounce)
    addressDebounce = setTimeout(() => {
      debouncedAddress = address
    }, ADDRESS_DEBOUNCE_MS)
    return () => {
      if (addressDebounce) clearTimeout(addressDebounce)
    }
  })

  const commandsKey = $derived(
    wizardState.status
      ? JSON.stringify([
          debouncedAddress,
          wizardState.status.instance.syslogPort,
          wizardState.status.pushKinds,
          token,
          tokenDevice,
          // The enrolment token is not sent -- the server reads the
          // router's own pending token and appends the enrol line
          // itself (#1281). It is in the key so that minting one, or
          // rerolling it, re-requests the block that carries it;
          // without that the step would print a block whose last line
          // quoted a token that no longer exists.
          wizardState.enrolment?.token ?? '',
          wizardState.pickedVersion,
          // The stored transport (#955) decides which of step 6's two
          // scripts the server renders, so a switch has to re-request
          // the blocks -- nothing in the request itself carries it.
          wizardState.backupTransport,
        ])
      : '',
  )

  $effect(() => {
    if (!commandsKey) return
    wizardState.refreshCommands({ token, device: tokenDevice, address: debouncedAddress })
  })

  // The router-standing warning (#436): one line per router outside the
  // dialect table's covered range, plus one for the operator's own pick
  // when it is. Never per code block, never a diagnosis about a specific
  // step -- see setupsteps.ts's module comment for why the commands
  // themselves moved server-side.
  interface WarningEntry {
    key: string
    kind: 'below-minimum' | 'ahead-of-review'
    name: string
    version: string
  }

  function isWarned(standing: RouterosStanding): standing is 'below-minimum' | 'ahead-of-review' {
    return standing === 'below-minimum' || standing === 'ahead-of-review'
  }

  const warningEntries = $derived.by((): WarningEntry[] => {
    const commands = wizardState.commands
    if (!commands) return []
    const entries: WarningEntry[] = commands.routers
      .filter((r) => isWarned(r.standing))
      .map((r) => ({ key: r.id, kind: r.standing as 'below-minimum' | 'ahead-of-review', name: r.name, version: r.routerosVersion }))
    if (commands.picked && isWarned(commands.picked.standing)) {
      entries.push({
        key: 'picked',
        kind: commands.picked.standing as 'below-minimum' | 'ahead-of-review',
        name: 'Your picked version',
        version: commands.picked.version,
      })
    }
    return entries
  })

  // Step 6 with no key mounted mints one here (#1133). The field starts
  // on a freshly generated key and stays editable, so an operator who
  // already has a key -- or who would rather generate their own -- can
  // type or paste it over the top and still get the steps below built
  // around it. Nothing sends it anywhere: history.keyFile is not
  // editable from the app (#853), there is no endpoint that accepts key
  // material, and none of the blocks below quote the value either --
  // the key goes into the file on standard input, which is what keeps
  // it out of the operator's shell history as well.
  //
  // loadOrMintHistoryKey, not a bare newHistoryKey(): this step still
  // shows (`blocked`) on a reload that happens before config.yaml picks
  // the key up and the app restarts, and a bare mint would hand back a
  // brand new value with the same "write this to keys/history.key"
  // instructions -- silently offering to overwrite the file the operator
  // already saved from the first mint. sessionStorage remembers this
  // tab's key across that reload; see lib/history.ts's own doc comment.
  let historyKey = $state(loadOrMintHistoryKey())

  // Whatever the field ends up holding -- the mint above, an explicit
  // Reroll, or the operator's own pasted key -- is what the next reload
  // in this tab should show too, not whichever of those happened first.
  //
  // And only while the step is still asking (v0.6.0 pre-release audit,
  // Security stage). Once it leaves `blocked` the server has read the
  // mounted key file, so no later reload can want the old value back
  // and keeping it only leaves a plaintext key in the tab -- one that
  // survived a logout and rehydrated into the next login, potentially
  // someone else's. What has not been asked decides nothing: forgetting
  // on `undefined` would hand a mid-setup operator a fresh key on their
  // next reload, which is the bug sessionStorage fixed.
  //
  // `blocked` is only knowable once the backups GET has answered, and
  // that request runs only while the modal is open (the poll above).
  // Reading the step state alone, this said `waiting` on an ordinary
  // reload with the wizard shut -- the step's own not-yet-pushed state,
  // standing in for a question nothing had asked -- and cleared the key
  // on it. Hence the null check ahead of the step state.
  const historyKeyStepState = $derived(
    wizardState.backups === null ? undefined : ledger.find((s) => s.key === 'backup')?.status.state,
  )
  $effect(() => {
    if (historyKeyStepState === undefined) return
    if (historyKeyStepState === 'blocked') saveHistoryKeyForSession(historyKey)
    else forgetHistoryKeyForSession()
  })

  // backupBlocked is #1217's reason the backup step printed nothing:
  // the server's own keys for whichever preconditions are unmet. Never
  // read in the lost-router shape or the ledger's own key-mint "blocked"
  // state -- both already have their own dedicated body and lead text
  // above this, and the retention-key precondition the two states can
  // share is the ledger's to explain when it applies (#1133's richer
  // "generate one here" flow beats a plain sentence pointing at
  // config.yaml).
  const backupBlocked = $derived(
    step && step.key === 'backup' && step.status.state !== 'blocked' && !wizardState.lostRouterDevice
      ? (wizardState.commands?.steps.backup.blocked ?? [])
      : [],
  )

  // The steps under the field, one block each, in the order they have to
  // happen. Constants so the copy buttons, the tests and the scenario
  // all quote the same text.
  const KEY_SAVE_COMMAND = `mkdir -p ${KEY_DIR} && umask 077 && cat > ${KEY_FILE_PATH}`
  // The app folder's two mount lines, not a mount per file (#1209,
  // #1243): the key is one of several things that now arrive by being
  // put in the folder, so the block an operator pastes has to be the
  // one that carries all of them, or the next feature asks them to
  // paste a different one.
  const KEY_MOUNT_COMMAND = `services:\n  mikroview:\n    volumes:\n      - ./mikroview:/etc/mikroview:ro\n      - ./mikroview/data:/var/lib/mikroview`
  const KEY_RESTART_COMMAND = 'docker compose up -d'

  // Step 6's own three departures from every other step's fixed
  // lead/header (#394, round 45): the title gains "<router> is gone" in
  // the lost-router shape, the lead reads one of three ways depending
  // on state (rest/warrived share one, blocked and lost each have their
  // own), and the observation line reads this one router's own kept
  // count rather than the ledger's fleet-wide receipt.
  const lostRouterTitle = $derived(
    step && step.key === 'backup' && wizardState.lostRouterDevice
      ? `${step.title} — ${wizardState.lostRouterDevice} is gone`
      : null,
  )

  const leadText = $derived.by(() => {
    if (!step) return ''
    if (step.key === 'backup') {
      if (wizardState.lostRouterDevice) {
        return (
          'The router that pushed these is not answering. Everything a replacement needs from this ' +
          'side is here, in the order it needs it: trust the certificate, send logs, then run the ' +
          'backup script again so the new router keeps pushing. Its backups are under Settings.'
        )
      }
      if (step.status.state === 'blocked') {
        // Said once, and correctly (#1133). The key file is mikroview's
        // own -- it seals the backups, the event history and the state
        // store -- so the old "a key it does not hold" was describing
        // the vault passphrase, a different thing entirely.
        return (
          'MikroView encrypts backups — and the event history and the state store — under the key ' +
          'file you mount. None is mounted, so nothing can be stored yet. Generate one here.'
        )
      }
      if (backupBlocked.length > 0) {
        // #1217: no script exists yet, so the ordinary lead's promise of
        // a token "already in the script" would describe something not
        // on the screen.
        return backupLead(false)
      }
    }
    return step.lead
  })

  const lostGeneration = $derived.by(() => {
    const device = wizardState.lostRouterDevice
    if (!device) return null
    const router = wizardState.backups?.routers.find((r) => r.device === device)
    return router ? newestGeneration(router) : null
  })

  const lostObservationText = $derived(
    wizardState.lostRouterDevice ? backupReceiptForDevice(wizardState.backups, wizardState.lostRouterDevice) : '',
  )

  // lostRouterGated (#1218 audit finding 16): the "download the newest
  // .backup" link used to be a plain <a href>, so it ignored #1115's
  // vault passphrase gate entirely -- RouterBackups.svelte's own
  // downloads (the same endpoint) go through downloadFromUrl and hide
  // behind this exact check; a bare link here just navigated the whole
  // tab to whatever the server answered a locked vault with, including
  // a 403 page, rather than reading it as "gated" at all.
  const lostRouterGated = $derived(vaultGated(wizardState.backups?.lock))
  let lostDownloadError = $state<string | null>(null)

  async function downloadLostBackup(device: string, generation: string) {
    lostDownloadError = null
    const outcome = await downloadFromUrl(routerBackupDownloadUrl(device, generation, 'backup'), `${device}.backup`)
    if (outcome === 'forbidden') {
      // The idle timeout lapsing between the link being drawn and the
      // click -- re-read the lock (and the rest of the backups read
      // along with it, same as RouterBackups.svelte's refreshLock)
      // rather than trusting a client-side clock to have guessed right.
      await wizardState.refreshBackups()
    } else if (outcome === 'failed') {
      lostDownloadError = 'The download failed. Try again.'
    }
  }

  async function copy(text: string, label: string) {
    try {
      await navigator.clipboard.writeText(text)
      copied = label
      setTimeout(() => (copied = ''), 1500)
    } catch {
      // Clipboard access can fail (permissions, non-secure context).
      // The block stays selectable either way.
    }
  }

  // Next runs the check where one exists. Arrived proceeds; waiting
  // hands the body to the heavy warning instead of moving. Steps that
  // count, and the step with nothing to wait for, always proceed.
  async function onNext() {
    if (!step) return
    // The name step acts rather than checks (#1284): Next is what
    // creates the router. Nothing to create means nothing to stop --
    // a router already in hand, or a step already decided with the
    // field left empty, simply moves on.
    if (step.key === 'name') {
      const nothingToCreate = !!wizardState.ledgerDevice || (step.outcome !== 'open' && !routerName.trim())
      if (!nothingToCreate && !(await createRouter())) return
      wizardState.next()
      return
    }
    if (!step.hasCheck || step.outcome !== 'open') {
      wizardState.next()
      return
    }
    warning = true
  }

  // A decision is recorded under the step's canonical number, not its
  // row in whichever ledger is open (#1284): a mark is persisted, so it
  // cannot mean "second row of the list that happened to be showing".
  async function onSkip() {
    if (!step || busy) return
    busy = true
    await wizardState.record(step.canonical, 'skipped', notObserved(step))
    busy = false
    wizardState.next()
  }

  async function onForce() {
    if (!step || busy) return
    busy = true
    await wizardState.record(step.canonical, 'forced', notObserved(step))
    busy = false
    warning = false
    wizardState.next()
  }

  function onKeydown(e: KeyboardEvent) {
    if (!wizardState.open || e.key !== 'Escape') return
    // With a reader open (#1219), Esc closes the reader, not the
    // wizard: the operator is one level in, and one keystroke backs
    // out one level, to the same step the reader was opened from.
    if (expandedPaste) {
      expandedPaste = null
      return
    }
    // Explicit close, both ways. Esc is not a click-outside: it is a
    // deliberate keystroke, and the record names it alongside the ✕.
    dismiss()
  }

  // Leads out to the landing -- the fall, mikroview's real landing page
  // since #616 (this read "Stream is the landing" until then; #646
  // makes it explicit: the wizard ends by taking the operator back to
  // the fall, whichever path opened it). Only from the finish, where the
  // record says the primary and the ✕ do the same thing -- closing
  // part-way through is a modal closing, and it leaves the operator on
  // the page they opened it from rather than moving them.
  function leaveToLanding() {
    // The record's own rule: the finish leads out to the fleet when the
    // ledger was opened from there, and to the fall when it was opened
    // from setup.
    appState.view = wizardState.finishTo === 'fleet' ? 'fleet' : 'fall'
    wizardState.close()
  }

  // The finish screen's own door into Log every rule (#435 decision 2;
  // "Tune logging" until #1134 renamed the page, not the view key):
  // its other way in besides the topography's coverage lens. Closes the
  // modal rather than leaving it open behind the new page, the same way
  // leaveToLanding above does.
  function openLogEveryRule() {
    appState.view = 'tune-logging'
    wizardState.close()
  }

  // "see it in Settings" (round 45's observation line): the same door,
  // for the same reason -- the backups group this observation is a
  // preview of lives there, not in a second copy of it here.
  function openBackupsInSettings() {
    appState.view = 'engineroom'
    wizardState.close()
  }

  // "done — the replacement is pushing" (round 45's lost-router
  // footer): the operator's own word that the repair is finished. It
  // only ever clears the flag this step's whole shape is keyed on --
  // mikroview has no way to confirm a replacement is pushing other than
  // waiting for the next arrival, so this is the same "your act, your
  // word" pattern the wizard's other decisions already use.
  function finishLostRouter() {
    wizardState.lostRouterDevice = null
    wizardState.next()
  }

  function dismiss() {
    if (onFinish) {
      leaveToLanding()
      return
    }
    wizardState.close()
  }

  // #1217: the screen-reader announcement has to say the same thing the
  // visible observation line does. announceStep reads step.status.detail
  // straight, which carries the same "the script below runs once at the
  // end" promise the visible line overrides above -- without this, a
  // sighted operator would see the honest line while a screen reader
  // heard the false one.
  const announcement = $derived(
    step && step.key === 'backup' && !step.witnessed && backupBlocked.length > 0 && step.flavour === 'waiting'
      ? `Step ${step.n} of ${stepCount} — ${step.title} — ${BACKUP_WAITING_NO_SCRIPT}`
      : step
        ? announceStep(step, stepCount)
        : onFinish
          ? finishHeadline(ledger)
          : '',
  )

  const closeLabel = 'Close setup — finish later from your account menu ▸ Run setup…'
</script>

<svelte:window onkeydown={onKeydown} />

<!-- commandsHead (#436) sits at the head of every RouterOS command step
     (1-4): the version pick-list, and the router-standing warning --
     never blocking, never per code block, worded once here rather than
     once per step. -->
{#snippet commandsHead()}
  {#if wizardState.commands}
    {@const commands = wizardState.commands}
    <div class="routeros-version">
      <label for="routeros-version-select">Your RouterOS version</label>
      <select
        id="routeros-version-select"
        value={wizardState.pickedVersion}
        onchange={(e) => (wizardState.pickedVersion = (e.currentTarget as HTMLSelectElement).value)}
      >
        <option value="">Not sure — the router will report it</option>
        {#each commands.routeros.rows as row (row.from)}
          <option value={row.from}>{row.from === row.to ? row.from : `${row.from}–${row.to}`}</option>
        {/each}
      </select>
    </div>
    <!-- #1181: switching version left every command block byte-identical
         and the step said nothing about why, which reads as a control
         that does nothing. It is not -- one dialect covers the whole
         table today (dialects.go), so what the pick buys is the check
         against it: the standing lines below, and any note that belongs
         to that release (step 3 prints one for 7.24.0). Saying that is
         the fix rather than hiding the picker, because the check is
         what the pick is for, not the command text. -->
    <p class="note">
      One set of commands covers RouterOS {commands.routeros.minimum} to {commands.routeros.newest}, so
      picking a version does not change them. It is how your release gets checked against the ones
      these were verified on — anything outside that range, or with a warning of its own, is said
      here and on the step it concerns.
    </p>
    {#each warningEntries as w (w.key)}
      <p class="note" class:below-minimum={w.kind === 'below-minimum'}>
        {#if w.kind === 'below-minimum'}
          <strong>{w.name} runs RouterOS {w.version}.</strong> These commands were written for {commands
            .routeros.minimum} and later; on {w.version} some may not apply as written. Check each against
          your router before running it.
        {:else}
          <strong>{w.name} runs RouterOS {w.version}.</strong> These commands were last checked against {commands
            .routeros.newest}. Newer releases rarely change them, but if one is refused, that is the first
          thing to suspect.
        {/if}
      </p>
    {/each}
  {/if}
{/snippet}

<!-- Step 6's one choice (#955): how the router hands its backup over.
     Drawn as the pair the wizard already uses for a two-way choice --
     the answer in ink, the alternative as the link that switches to it
     -- and sat directly above the script block, because it is what the
     block below is. Stored server-side, so it is the deployment's
     answer and not this browser's: an operator opening the wizard on
     another machine is offered the step their install actually uses. -->
{#snippet backupTransportPair()}
  <p class="note transport" role="group" aria-label="How the router sends its backup">
    The router sends its backup
    {#each BACKUP_TRANSPORTS as t, i (t.value)}
      {#if i > 0}&nbsp;·{/if}
      <button
        type="button"
        class="olink"
        class:on={wizardState.backupTransport === t.value}
        aria-pressed={wizardState.backupTransport === t.value}
        onclick={() => wizardState.setBackupTransport(t.value)}
      >
        {t.label}
      </button>
    {/each}
  </p>
  {#if wizardState.backupTransportError}
    <p class="load-error">{wizardState.backupTransportError}</p>
  {/if}
{/snippet}

{#if wizardState.open}
  <!-- No click-outside dismissal, per the record: the veil is a veil and
       nothing else, so it carries no click handler and is not a button.
       Losing a half-finished setup to a stray click is exactly what
       "explicit close only" exists to prevent. Below the pointer-width
       breakpoint the modal is the screen and there is no veil at all. -->
  <div class="veil" class:sheet={viewportState.isMobile} role="presentation">
    <div
      class="modal setup-wizard"
      class:sheet={viewportState.isMobile}
      class:reading={!!expandedPaste}
      role="dialog"
      aria-modal="true"
      aria-labelledby="setup-wizard-title"
      tabindex="-1"
      use:trapFocus
    >
      <header>
        {#if viewportState.isMobile}
          <!-- The step list becomes a view of its own on a phone, and
               the thing that flips to it is a real button with a spoken
               label -- not a swipe, not a tab strip. -->
          <button
            type="button"
            class="flip"
            onclick={() => (wizardState.showStepList = !wizardState.showStepList)}
          >
            {wizardState.showStepList ? 'Show this step' : 'Show setup steps'}
          </button>
        {/if}
        <span class="crumb">
          {#if step}Step {step.n} of {stepCount}{:else}Setup{/if}
        </span>
        <h2 id="setup-wizard-title">
          {#if lostRouterTitle && step}
            {step.title} — <span class="lost">{wizardState.lostRouterDevice} is gone</span>
          {:else}
            {step ? step.title : 'Where setup stands'}
          {/if}
        </h2>
        <button type="button" class="close" onclick={dismiss} aria-label={closeLabel}>✕</button>
      </header>

      <!-- The address field (#1213): a required part of the header, above
           the numbered steps, answered before any command block renders --
           not a numbered step itself, since a step number is persisted in
           internal/setup's marks and inserting one here would silently
           renumber every stored mark. Editable at any time: changing it
           re-renders every command block below, through commandsKey. -->
      <div class="address-field">
        <label for="setup-wizard-address">What address can your router reach MikroView on?</label>
        <input
          id="setup-wizard-address"
          type="text"
          spellcheck="false"
          autocomplete="off"
          autocapitalize="off"
          bind:value={wizardState.address}
          onblur={() => wizardState.saveAddress()}
          onkeydown={(e) => {
            if (e.key === 'Enter') (e.currentTarget as HTMLInputElement).blur()
          }}
        />
        <p class="note">
          The router has to reach this address, which may not be the one you typed — a proxy, a
          second interface or a mapped port all change it.
        </p>
        <!-- The addresses the server finds itself bound to (#1213).
             Offered, never chosen for the operator: on a host with
             several interfaces, picking one is a different guess rather
             than a better one, and only they know which one the router
             can route to. -->
        {#if addressCandidates.length > 0}
          <p class="note">
            This machine also answers on
            {#each addressCandidates as candidate, i (candidate)}{i > 0 ? ', ' : ''}<button
                type="button"
                class="addr-candidate"
                onclick={() => {
                  wizardState.address = candidate
                  wizardState.saveAddress()
                }}>{candidate}</button
              >{/each}.
          </p>
        {/if}
        {#if wizardState.addressSaveError}
          <p class="load-error">{wizardState.addressSaveError}</p>
        {/if}
      </div>

      <div class="middle">
        {#if !viewportState.isMobile || wizardState.showStepList}
          <!-- The step list carries each step's receipt sub-line for the
               wizard's life: what arrived and when, or the decision that
               was recorded, or an honest gap. -->
          <nav class="steps" aria-label="Setup steps">
            <ol>
              {#each ledger as s (s.n)}
                <li>
                  <button
                    type="button"
                    class="step-row {s.outcome}"
                    class:current={s.n === wizardState.pane}
                    aria-current={s.n === wizardState.pane ? 'step' : undefined}
                    onclick={() => {
                      wizardState.goTo(s.n)
                      wizardState.showStepList = false
                    }}
                  >
                    <span class="step-n">{s.n}</span>
                    <span class="step-text">
                      <span class="step-title">{s.title}</span>
                      {#if s.key === 'backup' && wizardState.lostRouterDevice}
                        <!-- Round 45's lost-router receipt: this one
                             router's own kept count, not the ledger's
                             fleet-wide "arrived ..." line. -->
                        <span class="step-receipt">
                          {lostObservationText || 'nothing kept for this router yet'}
                        </span>
                      {:else if s.receipt}
                        <span class="step-receipt">{s.receipt}</span>
                      {:else if s.outcome === 'open'}
                        <span class="step-receipt gap">nothing has arrived yet</span>
                      {/if}
                      {#if s.outcome === 'skipped'}
                        <span class="step-receipt consequence">{SKIP_CONSEQUENCES[s.key]}</span>
                      {/if}
                    </span>
                  </button>
                </li>
              {/each}
              <li>
                <button
                  type="button"
                  class="step-row finish-row"
                  class:current={onFinish}
                  aria-current={onFinish ? 'step' : undefined}
                  onclick={() => {
                    wizardState.goTo(wizardState.finishPane)
                    wizardState.showStepList = false
                  }}
                >
                  <span class="step-n">✓</span>
                  <span class="step-text"><span class="step-title">Where setup stands</span></span>
                </button>
              </li>
            </ol>
          </nav>
        {/if}

        {#if !viewportState.isMobile || !wizardState.showStepList}
          {#if expandedPaste}
            <!-- The reader (#1219): takes the body's own slot rather than
                 stacking a panel over it, so the step list stays exactly
                 where it is and the script gets the whole height of the
                 column the body was already using. -->
            <div class="reader">
              <div class="reader-head">
                <span class="reader-label">{expandedPaste.label}</span>
                <button
                  type="button"
                  class="close"
                  onclick={() => (expandedPaste = null)}
                  aria-label="Close and return to this step"
                >
                  ✕
                </button>
              </div>
              <!-- No 14-line cap here: the column's own height is the
                   limit, and it is already close to the screen's. -->
              <pre class="reader-pre scrollbar">{expandedPaste.text}</pre>
            </div>
          {:else}
          <div class="body scrollbar">
            {#if wizardState.error}
              <p class="load-error">Could not load setup status: {wizardState.error}</p>
            {/if}

            {#if warning && step}
              <!-- The heavy warning takes the body in place. Two
                   choices, no third option and no "are you sure": the
                   amber button quotes the exact record it will write. -->
              <div class="heavy">
                <h3>MikroView cannot check the router's side</h3>
                <p>
                  It only sees what arrives here, and {notObserved(step)}. That is not the same as
                  the step having failed — it may simply not have happened yet.
                </p>
                <p class="quote">{forcedPastRecord(step, authState.username, new Date())}</p>
                <div class="heavy-actions">
                  <button type="button" class="primary" onclick={() => (warning = false)}>
                    Keep waiting
                  </button>
                  <button type="button" class="amber" onclick={onForce} disabled={busy}>
                    Go on anyway — recorded
                  </button>
                </div>
              </div>
            {:else if step}
              <p class="lead">{leadText}</p>

              {#if step.key === 'backup' && step.status.state !== 'blocked' && backupBlocked.length === 0}
                <!-- Round 45's caveat, in the amber the heavy warning
                     above already uses, before the script rather than
                     after: RouterOS never verifies who it is sending a
                     backup to (#394's measured finding), so this is
                     said before the operator copies anything. -->
                <div class="wzcaveat">
                  <b>Only on a network you trust.</b> RouterOS never checks who it is sending a backup to
                  — anyone on the path between the router and {wizardState.address} could read the pair, and
                  the token with it. On a LAN you control that is fine; across the internet it is not, and
                  MikroView cannot tell the difference from here.
                </div>
              {/if}

              <!-- Not on step 6 with no key mounted (#1133): there is no
                   RouterOS command on that pane to pick a version for --
                   the pane is about the key file, and the picker only
                   stands between the operator and it. -->
              {#if step.key === 'ca' || step.key === 'syslog' || step.key === 'rules' || step.key === 'push' || (step.key === 'backup' && step.status.state !== 'blocked' && backupBlocked.length === 0)}
                {@render commandsHead()}
              {/if}

              {#if step.key === 'ca' && wizardState.status}
                {#if step.status.state !== 'blocked'}
                  {#if wizardState.commands?.steps.caTrust.blocked?.length}
                    <!-- #1213: no address answered yet, so there is
                         nothing to fetch the certificate from. -->
                    <div class="no-script">
                      <h4>{NO_COMMAND_HEADING}</h4>
                      <p class="note">{NO_ADDRESS_LINE}</p>
                    </div>
                  {:else}
                    <pre>{wizardState.commands?.steps.caTrust.commands ?? ''}</pre>
                    <button
                      type="button"
                      class="copy"
                      onclick={() => copy(wizardState.commands?.steps.caTrust.commands ?? '', 'ca')}
                    >
                      {copied === 'ca' ? 'Copied' : 'Copy'}
                    </button>
                    {#if wizardState.commands?.steps.caTrust.note}
                      <p class="note">{wizardState.commands.steps.caTrust.note}</p>
                    {/if}
                    <p class="note">
                      <code>check-certificate=no</code> belongs on this one line only — it is fetching
                      the thing everything else checks against.
                    </p>
                  {/if}
                {/if}
              {:else if step.key === 'syslog' && wizardState.status}
                {#if step.status.state !== 'blocked'}
                  {#if wizardState.commands?.steps.syslog.blocked?.length}
                    <!-- #1213: no address answered yet, so there is
                         nowhere to point the router's logging action. -->
                    <div class="no-script">
                      <h4>{NO_COMMAND_HEADING}</h4>
                      <p class="note">{NO_ADDRESS_LINE}</p>
                    </div>
                  {:else}
                    <!-- #1291: the token is minted only when the operator
                         asks, because minting needs two things only they
                         have -- the router's own address, which the
                         enrolment window then opens for and nothing else,
                         and their password, re-typed at that moment so a
                         session on its own cannot open the port. -->
                    {#if mintFormOpen}
                      <div class="mint-ask">
                        <label for="setup-wizard-enrol-address">What is this router's own address?</label>
                        <input
                          id="setup-wizard-enrol-address"
                          type="text"
                          spellcheck="false"
                          autocomplete="off"
                          autocapitalize="off"
                          bind:value={wizardState.enrolExpectedAddress}
                        />
                        <p class="note">
                          MikroView will listen for this router on that address only, until its token
                          arrives or lapses. If you get it wrong, whatever is turned away is shown
                          below and you can point the window at it in one click.
                        </p>
                        <label for="setup-wizard-enrol-password">Your password</label>
                        <input
                          id="setup-wizard-enrol-password"
                          type="password"
                          autocomplete="current-password"
                          bind:value={wizardState.enrolPassword}
                          onkeydown={(e) => {
                            if (e.key === 'Enter') mintNow()
                          }}
                        />
                        <p class="note">
                          Minting a token is what opens the port, so it asks who you are rather than
                          trusting the session you are already in.
                        </p>
                        <button type="button" class="copy" disabled={wizardState.enrolMinting} onclick={mintNow}>
                          {wizardState.enrolMinting ? 'Minting…' : 'Mint the token'}
                        </button>
                        {#if rerollAsked && !enrolExpired}
                          <!-- 2026-09-18 audit, stage 6 finding 10: this
                               opened for an expired token too, offering
                               to "keep" one that is already dead -- the
                               banner right above says so, and there is
                               nothing left to keep. -->
                          <button type="button" class="link" onclick={() => (rerollAsked = false)}>
                            Keep the token I have
                          </button>
                        {/if}
                      </div>
                    {/if}
                    <!-- The block dims once the token in its last line has
                         lapsed (#1281): what it prints can no longer be
                         pasted, and a live-looking block that would be
                         refused is the small lie this wizard exists not
                         to tell. -->
                    <pre class:stale={enrolExpired}>{wizardState.commands?.steps.syslog.commands ?? ''}</pre>
                    <button
                      type="button"
                      class="copy"
                      onclick={() => copy(wizardState.commands?.steps.syslog.commands ?? '', 'syslog')}
                    >
                      {copied === 'syslog' ? 'Copied' : 'Copy'}
                    </button>
                    {#if wizardState.enrolment}
                      <!-- One plain line under the block: how long the
                           token in it is good for, and the one control --
                           Reroll, step 6's own, with the same quiet
                           confirmation. -->
                      <p class="note token-life" class:expired={enrolExpired}>
                        {#if enrolExpired}
                          {TOKEN_EXPIRED_LINE}
                          <button type="button" class="link" onclick={() => (rerollAsked = true)}>
                            {TOKEN_REROLL_EXPIRED_LABEL}
                          </button>
                        {:else}
                          {enrolLine} ·
                          <button type="button" class="link" onclick={() => (rerollAsked = true)}>
                            {TOKEN_REROLL_LABEL}
                          </button>
                        {/if}
                      </p>
                    {/if}
                    {#if wizardState.enrolmentError}
                      <p class="load-error">{wizardState.enrolmentError}</p>
                    {/if}
                    {#if wizardState.commands?.steps.syslog.note}
                      <p class="note">{wizardState.commands.steps.syslog.note}</p>
                    {/if}
                  {/if}
                {/if}
              {:else if step.key === 'rules'}
                <pre>{wizardState.commands?.steps.ruleTagging.commands ?? ''}</pre>
                <button
                  type="button"
                  class="copy"
                  onclick={() => copy(wizardState.commands?.steps.ruleTagging.commands ?? '', 'rules')}
                >
                  {copied === 'rules' ? 'Copied' : 'Copy'}
                </button>
                {#if wizardState.commands?.steps.ruleTagging.note}
                  <p class="note">{wizardState.commands.steps.ruleTagging.note}</p>
                {/if}
                <p class="note">
                  The letter is how MikroView knows what a rule did — <code>A</code>ccept,
                  <code>D</code>rop, <code>R</code>eject, <code>L</code>og. The trailing
                  <code>|</code> is required.
                </p>
                <!-- #1174: what the bulk form costs, said rather than
                     left to be discovered. One command can only label
                     by action -- that is why it is one command -- so
                     the label after the letter is the action's own
                     name, and every drop rule carries the same one.
                     The setup guide's per-rule slugs are the way to
                     get further, and this points at them rather than
                     growing a rule-by-rule generator here. -->
                <p class="note">
                  These three label rules by what they do, so every drop rule logs as
                  <code>D|drop|</code> and the log cannot tell one from another. To name them
                  individually — <code>D|wan-in|</code> — set each rule's own
                  <code>log-prefix</code>, as <code>docs/routeros-setup.md</code> step 3 walks
                  through; the first letter must still match the action.
                </p>
              {:else if step.key === 'push' && wizardState.status}
                {#if !token}
                  <div class="mint">
                    <select bind:value={wizardState.tokenDevice} aria-label="Router this token is for">
                      <option value="" disabled>Which router is this for?…</option>
                      {#each wizardState.devices as d (d.id)}
                        <option value={d.id}>
                          {d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}
                        </option>
                      {/each}
                    </select>
                    <button type="button" class="primary" onclick={mintToken} disabled={minting}>
                      {minting ? 'Creating…' : 'Create token & script'}
                    </button>
                  </div>
                  {#if wizardState.devices.length === 0}
                    <p class="note">
                      No routers known yet — finish {TITLES.name} first, and this list fills in on its own.
                    </p>
                  {/if}
                  {#if tokenError}<p class="load-error">{tokenError}</p>{/if}
                {:else if wizardState.commands?.steps.schedule.blocked?.length}
                  <!-- #1213: a token exists, but there is still no
                       address to embed it against. -->
                  <div class="no-script">
                    <h4>{NO_COMMAND_HEADING}</h4>
                    <p class="note">{NO_ADDRESS_LINE}</p>
                  </div>
                {:else}
                  <!-- The token is shown, not merely described (#1131):
                       it is minted once and never shown again, so a
                       step that says "the token below" and prints only
                       a script leaves the operator nothing to keep. -->
                  <p class="note token-note">
                    This token is shown once, and is already in the script below. Anyone who can read
                    the script on the router can read it, so it is scoped to that one router.
                  </p>
                  <pre class="token">{token}</pre>
                  <button type="button" class="copy" onclick={() => copy(token, 'token')}>
                    {copied === 'token' ? 'Copied' : 'Copy token'}
                  </button>
                  <!-- One block, pasted as it stands: the script saved
                       under its own name, the scheduler entry, and one
                       run now. It used to be two boxes, the second
                       carrying `source="<paste the script above>"` --
                       which asked the operator to nest one clipboard
                       inside another and to do RouterOS's escaping by
                       hand (#1131). -->
                  <div class="paste">
                    <pre class="script scrollbar">{wizardState.commands?.steps.schedule.commands ?? ''}</pre>
                    {#if !viewportState.isMobile}
                      <!-- The drawer handle (#1219): part of the box, not a
                           button beside it -- the owner's words. Only the
                           two boxes that actually hide content behind the
                           14-line cap get one; the short, always-fully-shown
                           blocks elsewhere on this step have nothing to
                           expand into. -->
                      <button
                        type="button"
                        class="handle"
                        onclick={() =>
                          (expandedPaste = {
                            key: 'script',
                            label: 'the push scheduling script',
                            text: wizardState.commands?.steps.schedule.commands ?? '',
                          })}
                        aria-label="Read the push scheduling script in full"
                      >
                        ‹
                      </button>
                    {/if}
                  </div>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.schedule.commands ?? '', 'script')}
                  >
                    {copied === 'script' ? 'Copied' : 'Copy script'}
                  </button>
                  {#if wizardState.commands?.steps.schedule.note}
                    <p class="note">{wizardState.commands.steps.schedule.note}</p>
                  {/if}
                {/if}
              {:else if step.key === 'name'}
                <!-- The name is the only field (#1284), and Next is what
                     creates the router: naming it and creating it are one
                     act, because the enrolment token the next step mints
                     belongs to a named router. No Copy and no command
                     block -- there is nothing here to paste on a router,
                     which is the one step body that never has. -->
                {#if wizardState.ledgerDevice}
                  {@const named = wizardState.devices.find((d) => d.id === wizardState.ledgerDevice)}
                  <p class="note">
                    <strong>{named?.name || wizardState.ledgerDevice}</strong> is on the fleet. Its enrolment
                    token is minted in the next step.
                  </p>
                {:else}
                  <div class="namefield">
                    <label for="router-name">Name</label>
                    <input
                      id="router-name"
                      type="text"
                      spellcheck="false"
                      autocomplete="off"
                      autocapitalize="off"
                      placeholder="edge-1"
                      bind:value={routerName}
                      onkeydown={(e) => {
                        if (e.key === 'Enter') onNext()
                      }}
                    />
                  </div>
                  <p class="note">
                    A name you will recognise in the fleet. It is MikroView's own name for this router —
                    nothing on the router changes.
                  </p>
                  {#if nameError}<p class="load-error">{nameError}</p>{/if}
                {/if}
              {:else if step.key === 'backup'}
                {#if step.status.state === 'blocked'}
                  <!-- #1133: the step mints the key rather than
                       describing one twice. 32 random bytes, base64 --
                       the same shape as the setup guide's `head -c 32
                       /dev/urandom | base64` -- generated in this tab
                       and never sent anywhere, which is exactly why the
                       warning beside it has to be believed: mikroview
                       cannot reprint what it has never been given. -->
                  <div class="keymint">
                    <label for="history-key">Your key</label>
                    <input
                      id="history-key"
                      type="text"
                      spellcheck="false"
                      autocomplete="off"
                      autocapitalize="off"
                      bind:value={historyKey}
                    />
                    <CopyButton value={historyKey} label="the history key" />
                    <button type="button" onclick={() => (historyKey = newHistoryKey())}>Reroll</button>
                  </div>
                  <p class="wzcaveat">
                    <b>Save this now.</b> MikroView can never show it again — it never receives this
                    value and never stores it, it only reads the file you are about to write. Lose
                    the key and everything kept under it, backups included, is unreadable.
                  </p>
                  <p class="note">
                    Write it into the app folder's <code>keys/</code>, beside <code>data/</code> and
                    never inside it — a key kept among the files it protects travels with any copy
                    of them:
                  </p>
                  <pre>{KEY_SAVE_COMMAND}</pre>
                  <button type="button" class="copy" onclick={() => copy(KEY_SAVE_COMMAND, 'keysave')}>
                    {copied === 'keysave' ? 'Copied' : 'Copy'}
                  </button>
                  <p class="note">
                    Paste the key at the prompt, then press Ctrl-D. It goes in on standard input, so
                    it never reaches your shell history or a process list.
                  </p>
                  <p class="note">Mount the whole folder into the container, read-only:</p>
                  <pre>{KEY_MOUNT_COMMAND}</pre>
                  <button type="button" class="copy" onclick={() => copy(KEY_MOUNT_COMMAND, 'keymount')}>
                    {copied === 'keymount' ? 'Copied' : 'Copy'}
                  </button>
                  <p class="note">Running MikroView directly on the host instead? Skip this one.</p>
                  <p class="note">
                    There is nothing to set: MikroView reads
                    <code>{KEY_FILE_CONTAINER_PATH}</code> from the folder on its own. Naming the
                    path yourself still works and still wins —
                    <code>history.keyFile</code> in config.yaml, or
                    <code>MIKROVIEW_HISTORY_KEY_FILE</code> — and no setting anywhere carries the
                    key itself.
                  </p>
                  <p class="note">Then restart:</p>
                  <pre>{KEY_RESTART_COMMAND}</pre>
                  <button type="button" class="copy" onclick={() => copy(KEY_RESTART_COMMAND, 'keyrestart')}>
                    {copied === 'keyrestart' ? 'Copied' : 'Copy'}
                  </button>
                  <p class="note">
                    The key is read once, at startup. This step notices on its own and prints the
                    router script ·
                    <a
                      class="olink ext"
                      href={HOW_TO_MOUNT_URL}
                      target="_blank"
                      rel="noopener noreferrer"
                      title="Opens the setup guide on github.com, in a new tab"
                    >
                      more on mounting a key
                    </a>
                  </p>
                {:else if !token}
                  <!-- Round 45 draws no mint form here at all: it
                       assumes step 4 already minted the token this
                       script needs (the same credential, #394 -- see
                       mintToken's comment). This is only reached when
                       that has not happened yet -- step 4 skipped, or a
                       fresh session -- and without it there is nothing
                       to print, which the "done when" bar does not
                       allow. -->
                  <div class="mint">
                    <select bind:value={wizardState.tokenDevice} aria-label="Router this token is for">
                      <option value="" disabled>Which router is this for?…</option>
                      {#each wizardState.devices as d (d.id)}
                        <option value={d.id}>
                          {d.name && d.name !== d.id ? `${d.name} (${d.id})` : d.id}
                        </option>
                      {/each}
                    </select>
                    <button type="button" class="primary" onclick={mintToken} disabled={minting}>
                      {minting ? 'Creating…' : 'Create token & script'}
                    </button>
                  </div>
                  {#if wizardState.devices.length === 0}
                    <p class="note">
                      No routers known yet — finish {TITLES.name} first, and this list fills in on its own.
                    </p>
                  {/if}
                  {#if tokenError}<p class="load-error">{tokenError}</p>{/if}
                {:else if backupBlocked.length > 0}
                  <!-- #1217: a token exists, but the backup block still
                       came back blank -- backups switched off in
                       config.yaml is the reachable case (the owner's
                       instance), no-device/no-token are named for
                       completeness. No input box, no Copy button: there
                       is nothing behind either to copy. -->
                  {@render backupTransportPair()}
                  <div class="no-script">
                    <h4>{BACKUP_NO_SCRIPT_HEADING}</h4>
                    <ul>
                      {#each backupBlockedLines(backupBlocked) as line (line)}
                        <li class="note">{line}</li>
                      {/each}
                    </ul>
                  </div>
                {:else}
                  {#if wizardState.lostRouterDevice}
                    <p class="note token-note">
                      The old router's token still opens the drop box, so a replacement can use this
                      script as it stands;
                      <button type="button" class="link" onclick={mintNewBackupToken}>mint a new one</button>
                      retires the old.
                    </p>
                  {:else}
                    <p class="note token-note">
                      The token is shown once. Anyone who can read the script on the router can read it,
                      so it is scoped to that one router and to this drop box.
                    </p>
                  {/if}
                  {#if wizardState.backupTransport === 'https'}
                    <p class="note token-note">{BACKUP_PORT_NOTE_HTTPS}</p>
                  {:else if wizardState.backups?.port}
                    <!-- #1220: the router timing out mid-upload read as a
                         stalled transfer, not an unreachable port -- this
                         is the one thing the script itself cannot say.
                         Only the drop box has a port to publish: the
                         HTTPS push (#955) says the opposite, above. -->
                    <p class="note token-note">
                      The router has to reach this host on port {portOf(wizardState.backups.port)} for the
                      push to land — publish it in docker-compose.yml's <code>ports:</code> (commented in,
                      beside the others) if you have not already.
                    </p>
                  {/if}
                  {@render backupTransportPair()}
                  <div class="paste">
                    <pre class="script scrollbar">{wizardState.commands?.steps.backup.commands ?? ''}</pre>
                    {#if !viewportState.isMobile}
                      <button
                        type="button"
                        class="handle"
                        onclick={() =>
                          (expandedPaste = {
                            key: 'backup',
                            label: 'the backup script',
                            text: wizardState.commands?.steps.backup.commands ?? '',
                          })}
                        aria-label="Read the backup script in full"
                      >
                        ‹
                      </button>
                    {/if}
                  </div>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.backup.commands ?? '', 'backup')}
                  >
                    {copied === 'backup' ? 'Copied' : 'Copy script'}
                  </button>
                  {#if wizardState.commands?.steps.backup.note}
                    <p class="note">{wizardState.commands.steps.backup.note}</p>
                  {/if}
                  <p class="note">Then have it run nightly, and once now to test it:</p>
                  <pre>{wizardState.commands?.steps.backupSchedule.commands ?? ''}</pre>
                  <button
                    type="button"
                    class="copy"
                    onclick={() => copy(wizardState.commands?.steps.backupSchedule.commands ?? '', 'backupsched')}
                  >
                    {copied === 'backupsched' ? 'Copied' : 'Copy'}
                  </button>
                {/if}
              {:else if step.key === 'register'}
                <!-- #1291: the ledger's last step. There is no
                     router-side command here and nothing to wait for --
                     this is the operator saying the router is one they
                     meant to add. It grants the router nothing, and the
                     body says so plainly rather than letting a confirm
                     button read as though it were what lets the logs
                     in. -->
                <div class="mint-ask">
                  <label for="setup-wizard-register-name">What should this router be called?</label>
                  <input
                    id="setup-wizard-register-name"
                    type="text"
                    spellcheck="false"
                    autocomplete="off"
                    autocapitalize="off"
                    bind:value={registerName}
                  />
                  <p class="note">
                    Registering records that you confirmed this router, and nothing else. Its logs
                    are accepted because its enrolment token arrived from its address — that is what
                    lets them in, and it does not change here.
                  </p>
                  <button
                    type="button"
                    class="copy"
                    disabled={wizardState.registering}
                    onclick={() => wizardState.registerRouter(registerName.trim())}
                  >
                    {wizardState.registering ? 'Registering…' : 'Register this router'}
                  </button>
                  {#if wizardState.registerError}
                    <p class="load-error">{wizardState.registerError}</p>
                  {/if}
                </div>
              {/if}

              <!-- The observation line. Four flavours and no more:
                   waiting is patient and never says "error" (nothing is
                   wrong, nothing has arrived), arrived is dated and
                   sourced, counting only counts upward, quiet has
                   nothing to wait for. `attention` is the inherited
                   mikroview-side check logic (#371/#374), not an
                   observation -- see setupsteps.ts. Step 6's lost-router
                   shape reads this one router's own kept count instead
                   of the ledger's fleet-wide receipt (lostObservationText).
                   A partial step is not a fifth flavour: it is this line
                   saying what arrived, with its shortfall in the warning
                   box below (#1132), and it renders only when there was
                   an arrival to word. -->
              {#if step.witnessed}
                <!-- #1221: a witness is a floor under evidence that did
                     not survive a restart, not a current reading --
                     status.detail below is whatever the live check
                     falls back to with nothing to look at, so this line
                     is the receipt instead: past tense, dated, and
                     never worded as though mikroview is watching the
                     router right now. -->
                <p class="observation arrived">{step.receipt}</p>
              {:else if step.status.detail || !step.status.shortfall}
                <p class="observation {step.key === 'backup' && wizardState.lostRouterDevice ? (lostGeneration ? 'arrived' : 'waiting') : step.flavour}">
                  {#if step.key === 'backup' && wizardState.lostRouterDevice}
                    {#if step.flavour !== 'arrived' && !lostGeneration}<span class="dot" aria-hidden="true"></span>{/if}
                    {lostObservationText || 'nothing kept for this router yet'}
                    {#if lostGeneration}
                      {#if lostRouterGated}
                        ·
                        {wizardState.backups?.lock.locked
                          ? 'locked — the vault passphrase opens downloads'
                          : 'unlocked by another of your sign-ins — unlock it in Settings to download here'}
                        <button type="button" class="link" onclick={openBackupsInSettings}>open Settings</button>
                      {:else}
                        ·
                        <button
                          type="button"
                          class="olink"
                          onclick={() => downloadLostBackup(wizardState.lostRouterDevice ?? '', lostGeneration.id)}
                        >
                          download the newest .backup
                        </button>
                        to restore the replacement, then run the script above
                        {#if lostDownloadError}<span class="load-error">{lostDownloadError}</span>{/if}
                      {/if}
                    {/if}
                  {:else}
                    {#if step.flavour === 'waiting'}<span class="dot" aria-hidden="true"></span>{/if}
                    {#if step.key === 'backup' && backupBlocked.length > 0 && step.flavour === 'waiting'}
                      <!-- #1217: backupStep's ordinary wording promises
                           "the script below runs once at the end" --
                           false with no script below, since the no-script
                           state above already says why. -->
                      {BACKUP_WAITING_NO_SCRIPT}
                    {:else}
                      {step.status.detail}
                    {/if}
                    {#if step.key === 'backup' && step.status.state === 'done'}
                      ·
                      <button type="button" class="link" onclick={openBackupsInSettings}>see it in Settings</button>
                    {/if}
                  {/if}
                </p>
              {/if}
              {#if step.status.shortfall}
                <p class="observation shortfall">{step.status.shortfall}</p>
              {/if}
              {#if step.key === 'syslog' && step.flavour === 'waiting' && refusedLine}
                <!-- #1132's shape, reused rather than a fifth flavour:
                     lines did arrive, from an address that is not
                     enrolled, and were dropped. It never claims the
                     address is this router -- only the operator knows
                     that. -->
                <p class="observation shortfall refused">{refusedLine}</p>
                <!-- #1291, ruling 23a: the window opens for the one
                     address the operator named, so a router at a
                     different address is turned away at accept and
                     lands here. They are standing at the router and
                     know which of these is theirs, so each is offered
                     as one click. It still claims nothing -- pointing
                     the window at an address accepts nothing by itself,
                     the token has to arrive from it -- and the token is
                     untouched, so nothing is pasted into the router
                     again. -->
                {#if wizardState.enrolment}
                  <p class="note">
                    If one of these is this router, point the enrolment window at it — the token you
                    already pasted stays as it is:
                    {#each wizardState.refusedForThisWalk as sender, i (sender.ip)}{i > 0
                        ? ', '
                        : ''}<button
                        type="button"
                        class="addr-candidate"
                        disabled={wizardState.enrolRebinding}
                        onclick={() => wizardState.rebindEnrolmentWindow(sender.ip)}>{sender.ip}</button
                      >{/each}.
                  </p>
                  {#if wizardState.rebindError}
                    <p class="load-error">{wizardState.rebindError}</p>
                  {/if}
                {/if}
              {/if}
              {#if step.key === 'syslog' && step.status.state === 'partial' && splits.length > 0}
                <!-- The source-address split (#442), under the
                     observation line. The mismatch sentence's shape:
                     what you told mikroview, what the router shows, no
                     diagnosis; "MikroView can't tell X. You can."; then
                     the printed command. #436 reads the same way. -->
                <div class="split">
                  <p class="note">
                    MikroView can't tell whether these are the same router — a router holds an address
                    on every network it routes, and its logs arrive stamped with whichever one faces
                    this instance. You can tell.
                  </p>
                  <p class="note">
                    <strong>If they are the same router</strong>, pick the address it should be known
                    by here:
                  </p>
                  {#each splits as split (split.declared)}
                    <p class="note">
                      <strong>Keep {split.declared}</strong> (recommended). Run this on the router — it
                      makes the logs arrive from the address you declared:
                    </p>
                    <pre>{srcAddressCommand(split.declared)}</pre>
                    <button
                      type="button"
                      class="copy"
                      onclick={() => copy(srcAddressCommand(split.declared), `src-${split.declared}`)}
                    >
                      {copied === `src-${split.declared}` ? 'Copied' : 'Copy'}
                    </button>
                    <p class="note">
                      Recommended because everything else — the token step 4 mints, the tables it
                      pushes — follows the declared identity, so nothing has to be reissued.
                    </p>
                    <p class="note">
                      {#if arriving.length === 1}
                        <strong>Or keep {arriving[0]}</strong>: change <code>sourceIp</code> to
                        {arriving[0]} in config.yaml and restart.
                      {:else}
                        <strong>Or keep the arriving address</strong>: change <code>sourceIp</code> to
                        whichever of {prose(arriving)} this router is, in config.yaml, and restart.
                      {/if}
                      MikroView then matches what actually arrives. If a token was already minted for
                      {split.declared}, reissue it afterwards — a token keeps the identity it was minted
                      for.
                    </p>
                  {/each}
                  <p class="note">
                    <strong>If they are two different routers, nothing is wrong.</strong>
                    {prose(arriving)} just {arriving.length === 1 ? "hasn't" : "haven't"} been named yet
                    — step 5 covers that — and this notice clears itself when
                    {prose(splits.map((s) => s.declared))}
                    {splits.length === 1 ? 'sends its first log' : 'send their first logs'}.
                  </p>
                </div>
              {/if}
              {#if step.outcome === 'skipped'}
                <p class="decision skipped">
                  Skipped — {SKIP_CONSEQUENCES[step.key]}. {step.receipt}
                </p>
              {:else if step.outcome === 'forced'}
                <p class="decision forced">Forced past — {step.receipt}</p>
              {/if}
            {:else if onFinish}
              <p class="headline">{finishHeadline(ledger)}</p>
              <ol class="readback">
                {#each ledger as s (s.n)}
                  <li class={s.outcome}>
                    <span class="rb-title">{s.n}. {s.title}</span>
                    <span class="rb-detail">
                      {s.receipt || (s.status.state === 'quiet' ? s.status.detail : 'nothing has arrived yet')}
                    </span>
                  </li>
                {/each}
              </ol>
              <!-- The one place the wizard offers the buffer's size
                   (#796): the same track and the same sentence as
                   Settings' memory group, once, on the pane where the
                   operator has finished pointing a router at mikroview
                   and is about to go and look at what arrives. It is
                   asked here rather than as a step of its own because
                   nothing about it can be checked or waited for -- it is
                   a choice, not an observation, and the wizard's steps
                   are all observations. -->
              {#if appState.stats?.memory}
                <div class="memory">
                  <h3>How much to hold</h3>
                  <p class="note">
                    Every event lives in memory and nothing else. This is how much of this machine's
                    memory to spend on it; the oldest events fall away as new ones arrive.
                  </p>
                  <MemoryControl
                    mem={appState.stats.memory}
                    stats={appState.stats}
                    canEdit={isAdmin}
                    onapplied={() => appState.refreshDevicesAndStats().catch(() => {})}
                  />
                </div>
              {/if}
              <p class="note">Run setup… reopens this any time, from your account menu.</p>
              <p class="note">
                <button type="button" class="link" onclick={openLogEveryRule}>Log every rule…</button>
                turns a dark connection into a watched one once MikroView has been listening a day.
              </p>
            {/if}
          </div>
          {/if}
        {/if}
      </div>

      <footer>
        <button type="button" class="ghost" onclick={() => wizardState.back()} disabled={wizardState.pane === 1}>
          Back
        </button>
        <div class="footer-right">
          {#if step && step.hasCheck && step.outcome === 'open' && !warning}
            <span class="hint">{step.n === stepCount ? 'Finish' : 'Next'} checks what has arrived</span>
          {/if}
          {#if step && step.key === 'backup' && wizardState.lostRouterDevice}
            <!-- Round 45's lost-router footer: no skip (there is
                 nothing to skip past -- the router this step is about
                 is already gone), and the primary button is the
                 operator's own word that the repair is done rather
                 than a check on evidence mikroview cannot see. -->
            <button type="button" class="primary" onclick={finishLostRouter}>
              done — the replacement is pushing
            </button>
          {:else if step}
            <button type="button" class="ghost" onclick={onSkip} disabled={busy || naming}>Skip this step</button>
            <button type="button" class="primary" onclick={onNext} disabled={busy || naming}>
              {step.n === stepCount ? 'Finish' : 'Next'}
            </button>
          {:else}
            <!-- The record's own rule: the finish leads out to where
                 the ledger was opened from, and says which. -->
            <button type="button" class="primary" onclick={leaveToLanding}>
              {wizardState.finishTo === 'fleet' ? 'Take me to the fleet' : 'Take me to the fall'}
            </button>
          {/if}
        </div>
      </footer>
    </div>
  </div>

  <!-- Clipped, not hidden: display:none would take it out of the
       accessibility tree and silence the announcement it carries. -->
  <p class="sr-only" role="status">{announcement}</p>
{/if}

<style>
  .veil {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 3vh 3vw;
    z-index: 60;
  }

  /* Below the pointer-width breakpoint the modal is the screen: a
     full-bleed sheet, no veil. */
  .veil.sheet {
    background: var(--bg);
    padding: 0;
  }

  .modal {
    /* The round-1 size correction, applied: "use more of the screen, no
       need to squash things in". */
    width: 940px;
    max-width: 94%;
    max-height: 92vh;
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 10px;
    display: flex;
    flex-direction: column;
    box-shadow: 0 24px 60px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }

  .modal.sheet {
    width: 100%;
    max-width: 100%;
    height: 100%;
    max-height: 100%;
    border: none;
    border-radius: 0;
    box-shadow: none;
  }

  /* The reader (#1219): "grows the modal to near-full-screen" per the
     owner's own words. An explicit height, not just a taller max-height
     -- .modal is otherwise sized to its content, and the reader wants
     the column to fill the space, not merely be allowed to. Never
     reached on a phone: the sheet is already this size, so there is
     nothing to grow into (see the handle's own comment). */
  .modal.reading {
    width: 98vw;
    max-width: 98vw;
    height: 92vh;
  }

  header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .crumb {
    font-size: 12px;
    color: var(--fg-muted);
    white-space: nowrap;
  }

  h2 {
    margin: 0;
    flex: 1;
    font-size: 16px;
    color: var(--fg);
    min-width: 0;
  }

  .close,
  .flip {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
    border-radius: 5px;
    font-size: 12px;
    line-height: 1;
    padding: 8px 10px;
  }

  .close {
    width: 30px;
    height: 30px;
    padding: 0;
    flex: none;
  }

  .close:hover,
  .flip:hover {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  /* The address field (#1213): sits between the header and the step
     list/body split below, so it reads as one question every step
     shares rather than part of any single one. */
  .address-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 12px 18px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .address-field label {
    font-size: 12.5px;
    font-weight: 600;
    color: var(--fg);
  }

  .address-field input {
    background: var(--bg);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-family: var(--font-mono);
    font-size: 12.5px;
    max-width: 420px;
  }

  /* An offered address reads as the link it behaves like, not as another
     control competing with the field above it. */
  .addr-candidate {
    background: none;
    border: 0;
    padding: 0;
    color: var(--log);
    font-family: var(--font-mono);
    font-size: inherit;
    cursor: pointer;
    text-decoration: underline;
  }

  .middle {
    flex: 1;
    display: flex;
    min-height: 0;
  }

  .steps {
    width: 224px;
    flex: none;
    border-right: 1px solid var(--border);
    overflow-y: auto;
    padding: 10px 8px;
  }

  .modal.sheet .steps {
    width: 100%;
    border-right: none;
  }

  .steps ol {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .step-row {
    width: 100%;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    text-align: left;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 6px;
    padding: 9px 10px;
    color: var(--fg-muted);
  }

  .step-row:hover {
    background: var(--bg-hover);
  }

  .step-row.current {
    background: var(--bg-elevated);
    border-color: var(--border);
    color: var(--fg);
  }

  .step-n {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    flex: none;
    border-radius: 50%;
    background: var(--bg);
    border: 1px solid var(--border);
    font-size: 11px;
  }

  .step-row.done .step-n {
    border-color: var(--accept);
    color: var(--accept);
  }

  /* Forced-past: pushed through without evidence, which is a caution,
     not a choice (owner ruling, #1216). */
  .step-row.forced .step-n {
    border-color: var(--caution);
    color: var(--caution);
  }

  /* Skipped: seen and declined, so it gets done's solid treatment --
     just in --log's bright blue instead of --accept's green, so a
     deliberate skip reads as a decision rather than a gap. The dashed
     border stays as the secondary cue that distinguishes it from done
     (owner ruling, #1216). */
  .step-row.skipped .step-n {
    border-style: dashed;
    border-color: var(--log);
    color: var(--log);
  }

  .step-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .step-title {
    font-size: 13px;
  }

  .step-receipt {
    font-size: 11px;
    line-height: 1.4;
    color: var(--accept);
    word-break: break-word;
  }

  .step-receipt.gap,
  .step-receipt.consequence {
    color: var(--fg-muted);
  }

  /* Skipped's receipt follows its disc's ink rather than staying grey,
     so the row is not half-coloured (#1216). Its own gap/consequence
     sub-line stays muted regardless -- more specific so it is not
     outweighed by the rule just above. */
  .step-row.skipped .step-receipt {
    color: var(--log);
  }

  .step-row.skipped .step-receipt.gap,
  .step-row.skipped .step-receipt.consequence {
    color: var(--fg-muted);
  }

  .step-row.forced .step-receipt {
    color: var(--caution);
  }

  .body {
    flex: 1;
    min-width: 0;
    overflow-y: auto;
    padding: 18px 22px 22px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }

  .lead,
  .note,
  .headline {
    margin: 0;
    font-size: 14px;
    line-height: 1.6;
    color: var(--fg);
  }

  .note {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  /* The below-minimum router-standing warning (#436): the same amber
     already used for this wizard's "heavy" caution register (.amber,
     .heavy below), as a left rule rather than a new colour -- a router
     outside the table's floor never blocks, but the note still reads as
     the loudest thing on the step. */
  .note.below-minimum {
    border-left: 3px solid var(--log);
    padding-left: 10px;
  }

  /* Step 6's caveat (#394, round 45): the same amber the heavy warning
     above already uses, ahead of the script rather than after it. */
  .wzcaveat {
    align-self: stretch;
    border-left: 2px solid var(--log);
    padding: 6px 12px;
    margin: 0;
    font-size: 12px;
    line-height: 1.5;
    color: var(--fg-muted);
  }

  .wzcaveat b {
    color: var(--log);
    font-weight: 600;
  }

  /* Step 6's no-script state (#1217): a heading and one line per unmet
     precondition, in place of the input boxes and Copy buttons the step
     would otherwise show -- there is nothing behind either to copy. */
  .no-script {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .no-script h4 {
    margin: 0;
    font-size: 13px;
    color: var(--fg-muted);
  }

  .no-script ul {
    margin: 0;
    padding-left: 18px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .no-script li {
    margin: 0;
  }

  /* Step 6's key field with no key mounted (#1133): the field, its copy
     control and Reroll on one line, laid out like the version pick-list
     above rather than as a new idiom. */
  .keymint {
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .keymint label {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .keymint input {
    flex: 1 1 320px;
    min-width: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-family: var(--font-mono);
    font-size: 12.5px;
  }

  /* #1291's two asks -- the mint form on Send logs, and the Register
     step's name -- drawn as the key mint's field above is, for the same
     reason the name step is: they are the same shape, a label, a box
     and a button. Wrapping rather than a row, since the mint form has
     two fields and their notes between them. */
  .mint-ask {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .mint-ask label {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .mint-ask input {
    min-width: 0;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-family: var(--font-mono);
    font-size: 12.5px;
  }

  .mint-ask button {
    align-self: flex-start;
  }

  /* The name step's one field (#1284). Drawn as the key mint's field
     above is, because it is the same shape -- a label, a box, and the
     step's own primary in the footer doing the act. */
  .namefield {
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .namefield label {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .namefield input {
    flex: 1 1 260px;
    min-width: 0;
    max-width: 320px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-family: var(--font-mono);
    font-size: 12.5px;
  }

  /* The enrolment token's plain line under the block (#1281), and the
     block itself once the token in it has lapsed: dimmed, because what
     it prints can no longer be pasted. No new colour -- the dim is the
     muted ink the wizard already uses for a thing that is not current. */
  pre.stale {
    opacity: 0.5;
  }

  .token-life.expired {
    color: var(--fg-dim);
  }

  /* The lost-router title (#394, round 45): "<router> is gone" in the
     same amber. */
  .lost {
    color: var(--log);
    font-weight: 600;
  }

  .routeros-version {
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .routeros-version label {
    font-size: 12.5px;
    color: var(--fg-muted);
  }

  .routeros-version select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-size: 13px;
  }

  .headline {
    font-size: 15px;
  }

  /* The buffer-size control on the finish pane (#796) -- separated from
     the read-back above it the way the wizard already separates its own
     blocks, and left to MemoryControl for everything inside. */
  .memory {
    margin-top: 18px;
    padding-top: 14px;
    border-top: 1px solid var(--border);
  }

  .memory h3 {
    margin: 0 0 4px;
    font-size: 13px;
    font-weight: 600;
  }

  /* #1219: every paste block in the wizard shares this one selector, so
     the fix lives here once rather than per box. white-space: pre with
     overflow-x: auto (the old rule) is what let the scheduler box run
     off the right edge, cut off mid-word, with no wrap and -- because
     it was a bare `<pre>`, missing the `scrollbar` class its siblings
     carry -- no visible bar to say so. Wrapping means there is no
     right edge to run off: every character is on screen without
     scrolling, and the Copy button still copies the real, un-wrapped
     string underneath.
     text-indent's `hanging each-line` needs no per-line markup to tell
     a wrapped continuation from the next command: `each-line` scopes
     text-indent to every line the browser treats as forced (which
     includes a preserved `\n` under pre-wrap, not only the block's
     first line), and `hanging` flips its target from that line to
     every other -- i.e. exactly the rows a soft wrap adds. Widely
     supported (Chromium, Firefox and Safari all ship it); where it
     is not, the declaration is simply ignored and wrapping still
     works, just without the hanging cue. */
  pre {
    margin: 0;
    align-self: stretch;
    padding: 12px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 6px;
    font-size: 12.5px;
    line-height: 1.6;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    text-indent: 1.1em hanging each-line;
    color: var(--fg);
    user-select: all;
  }

  /* #1146's cap, still whole lines -- 14 of them, plus the padding this
     border-box height includes, in `em` so the sheet's smaller type
     below still lands on a line boundary. What #1146 did not hold
     against, and #1219 fixes: a flex item shrinks by default, and a
     short modal was squeezing this box under that cap rather than
     merely limiting it above it, so the last visible row was a
     mid-glyph fragment rather than one of 14 whole lines.
     flex-shrink: 0 pins it at its intended size regardless of the
     modal's own height; .body's own overflow (below) absorbs a short
     viewport instead of this box being asked to. Content shorter than
     14 lines was never at risk -- box-sizing gives it exactly its own
     line count, always a whole multiple of the line height -- so this
     is the one property the squeeze needed. */
  pre.script {
    max-height: calc(14 * 1.6em + 24px);
    overflow-y: auto;
    flex-shrink: 0;
  }

  /* The token's own box (#1131): one line, and the one thing on this
     step that is shown once and never again. word-break: break-all is
     kept on top of the shared pre's own wrap -- a token is one run
     with no spaces to wrap at, so anywhere is the only place it can
     break. */
  pre.token {
    word-break: break-all;
  }

  /* Slightly smaller so a wrapped command reads as one paragraph
     rather than a wall of short broken lines on a narrow sheet; the
     wrap itself is already the shared pre's default. */
  .modal.sheet pre {
    font-size: 12px;
  }

  /* No reader exists on a phone (the handle is not rendered there --
     see its own comment), so a script pre needs to be readable inline
     instead: the 14-line cap would strand the rest of it with nothing
     to expand into. The sheet's own body already scrolls, the same way
     it does for every other step's overflowing content. */
  .modal.sheet pre.script {
    max-height: none;
    overflow-y: visible;
    flex-shrink: 1;
  }

  /* The paste block (#1219): the pre plus its drawer handle, laid out
     as one row so the handle reads as part of the box's right edge
     rather than a control beside it. */
  .paste {
    align-self: stretch;
    display: flex;
    align-items: stretch;
    gap: 0;
  }

  .paste pre.script {
    flex: 1;
    min-width: 0;
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
    border-right: none;
  }

  /* The handle itself: full box height, on the right edge, drawn as
     part of the pre rather than a button floating beside it -- "like a
     drawer handle" (the owner's words, verbatim). ‹ points at the
     column it opens into. */
  .handle {
    flex: none;
    width: 26px;
    padding: 0;
    border: 1px solid var(--border);
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    background: var(--bg-elevated);
    color: var(--fg-muted);
    font-size: 15px;
    line-height: 1;
    cursor: pointer;
  }

  .handle:hover {
    color: var(--fg);
    background: var(--bg-hover);
  }

  /* The reader (#1219): the body's own slot, filled instead of the
     step's usual content, so the script gets the whole height of the
     column the body already had -- no cap, because the column itself
     is now the limit and it is already close to the screen's. */
  .reader {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 18px 22px 22px;
    gap: 10px;
  }

  .reader-head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .reader-label {
    font-size: 13px;
    color: var(--fg-muted);
  }

  .reader-pre {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  button {
    border-radius: 5px;
    padding: 7px 13px;
    font-size: 13px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  button:hover:not(:disabled) {
    color: var(--fg);
    border-color: var(--fg-muted);
  }

  button.primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--bg);
    font-weight: 600;
  }

  button.amber {
    background: var(--log);
    border-color: var(--log);
    color: var(--bg);
    font-weight: 600;
  }

  /* Log every rule's own door on the finish pane (#435): reads as a link
     inline with the sentence beside it, not a second boxed button next
     to "Run setup… reopens this". Step 6's "see it in Settings", "mint
     a new one" and "how to mount one"/"download the newest .backup"
     (#394, round 45) read the same way -- the download is a <button>
     now, not a real <a>, so downloadFromUrl can read a locked vault as
     a status rather than the browser navigating to whatever a 403
     answers with (#1218 audit finding 16). */
  button.link,
  a.olink,
  button.olink {
    display: inline;
    border: none;
    padding: 0;
    background: none;
    color: var(--accent);
    text-decoration: underline;
    font-size: inherit;
    cursor: pointer;
  }

  /* Step 6's transport pair (#955). The chosen side is the answer, not
     a link: body ink, no underline, and nothing to click towards --
     which leaves exactly one thing on the line that looks clickable,
     the one that would change something. */
  button.olink.on {
    color: var(--fg);
    text-decoration: none;
    font-weight: 600;
    cursor: default;
  }

  .note.transport {
    margin-top: 2px;
  }

  button:disabled {
    opacity: 0.5;
  }

  .observation {
    margin: 0;
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    line-height: 1.5;
    padding: 9px 12px;
    border-radius: 6px;
    border: 1px solid var(--border);
    color: var(--fg-muted);
  }

  .observation.waiting {
    border-style: dashed;
  }

  .observation.arrived,
  .observation.counting {
    border-color: var(--accept);
    color: var(--accept);
  }

  .observation.attention {
    border-color: var(--reject);
    color: var(--reject);
  }

  /* A partial step's shortfall (#1132): its own box under the arrived
     line, in the warning colour. Deliberately --warn and not --reject:
     nothing is wrong on mikroview's side, something simply has not
     arrived yet, and the red is spoken for by `attention`. */
  .observation.shortfall {
    border-color: var(--warn);
    color: var(--warn);
  }

  .dot {
    width: 7px;
    height: 7px;
    flex: none;
    border-radius: 50%;
    background: var(--fg-muted);
    animation: pulse 1.8s ease-in-out infinite;
  }

  @keyframes pulse {
    0%,
    100% {
      opacity: 0.35;
    }
    50% {
      opacity: 1;
    }
  }

  /* The source-address split's body (#442): the wizard's own note and
     pre blocks, grouped so they read as one explanation under the
     observation line rather than as loose notes. */
  .split {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 10px;
  }

  .split strong {
    color: var(--fg);
  }

  .decision {
    margin: 0;
    align-self: stretch;
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--fg-muted);
  }

  .decision.forced {
    color: var(--log);
  }

  .heavy {
    align-self: stretch;
    display: flex;
    flex-direction: column;
    gap: 12px;
    border: 1px solid var(--log);
    border-radius: 8px;
    padding: 16px;
    background: var(--bg-elevated);
  }

  .heavy h3 {
    margin: 0;
    font-size: 15px;
    color: var(--log);
  }

  .heavy p {
    margin: 0;
    font-size: 13.5px;
    line-height: 1.6;
    color: var(--fg);
  }

  .heavy .quote {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
    padding: 10px 12px;
    border: 1px dashed var(--log);
    border-radius: 6px;
    color: var(--log);
    word-break: break-word;
  }

  .heavy-actions {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }

  .readback {
    align-self: stretch;
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .readback li {
    display: flex;
    flex-direction: column;
    gap: 2px;
    border-left: 2px solid var(--border);
    padding: 2px 0 2px 12px;
  }

  .readback li.done {
    border-left-color: var(--accept);
  }

  .readback li.forced {
    border-left-color: var(--log);
  }

  .readback li.skipped {
    border-left-style: dashed;
  }

  .rb-title {
    font-size: 13px;
    color: var(--fg);
  }

  .rb-detail {
    font-size: 12px;
    color: var(--fg-muted);
  }

  .load-error {
    margin: 0;
    color: var(--reject);
    font-size: 13px;
  }

  .mint {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
  }

  .mint select {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--fg);
    border-radius: 5px;
    padding: 7px 10px;
    font-size: 13px;
  }

  .token-note {
    color: var(--fg);
  }

  code {
    font-size: 11.5px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 3px;
    padding: 1px 4px;
  }

  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 18px;
    border-top: 1px solid var(--border);
    background: var(--bg-elevated);
  }

  .footer-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .hint {
    font-size: 12px;
    color: var(--fg-muted);
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  /* Reduced motion stops the waiting-dot pulse and makes the phone's
     body <-> ledger flip instant, per the record. */
  @media (prefers-reduced-motion: reduce) {
    .dot {
      animation: none;
      opacity: 0.7;
    }
  }
</style>
