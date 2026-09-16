// SPDX-License-Identifier: AGPL-3.0-only
//
// #1126: a router backup an admin wants to hold past the vault's own
// ten-generation cycle -- "before the 7.16 upgrade", say -- can be
// protected. POST .../protect moves it out of the cycling set into the
// kept pool with a comment; DELETE moves it back; PATCH rewrites the
// comment. routerbackupskeep_test.go pins the HTTP contract
// (internal/api/routerbackupskeep_test.go); this delivers a real backup
// pair over the same route a real router's push script uses
// (internal/api/ingestbackup.go: begin, then slices, bearer ingest
// token), protects the generation it lands in, and reads the result
// back from both GET /api/router-backups and the real Settings screen
// (RouterBackups.svelte) -- the ✱ line, its comment, and "release…" --
// then releases it and confirms both surfaces agree it moved back.
//
// Needs the retention key: internal/backups.go's openRouterBackupVault
// loads it from cfg.History.KeyFile, the same file history.keyFile
// names (#394's vault and #853's state store share one key). live-env.sh
// always writes one and turns history on (`history: {enabled: true,
// keyFile: ...}`), so the vault is enabled on every live-check instance
// without asking for anything extra -- confirmed by GET /api/router-backups
// below, whose own `enabled` field is the honest answer either way.

import { session, check, done, goTo } from './live-browser.mjs'
import crypto from 'node:crypto'

const URL_BASE = process.env.MV_URL
const DEVICE = 'mv1126-backups'
const COMMENT = 'before the 7.16 upgrade'
const EDITED_COMMENT = 'before the 7.16 upgrade (confirmed clean)'

const { page, consoleErrors } = await session()

async function api(method, path_, body) {
  const res = await page.request.fetch(`${URL_BASE}${path_}`, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'mikroview' },
    data: body,
  })
  return { status: res.status(), body: res.status() < 400 ? await res.json() : await res.text() }
}

async function waitUntil(fn, timeoutMs = 15000, intervalMs = 500) {
  const deadline = Date.now() + timeoutMs
  let last
  while (Date.now() < deadline) {
    last = await fn().catch(() => undefined)
    if (last) return last
    await new Promise((r) => setTimeout(r, intervalMs))
  }
  return last
}

// --- confirm the vault is actually enabled here before relying on it ----

const preflight = await api('GET', '/api/router-backups')
check(preflight.status === 200, `GET /api/router-backups answers 200 (${preflight.status})`)
if (!preflight.body?.enabled) {
  check(
    false,
    'the router-backup vault reports disabled on this instance (no usable retention key) -- cannot exercise kept backups here',
  )
  done()
}

// --- issue an ingest token and deliver one backup pair, the way a -------
// --- router's own push script does (internal/api/ingestbackup.go) -------

const tokenRes = await api('POST', '/api/tokens', { name: 'mv1126-kept-backups', kind: 'ingest', device: DEVICE })
check(tokenRes.status === 201, `an ingest token is issued for ${DEVICE} (${tokenRes.status})`)
const token = tokenRes.body?.value

async function pushSlice(kind, bytes) {
  const sha256 = crypto.createHash('sha256').update(bytes).digest('hex')
  const beginRes = await fetch(`${URL_BASE}/api/ingest/router-backup`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ op: 'begin', kind, totalBytes: bytes.length, totalSlices: 1, sha256 }),
  })
  if (beginRes.status !== 200) return { ok: false, status: beginRes.status, step: 'begin' }
  const started = await beginRes.json()
  if (!started.transferId) return { ok: false, status: beginRes.status, step: 'begin (no transferId)' }

  const sliceRes = await fetch(`${URL_BASE}/api/ingest/router-backup`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ op: 'slice', transferId: started.transferId, index: 0, data: bytes.toString('base64') }),
  })
  if (sliceRes.status !== 200) return { ok: false, status: sliceRes.status, step: 'slice' }
  const finished = await sliceRes.json()
  return { ok: finished.done === true, status: sliceRes.status, step: 'slice' }
}

if (token) {
  // The RouterOS "plain" backup header, same fixture routerbackups_test.go
  // uses, so the vault's own header detection reads it the way a real
  // .backup does.
  const backupFile = Buffer.concat([Buffer.from([0x88, 0xac, 0xa1, 0xb1]), Buffer.from('mv1126 configuration payload')])
  const rscFile = Buffer.from('# jan/01/2026 03:00:00 by RouterOS\n/interface print\n')

  const backupPush = await pushSlice('backup', backupFile)
  check(backupPush.ok, `the .backup slice completes its transfer (${backupPush.step} -> ${backupPush.status})`)
  const rscPush = await pushSlice('rsc', rscFile)
  check(rscPush.ok, `the .rsc slice completes its transfer (${rscPush.step} -> ${rscPush.status})`)
} else {
  check(false, 'skipped the backup push -- no ingest token was issued')
}

// --- the pair lands as one generation, in the cycling set ---------------

async function routerRow() {
  const { body } = await api('GET', '/api/router-backups')
  return body?.routers?.find((r) => r.device === DEVICE)
}

const seeded = await waitUntil(async () => {
  const row = await routerRow()
  return row?.generations?.length > 0 ? row : undefined
})
check(!!seeded, `${DEVICE} shows a generation in GET /api/router-backups`)
check(seeded?.generations?.length === 1, `the backup+rsc pair landed as one generation (got ${seeded?.generations?.length})`)
check((seeded?.protected ?? []).length === 0, `nothing is kept yet (got ${JSON.stringify(seeded?.protected)})`)

