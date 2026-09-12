// SPDX-License-Identifier: AGPL-3.0-only
//
// The setup wizard as a modal (#487, replacing the page from #320)
// against a real running mikroview.
//
// The unit tests cover command generation, the step rules and the
// ledger's arithmetic. What they cannot show is the part that matters
// most: that the wizard's claims track what the server actually
// observed, and that a decision the operator records in the modal really
// reaches the places the design record promises it reaches -- the step
// list, the setup status every other surface reads, and the audit log.
// So every assertion here goes through a real browser against a real
// server.

import { session, feedSyslog, check, done, goTo, waitForStreamRows } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// dismissSetup: false, because this scenario drives the modal itself.
// Auto-launch will not have fired: it is gated on the instance having no
// devices, and the harness declares a router the reset keeps -- so the
// door under test here is the relaunch one, which is the same door.
const { page, consoleErrors } = await session({ dismissSetup: false })

// Its own traffic: the instance is reset before every scenario (#1064),
// so nothing a sibling fed is there to count.
feedSyslog(20, 'live-setup-wizard')
await waitForStreamRows(page, 20)

// commandsSeen records every /api/setup/commands answer, from before the
// modal is even opened, so the version pick below always has a pre-pick
// response to compare against. Registered here rather than next to the
// pick because the poll that produces these is the only thing that
// refills it, and missing the last one would make the comparison itself
// the flake.
const commandsSeen = []
page.on('response', (r) => {
  if (!r.url().includes('/api/setup/commands')) return
  r.json()
    .then((body) => commandsSeen.push({ version: r.request().postDataJSON()?.version, body }))
    .catch(() => {})
})

// Every request this page makes, and every websocket frame it sends,
// recorded from before the modal is ever opened. The no-key section at
// the end needs it to prove a negative: a key minted in the browser
// (#1133) must never appear in a URL or a body, and the only way to show
// that is to have watched everything that left.
const requestsSeen = []
page.on('request', (r) => requestsSeen.push({ url: r.url(), body: r.postData() ?? '' }))
page.on('websocket', (ws) =>
  ws.on('framesent', (f) => requestsSeen.push({ url: ws.url(), body: String(f.payload ?? '') })),
)

const modal = page.locator('.setup-wizard')
if (await modal.count()) {
  await page.keyboard.press('Escape')
  await modal.waitFor({ state: 'detached' })
}

// --- Run setup… opens the modal, over whatever page is showing --------
// It is an action, not a page: the shell behind it stays mounted, which
// is the whole difference from the view this replaced.
await goTo(page, 'Run setup…')
await modal.waitFor({ state: 'visible' })
check(
  await page.locator('.card[aria-hidden="false"] .scene-bar').isVisible(),
  'the shell is still there behind the modal — this is a modal, not a page',
)
check(!(await page.locator('main .setup').count()), 'no wizard page route remains — the view was removed wholesale')

// --- Explicit close only ----------------------------------------------
// Progress is never lost to a stray click. Clicking the veil (well
// outside the modal box) must do nothing at all.
const veil = page.locator('.veil')
const box = await modal.boundingBox()
await page.mouse.click(Math.max(4, Math.floor(box.x / 2)), Math.max(4, Math.floor(box.y / 2)))
// Genuine negative assertion: proving the click did nothing has no
// end-state to wait for, only an interval long enough for a dismissal
// to have shown up if the veil were not inert.
await page.waitForTimeout(300)
check(await modal.isVisible(), 'clicking outside does not dismiss the modal')
check((await veil.count()) === 1, 'the veil is present but inert')

// --- The step list is the ledger --------------------------------------
// Six steps plus the read-back, since #394 (round 44/45) added "Back up
// the router" as the ledger's sixth entry, straight after "Name your
// router" -- see setupsteps.ts's buildLedger and its STEP_TITLES.
const stepTitles = await page.$$eval('.setup-wizard .steps .step-title', (els) =>
  els.map((e) => e.textContent?.trim() ?? ''),
)
check(
  stepTitles.length === 7,
  `six steps and the read-back, always the same count (${JSON.stringify(stepTitles)})`,
)

