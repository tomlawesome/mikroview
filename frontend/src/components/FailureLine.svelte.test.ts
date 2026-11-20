// SPDX-License-Identifier: AGPL-3.0-only
//
// #1540 (C4): one failure-line component. role="alert", text
// "Could not <verb> <thing>: <server words>". The component is
// FailureLine.svelte with props { verb, thing, said }; every screen's
// failure render goes through it (site tests in failureLoading.svelte.test.ts).
import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import FailureLine from './FailureLine.svelte'

describe('FailureLine (#1540 C4)', () => {
  it('is a role="alert" line reading "Could not <verb> <thing>: <server words>"', () => {
    render(FailureLine, { verb: 'load', thing: 'the audit log', said: 'network unreachable' })
    const line = screen.getByRole('alert')
    expect(line.textContent?.trim()).toBe('Could not load the audit log: network unreachable')
  })

  it('carries the server words verbatim, whatever they are', () => {
    render(FailureLine, { verb: 'save', thing: 'this rule', said: 'name already in use (409)' })
    expect(screen.getByRole('alert').textContent).toContain(': name already in use (409)')
  })

  it('drops the colon rather than dangling it when the server said nothing', () => {
    render(FailureLine, { verb: 'delete', thing: 'this host', said: '' })
    expect(screen.getByRole('alert').textContent?.trim()).toBe('Could not delete this host')
  })
})