const genId = seeded?.generations?.[0]?.id

if (genId) {
  // --- protect it, as an admin, over the same route routerbackupskeep_test.go pins -
  const protectRes = await api('POST', `/api/router-backups/${DEVICE}/${genId}/protect`, { comment: COMMENT })
  check(protectRes.status === 200, `POST .../protect is accepted (${protectRes.status})`)
  check(
    (protectRes.body?.generations ?? []).length === 0,
    `the cycling set is empty once the only generation is kept (got ${JSON.stringify(protectRes.body?.generations)})`,
  )
  check(
    protectRes.body?.protected?.length === 1 && protectRes.body.protected[0].id === genId && protectRes.body.protected[0].comment === COMMENT,
    `the response's own protected list carries the kept generation and comment (got ${JSON.stringify(protectRes.body?.protected)})`,
  )

  // --- GET /api/router-backups agrees -------------------------------------
  const afterProtect = await routerRow()
  check(
    (afterProtect?.generations ?? []).length === 0 && afterProtect?.protected?.length === 1,
    `GET /api/router-backups lists it under protected, not generations (got generations=${JSON.stringify(afterProtect?.generations)} protected=${JSON.stringify(afterProtect?.protected)})`,
  )
  check(
    afterProtect?.protected?.[0]?.comment === COMMENT,
    `the listed kept generation carries the comment (got "${afterProtect?.protected?.[0]?.comment}")`,
  )

  // --- the real Settings screen shows the kept line -----------------------
  let keptCard = null
  try {
    await goTo(page, 'Settings')
    await page.waitForSelector('#bakg', { timeout: 15000 })
    const router = page.locator('.brtr', { hasText: DEVICE })
    await router.waitFor({ timeout: 15000 })
    await router.locator('.brkept').waitFor({ timeout: 15000 })
    keptCard = router
  } catch (e) {
    check(false, `the Settings router-backups group never showed ${DEVICE}'s kept line: ${e}`)
  }

  if (keptCard) {
    const groupText = (await keptCard.textContent()) ?? ''
    check(groupText.includes('kept'), `the "kept" group label is drawn for ${DEVICE} (got: ${groupText.replace(/\s+/g, ' ').trim()})`)
    check(groupText.includes('✱'), `the kept generation's line carries its ✱ mark`)
    check(groupText.includes(COMMENT), `the kept generation's line carries its comment "${COMMENT}"`)
    check(groupText.includes('release…'), `an admin is offered "release…" on the kept line`)
    check(!groupText.includes('keep…'), `no "keep…" offer remains -- the only generation is already kept`)
  }

  // --- edit the comment (PATCH), the same route, and see it change --------
  const patchRes = await api('PATCH', `/api/router-backups/${DEVICE}/${genId}/protect`, { comment: EDITED_COMMENT })
  check(patchRes.status === 200, `PATCH .../protect (editing the comment) is accepted (${patchRes.status})`)
  check(
    patchRes.body?.protected?.[0]?.comment === EDITED_COMMENT,
    `the response carries the rewritten comment (got "${patchRes.body?.protected?.[0]?.comment}")`,
  )

  // --- release it (DELETE), back into the cycling set ----------------------
  const releaseRes = await api('DELETE', `/api/router-backups/${DEVICE}/${genId}/protect`)
  check(releaseRes.status === 200, `DELETE .../protect (releasing) is accepted (${releaseRes.status})`)
  check(
    (releaseRes.body?.protected ?? []).length === 0,
    `the response's protected list is empty after release (got ${JSON.stringify(releaseRes.body?.protected)})`,
  )
  check(
    releaseRes.body?.generations?.length === 1 && releaseRes.body.generations[0].id === genId,
    `the generation is back in the response's cycling set (got ${JSON.stringify(releaseRes.body?.generations)})`,
  )
  check(
    !releaseRes.body?.generations?.[0]?.comment,
    `the released generation carries no comment any more (got "${releaseRes.body?.generations?.[0]?.comment}")`,
  )

  const afterRelease = await routerRow()
  check(
    (afterRelease?.protected ?? []).length === 0 && afterRelease?.generations?.length === 1,
    `GET /api/router-backups agrees it moved back (got generations=${JSON.stringify(afterRelease?.generations)} protected=${JSON.stringify(afterRelease?.protected)})`,
  )

  // --- and the Settings screen, reloaded, no longer shows a kept line -----
  try {
    await page.reload({ waitUntil: 'networkidle' })
    await page.waitForSelector('#main-content', { timeout: 15000 })
    await goTo(page, 'Settings')
    await page.waitForSelector('#bakg', { timeout: 15000 })
    const router = page.locator('.brtr', { hasText: DEVICE })
    await router.waitFor({ timeout: 15000 })
    const groupText = (await router.textContent()) ?? ''
    check(!groupText.includes('✱'), `the ✱ mark is gone once the generation is released (got: ${groupText.replace(/\s+/g, ' ').trim()})`)
    check(groupText.includes('keep…'), `the released generation offers "keep…" again`)
  } catch (e) {
    check(false, `could not re-read the Settings router-backups group after release: ${e}`)
  }
} else {
  check(false, 'skipped the protect/release checks -- no generation ever arrived')
}

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join('; ')})`)
done()
