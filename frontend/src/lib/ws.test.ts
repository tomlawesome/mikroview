// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { LiveSocket, type ServerChange } from './ws'

// A stand-in WebSocket: jsdom does not implement one, and the behaviour
// under test here is what LiveSocket does with a frame, not how it opens
// a connection. Only the surface LiveSocket touches is provided, so a
// call it starts making that this does not have fails loudly rather than
// being silently absorbed.
class FakeWebSocket {
  static last: FakeWebSocket | null = null
  onopen: (() => void) | null = null
  onmessage: ((ev: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false

  constructor() {
    FakeWebSocket.last = this
  }

  close() {
    this.closed = true
  }

  deliver(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) })
  }
}

function connected(): { socket: LiveSocket; ws: FakeWebSocket } {
  const socket = new LiveSocket()
  socket.connect()
  const ws = FakeWebSocket.last
  if (!ws) throw new Error('LiveSocket.connect did not open a socket')
  ws.onopen?.()
  return { socket, ws }
}

describe('change notices over the live socket', () => {
  beforeEach(() => {
    FakeWebSocket.last = null
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })

  // The point of the whole mechanism: coverage is an answer about pushed
  // router tables, and the server already knows the moment one arrives.
  // Before this, a screen showing that answer waited out its own 60s poll.
  it('hands a changed frame to every registered listener', () => {
    const { socket, ws } = connected()
    const seen: ServerChange[] = []
    const alsoSeen: ServerChange[] = []
    socket.onChange((c) => seen.push(c))
    socket.onChange((c) => alsoSeen.push(c))

    ws.deliver({ type: 'changed', change: 'router-state' })

    expect(seen).toEqual(['router-state'])
    expect(alsoSeen).toEqual(['router-state'])
  })

  it('names which thing changed, so a listener can ignore the ones it does not show', () => {
    const { socket, ws } = connected()
    const seen: ServerChange[] = []
    socket.onChange((c) => seen.push(c))

    ws.deliver({ type: 'changed', change: 'definitions' })
    ws.deliver({ type: 'changed', change: 'router-state' })

    expect(seen).toEqual(['definitions', 'router-state'])
  })

  // The unsubscribe half matters as much as the subscribe half: a
  // listener left behind after sign-out would keep refetching for a
  // session that has ended, and every one of those calls would 401.
  it('stops delivering once the listener unregisters', () => {
    const { socket, ws } = connected()
    const seen: ServerChange[] = []
    const off = socket.onChange((c) => seen.push(c))

    ws.deliver({ type: 'changed', change: 'router-state' })
    off()
    ws.deliver({ type: 'changed', change: 'definitions' })

    expect(seen).toEqual(['router-state'])
  })

  // An events frame is not a change notice. Firing one would refetch
  // coverage on every batch of live traffic -- the request rate the 60s
  // interval exists to avoid, arrived at from the other direction.
  it('does not treat an events frame as a change', () => {
    const { socket, ws } = connected()
    const seen: ServerChange[] = []
    socket.onChange((c) => seen.push(c))

    ws.deliver({ type: 'events', events: [] })
    ws.deliver({ type: 'changed' })

    expect(seen).toEqual([])
  })
})
