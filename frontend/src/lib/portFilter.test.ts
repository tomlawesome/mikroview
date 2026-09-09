// SPDX-License-Identifier: AGPL-3.0-only
import { describe, it, expect } from 'vitest'
import type { PortDoor, PortRib } from './api'
import {
  chooseDoorSpot,
  doorHalf,
  doorTs,
  emptyNote,
  litRibs,
  parsePortList,
  pillSummary,
  pickerPorts,
  placeableDoors,
  portLabel,
  ribKey,
  plaqueTally,
  zoneTally,
} from './portFilter'

function rib(o: Partial<PortRib> = {}): PortRib {
  return { in: 'bridge1', out: 'ether3', events: 1, accepts: 1, drops: 0, ...o }
}

function door(o: Partial<PortDoor> = {}): PortDoor {
  return {
    device: 'core',
    label: '#12',
    ordinal: 12,
    action: 'accept',
    chain: 'forward',
    dstPort: '445',
    in: 'bridge1',
    out: 'ether3',
    who: 'bridge1 → ether3 accept',
    ...o,
  }
}

describe('the typed port list (#1018)', () => {
  it('reads single ports, lists and ranges, sorted and deduplicated', () => {
    expect(parsePortList('445')).toEqual([445])
    expect(parsePortList('23,22')).toEqual([22, 23])
    expect(parsePortList(' 22 , 22 ')).toEqual([22])
    expect(parsePortList('80-82,443')).toEqual([80, 81, 82, 443])
    expect(parsePortList('')).toEqual([])
  })

  // A map filtered to a different set from the one typed would be a
  // wrong answer wearing the operator's own words, so half a list is
  // refused rather than applied.
  it('refuses a list it cannot read whole rather than taking the parseable half', () => {
    expect(parsePortList('22,nope')).toBeNull()
    expect(parsePortList('0')).toBeNull()
    expect(parsePortList('70000')).toBeNull()
    expect(parsePortList('80-22')).toBeNull()
    expect(parsePortList('1-65535')).toBeNull()
  })
})

describe('what the pill says', () => {
  it('names the selection the way it was asked for', () => {
    expect(portLabel({ ports: [445], proto: 'tcp' })).toBe('445/tcp')
    expect(portLabel({ ports: [445], proto: '' })).toBe('445')
    expect(portLabel({ ports: [22, 23], proto: 'tcp' })).toBe('22-23/tcp')
    expect(portLabel({ ports: [22, 80, 443], proto: 'udp' })).toBe('22,80,443/udp')
    expect(portLabel({ ports: [], proto: 'tcp' })).toBe('')
  })

  // "nothing seen" rather than "0 lines": zero is a number about
  // traffic, and the thing being said is that there was none.
  it('says nothing seen rather than counting to zero, and pluralises the doors', () => {
    expect(pillSummary(2, 3)).toBe('2 lines seen · 3 doors')
    expect(pillSummary(1, 1)).toBe('1 line seen · 1 door')
    expect(pillSummary(0, 1)).toBe('nothing seen · 1 door')
  })
})

describe('which ribs the port lights', () => {
  it('lights both halves of a crossing, because a crossing is one packet through the router', () => {
    const lit = litRibs([rib({ in: 'bridge1', out: 'ether3', accepts: 2 })])
    expect(lit.get(ribKey('bridge1', 'ether3'))).toBe('accept')
    expect(lit.get(ribKey('ether3', 'bridge1'))).toBe('accept')
    expect(lit.size).toBe(2)
  })

  // A refused line died at the router, so only the half it arrived on
  // lights: lighting the far half would draw a packet where none went.
  it('lights only the arriving half of a refusal, and keeps the empty out-interface', () => {
    const lit = litRibs([rib({ in: 'ether4', out: '', accepts: 0, drops: 14 })])
    expect([...lit.entries()]).toEqual([[ribKey('ether4', ''), 'refused']])
  })

  it('colours a direction that ever passed the port as accepted', () => {
    const lit = litRibs([
      rib({ in: 'bridge1', out: 'ether3', accepts: 0, drops: 3 }),
      rib({ in: 'bridge1', out: 'ether3', accepts: 1, drops: 0 }),
    ])
    expect(lit.get(ribKey('bridge1', 'ether3'))).toBe('accept')
  })

  it('draws nothing for a direction naming no boundary at all', () => {
    expect(litRibs([rib({ in: '', out: '', accepts: 1 })]).size).toBe(0)
  })
})

