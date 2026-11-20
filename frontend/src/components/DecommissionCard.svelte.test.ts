// SPDX-License-Identifier: AGPL-3.0-only
//
// #1539: the decommission card is a dialog (`role="dialog"`), so it
// follows the house rules for one: Escape closes it (C2, decision 18a)
// and closing returns focus to what had it when the card opened (C3,
// decision 19a). Both were absent.
import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import DecommissionCard from './DecommissionCard.svelte'
import { focusedTrigger } from './dialogKit'
import type { DecommissionWatch } from '../lib/types'

const WATCH: DecommissionWatch = {
  id: 'w1',
  cidr: '203.0.113.0/24',
  device: 'core',
  interface: 'ether3',
  name: 'lab',
  createdAt: '2026-08-24T09:00:00Z',
  cleanWindow: 6 * 3_600_000_000_000,
  trafficCount: 0,
  covered: true,
  detached: false,
  replayCount: 0,
  replaySpan: 0,
  state: 'holding',
  retiresIn: '6h0m0s',
  coverage: 'covered',
}

function mount(onclose = vi.fn()) {
  return { onclose, view: render(DecommissionCard, { props: { kind: 'ghost', watch: WATCH, nowMs: Date.parse('2026-08-24T10:00:00Z'), onclose } }) }
}

describe('DecommissionCard (#1539)', () => {
  it('Escape closes it', async () => {
    const { onclose } = mount()
    await fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' })
    expect(onclose).toHaveBeenCalledTimes(1)
  })

  it('hands focus back to what had it before the card opened', () => {
    const trigger = focusedTrigger()
    const { view } = mount()
    screen.getByTitle('close this card').focus()
    view.unmount()
    expect(document.activeElement).toBe(trigger)
    trigger.remove()
  })
})