// --- Commands carry real values, never placeholders --------------------
const host = new URL(URL_BASE).host
const status = await page.request.get(`${URL_BASE}/api/setup/status`).then((r) => r.json())
const syslogPort = status.instance.syslogPort.split(':').pop()

const seen = []
for (let step = 1; step <= 5; step++) {
  await page.locator(`.setup-wizard .steps li:nth-child(${step}) .step-row`).click()
  await page.locator('.setup-wizard .body').waitFor({ state: 'visible' })
  const blocks = await page.$$eval('.setup-wizard .body pre', (els) => els.map((e) => e.textContent ?? ''))
  seen.push(...blocks)
}
check(seen.length > 0, 'the wizard renders command blocks')

// No exemption for "<paste the script above>" any more: #1131 retired
// the placeholder along with the second box it belonged to.
const withPlaceholders = seen.filter((b) => /<[a-z-]+>/.test(b))
check(
  withPlaceholders.length === 0,
  `no block still contains a placeholder (${withPlaceholders.length} did)`,
)
check(
  seen.some((b) => b.includes(`https://${host}/ca.crt`)),
  `the CA fetch names this instance (${host})`,
)
// The syslog port comes from the running config, not an assumed 6514 --
// live-env.sh uses a non-default port, so this would fail if it were
// hard-coded.
check(
  seen.some((b) => b.includes(`remote-port=${syslogPort}`)),
  `the syslog command uses this instance's port (${syslogPort})`,
)

// --- The RouterOS version pick-list (#436 item 4) -----------------------
// commandsHead renders on steps 1-4 only (the loop above ended on step
// 5, which never shows it), so land back on step 1 before looking for
// it.
await page.locator('.setup-wizard .steps li:nth-child(1) .step-row').click()
await page.locator('.setup-wizard .body').waitFor({ state: 'visible' })

const versionSelect = page.locator('.setup-wizard select#routeros-version-select')
check((await versionSelect.count()) === 1, 'the "your RouterOS version" pick-list is present')
const versionOptions = await versionSelect.locator('option').allTextContents()
check(
  versionOptions[0] === 'Not sure — the router will report it',
  `its first option is the "not sure" one (${JSON.stringify(versionOptions[0])})`,
)
check(
  versionOptions.length > 1 &&
    versionOptions
      .slice(1)
      .every((label) => /^\d+\.\d+(?:\.\d+)?(–\d+\.\d+(?:\.\d+)?)?$/.test(label)),
  `at least one further option is built from a dialect row (${JSON.stringify(versionOptions)})`,
)

// --- No standing warning on a fresh instance (#436 item 5) --------------
// Nothing has pushed a routerosVersion on this harness and the picker is
// still at "not sure", so the router-standing warning must not render --
// below-minimum and ahead-of-review share the same copy fragment
// ("runs RouterOS"), so this catches either kind.
const standingWarning = page.locator('.setup-wizard .note', { hasText: 'runs RouterOS' })
check(
  (await standingWarning.count()) === 0,
  'no standing warning renders when no router has reported a version',
)

// --- Choosing a row re-renders the same dialect's commands --------------
// Picking an explicit version feeds `version` into the commands request
// (commandsKey above). The wizard keeps the old blocks on screen until
// the new response lands, so a stale render would pass a text
// comparison on its own: wait for the request the pick triggers, and
// check the server answered it, before looking at what came back.
const pickedLabel = versionOptions.find((label, i) => i > 0)
// Matched on the request's own `version` field, not just the URL.
// wizard.svelte.ts's refreshCommands runs on a 5s poll for as long as
// the modal is open (SetupWizard.svelte's POLL_MS), and re-fires
// whenever commandsKey changes -- which includes pushKinds, so a
// backend still catching up on an earlier scenario's push can flip it
// independently of anything this scenario does. Its own comment records
// this exact test failing that way before the seq guard existed: a
// still-in-flight, unrelated commands request resolving around the same
// time as the deliberate pick. The guard fixes which response the app
// keeps; it does nothing for a test that matches on URL alone, which can
// just as easily catch the unrelated response, see 200 (a normal
// success -- nothing here was ever a bad response), and read `reseen`
// before the version-scoped one has landed. Requiring `version` to equal
// what was actually picked -- not merely "some commands request
// answered" -- is what ties the wait to the exchange this step needs.
const pickedValue = await versionSelect.locator('option').nth(1).getAttribute('value')
const [pickResponse] = await Promise.all([
  page.waitForResponse(
    (r) => r.url().includes('/api/setup/commands') && r.request().postDataJSON()?.version === pickedValue,
  ),
  versionSelect.selectOption({ label: pickedLabel }),
])
check(pickResponse.status() === 200, `picking ${pickedLabel} re-requests the commands (${pickResponse.status()})`)
await page.locator('.setup-wizard .body').waitFor({ state: 'visible' })

