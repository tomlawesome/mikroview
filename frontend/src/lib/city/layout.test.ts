import { describe, expect, it } from 'vitest'
import { mockupEstate } from './fixture'
import { bankV, layoutGround, plateRadius } from './layout'
import { bezAt, bezTangent, dm, segsOf } from './roads'
import type { Pt } from './project'
import type { District, Ground, Road } from './types'

const ground: Ground = layoutGround(mockupEstate())

/** The plates a road is allowed inside: the ones it starts or ends in. */
function ownPlates(r: Road, g: Ground): Set<string> {
  const own = new Set<string>()
  const ends = [r.pts[0], r.pts[r.pts.length - 1]]
  for (const d of g.districts) {
    for (const p of ends) if (dm(p, [d.u, d.v]) <= d.r + 0.01) own.add(d.id)
    for (const b of d.buildings) if (b.id === r.from || b.id === r.to) own.add(d.id)
  }
  return own
}

describe('city layout: the ground plan', () => {
  it('sizes a plate by its hosts, within bounds', () => {
    expect(plateRadius(0)).toBe(13)
    expect(plateRadius(2)).toBe(16)
    expect(plateRadius(40)).toBe(21)
  })

  it('lays out every zone as a plate, none overlapping', () => {
    expect(ground.districts.map((d) => d.id).sort()).toEqual(['bridge-lan', 'vlan-guest', 'vlan-iot', 'vlan-srv', 'wlan-cams', 'wlan-wsh'])
    const ds = ground.districts
    for (let i = 0; i < ds.length; i++)
      for (let j = i + 1; j < ds.length; j++) expect(dm([ds[i].u, ds[i].v], [ds[j].u, ds[j].v])).toBeGreaterThan(ds[i].r + ds[j].r)
  })

  it('stands every building inside its plate with the busiest tallest', () => {
    for (const d of ground.districts) {
      expect(d.buildings.length).toBeGreaterThan(0)
      for (const b of d.buildings) {
        expect(dm([b.u, b.v], [d.u, d.v]) + b.R).toBeLessThanOrEqual(d.r + 1e-9)
        expect(b.districtId).toBe(d.id)
        expect(b.name).toBeTruthy()
      }
      for (let i = 1; i < d.buildings.length; i++) expect(d.buildings[i].h).toBeLessThanOrEqual(d.buildings[i - 1].h)
    }
    const lan = ground.districts.find((d) => d.id === 'bridge-lan') as District
    expect(lan.more).toBe(3)
  })

  it('puts the second router in its own borough down the map', () => {
    expect(ground.boroughs).toHaveLength(2)
    const [first, second] = ground.boroughs
    expect(first.districtIds).toHaveLength(4)
    expect(second.districtIds).toEqual(['wlan-wsh', 'wlan-cams'])
    expect(second.bounds.v0).toBeGreaterThan(first.bounds.v1)
    const hap = ground.nodes.find((n) => n.id === 'hapax3')
    expect(hap?.kind).toBe('router-ant')
    // Its link road runs to the plate whose CIDR holds its address.
    const link = ground.roads.find((r) => r.id === 'link-hapax3') as Road
    const lan = ground.districts.find((d) => d.id === 'bridge-lan') as District
    expect(dm(link.pts[link.pts.length - 1], [lan.u, lan.v])).toBeCloseTo(lan.r)
  })

  it('keeps the river clear of the town with a bridge per way out', () => {
    expect(ground.river).not.toBeNull()
    const river = ground.river as NonNullable<Ground['river']>
    const sampled: Pt[] = []
    for (const s of segsOf(river.bankN)) for (let i = 0; i <= 24; i++) sampled.push(bezAt(s, i / 24))
    for (const d of ground.districts) expect(d.v - d.r).toBeGreaterThanOrEqual(bankV(sampled, d.u) + 12 - 1e-6)
    expect(ground.bridges.map((b) => b.iface)).toEqual(['ether1', 'l2tp-out1', 'wg0'])
    for (const b of ground.bridges) {
      expect(b.t[1]).toBeCloseTo(bankV(sampled, b.t[0]), 1)
      // The deck runs on the d1 diagonal.
      expect(b.t[0] - b.f[0]).toBeCloseTo(b.t[1] - b.f[1])
      expect(ground.nodes.some((n) => n.id === b.post)).toBe(true)
    }
    expect(ground.bridges[0].kind).toBe('road')
    expect(ground.bridges[1].kind).toBe('foot')
  })

  it('derives each bridge state from the fixture tunnel, never a guess', () => {
    const wan = ground.bridges.find((b) => b.iface === 'ether1')
    expect(wan?.state).toBe('up') // wanLogged: true
    const l2tp = ground.bridges.find((b) => b.iface === 'l2tp-out1')
    expect(l2tp?.state).toBe('up') // apiState up, events > 0
    expect(l2tp?.peers.map((p) => p.name)).toEqual(['branch-office'])
    const wg0 = ground.bridges.find((b) => b.iface === 'wg0')
    expect(wg0?.state).toBe('down') // apiState down
    expect(wg0?.peers).toEqual([])
  })
})

