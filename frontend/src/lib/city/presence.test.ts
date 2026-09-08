// SPDX-License-Identifier: AGPL-3.0-only
//
// Living hosts (round 49, #1016): what the city draws a host as, and
// which hosts get a building at all.
import { describe, expect, it } from 'vitest'
import { bufferHost, hostMarksFrom, mergeZoneHosts, presenceNote, quietFor, type CityHost } from './presence'
import type { Flag, WatchlistEntry } from '../types'

const H = 3_600_000
const NOW = Date.parse('2026-09-07T12:00:00Z')

function reg(ip: string, over: Partial<CityHost> = {}): CityHost {
  return {
    key: 'ether1|' + ip,
    ip,
    label: '',
    presence: 'live',
    lastSeen: new Date(NOW).toISOString(),
    firstSeen: '2026-08-03T09:00:00Z',
    events: 1204,
    reason: null,
    markedBy: null,
    markedAt: null,
    flags: 0,
    watch: 0,
    spike: false,
    ...over,
  }
}

const openFlag = (type: string, target: string, over: Partial<Flag> = {}): Flag =>
  ({ id: type + '/' + target, type, target, detail: '', count: 1, firstSeen: '', lastSeen: '', cleared: false, ...over }) as Flag

const watch = (over: Partial<WatchlistEntry>): WatchlistEntry => ({ id: 'w', ...over }) as WatchlistEntry

describe('quietFor', () => {
  it('says how long in hours, the ratified wording of `quiet · 26 h`', () => {
    expect(quietFor(new Date(NOW - 26 * H).toISOString(), NOW)).toBe('26 h')
  })

  it('switches to days once hours stop being readable', () => {
    expect(quietFor(new Date(NOW - 96 * H).toISOString(), NOW)).toBe('4 d')
  })

  // The window is 24 hours, so nothing honest reaches this in minutes.
  // A minutes figure would invite reading the drawing as a live-ness
  // meter, which presence deliberately is not.
  it('never drops below an hour', () => {
    expect(quietFor(new Date(NOW - 90_000).toISOString(), NOW)).toBe('1 h')
  })

  it('claims nothing when there is no stamp to claim it from', () => {
    expect(quietFor(null, NOW)).toBe('not heard')
    expect(quietFor('not a date', NOW)).toBe('not heard')
  })
})

describe('presenceNote', () => {
  it('says how long a quiet host has been quiet', () => {
    expect(presenceNote(reg('10.0.10.60', { presence: 'quiet', lastSeen: new Date(NOW - 26 * H).toISOString() }), NOW)).toBe('quiet · 26 h')
  })

  it('says quiet on purpose, and no duration -- the mark is the fact, not the clock', () => {
    expect(presenceNote(reg('10.0.10.61', { presence: 'intended' }), NOW)).toBe('quiet on purpose')
  })

  // Being drawn normally is the whole statement about a live host, so
  // the label falls back to its address.
  it('says nothing about a live host', () => {
    expect(presenceNote(reg('10.0.10.21'), NOW)).toBeNull()
  })
})