const reseen = []
for (let step = 1; step <= 5; step++) {
  await page.locator(`.setup-wizard .steps li:nth-child(${step}) .step-row`).click()
  await page.locator('.setup-wizard .body').waitFor({ state: 'visible' })
  const blocks = await page.$$eval('.setup-wizard .body pre', (els) => els.map((e) => e.textContent ?? ''))
  reseen.push(...blocks)
}
check(reseen.length > 0, `the wizard still renders command blocks after picking ${pickedLabel} (${reseen.length})`)
// The version pick only feeds `version` into the request, and every row
// in dialects.go carries the same dialect today, so the steps that are
// built from the dialect alone -- CA trust, syslog, rule tagging and
// schedule -- must come back byte-identical. That is the real invariant,
// and it is what the previous assertion was reaching for.
//
// It used to compare the whole DOM scrape instead, and that is why it
// failed at 72ca083 (#1025): `seen` and `reseen` also contain the push
// and backup blocks, which are built from the token, device and
// pushKinds rather than the dialect. refreshCommands re-fires whenever
// commandsKey changes, pushKinds is part of that key, and a backend
// still catching up on an earlier scenario's push can flip it while this
// scenario is mid-walk -- so the two scrapes could differ with nothing
// about the version having changed. The diagnosis on the issue ("several
// dialects ship now, so picking renders different text") did not hold:
// all four rows are dialect "a", and a row's Note renders as `p.note`,
// never as a `pre` block.
//
// Comparing the responses rather than the rendering also drops the
// dependency on which step happens to be open when a block is scraped.
const DIALECT_STEPS = ['caTrust', 'syslog', 'ruleTagging', 'schedule']
const dialectSteps = (body) => DIALECT_STEPS.map((k) => body.steps[k].commands)
const beforePick = commandsSeen.filter((c) => !c.version).at(-1)
// Opening the modal issues one of these before anything is picked, and
// the poll issues more while the step walk above runs, so this is only
// ever absent if the wizard stopped asking at all -- worth failing on
// rather than reading past.
check(beforePick !== undefined, 'a commands response was recorded before the pick, to compare it against')
if (beforePick) {
  const before = dialectSteps(beforePick.body)
  const after = dialectSteps(await pickResponse.json())
  const differing = DIALECT_STEPS.filter((_, i) => before[i] !== after[i])
  check(
    differing.length === 0,
    `picking ${pickedLabel} leaves the dialect-built commands unchanged, as one dialect requires (${differing.join(', ') || 'all four identical'})`,
  )
}

// The blocks the pick does not control are still required to render --
// this is what the old whole-scrape comparison was also catching, kept
// as its own check so a blank render fails loudly instead of comparing
// equal to another blank render.
check(
  reseen.every((b) => b.length > 0),
  `every command block still has content after picking ${pickedLabel} (${reseen.length} blocks)`,
)

// --- Observation lines reflect what the server observed ----------------
// The harness has already fed events, so events-arriving must be true,
// and those events carry log-prefixes, so actions decode.
const deviceObs = status.devices.find((d) => d.events > 0)
check(!!deviceObs, `the server observed events (${JSON.stringify(status.devices)})`)
check(
  !!deviceObs && deviceObs.decodedActions > 0,
  'the server observed events carrying a decoded action',
)

