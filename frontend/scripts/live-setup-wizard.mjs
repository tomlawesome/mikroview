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

import { session, check, done } from './live-browser.mjs'

const URL_BASE = process.env.MV_URL

// dismissSetup: false, because this scenario drives the modal itself.
// On a shared instance earlier scenarios have already fed events, so
// auto-launch will not have fired -- the door under test here is the
// relaunch one, which is the same door.
const { page, consoleErrors } = await session({ waitForEvents: 20, dismissSetup: false })

const modal = page.locator('.setup-wizard')
if (await modal.count()) {
  await page.keyboard.press('Escape')
  await modal.waitFor({ state: 'detached' })
}

// --- Run setup… opens the modal, over whatever page is showing --------
// It is an action, not a page: the shell behind it stays mounted, which
// is the whole difference from the view this replaced.
await page.click('.rail .item:has-text("Run setup…")')
await modal.waitFor({ state: 'visible' })
check(
  await page.locator('.rail').isVisible(),
  'the shell is still there behind the modal — this is a modal, not a page',
)
check(!(await page.locator('main .setup').count()), 'no wizard page route remains — the view was removed wholesale')

// --- Explicit close only ----------------------------------------------
// Progress is never lost to a stray click. Clicking the veil (well
// outside the modal box) must do nothing at all.
const veil = page.locator('.veil')
const box = await modal.boundingBox()
await page.mouse.click(Math.max(4, Math.floor(box.x / 2)), Math.max(4, Math.floor(box.y / 2)))
await page.waitForTimeout(300)
check(await modal.isVisible(), 'clicking outside does not dismiss the modal')
check((await veil.count()) === 1, 'the veil is present but inert')

// --- The step list is the ledger --------------------------------------
const stepTitles = await page.$$eval('.setup-wizard .steps .step-title', (els) =>
  els.map((e) => e.textContent?.trim() ?? ''),
)
check(
  stepTitles.length === 6,
  `five steps and the read-back, always the same count (${JSON.stringify(stepTitles)})`,
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

const withPlaceholders = seen.filter((b) => /<[a-z-]+>/.test(b) && !b.includes('<paste the script above>'))
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
await page.locator('.setup-wizard .steps li:nth-child(3) .step-row').click()
const ruleObservation = (await page.locator('.setup-wizard .observation').getAttribute('class')) ?? ''
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
await page.locator('.setup-wizard .steps li:nth-child(4) .step-row').click()
const pushObservation = (await page.locator('.setup-wizard .observation').getAttribute('class')) ?? ''
check(
  pushObservation.includes('arrived') === alreadyPushed,
  `the push step ${alreadyPushed ? 'reports what arrived' : 'does not claim success before anything pushed'} ` +
    `(${pushObservation}, server says pushed=${alreadyPushed})`,
)

// --- Forcing past is loud, and the record is the feature ---------------
// Step 1 is the CA fetch. No router fetches /ca.crt on this harness, so
// it is genuinely waiting -- the exact state the heavy warning is for.
await page.locator('.setup-wizard .steps li:nth-child(1) .step-row').click()
const stepOneObservation = (await page.locator('.setup-wizard .observation').getAttribute('class')) ?? ''
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

// Stop at the closing quote: \S+ swallows it, and a token with a
// trailing " authenticates as nothing (401) -- which looked like a
// product bug on first run and was this line.
const tokenMatch = script.match(/Bearer ([^"\s)]+)/)
check(!!tokenMatch, 'the generated script embeds a bearer token')
const token = tokenMatch?.[1] ?? ''

// Every kind the server declares must appear in the script.
for (const kind of status.pushKinds) {
  check(script.includes(`"kind"="${kind}"`), `the script pushes ${kind}`)
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

// --- The finish reads the ledger back ---------------------------------
await page.locator('.setup-wizard .steps li:nth-child(6) .step-row').click()
const headline = ((await page.textContent('.setup-wizard .headline')) ?? '').trim()
check(headline.length > 0, `the finish reads the ledger back in a sentence (${headline})`)
check(
  (await page.locator('.setup-wizard .readback li').count()) === 5,
  'one row per step — receipt or honest gap',
)

// --- Esc closes, and reopening shows the ledger as it stands ----------
await page.keyboard.press('Escape')
await modal.waitFor({ state: 'detached' })
check(true, 'Esc closes the modal')

await page.click('.rail .item:has-text("Run setup…")')
await modal.waitFor({ state: 'visible' })
const reopened =
  (await page.locator('.setup-wizard .steps li:nth-child(4) .step-row').getAttribute('class')) ?? ''
check(
  reopened.includes('done'),
  `reopening shows the ledger as it stands — evidence that arrived is already green (${reopened})`,
)

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