describe('city layout: roads', () => {
  const roads = ground.roads.filter((r) => !r.lane)

  it('draws a road for every observed pair, folded by direction', () => {
    const ids = roads.map((r) => r.id)
    expect(ids).toContain('bridge-lan|vlan-srv')
    expect(ids).not.toContain('vlan-srv|bridge-lan')
    expect(ids).toContain('bridge-lan|vlan-iot')
    expect(roads.find((r) => r.id === 'bridge-lan|vlan-iot')?.k).toBe('x')
    expect(roads.find((r) => r.id === 'bridge-lan|vlan-guest')?.stop).toBe('drop')
    expect(roads.find((r) => r.id === 'rb-wan')?.to).toBe('post:ether1')
    expect(roads.find((r) => r.id === 'wan-span')?.fade).toBe(true)
  })

  it('has no straight run: every road bends somewhere', () => {
    for (const r of ground.roads) {
      expect(r.pts.length).toBeGreaterThanOrEqual(3)
      let bent = false
      for (let i = 1; i < r.pts.length - 1; i++) {
        const a = r.pts[i - 1]
        const b = r.pts[i]
        const c = r.pts[i + 1]
        const cross = (b[0] - a[0]) * (c[1] - b[1]) - (b[1] - a[1]) * (c[0] - b[0])
        if (Math.abs(cross) > 1e-6) bent = true
      }
      expect(bent, r.id).toBe(true)
    }
  })

  it('has no elbow: the tangent is continuous at every join', () => {
    for (const r of ground.roads) {
      const segs = segsOf(r.pts)
      for (let i = 1; i < segs.length; i++) {
        const out = bezTangent(segs[i - 1], 1)
        const into = bezTangent(segs[i], 0)
        expect(out[0]).toBeCloseTo(into[0], 6)
        expect(out[1]).toBeCloseTo(into[1], 6)
      }
    }
  })

  it('leaves and arrives at a plate square-on to its edge', () => {
    for (const r of roads) {
      const segs = segsOf(r.pts)
      const ends: [Pt, Pt][] = [
        [r.pts[0], bezTangent(segs[0], 0)],
        [r.pts[r.pts.length - 1], bezTangent(segs[segs.length - 1], 1)],
      ]
      for (const [p, tan] of ends) {
        const d = ground.districts.find((d) => Math.abs(dm(p, [d.u, d.v]) - d.r) < 0.01)
        if (!d) continue
        // The edge's outward normal, diamond-normalised, is parallel to the tangent.
        const n: Pt = [p[0] - d.u, p[1] - d.v]
        const cross = n[0] * tan[1] - n[1] * tan[0]
        const scale = Math.hypot(n[0], n[1]) * Math.hypot(tan[0], tan[1])
        expect(Math.abs(cross) / scale, r.id).toBeLessThan(1e-6)
      }
    }
  })

  it('routes round every plate it does not start or end in', () => {
    for (const r of ground.roads) {
      const own = ownPlates(r, ground)
      for (const s of segsOf(r.pts))
        for (let t = 0; t <= 1; t += 0.02) {
          const p = bezAt(s, t)
          for (const d of ground.districts) {
            if (own.has(d.id)) continue
            expect(dm(p, [d.u, d.v]), r.id + ' through ' + d.id).toBeGreaterThan(d.r - 0.5)
          }
        }
    }
  })

  it('gives every lane its building and a plain name', () => {
    for (const r of ground.roads) {
      expect(r.label).toBeTruthy()
      if (r.lane) expect(r.from).toMatch(/\//)
    }
  })
})