// Which flavour the rule-tagging step shows is derived from what the
// server reports, not hard-coded. Step 3 counts and can only count
// upward, so any arrival there reads as counting -- hard-coding "done"
// would be asserting a property of the harness rather than of the
// wizard.
// #1096: read the observation until the wizard has caught up, not once.
// The wizard's copy of the status is its own open-time fetch plus a
// 5-second timer (SetupWizard.svelte's POLL_MS) -- a different fetch
// from the `status` this scenario read above. On a fresh instance the
// wizard's first fetch can land before the server has ingested the feed,
// so a single read raced the next poll: the scenario failed on a clean
// instance and passed mid-shard, where events were already there.
// settledObservation polls past three of the wizard's own intervals and
// then returns whatever it settled at, so check() below still reports
// the class it actually saw rather than throwing on a timeout.
//
// `:not(.shortfall)` because a partial step renders two observation
// boxes (#1132) -- the arrived line and the shortfall under it -- and
// an unqualified locator would match both and fail Playwright's strict
// mode rather than read the line this is asking about.
async function settledObservation(want, { timeoutMs = 16000 } = {}) {
  const read = async () => (await page.locator('.setup-wizard .observation:not(.shortfall)').getAttribute('class')) ?? ''
  const deadline = Date.now() + timeoutMs
  let seen = await read()
  while (!want(seen) && Date.now() < deadline) {
    await page.waitForTimeout(500)
    seen = await read()
  }
  return seen
}

await page.locator('.setup-wizard .steps li:nth-child(3) .step-row').click()
const ruleObservation = await settledObservation((c) => c.includes('counting'))
check(
  ruleObservation.includes('counting'),
  `the rule-tagging step counts what has arrived (${ruleObservation})`,
)

// The push step must agree with the server about whether anything has
// pushed -- a wizard that reports a step it cannot see is the whole
// failure mode being guarded against.
//
// Derived from /api/setup/status, not hard-coded to "waiting". Every
// scenario shares one instance and live-routeros-ingest.mjs pushes real
// tables before this runs, so whether the push step has evidence
// depends on what else has run -- asserting "waiting" was asserting a
// property of the harness rather than of the wizard, which is the exact
// mistake the rule-tagging check above avoids. It failed that way on
// first run.
const alreadyPushed = status.devices.some((d) => Object.keys(d.pushedKinds ?? {}).length > 0)
// Same race as step 3, and the same wait: the wizard has to agree with
// the server before its answer means anything. The target here is
// whichever answer the server gave, not a fixed one.
await page.locator('.setup-wizard .steps li:nth-child(4) .step-row').click()
const pushObservation = await settledObservation((c) => c.includes('arrived') === alreadyPushed)
check(
  pushObservation.includes('arrived') === alreadyPushed,
  `the push step ${alreadyPushed ? 'reports what arrived' : 'does not claim success before anything pushed'} ` +
    `(${pushObservation}, server says pushed=${alreadyPushed})`,
)

// --- Forcing past is loud, and the record is the feature ---------------
// Step 1 is the CA fetch. No router fetches /ca.crt on this harness, so
// it is genuinely waiting -- the exact state the heavy warning is for.
await page.locator('.setup-wizard .steps li:nth-child(1) .step-row').click()
const stepOneObservation =
  (await page.locator('.setup-wizard .observation:not(.shortfall)').getAttribute('class')) ?? ''