describe('where a door is drawn', () => {
  // A rule that refuses stops the packet on the way in; a rule that
  // accepts opens the way out. Round 53 draws exactly this: #12 lan →
  // srv accept on the Servers half, #23 wan → any drop on the WAN half,
  // #31 guest → srv drop on the Guest half -- the last on a rib nobody
  // used, which is the whole point of drawing policy at all.
  it('puts an accepting rule on the way out and a refusing one on the way in', () => {
    expect(doorHalf(door({ action: 'accept', in: 'bridge1', out: 'ether3' }))).toEqual({ from: 'ether3', to: 'bridge1' })
    expect(doorHalf(door({ action: 'drop', in: 'ether1', out: '' }))).toEqual({ from: 'ether1', to: '' })
    expect(doorHalf(door({ action: 'drop', in: 'ether5', out: 'ether3' }))).toEqual({ from: 'ether5', to: 'ether3' })
  })

  it('falls back to the one interface a rule does name', () => {
    expect(doorHalf(door({ action: 'accept', in: 'ether1', out: '' }))).toEqual({ from: 'ether1', to: '' })
    expect(doorHalf(door({ action: 'drop', in: '', out: 'ether3' }))).toEqual({ from: 'ether3', to: '' })
  })

  // A rule naming neither interface guards every boundary, and a door
  // on all of them at once says nothing.
  it('draws no door for a rule that names no interface', () => {
    expect(doorHalf(door({ action: 'drop', in: '', out: '' }))).toBeNull()
  })
})

describe('what the map writes', () => {
  it('counts the lane against itself, not against the ten dots that fit', () => {
    expect(zoneTally(2, 12, '445/tcp')).toBe('2 of 12 hosts on 445/tcp')
    expect(zoneTally(0, 1, '445/tcp')).toBe('0 of 1 host on 445/tcp')
  })

  // The city's plaque has a chip's width to say the same thing in, so it
  // says it shorter (#1055, round 54's own `city-port`). Same count,
  // fewer words -- never a second count.
  it('says the same tally short enough for a plaque', () => {
    expect(plaqueTally(2, 12, '445/tcp')).toBe('2 of 12 · 445/tcp')
    expect(plaqueTally(0, 3, '3389/tcp')).toBe('0 of 3 · 3389/tcp')
  })

  // Nothing seen is a sentence, not an empty state, and the wording
  // follows the number of rules because "no rule names it either" is a
  // different -- and more interesting -- finding.
  it('says what policy has to say about a port nothing carried', () => {
    expect(emptyNote('3389/tcp', [door({ label: '#23', action: 'drop', in: 'ether1', who: 'ether1 → any drop' })])).toBe(
      'no logged traffic on 3389/tcp in the window · one rule names it — #23 ether1 → any drop, the door on the ether1 side',
    )
    expect(emptyNote('3389/tcp', [])).toBe(
      'no logged traffic on 3389/tcp in the window · no pushed rule names it either',
    )
    expect(emptyNote('3389/tcp', [door({ label: '#23' }), door({ label: '#31' })])).toBe(
      'no logged traffic on 3389/tcp in the window · 2 rules name it — #23, #31, their doors drawn',
    )
  })
})

describe('a rule that names the port but no boundary', () => {
  // Round 53 draws a door as two posts across a rib. A rule naming
  // neither interface guards every boundary at once, so there is no rib
  // to stand it on and the map draws none -- and the pill counts what
  // is drawn, so its number and the map are one claim. The words carry
  // what the drawing cannot; inventing a place to put it is the thing
  // this repo's build rule forbids.
  it('is not counted among the doors the map draws', () => {
    const everywhere = door({ label: '#40', in: undefined, out: undefined })
    expect(placeableDoors([door(), everywhere]).map((d) => d.label)).toEqual(['#12'])
  })

  it('is still named in the line the map writes, and says it is everywhere', () => {
    expect(emptyNote('445/tcp', [door({ label: '#40', action: 'drop', in: undefined, out: undefined, who: 'any → any drop' })])).toBe(
      'no logged traffic on 445/tcp in the window · one rule names it — #40 any → any drop, on every boundary, so no one door is drawn',
    )
  })
})

