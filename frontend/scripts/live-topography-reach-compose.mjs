// SPDX-License-Identifier: AGPL-3.0-only
//
// The reach's composer (#626/#633, round 2 scene 4): a blocked strand's
// label opens the port panel and the printed command -- drafted from
// what was observed, pasted by the operator, never run by mikroview.
// Runs after the other topography scenarios and feeds its own denial.

import { session, check, done, feedRaw } from './live-browser.mjs'

const { page, consoleErrors } = await session()

// A host with an accepted presence (so it stands on the zone card) and
// a blocked ask toward the internet on a known port.
for (let i = 0; i < 4; i++) {
  feedRaw(`firewall,info A|compose-web| forward: in:bridge1 out:ether1, connection-state:new, proto TCP (SYN), 192.168.1.77:51${40 + i}->203.0.113.9:443, len 60`)
  feedRaw(`firewall,info D|compose-deny| forward: in:bridge1 out:ether1, connection-state:new, proto TCP (SYN), 192.168.1.77:52${40 + i}->203.0.113.77:445, len 60`)
}
await new Promise((r) => setTimeout(r, 1200))
await page.reload()

await page.click('.rail-name >> text=Topography')
await page.waitForSelector('[data-card="topography"] .zone', { timeout: 10000 })

// Descend on the host, then open the composer through the blocked
// strand's own label.
await page.click('[data-card="topography"] .host-link >> text=192.168.1.77')
await page.waitForSelector('[data-card="topography"] .membrane-layer', { timeout: 5000 })
await page.click('[data-card="topography"] .strand-door >> nth=0')
await page.waitForSelector('.composer', { timeout: 5000 })

const panelText = await page.textContent('.composer .portpanel')
check(panelText.includes('What may 192.168.1.77 say to the internet?'), 'the panel asks the strand question in words')
check(panelText.includes('tcp/445') && panelText.includes('asking'), `the asked-for port leads the chips (${panelText.slice(0, 120)})`)

let cmd = await page.textContent('.composer .cmd')
check(cmd.includes('src-address=192.168.1.77') && cmd.includes('dst-address=203.0.113.77'), 'the drafted allow runs host → far side')
check(cmd.includes('dst-port=445') && cmd.includes('action=accept') && cmd.includes('log=yes'), 'allow: right port, logged and named')

await page.click('.composer .ctab >> text=Name the block instead')
cmd = await page.textContent('.composer .cmd')
check(cmd.includes('action=drop') && cmd.includes('named block'), 'the named block drafts the explicit logged drop')

const noteText = await page.textContent('.composer .cmdnote')
check(noteText.includes('mikroview never touches the router'), 'the invariant is said where the command is')

// Esc walks out one level at a time: composer, then the reach.
await page.keyboard.press('Escape')
check(!(await page.isVisible('.composer')), 'Escape closes the composer first')
check(await page.isVisible('[data-card="topography"] .membrane-layer svg'), 'the reach stays beneath it')

check(consoleErrors.length === 0, `no console errors (${consoleErrors.join(' | ')})`)
done()
