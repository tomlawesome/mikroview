// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import EventDetailSheet from './EventDetailSheet.svelte'
import { expectFocusInside, expectTabWraps, focusedTrigger } from './dialogKit'
import type { FirewallEvent } from '../lib/types'

function makeEvent(overrides: Partial<FirewallEvent> = {}): FirewallEvent {
  return {
    id: 1,
    time: '2026-09-12T10:56:00Z',
    deviceId: 'router1',
    sourceIp: '203.0.113.10',
    action: 'natted',
    ruleLabel: 'nat-out',
    chain: 'srcnat',
    raw: 'srcnat: in:bridge-lan out:ether1',
    ...overrides,
  }
}

// #1151: every row in the sheet is a flex box with
// `justify-content: space-between` -- a key on the left, the value on
// the right. That holds only while the row has exactly two children, and
// NAT had three (value, lookup button, key), which parked its value in
// the middle of the row while every other value sat on the right edge.
describe('EventDetailSheet NAT row (#1151)', () => {
  it('keeps the NAT value and its lookup trigger together in one .v-group', () => {
    const { container } = render(EventDetailSheet, {
      props: {
        event: makeEvent({ natIp: '203.0.113.7', natPort: 51512 }),
        deviceName: 'router1',
        onClose: () => {},
      },
    })

    const natRow = [...container.querySelectorAll('.row')].find(
      (row) => row.querySelector('.k')?.textContent?.trim() === 'NAT',
    )
    expect(natRow).toBeTruthy()
    expect(natRow?.children.length).toBe(2)

    const group = natRow?.querySelector('.v-group')
    expect(group?.querySelector('.v.accent')?.textContent).toContain('203.0.113.7:51512')
    expect(group?.querySelector('.natlookup')).toBeTruthy()
  })

  it('lays every other row out the same way -- two children, value last', () => {
    const { container } = render(EventDetailSheet, {
      props: {
        event: makeEvent({ srcIp: '10.0.10.2', dstIp: '203.0.113.5', dstPort: 443, natIp: '203.0.113.7' }),
        deviceName: 'router1',
        onClose: () => {},
      },
    })

    for (const row of container.querySelectorAll('.row')) {
      expect(row.children.length, `${row.querySelector('.k')?.textContent} row`).toBe(2)
    }
  })
})

// #1539 C3 (decision 19a): an aria-modal dialog owns focus -- it moves in
// on open, Tab wraps inside it, and closing hands focus back to what
// opened it.
describe('EventDetailSheet owns focus (#1539 C3)', () => {
  function mount() {
    const trigger = focusedTrigger()
    const view = render(EventDetailSheet, { props: { event: makeEvent(), deviceName: 'router1', onClose: () => {} } })
    return { trigger, view, sheet: view.getByRole('dialog', { name: 'Event detail' }) }
  }

  it('moves focus inside on open and wraps Tab and Shift+Tab at its ends', () => {
    const { trigger, sheet } = mount()
    expectFocusInside(sheet)
    expectTabWraps(sheet)
    trigger.remove()
  })

  it('hands focus back to the trigger when it closes', () => {
    const { trigger, view, sheet } = mount()
    sheet.querySelector<HTMLElement>('button.close')!.focus()
    expect(document.activeElement).not.toBe(trigger)
    view.unmount()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })
})