describe('the picker offers one chip per port (#1018)', () => {
  // The answer counts a port per protocol, because that is what the
  // window carried. The picker selects a *port*; the protocol chip
  // beside it decides which. Two identical `53` chips would be one
  // control drawn twice, and toggling either would light both.
  it('folds a port carried on both protocols into one chip', () => {
    const got = pickerPorts([
      { port: 53, proto: 'udp', count: 40, named: false },
      { port: 53, proto: 'tcp', count: 2, named: true },
      { port: 445, proto: 'tcp', count: 16, named: false },
    ])
    expect(got.map((c) => c.port)).toEqual([53, 445])
    expect(got[0]).toMatchObject({ count: 42, named: true })
    expect([...new Set(got[0].protos)].sort()).toEqual(['tcp', 'udp'])
  })

  it('keeps a port only a rule names, with nothing carried on it', () => {
    const got = pickerPorts([{ port: 3389, proto: 'tcp', count: 0, named: true }])
    expect(got).toEqual([{ port: 3389, count: 0, named: true, protos: [] }])
  })
})

describe('where a door stands on its rib (#1018)', () => {
  // Every rib on this map converges on the router, so a fixed fraction
  // put every door in the same crowded place -- on top of each other and
  // across whatever else passed through. The chooser samples its own rib
  // and takes the point with the most room.
  const own = doorTs().map((t) => ({ x: 0, y: 100 - t * 100 }))

  it('walks away from a rib converging on the same end', () => {
    // A neighbour that hugs the far end (y near 0) and falls away toward
    // the near end -- the shape of two ribs meeting at the router.
    const other = [
      { x: 2, y: 0 },
      { x: 6, y: 20 },
      { x: 20, y: 40 },
      { x: 60, y: 70 },
    ]
    const got = chooseDoorSpot(own, [other])
    // The lane end (index 0, t = 0.3, y = 70) is furthest from it.
    expect(got.index).toBe(0)
    expect(got.toward).not.toBeNull()
    expect(got.clearance).toBeGreaterThan(30)
  })

  it('moves toward the router when that is where the room is', () => {
    // The mirror image: the neighbour crowds the lane end instead.
    const other = [
      { x: 2, y: 100 },
      { x: 6, y: 80 },
      { x: 20, y: 60 },
      { x: 60, y: 30 },
    ]
    const got = chooseDoorSpot(own, [other])
    expect(got.index).toBe(doorTs().length - 1)
  })

  it('takes the lane end when nothing is near, and when everything ties', () => {
    expect(chooseDoorSpot(own, []).index).toBe(0)
    expect(chooseDoorSpot(own, []).toward).toBeNull()
    // Equidistant from every candidate: a tie keeps the earlier one, and
    // the candidates run lane end first.
    const parallel = own.map((p) => ({ x: p.x + 40, y: p.y }))
    expect(chooseDoorSpot(own, [parallel]).index).toBe(0)
  })

  it('keeps two doors on converging ribs apart', () => {
    // The first door lands, then the second is measured against it as
    // well as against the ribs -- which is what stops them stacking
    // where the two ribs meet.
    const ribA = doorTs().map((t) => ({ x: -t * 60, y: 100 - t * 100 }))
    const ribB = doorTs().map((t) => ({ x: t * 60, y: 100 - t * 100 }))
    const first = chooseDoorSpot(ribA, [ribB])
    const second = chooseDoorSpot(ribB, [ribA, [ribA[first.index]]])
    const a = ribA[first.index]
    const b = ribB[second.index]
    expect(Math.hypot(a.x - b.x, a.y - b.y)).toBeGreaterThan(40)
  })

  it('offers eleven points, lane end first, over the middle of the rib', () => {
    const ts = doorTs()
    expect(ts.length).toBe(11)
    expect(ts[0]).toBeCloseTo(0.3, 6)
    expect(ts[ts.length - 1]).toBeCloseTo(0.8, 6)
    expect([...ts].sort((x, y) => x - y)).toEqual(ts)
  })
})
