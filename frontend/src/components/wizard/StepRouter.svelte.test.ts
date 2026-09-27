// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'

// jsdom has no matchMedia, which lib/viewport.svelte.ts reads at module
// load somewhere down the import chain.
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
})

import { wizardState } from '../../lib/wizard.svelte'
import { wizardRun } from '../../lib/wizardRun.svelte'
import { rowState } from '../../lib/wizardRun'
import StepRouter from './StepRouter.svelte'

// The router step is one form and nothing else: no call leaves it. So
// nothing is faked here; the step is exercised the way an operator
// types into it, and what it leaves in the run is read back.
describe('StepRouter: the router, one form', () => {
  beforeEach(() => {
    wizardState.reset()
    wizardRun.reset()
    wizardState.open = true
  })

  afterEach(() => {
    wizardState.reset()
    wizardRun.reset()
  })

  function address(): HTMLInputElement {
    return screen.getByLabelText(/^Its address/) as HTMLInputElement
  }

  function problem(): string {
    return document.querySelector('.form .problem')?.textContent ?? ''
  }

  it('gives the name field focus when the step arrives', async () => {
    render(StepRouter)
    await tick()
    expect(document.activeElement).toBe(screen.getByLabelText(/^Name/))
    expect(screen.getByText('Your first router.')).toBeTruthy()
    expect(screen.getByText(/MikroView never connects to it — the router sends\./)).toBeTruthy()
  })

  it('says what is wrong with the address as you type, and holds the four until it is right (#1380)', async () => {
    render(StepRouter)
    await tick()
    await fireEvent.input(screen.getByLabelText(/^Name/), { target: { value: 'rb5009' } })
    await fireEvent.click(screen.getAllByRole('radio', { name: 'Yes' })[0])
    await fireEvent.click(screen.getAllByRole('radio', { name: 'No' })[1])

    const cases: [string, RegExp][] = [
      ['192.168.1', /^Four numbers, 0–255, separated by dots\.$/],
      ['192.168.1.1:514', /^No port here — just the address\. The port is MikroView’s side\.$/],
      ['rb5009.lan', /^A name will not do: the enrolment window binds to an address\.$/],
      ['10.0.0.0/24', /^No prefix length — the router’s own address, not its network\.$/],
      ['192.168.001.1', /^Four numbers, 0–255, separated by dots\.$/],
    ]
    for (const [typed, line] of cases) {
      await fireEvent.input(address(), { target: { value: typed } })
      await tick()
      expect(problem(), typed).toMatch(line)
      expect(address().getAttribute('aria-invalid'), typed).toBe('true')
      expect(address().classList.contains('bad'), typed).toBe(true)
      expect(wizardRun.routerDone, typed).toBe(false)
    }
    // The line is aria-live, so the wording reaches a screen reader as
    // it changes.
    expect(document.querySelector('.form .problem')?.getAttribute('aria-live')).toBe('polite')

    await fireEvent.input(address(), { target: { value: '192.168.13.1' } })
    await tick()
    expect(problem()).toBe('')
    expect(address().getAttribute('aria-invalid')).toBe('false')
    expect(address().classList.contains('bad')).toBe(false)
    expect(wizardRun.routerDone).toBe(true)
  })

  it('accepts an IPv6 literal, as the server does', async () => {
    render(StepRouter)
    await tick()
    await fireEvent.input(address(), { target: { value: 'fd00:13::1' } })
    await tick()
    expect(problem()).toBe('')
    expect(address().getAttribute('aria-invalid')).toBe('false')
  })

  it('takes push and backup as a plain Yes / No each, one answer at a time', async () => {
    render(StepRouter)
    await tick()
    const groups = screen.getAllByRole('radiogroup')
    expect(groups).toHaveLength(2)
    expect(groups[0].getAttribute('aria-labelledby')).toBe('l-push')
    expect(groups[1].getAttribute('aria-labelledby')).toBe('l-backup')
    const [pushYes, backupYes] = screen.getAllByRole('radio', { name: 'Yes' })
    const [pushNo, backupNo] = screen.getAllByRole('radio', { name: 'No' })
    // Unanswered: neither side is on, and the button carries no
    // interval -- the label's own line already says what each does
    // (owner, round 15).
    expect(pushYes.getAttribute('aria-checked')).toBe('false')
    expect(pushNo.getAttribute('aria-checked')).toBe('false')
    expect(pushYes.textContent).toBe('Yes')
    expect(pushNo.textContent).toBe('No')
    expect(wizardRun.push).toBeNull()

    await fireEvent.click(pushYes)
    await tick()
    expect(wizardRun.push).toBe(true)
    expect(pushYes.classList.contains('on')).toBe(true)
    expect(pushYes.getAttribute('aria-checked')).toBe('true')
    expect(pushNo.classList.contains('on')).toBe(false)

    await fireEvent.click(pushNo)
    await tick()
    expect(wizardRun.push).toBe(false)
    expect(pushNo.classList.contains('on')).toBe(true)
    expect(pushNo.classList.contains('no')).toBe(true)
    expect(pushYes.getAttribute('aria-checked')).toBe('false')

    await fireEvent.click(backupYes)
    await tick()
    expect(wizardRun.backup).toBe(true)
    expect(backupNo.getAttribute('aria-checked')).toBe('false')
    expect(wizardRun.push).toBe(false)
  })

  it('feeds the receipt the rail shows once all four are answered', async () => {
    render(StepRouter)
    await tick()
    await fireEvent.input(screen.getByLabelText(/^Name/), { target: { value: 'rb5009' } })
    await fireEvent.input(address(), { target: { value: '192.168.13.1' } })
    await fireEvent.click(screen.getAllByRole('radio', { name: 'Yes' })[0])
    await fireEvent.click(screen.getAllByRole('radio', { name: 'No' })[1])
    await tick()
    const row = rowState(wizardRun.answers, wizardRun.evidence, 'router')
    expect(row.cls).toBe('chosen')
    expect(row.ink).toBe('token')
    expect(row.receipt).toBe('rb5009 · 192.168.13.1 · push yes · backup not now')
  })
})
