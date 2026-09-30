// SPDX-License-Identifier: AGPL-3.0-only
//
// #1363: the line every role sees once a reload was not safe to do by
// itself. Pins the copy, that it shows for every role (unlike
// UpgradeNotice's admin-only gate), that there is no ✕, and that its
// button drives freshnessState.reloadNow() rather than reloading
// directly -- see that module's own tests for 7a and the safe-reload
// mechanics themselves.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from '@testing-library/svelte'

import { authState } from '../lib/auth.svelte'
import { freshnessState } from '../lib/freshness.svelte'
import FreshnessBanner from './FreshnessBanner.svelte'

beforeEach(() => {
  cleanup()
  ;(freshnessState as unknown as { banner: boolean }).banner = false
})

describe('the freshness banner', () => {
  it('says nothing until a reload was unsafe', () => {
    const { container } = render(FreshnessBanner)
    expect(container.querySelector('.banner')).toBeNull()
  })

  it('names no version -- 8a', () => {
    ;(freshnessState as unknown as { banner: boolean }).banner = true
    const { container } = render(FreshnessBanner)

    expect(container.querySelector('.line')?.textContent).toBe('mikroview has been upgraded to a newer version')
  })

  it('has no ✕', () => {
    ;(freshnessState as unknown as { banner: boolean }).banner = true
    const { container, getByText } = render(FreshnessBanner)

    expect(getByText('reload')).toBeTruthy()
    expect(container.textContent).not.toContain('✕')
  })

  it('shows for every role, not just admin', () => {
    for (const role of ['admin', 'user', 'viewer', ''] as const) {
      authState.role = role
      ;(freshnessState as unknown as { banner: boolean }).banner = true
      const { container, unmount } = render(FreshnessBanner)

      expect(container.querySelector('.banner')).toBeTruthy()
      unmount()
    }
  })

  it('reload calls freshnessState.reloadNow(), not location.reload() directly', () => {
    ;(freshnessState as unknown as { banner: boolean }).banner = true
    const reloadNow = vi.spyOn(freshnessState, 'reloadNow').mockResolvedValue()
    const { getByText } = render(FreshnessBanner)

    getByText('reload').click()

    expect(reloadNow).toHaveBeenCalledOnce()
  })
})