describe('mergeZoneHosts', () => {
  it('keeps a quiet host the event buffer has forgotten', () => {
    const out = mergeZoneHosts([], [reg('10.0.10.60', { label: 'tv-lounge', presence: 'quiet' })])
    expect(out.map((h) => h.ip)).toEqual(['10.0.10.60'])
    expect(out[0].presence).toBe('quiet')
  })

  it('keeps a host marked quiet on purpose, drawn white rather than removed', () => {
    const out = mergeZoneHosts([], [reg('10.0.10.61', { label: 'printer-old', presence: 'intended', reason: 'switched off at the wall' })])
    expect(out).toHaveLength(1)
    expect(out[0].presence).toBe('intended')
    expect(out[0].reason).toBe('switched off at the wall')
  })

  // Dismissed is the one state that means "off my map". It is dropped
  // here and nowhere else; nothing has to remember it, because the
  // server clears the dismissal when the feed hears the host again.
  it('removes a dismissed host from the map', () => {
    const out = mergeZoneHosts([], [reg('10.0.10.62', { presence: 'dismissed' }), reg('10.0.10.63', { presence: 'quiet' })])
    expect(out.map((h) => h.ip)).toEqual(['10.0.10.63'])
  })

  it('drops a dismissed host even while the buffer still lists it', () => {
    const out = mergeZoneHosts([{ label: 'gone', ip: '10.0.10.62' }], [reg('10.0.10.62', { presence: 'dismissed' })])
    expect(out).toEqual([])
  })

  // The plate's cap (layout.ts's MAX_BUILDINGS) takes the front of this
  // list, so the order decides who loses a slot when a district holds
  // more hosts than it can draw legibly: a silent machine before a
  // talking one.
  it("puts the buffer's live hosts first and the register's quiet ones behind", () => {
    const out = mergeZoneHosts(
      [
        { label: 'a', ip: '10.0.10.1' },
        { label: 'b', ip: '10.0.10.2' },
      ],
      [reg('10.0.10.9', { label: 'old', presence: 'quiet' })],
    )
    expect(out.map((h) => h.ip)).toEqual(['10.0.10.1', '10.0.10.2', '10.0.10.9'])
    expect(out[0].presence).toBe('live')
  })

  it("carries the register's record onto a host the buffer also sees", () => {
    const out = mergeZoneHosts([{ label: 'tom-desktop', ip: '10.0.10.21' }], [reg('10.0.10.21', { events: 4096, firstSeen: '2026-08-03T09:00:00Z' })])
    expect(out).toHaveLength(1)
    expect(out[0].key).toBe('ether1|10.0.10.21')
    expect(out[0].events).toBe(4096)
    expect(out[0].label).toBe('tom-desktop')
  })

  it("falls back to the register's label for a host the window never named", () => {
    const out = mergeZoneHosts([{ label: '', ip: '10.0.10.30' }], [reg('10.0.10.30', { label: 'nas' })])
    expect(out[0].label).toBe('nas')
  })

  it('names a host by its address when nothing else names it', () => {
    expect(mergeZoneHosts([{ label: '', ip: '10.0.10.40' }], [])[0].label).toBe('10.0.10.40')
  })

  // One machine seen on two interfaces holds two register keys. Presence
  // is a claim about silence, so one interface still hearing it is
  // evidence it is not silent: the kindest reading wins.
  it('takes the kindest reading when one address holds two records', () => {
    const out = mergeZoneHosts([], [reg('10.0.10.50', { key: 'ether1|10.0.10.50', presence: 'quiet' }), reg('10.0.10.50', { key: 'ether2|10.0.10.50', presence: 'live' })])
    expect(out).toHaveLength(1)
    expect(out[0].presence).toBe('live')
  })

  it('reads as it always did when the register says nothing at all', () => {
    const out = mergeZoneHosts([{ label: 'a', ip: '10.0.10.1' }], [])
    expect(out).toEqual([bufferHost('a', '10.0.10.1')])
    expect(out[0].presence).toBe('live')
    expect(out[0].key).toBe('')
  })
})

describe('hostMarksFrom (#981)', () => {
  it('counts the open flags on an address and ignores the cleared ones', () => {
    const m = hostMarksFrom(
      [openFlag('port_scan', '10.0.10.5'), openFlag('critical_port', '10.0.10.5'), openFlag('repeated_drops', '10.0.10.5', { cleared: true })],
      [],
    )
    expect(m.get('10.0.10.5')?.flags).toBe(2)
  })

  // A flag whose target is a port, a rule label or `global` names no
  // host, so it marks none rather than being mis-attributed to one.
  it('counts nothing for a flag whose target is not a single address', () => {
    const m = hostMarksFrom([openFlag('distributed_brute_force', 'port 22'), openFlag('global_spike', 'global')], [])
    expect(m.size).toBe(0)
  })

  // repeated_drops writes `<ip> -> port <N>`; the address is what the
  // building is, and the port belongs on the flag's own card.
  it('reads the address out of a target that carries a port with it', () => {
    const m = hostMarksFrom([openFlag('repeated_drops', '10.0.10.5 -> port 22')], [])
    expect(m.get('10.0.10.5')?.flags).toBe(1)
  })

  it('says so when one of those flags is an activity spike, and only then', () => {
    expect(hostMarksFrom([openFlag('activity_spike', '10.0.10.5')], []).get('10.0.10.5')?.spike).toBe(true)
    expect(hostMarksFrom([openFlag('port_scan', '10.0.10.5')], []).get('10.0.10.5')?.spike).toBe(false)
  })

  it('counts a watchlist entry against each end it names', () => {
    const m = hostMarksFrom([], [watch({ source: { ip: '10.0.10.5' }, destIp: '10.0.10.9' } as Partial<WatchlistEntry>)])
    expect(m.get('10.0.10.5')?.watch).toBe(1)
    expect(m.get('10.0.10.9')?.watch).toBe(1)
  })

  it('counts one entry naming the same address at both ends once', () => {
    const m = hostMarksFrom([], [watch({ source: { ip: '10.0.10.5' }, destIp: '10.0.10.5' } as Partial<WatchlistEntry>)])
    expect(m.get('10.0.10.5')?.watch).toBe(1)
  })
})

describe('mergeZoneHosts puts the marks on (#981)', () => {
  it('marks a registered host and a buffer-only one alike, and leaves an unmarked one at zero', () => {
    const marks = new Map([['10.0.10.1', { flags: 2, watch: 1, spike: true }]])
    const out = mergeZoneHosts([{ label: 'a', ip: '10.0.10.1' }, { label: 'b', ip: '10.0.10.2' }], [reg('10.0.10.2')], marks)
    expect(out.find((h) => h.ip === '10.0.10.1')).toMatchObject({ flags: 2, watch: 1, spike: true })
    expect(out.find((h) => h.ip === '10.0.10.2')).toMatchObject({ flags: 0, watch: 0, spike: false })
  })
})
