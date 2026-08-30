// SPDX-License-Identifier: AGPL-3.0-only
//
// #645/#659: the door's geometry, checked in the engines the harness
// doesn't drive. The whole live-check suite runs Chromium, and Chromium
// tolerated the static style attributes the app's CSP forbids -- so the
// door shipped raining in the harness and collapsed to one block in the
// owner's Firefox. This scenario opens the door in Firefox (required)
// and WebKit (when the host can launch it -- see the log line) and
// asserts the geometry that broke: strokes spread across the frame,
// desynchronised, behind a masked layer, inside the amber brink.
//
// Pre-auth only, no state touched: safe at any point in the suite's
// filename order, and no cleanup owed to later scenarios.

import { firefox, webkit } from 'playwright'
import { check, done } from './live-browser.mjs'

const URL = process.env.MV_URL

async function doorGeometry(page) {
  await page.waitForSelector('.screen .fullfall', { timeout: 15000 })
  return page.evaluate(() => {
    const strokes = [...document.querySelectorAll('.fullfall i')]
    const lefts = new Set(), transforms = new Set()
    for (const el of strokes) {
      const s = getComputedStyle(el)
      lefts.add(s.left)
      transforms.add(s.transform)
    }
    const layer = getComputedStyle(document.querySelector('.fullfall'))
    const brink = getComputedStyle(document.querySelector('.wm-box'))
    return {
      strokes: strokes.length,
      distinctLefts: lefts.size,
      distinctTransforms: transforms.size,
      transforms: [...transforms],
      masked: (layer.maskImage || layer.webkitMaskImage || '').includes('gradient'),
      brinkWidth: parseFloat(brink.borderTopWidth),
    }
  })
}

for (const [name, engine, required] of [
  ['firefox', firefox, true],
  ['webkit', webkit, false],
]) {
  let browser
  try {
    browser = await engine.launch()
  } catch (e) {
    if (required) {
      check(false, `${name} launches -- ${e.message.split('\n')[0]}`)
      continue
    }
    // Optional engine, and an absent one is a host limitation rather
    // than a product defect (owner, 2026-08-30: "No worries if not") --
    // but say so loudly instead of skipping silently.
    console.log(`  -- ${name} cannot launch on this host (missing system deps); skipped`)
    continue
  }
  const page = await browser.newPage({ ignoreHTTPSErrors: true, viewport: { width: 1440, height: 900 } })
  await page.goto(URL, { waitUntil: 'networkidle' })
  // The scene's longest animation-delay is 5.3s; before it elapses the
  // unstarted strokes legitimately share one resting transform, so the
  // desync read below would report sync where there is only patience.
  await page.waitForTimeout(6000)
  const g = await doorGeometry(page)
  await page.waitForTimeout(700)
  const g2 = await doorGeometry(page)
  check(g.strokes >= 10, `[${name}] the fall renders its strokes -- ${g.strokes}`)
  check(g.distinctLefts >= 10, `[${name}] the strokes spread across the frame -- ${g.distinctLefts} distinct positions`)
  check(g.distinctTransforms >= 10, `[${name}] the strokes fall out of step with each other -- ${g.distinctTransforms} distinct offsets`)
  const moved = g2.transforms.filter((t) => !g.transforms.includes(t)).length
  check(
    g2.distinctTransforms >= 10 && moved >= 10,
    `[${name}] the rain is falling, not frozen -- ${moved} strokes moved over 700ms`,
  )
  check(g.masked, `[${name}] the rain layer carries its centre mask`)
  check(g.brinkWidth >= 1 && g.brinkWidth <= 2, `[${name}] the amber brink draws thin -- ${g.brinkWidth}px`)
  await browser.close()
}

done()