if (stepOneObservation.includes('waiting')) {
  await page.click('.setup-wizard footer button.primary')
  const heavy = page.locator('.setup-wizard .heavy')
  await heavy.waitFor({ state: 'visible' })
  check(true, 'Next on a waiting step raises the heavy warning instead of proceeding')

  const quoted = ((await page.textContent('.setup-wizard .heavy .quote')) ?? '').trim()
  check(
    /^setup · step 1 forced past · /.test(quoted),
    `the amber button quotes the exact record it will write (${quoted})`,
  )
  check(
    (await page.locator('.setup-wizard .heavy button').count()) === 2,
    'two choices and no third — keep waiting, or go on anyway',
  )

  await page.click('.setup-wizard .heavy button.amber')
  await heavy.waitFor({ state: 'detached' })

  // It reached the server's ledger, which is what every other surface
  // reads to explain its own silence.
  const after = await page.request.get(`${URL_BASE}/api/setup/status`).then((r) => r.json())
  const mark = (after.marks ?? []).find((m) => m.step === 1)
  check(!!mark && mark.outcome === 'forced', `the force reached the ledger (${JSON.stringify(mark)})`)
  check(!!mark && !!mark.note, 'the mark records what was not observed, not just that a button was pressed')

  // And the audit log, which is the done-when's "visibly recorded where
  // diagnostics can reach it".
  const audit = await page.request.get(`${URL_BASE}/api/audit`).then((r) => r.json())
  check(
    (audit.entries ?? []).some((e) => e.action === 'setup.step_forced' && e.target === 'step 1'),
    'the forced-past decision is in the audit log',
  )

  // And the step list, for the wizard's life.
  const rowOne =
    (await page.locator('.setup-wizard .steps li:nth-child(1) .step-row').getAttribute('class')) ?? ''
  check(rowOne.includes('forced'), `the step list carries the forced-past mark (${rowOne})`)
  const receipt =
    ((await page.textContent('.setup-wizard .steps li:nth-child(1) .step-receipt')) ?? '').trim()
  check(/forced past by /.test(receipt), `the step list names who forced it and when (${receipt})`)
} else {
  check(true, `step 1 already has its evidence on this instance (${stepOneObservation}) — nothing to force`)
}

// --- The version hint never breaks the CA fetch (#436 item 3) -----------
// Step 1's CA fetch gains a `?ros=` query string so mikroview learns the
// version from the very first request, before any push. The value is
// untrusted operator-controlled text, so the one thing that must always
// be true is that the fetch still works and still returns the
// certificate -- not that a warning follows, since whether the browser's
// own source address maps to a device the gate has declared is not
// something this scenario controls.
//
// Run after the forcing-past block above, deliberately: this is a GET to
// the same /ca.crt endpoint a real router fetches, and the server cannot
// tell this scenario's own probe apart from one -- caStep
// (setupsteps.ts) marks step 1 "done" the moment *anything* fetches it.
// Doing this earlier used to satisfy step 1's evidence as a side effect,
// so by the time the forcing-past block ran, "evidence outranks a mark"
// (buildLedger's own rule) correctly showed the row as done from a real
// CA fetch instead of forced -- which read as a forced-past rendering
// bug but was this ordering.
const caResponse = await page.request.get(`${URL_BASE}/ca.crt?ros=7.16`)
check(caResponse.status() === 200, `the CA fetch with a version hint still succeeds (${caResponse.status()})`)
const caBody = await caResponse.text()
check(
  caBody.startsWith('-----BEGIN CERTIFICATE-----'),
  'and the hint never breaks the certificate it returns',
)

// --- Skip is quiet, and states its consequence -------------------------
await page.locator('.setup-wizard .steps li:nth-child(5) .step-row').click()
await page.click('.setup-wizard footer button:has-text("Skip this step")')
await page.locator('.setup-wizard .steps li:nth-child(5) .step-row.skipped').waitFor({ state: 'visible' })
const skippedReceipt =
  ((await page.textContent('.setup-wizard .steps li:nth-child(5) .step-text')) ?? '').trim()
check(/skipped by /.test(skippedReceipt), `a skipped step records who and when (${skippedReceipt})`)
check(
  /address/.test(skippedReceipt),
  `and states its consequence rather than reproaching anyone (${skippedReceipt})`,
)

// --- Minting a token produces a script that actually works -------------
await page.locator('.setup-wizard .steps li:nth-child(4) .step-row').click()
if (await page.locator('.setup-wizard .mint select').count()) {
  await page.selectOption('.setup-wizard .mint select', deviceObs.device)
  await page.click('.setup-wizard .mint button.primary')
}
await page.locator('.setup-wizard pre.script').waitFor({ state: 'visible' })
const script = (await page.textContent('.setup-wizard pre.script')) ?? ''

