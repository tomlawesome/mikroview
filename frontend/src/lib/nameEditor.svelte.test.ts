// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'

// The three calls the editor makes. Mocked rather than hitting a fake
// server because what is under test is the *order*: ask what would
// happen, and only then decide whether a field exists at all.
vi.mock('./api', () => ({
  fetchNameProvenance: vi.fn(),
  upsertEntity: vi.fn(),
  deleteEntity: vi.fn(),
}))

import { fetchNameProvenance, upsertEntity, deleteEntity } from './api'
import { nameEditorState } from './nameEditor.svelte'
import { appState } from './state.svelte'
import { toastState } from './toast.svelte'
import type { ClientEvent, NameProvenance } from './types'

// A buffered event, as appState holds one -- `receivedAt` is the client
// stamp appendLive adds, so a bare FirewallEvent is not one.
function buffered(overrides: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: 1,
    time: '2026-01-01T00:00:00Z',
    receivedAt: 0,
    deviceId: 'core',
    sourceIp: '10.0.0.1',
    action: 'drop',
    ruleLabel: 'r',
    chain: 'forward',
    raw: '',
    ...overrides,
  }
}

const RECT = { left: 10, bottom: 20 } as DOMRect

function provenance(overrides: Partial<NameProvenance> = {}): NameProvenance {
  return {
    type: 'host',
    key: '10.0.0.5',
    device: 'core',
    name: '',
    source: 'none',
    label: '',
    editable: true,
    ...overrides,
  }
}

// Lets a test wait for the provenance promise's continuation to run.
const settle = () => new Promise((r) => setTimeout(r))

beforeEach(() => {
  vi.mocked(fetchNameProvenance).mockReset()
  vi.mocked(upsertEntity).mockReset().mockResolvedValue(null)
  vi.mocked(deleteEntity).mockReset().mockResolvedValue(null)
  nameEditorState.close()
  appState.autoscroll = true
  appState.streamHolds = 0
  appState.events = []
  appState.frozenPool = null
})

// The requirement the owner's 2026-08-22 ruling leaves standing, and the
// one thing this editor exists to prevent: RouterOS keeps winning, so a
// label typed over a router-supplied name would be saved and never
// shown. Silence about that is the lying affordance.
describe('the router-named gate (#413)', () => {
  it('offers no field, and refuses to save, when RouterOS supplies the name', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(
      provenance({ name: 'android-dhcp-1234', source: 'router-dhcp-lease', editable: false, router: 'core' }),
    )

    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()

    expect(nameEditorState.editable).toBe(false)

    // Not merely "the button is disabled": a save attempted anyway --
    // by Enter in a field a future refactor accidentally renders, or by
    // anything else reaching this method -- must write nothing.
    nameEditorState.draft = 'my better name'
    await nameEditorState.save()
    expect(upsertEntity).not.toHaveBeenCalled()
    expect(deleteEntity).not.toHaveBeenCalled()
  })

  it('says which pushed table holds the name and which device to change it on', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(
      provenance({ name: 'android-dhcp-1234', source: 'router-dhcp-lease', editable: false, router: 'core', label: 'nas' }),
    )

    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()

    // "The router named it" is not actionable -- an operator still has
    // to know whether to look at dns-static, the lease list or a peer
    // comment.
    expect(nameEditorState.identityLine).toContain('from a DHCP lease')
    expect(nameEditorState.refusal).toContain('core')
    // And the label they already saved is acknowledged rather than
    // silently ignored, which is how it went unnoticed in the first
    // place.
    expect(nameEditorState.refusal).toContain('nas')
  })

  it('stays shut while the answer is still unknown, and if the check fails', async () => {
    let resolve: (p: NameProvenance) => void = () => {}
    vi.mocked(fetchNameProvenance).mockReturnValue(new Promise((r) => (resolve = r)))

    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    // In flight: an editor that defaulted to editable would offer the
    // field for exactly the moment somebody starts typing into it.
    expect(nameEditorState.editable).toBe(false)

    resolve(provenance())
    await settle()
    expect(nameEditorState.editable).toBe(true)

    vi.mocked(fetchNameProvenance).mockRejectedValue(new Error('offline'))
    nameEditorState.close()
    nameEditorState.open('host', '10.0.0.6', 'core', RECT)
    await settle()
    // With the answer unknown the edit might be one that does nothing,
    // so no field is offered.
    expect(nameEditorState.editable).toBe(false)
    expect(nameEditorState.error).toBeTruthy()
  })
})

