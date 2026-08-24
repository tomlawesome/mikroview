// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import GhostRows from './GhostRows.svelte'

// #549's Loading chrome state: "shell plus ghost rows -- never a spinner
// page." What's worth pinning down here is the accessibility shape (the
// bars themselves say nothing on their own, so they're hidden, and the
// one thing that should be announced is the sr-only status) and that the
// row count is a real prop, not a hardcoded stand-in. The shimmer
// animation and the visual layout are style-only and covered by no test
// anywhere else in this codebase either, for the same reason.
describe('GhostRows (#549)', () => {
  it('renders the requested number of placeholder rows, all decorative', () => {
    const { container } = render(GhostRows, { props: { rows: 4 } })
    expect(container.querySelectorAll('.ghost-row').length).toBe(4)
    expect(container.querySelector('.ghost-rows')?.getAttribute('aria-hidden')).toBe('true')
  })

  it('defaults to a reasonable row count when none is given', () => {
    const { container } = render(GhostRows, {})
    expect(container.querySelectorAll('.ghost-row').length).toBeGreaterThan(0)
  })

  it('carries the accessible label as a status region, not on the hidden bars', () => {
    const { container } = render(GhostRows, { props: { label: 'Loading events…' } })
    const status = container.querySelector('[role="status"]')
    expect(status?.textContent).toBe('Loading events…')
  })
})