// #1131: the token is shown, in its own box above the script, rather
// than only claimed to be "in the script below".
const shownToken = ((await page.textContent('.setup-wizard pre.token')) ?? '').trim()
check(shownToken.length > 0, 'the minted token is shown in its own box')
check(
  (await page.locator('.setup-wizard button.copy:has-text("Copy token")').count()) === 1,
  'and has its own Copy control',
)

// One block, pastable as it stands: the /system script add that carries
// the whole push script, the scheduler entry, and the run that makes
// the first push happen now. No second box, and nothing asking the
// operator to paste one clipboard inside another.
check(
  script.startsWith('/system script add name=mv-push policy=read,test source="'),
  `the block saves the script itself (${script.slice(0, 60)})`,
)
check(!script.includes('<paste the script above>'), 'no placeholder is left for the operator to fill in')
check(
  script.includes('/system scheduler add name=mv-push') && script.trimEnd().endsWith('/system script run mv-push'),
  'the same block schedules it and runs it once',
)
check(
  (await page.locator('.setup-wizard .body pre').count()) === 2,
  'step 4 hands over exactly two boxes — the token, and the one block',
)

// Stop at the closing quote: \S+ swallows it, and a token with a
// trailing " authenticates as nothing (401) -- which looked like a
// product bug on first run and was this line. The quote is escaped
// (\") inside the source="..." wrapper since #1131, so the backslash
// has to be excluded too, for the same reason.
const tokenMatch = script.match(/Bearer ([^"\s)\\]+)/)
check(!!tokenMatch, 'the generated script embeds a bearer token')
const token = tokenMatch?.[1] ?? ''
check(token === shownToken, 'the token shown is the token the script carries')

// Every kind the server declares must appear in the script -- with its
// quotes escaped, since the script now sits inside source="...".
for (const kind of status.pushKinds) {
  check(script.includes(`\\"kind\\"=\\"${kind}\\"`), `the script pushes ${kind}`)
}

// The proof: the token the wizard minted, used the way the script uses
// it, is accepted.
const pushed = await fetch(`${URL_BASE}/api/ingest/routeros`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
  body: JSON.stringify({
    // arp, not filter-rule: scenarios share one instance and an earlier
    // one has already pushed filter rules, so waiting for those to
    // appear would pass whether or not this push worked at all.
    kind: 'arp',
    page: 1,
    pages: 1,
    records: [{ address: '192.0.2.77', mac: 'aa:bb:cc:dd:ee:77' }],
  }),
})
check(pushed.status === 200, `the wizard-minted token is accepted for a push (${pushed.status})`)

// And the wizard notices, without a reload -- evidence outranks a mark,
// which is what makes the ledger honest rather than a form.
//
// Waits for the receipt to name `arp` specifically, not merely for the
// row to be green: on a shared instance an earlier scenario may already
// have pushed other tables, so "green" can be true before this push
// lands and would prove nothing about it.
await page
  .locator('.setup-wizard .steps li:nth-child(4) .step-row.done .step-receipt:has-text("arp")')
  .waitFor({ state: 'visible', timeout: 20000 })
const pushReceipt =
  ((await page.textContent('.setup-wizard .steps li:nth-child(4) .step-receipt')) ?? '').trim()
check(true, `the push step records this push on its own once the table arrives (${pushReceipt})`)

// --- A partial step's shortfall is its own box (#1132) -----------------
// One table has arrived and the others have not, so step 4 is partial:
// the arrived line says what came, and what is still missing is a
// second box under it, in the warning register rather than the green
// one. Derived from what the server says has arrived, never assumed --
// the same rule the observation checks above follow.
const afterPush = await page.request.get(`${URL_BASE}/api/setup/status`).then((r) => r.json())
const arrivedKinds = new Set(
  afterPush.devices.flatMap((d) => Object.keys(d.pushedKinds ?? {})),
)
const missingKinds = (afterPush.pushKinds ?? []).filter((k) => !arrivedKinds.has(k))
const arrivedLine = page.locator('.setup-wizard .observation:not(.shortfall)')
const shortfallBox = page.locator('.setup-wizard .observation.shortfall')
if (missingKinds.length > 0) {
  await shortfallBox.waitFor({ state: 'visible', timeout: 20000 })
  const arrivedText = ((await arrivedLine.textContent()) ?? '').replace(/\s+/g, ' ').trim()
  const shortfallText = ((await shortfallBox.textContent()) ?? '').replace(/\s+/g, ' ').trim()
  check(
    arrivedText === `Arrived: ${[...arrivedKinds].sort().join(', ')}.`,
    `the arrived line says only what arrived (${arrivedText})`,
  )
  check(
    !/missing/.test(arrivedText),
    'the green line never carries the shortfall — that is the whole split',
  )
  check(
    shortfallText === `Still missing: ${missingKinds.join(', ')}.`,
    `the shortfall is its own box, naming what has not come (${shortfallText})`,
  )
  check(
    await arrivedLine.evaluate((el) => el.classList.contains('arrived')),
    'what arrived still reads in the arrived voice',
  )
  check(
    (await page.locator('.setup-wizard .observation.shortfall.attention').count()) === 0,
    'and the shortfall is a warning, never the reject red a mikroview-side fault uses',
  )
  // The colour itself, not just the class: the ruling is specifically
  // --warn and specifically not --reject, and a stylesheet is the one
  // place that can be wrong without any of the above noticing.
  const colours = await shortfallBox.evaluate((el) => {
    const probe = document.createElement('span')
    document.body.appendChild(probe)
    probe.style.color = 'var(--warn)'
    const warn = getComputedStyle(probe).color
    probe.style.color = 'var(--reject)'
    const reject = getComputedStyle(probe).color
    probe.remove()
    return { got: getComputedStyle(el).color, warn, reject }
  })
  check(
    colours.got === colours.warn && colours.got !== colours.reject,
    `the shortfall box is drawn in --warn (${JSON.stringify(colours)})`,
  )
} else {
  check(
    (await shortfallBox.count()) === 0,
    'every table has arrived on this instance, so there is no shortfall box to show',
  )
}

// --- The finish reads the ledger back ---------------------------------
// The finish row is the li *after* the ledger's six steps -- #394 made
// that nth-child(7), not nth-child(6) -- and the readback lists all six
// of them (SetupWizard.svelte's `{#each ledger as s}` under onFinish).
await page.locator('.setup-wizard .steps li:nth-child(7) .step-row').click()
const headline = ((await page.textContent('.setup-wizard .headline')) ?? '').trim()
check(headline.length > 0, `the finish reads the ledger back in a sentence (${headline})`)
check(
  (await page.locator('.setup-wizard .readback li').count()) === 6,
  'one row per step — receipt or honest gap',
)

// --- Esc closes, and reopening shows the ledger as it stands ----------
await page.keyboard.press('Escape')
await modal.waitFor({ state: 'detached' })
check(true, 'Esc closes the modal')

await goTo(page, 'Run setup…')
await modal.waitFor({ state: 'visible' })
const reopened =
  (await page.locator('.setup-wizard .steps li:nth-child(4) .step-row').getAttribute('class')) ?? ''
check(
  reopened.includes('done'),
  `reopening shows the ledger as it stands — evidence that arrived is already green (${reopened})`,
)

// --- With no key mounted, step 6 mints one (#1133) ----------------------
// The live instance always has a key (scripts/live-env.sh mounts one), so
// the no-key branch is reached by answering the wizard's own read of
// GET /api/router-backups with enabled:false -- the same page.route
// stand-in live-setup-wizard-tls-off-cert-mismatch.mjs uses for a state
// the harness cannot be put into. Nothing under test here is server-side:
// the key is minted in the browser and must stay there.
await page.route('**/api/router-backups', (route) =>
  route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ enabled: false, routers: [], totalGenerations: 0, totalRouters: 0, totalBytes: 0 }),
  }),
)

