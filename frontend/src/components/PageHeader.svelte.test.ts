// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import PageHeader from './PageHeader.svelte'

// #548/#490: read-only is declared once, in words, in the page-header
// chip -- never by disabling controls elsewhere on the page. This is
// the one place that wording lives, so it is pinned here rather than
// left to be reworded differently by each page that uses it.
describe('PageHeader', () => {
  it('renders the title with no chip by default', () => {
    render(PageHeader, { title: 'Fleet' })
    expect(screen.getByRole('heading', { name: 'Fleet' })).toBeTruthy()
    expect(screen.queryByText(/READ-ONLY/)).toBeNull()
  })

  it('declares read-only once, in the ratified wording, when asked to', () => {
    render(PageHeader, { title: 'Users', readOnly: true })
    expect(screen.getByText('READ-ONLY')).toBeTruthy()
  })
})