describe('editing a token the router does not name', () => {
  it('prefills from the best derivation already available', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(provenance({ label: 'nas', name: 'nas', source: 'entity' }))
    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()
    expect(nameEditorState.draft).toBe('nas')

    // A port with no label falls back to the well-known service name,
    // so the common case is confirming a suggestion rather than typing.
    vi.mocked(fetchNameProvenance).mockResolvedValue(provenance({ type: 'port', key: '8291' }))
    nameEditorState.close()
    nameEditorState.open('port', '8291', '', RECT)
    await settle()
    expect(nameEditorState.draft).toBe('Winbox')
  })

  it('saves the label and rewrites the rows already on screen', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(provenance())
    appState.events = [
      buffered({ id: 1, srcIp: '10.0.0.5' }),
      buffered({ id: 2, dstIp: '10.0.0.5' }),
      buffered({ id: 3, srcIp: '10.0.0.9' }),
    ]

    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()
    nameEditorState.draft = '  nas  '
    await nameEditorState.save()

    expect(upsertEntity).toHaveBeenCalledWith({ type: 'host', key: '10.0.0.5', label: 'nas' })
    // Names are resolved once, at ingest, so buffered events would
    // otherwise keep the old name until they aged out -- a rename that
    // does not visibly take reads as broken.
    expect(appState.events[0].srcHostName).toBe('nas')
    expect(appState.events[1].dstHostName).toBe('nas')
    // And only the rows carrying that raw value.
    expect(appState.events[2].srcHostName).toBeUndefined()
    expect(toastState.message).toContain('nas')
  })

  it('an emptied field removes the label and restores the raw value', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(provenance({ label: 'nas', name: 'nas', source: 'entity' }))
    appState.events = [buffered({ srcIp: '10.0.0.5', srcHostName: 'nas' })]

    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()
    nameEditorState.draft = ''
    await nameEditorState.save()

    expect(deleteEntity).toHaveBeenCalledWith('host', '10.0.0.5')
    expect(upsertEntity).not.toHaveBeenCalled()
    expect(appState.events[0].srcHostName).toBeUndefined()
  })
})

// #363 pushes rows down as events arrive, so an editor anchored to a
// moving row is hostile. The hold is transient: the Autoscroll button
// states a preference, and flipping it under the operator would leave it
// lying about what it does next time.
describe('holding the stream while open', () => {
  it('holds on open and releases on close, without touching the preference', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(provenance())

    expect(appState.streamHeld).toBe(false)
    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()

    expect(appState.streamHeld).toBe(true)
    expect(appState.autoscroll).toBe(true)

    nameEditorState.close()
    expect(appState.streamHeld).toBe(false)
    expect(appState.autoscroll).toBe(true)
  })

  it('composes as a no-op when the view is already frozen', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(provenance())
    appState.autoscroll = false

    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()
    nameEditorState.close()

    // Closing the editor must not release a freeze it did not take.
    expect(appState.streamHeld).toBe(true)
    expect(appState.autoscroll).toBe(false)
  })

  it('does not take a second hold when reopened for another token', async () => {
    vi.mocked(fetchNameProvenance).mockResolvedValue(provenance())

    nameEditorState.open('host', '10.0.0.5', 'core', RECT)
    await settle()
    nameEditorState.open('host', '10.0.0.6', 'core', RECT)
    await settle()
    nameEditorState.close()

    // A leaked hold freezes the live view permanently, with no control
    // anywhere that would release it.
    expect(appState.streamHolds).toBe(0)
    expect(appState.streamHeld).toBe(false)
  })
})