// Reopened rather than waited out: the modal refetches on open, so this
// does not hang on the 5-second poll landing at the right moment.
await page.keyboard.press('Escape')
await modal.waitFor({ state: 'detached' })
await goTo(page, 'Run setup…')
await modal.waitFor({ state: 'visible' })
await page.locator('.setup-wizard .steps li:nth-child(6) .step-row').click()

const keyField = page.locator('#history-key')
await keyField.waitFor({ state: 'visible' })
// 32 bytes, base64 -- docs/configuration.md's `head -c 32 /dev/urandom |
// base64`, the shape retention.LoadKey accepts.
const KEY_SHAPE = /^[A-Za-z0-9+/]{43}=$/
const mintedKey = await keyField.inputValue()
check(KEY_SHAPE.test(mintedKey), `the field arrives pre-filled with a generated key (${mintedKey.length} chars)`)

await page.click('.setup-wizard .keymint button:has-text("Reroll")')
const rerolledKey = await keyField.inputValue()
check(
  rerolledKey !== mintedKey && KEY_SHAPE.test(rerolledKey),
  'Reroll mints a different key, rather than redrawing the same one',
)

// The operator's own key, pasted over the top, is taken as it stands --
// this is a field, not a read-only display.
const pastedKey = 'PastedKeyPastedKeyPastedKeyPastedKeyPastedK='
await keyField.fill(pastedKey)
check((await keyField.inputValue()) === pastedKey, 'a key pasted into the field is kept as typed')

