// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import {
  IMPORTANCE_CEIL_H,
  IMPORTANCE_FLOOR_H,
  dependedOnImportance,
  tweenHeights,
  watchedImportance,
  watchedNotice,
  type Importance,
  type ImportanceBuilding,
} from './importance'
import type { ClientEvent, Flag, WatchlistEntry } from '../types'

const ROUTER = '10.10.0.1'
const SERVER = '10.20.0.10'
const WORKSTATION = '10.20.0.20'
const UNSEEN = '10.20.0.99'
const CAMERA = '10.60.0.10'
const HOST = '10.10.0.50'

function event(over: Partial<ClientEvent> = {}): ClientEvent {
  return {
    id: 1,
    time: '2026-09-03T12:00:00Z',
    receivedAt: 0,
    deviceId: 'rb5009',
    sourceIp: '10.10.0.1',
    action: 'accept',
    ruleLabel: 'r',
    chain: 'forward',
    raw: '',
    ...over,
  }
}

function flag(over: Partial<Flag> = {}): Flag {
  return {
    id: 'f' + Math.random(),
    type: 'port_scan',
    target: CAMERA,
    detail: '',
    count: 1,
    firstSeen: '2026-09-03T00:00:00Z',
    lastSeen: '2026-09-03T00:00:00Z',
    cleared: false,
    ...over,
  }
}

const BUILDINGS: ImportanceBuilding[] = [
  { id: 'router', ip: ROUTER },
  { id: 'server', ip: SERVER },
  { id: 'workstation', ip: WORKSTATION },
  { id: 'unseen', ip: UNSEEN },
]

describe('dependedOnImportance', () => {
  it('makes the router tallest, modelled on the demo feeder shape: several LAN hosts asking the router directly (its own DNS-to-the-router rule), fewer asking any one server, one asking a workstation', () => {
    const events: ClientEvent[] = []
    for (let i = 1; i <= 5; i++) {
      events.push(event({ id: i, chain: 'input', srcIp: `10.10.0.${10 + i}`, dstIp: ROUTER, action: 'accept' }))
    }
    events.push(event({ id: 100, srcIp: '10.10.0.11', dstIp: SERVER, action: 'accept' }))
    events.push(event({ id: 101, srcIp: '10.10.0.12', dstIp: SERVER, action: 'accept' }))
    events.push(event({ id: 200, srcIp: '10.10.0.13', dstIp: WORKSTATION, action: 'accept' }))
    // A refused attempt on the router from a sixth host never counts --
    // only a landed (accepted) connection is "talking to" it.
    events.push(event({ id: 300, srcIp: '10.10.0.99', dstIp: ROUTER, action: 'drop' }))

    const out = dependedOnImportance(BUILDINGS, events)
    expect(out.get('router')!.height).toBeGreaterThan(out.get('server')!.height)
    expect(out.get('server')!.height).toBeGreaterThan(out.get('workstation')!.height)
    expect(out.get('router')!.height).toBeCloseTo(IMPORTANCE_CEIL_H, 5)
    expect(out.get('router')!.known).toBe(true)
  })

  it('a host the window has no event for at all draws at the floor, marked unknown -- never "nothing depends on it"', () => {
    const events = [event({ srcIp: '10.10.0.11', dstIp: SERVER, action: 'accept' })]
    const out = dependedOnImportance(BUILDINGS, events)
    expect(out.get('unseen')).toEqual({ height: IMPORTANCE_FLOOR_H, known: false })
  })

  it('a host the window does cover, with zero accepted talkers, is a real answer at the floor -- known, not absent', () => {
    // Appears only as a source, never a destination: the window saw it,
    // and nobody accepted-reached it.
    const events = [event({ srcIp: SERVER, dstIp: '203.0.113.9', action: 'accept', outInterface: 'ether1' })]
    const out = dependedOnImportance([{ id: 'server', ip: SERVER }], events)
    expect(out.get('server')).toEqual({ height: IMPORTANCE_FLOOR_H, known: true })
  })

  it('nothing observed at all leaves every building unknown at the floor', () => {
    const out = dependedOnImportance(BUILDINGS, [])
    for (const b of BUILDINGS) expect(out.get(b.id)).toEqual({ height: IMPORTANCE_FLOOR_H, known: false })
  })
})

describe('watchedImportance', () => {
  it('a twice-flagged camera is the spike; everything unwatched sits at the floor', () => {
    const buildings: ImportanceBuilding[] = [
      { id: 'camera', ip: CAMERA },
      { id: 'host', ip: HOST },
    ]
    const flags: Flag[] = [flag({ target: CAMERA }), flag({ target: CAMERA, type: 'critical_port' })]
    const out = watchedImportance(buildings, flags, [])
    expect(out.get('camera')!.height).toBeCloseTo(IMPORTANCE_CEIL_H, 5)
    expect(out.get('host')).toEqual({ height: IMPORTANCE_FLOOR_H, known: true })
  })

  it('a cleared flag no longer weighs toward the reading', () => {
    const out = watchedImportance([{ id: 'camera', ip: CAMERA }], [flag({ target: CAMERA, cleared: true })], [])
    expect(out.get('camera')).toEqual({ height: IMPORTANCE_FLOOR_H, known: true })
  })

  it('an enabled watchlist entry scoped to a host adds weight; a disabled one does not', () => {
    const buildings: ImportanceBuilding[] = [
      { id: 'host', ip: HOST },
      { id: 'other', ip: '10.10.0.51' },
    ]
    const watchlist: WatchlistEntry[] = [
      { id: 'w1', enabled: true, source: { ip: HOST }, createdAt: '' },
      { id: 'w2', enabled: false, source: { ip: '10.10.0.51' }, createdAt: '' },
    ]
    const out = watchedImportance(buildings, [], watchlist)
    expect(out.get('host')!.height).toBeGreaterThan(out.get('other')!.height)
    expect(out.get('other')).toEqual({ height: IMPORTANCE_FLOOR_H, known: true })
  })
})

describe('watchedNotice', () => {
  it('says nothing once both stores have loaded', () => {
    expect(watchedNotice(true, true)).toBeNull()
  })

  it('names whichever store has not loaded yet, in the app\'s own "not loaded yet" wording', () => {
    expect(watchedNotice(false, true)).toMatch(/flags.*not loaded yet/i)
    expect(watchedNotice(true, false)).toMatch(/watchlist.*not loaded yet/i)
    expect(watchedNotice(false, false)).toMatch(/not loaded yet/i)
  })
})

describe('tweenHeights (the plinth transition)', () => {
  const target = new Map<string, Importance>([['a', { height: 9, known: true }]])

  it('interpolates part-way between the previous height and the target', () => {
    const from = new Map([['a', 1]])
    expect(tweenHeights(from, target, 0.5, false).get('a')).toBeCloseTo(5, 5)
  })

  it('a building with no previous height starts already at the target -- nothing to grow from', () => {
    expect(tweenHeights(new Map(), target, 0, false).get('a')).toBe(9)
  })

  it('snaps straight to the target under reduced motion, ignoring t entirely', () => {
    const from = new Map([['a', 1]])
    expect(tweenHeights(from, target, 0, true).get('a')).toBe(9)
  })
})
