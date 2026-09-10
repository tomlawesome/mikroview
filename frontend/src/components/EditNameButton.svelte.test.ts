// SPDX-License-Identifier: AGPL-3.0-only
//
// #1070's follow-up: EventRow hoists nameEditorState.available into one
// editAvailable = $derived(...) per row and passes it down as the
// `available` prop, so its four pencils no longer each independently
// re-subscribe to the same getter. EditNameButton must actually honour
// that prop -- not just accept and ignore it -- and standalone callers
// (EventDetailSheet) that omit the prop must keep deciding from the
// store, unchanged.

import { beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import EditNameButton from './EditNameButton.svelte'
import { authState } from '../lib/auth.svelte'

beforeEach(() => {
  // nameEditorState.available is authState.state === 'authenticated' &&
  // authState.canEdit -- true here so every test below starts from the
  // store saying "available", to isolate what the prop does.
  authState.state = 'authenticated'
  authState.role = 'admin'
})

function pencil(props: { available?: boolean } = {}) {
  return render(EditNameButton, {
    type: 'host',
    value: '10.0.0.5',
    label: '10.0.0.5',
    ...props,
  })
}

describe('EditNameButton available prop (#1070)', () => {
  it('an explicit available={false} hides the button even though the store says available', () => {
    pencil({ available: false })
    expect(screen.queryByRole('button', { name: /edit name/i })).toBeNull()
  })

  it('an explicit available={true} shows the button without consulting the store', () => {
    authState.state = 'unauthenticated'
    authState.role = ''
    pencil({ available: true })
    expect(screen.getByRole('button', { name: /edit name/i })).toBeTruthy()
  })

  it('omitting the prop falls back to the store: available store shows the button', () => {
    pencil()
    expect(screen.getByRole('button', { name: /edit name/i })).toBeTruthy()
  })

  it('omitting the prop falls back to the store: unavailable store hides the button', () => {
    authState.state = 'unauthenticated'
    authState.role = ''
    pencil()
    expect(screen.queryByRole('button', { name: /edit name/i })).toBeNull()
  })
})