await page.click('.setup-wizard .keymint button:has-text("Reroll")')
const copiedKey = await keyField.inputValue()

// Clipboard permissions granted explicitly, so this proves what landed on
// the clipboard rather than only that a toast appeared (live-token-copy's
// own reasoning).
await page.context().grantPermissions(['clipboard-read', 'clipboard-write'], { origin: URL_BASE })
await page.click('.setup-wizard .keymint .copy-btn')
await page.waitForSelector('.toast[role="status"]', { timeout: 3000 })
const clipboardKey = await page.evaluate(() => navigator.clipboard.readText())
check(clipboardKey === copiedKey, 'the copy control puts the key itself on the clipboard')

const caveat = ((await page.textContent('.setup-wizard .wzcaveat')) ?? '').replace(/\s+/g, ' ')
check(
  /Save this now/.test(caveat) && /never receives this value/.test(caveat),
  `the warning says save it now, and why nothing can reprint it (${caveat})`,
)

const keyBlocks = await page.$$eval('.setup-wizard .body pre', (els) => els.map((e) => e.textContent ?? ''))
check(
  keyBlocks.some((b) => b.includes('cat > /run/secrets/mikroview-history.key')),
  'the steps say how to write the key to a file outside the data directory',
)
check(
  keyBlocks.some((b) => b.includes('keyFile: /run/secrets/mikroview-history.key')),
  'and how to point mikroview at it',
)
check(
  keyBlocks.every((b) => !b.includes(copiedKey)),
  'no printed command quotes the key -- it goes in on standard input, not as an argument',
)

const noKeyLead = ((await page.textContent('.setup-wizard .lead')) ?? '').replace(/\s+/g, ' ')
check(
  /under the key file you mount/.test(noKeyLead) && !/does not hold/.test(noKeyLead),
  `the step says the model once, and says it correctly (${noKeyLead})`,
)
check(
  (await page.locator('.setup-wizard .routeros-version').count()) === 0,
  'no RouterOS version picker on a pane with no RouterOS command on it',
)

// The point of the whole design: mikroview never receives this value.
// Raw and percent-encoded, since a leak through a query string would
// arrive escaped.
const mintedKeys = [mintedKey, rerolledKey, pastedKey, copiedKey]
const needles = mintedKeys.flatMap((k) => [k, encodeURIComponent(k)])
const leaked = requestsSeen.filter((r) => needles.some((n) => r.url.includes(n) || r.body.includes(n)))
check(
  leaked.length === 0,
  `no request carries the key, in a URL or a body (${requestsSeen.length} inspected, ${leaked.length} leaked` +
    `${leaked.length ? `: ${leaked.map((r) => r.url).join(', ')}` : ''})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
