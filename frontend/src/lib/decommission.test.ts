import { describe, expect, it } from 'vitest'
import {
  boroughLabel,
  forceRemoveContract,
  ghostNote,
  ghostStateOf,
  ghostTally,
  hoursOfWindow,
  offerHeadline,
  offerSummary,
  receiptSentence,
  retiredNote,
  retiresAt,
  stragglerCallout,
  stragglerHeadline,
  stragglerProvenance,
  stragglerReading,
  undoable,
  watchlistRowText,
} from './decommission'
import { formatDayMonth } from './format'
import type { DecommissionOffer, DecommissionReceipt, DecommissionWatch } from './types'

const HOUR = 3_600_000
const NS = 1_000_000

// Round 55's own story: Garage, 10.0.70.0/24, gone from the router at
// 22:07, three last-known hosts, a straggler at 22:41 from .14.
const departed = '2026-08-31T22:07:00Z'

const watch = (over: Partial<DecommissionWatch> = {}): DecommissionWatch => ({
  id: 'w1',
  cidr: '10.0.70.0/24',
  device: 'rb5009',
  interface: 'garage',
  name: 'Garage',
  createdAt: departed,
  cleanWindow: 6 * HOUR * NS,
  trafficCount: 0,
  covered: true,
  detached: false,
  replayCount: 3,
  replaySpan: 24 * HOUR * NS,
  state: 'holding',
  retiresIn: '6h0m0s',
  coverage: 'covered',
  lastKnown: [
    { address: '10.0.70.14', name: 'garage-cam', source: 'dhcp lease', seenAt: '2026-08-01T09:00:00Z' },
    { address: '10.0.70.20', name: 'garage-door', source: 'dhcp lease', seenAt: '2026-08-01T09:00:00Z' },
  ],
  ...over,
})

const offer = (over: Partial<DecommissionOffer> = {}): DecommissionOffer => ({
  device: 'rb5009',
  cidr: '10.0.70.0/24',
  address: '10.0.70.1/24',
  interface: 'garage',
  name: 'Garage',
  departedAt: departed,
  coverage: 'covered',
  suggested: { cleanWindow: '6h0m0s' },
  lastKnown: [
    { address: '10.0.70.14', name: 'garage-cam', seenAt: '2026-08-01T09:00:00Z' },
    { address: '10.0.70.20', name: 'garage-door', seenAt: '2026-08-01T09:00:00Z' },
    { address: '10.0.70.31', name: 'ev-charger', seenAt: '2026-08-01T09:00:00Z' },
  ],
  ...over,
})

const receipt = (over: Partial<DecommissionReceipt> = {}): DecommissionReceipt => ({
  emissionCount: 3,
  start: '2026-08-30T22:07:00Z',
  end: departed,
  duration: '24h0m0s',
  eventCount: 41_233,
  truncated: false,
  ...over,
})

describe('the ghost s state', () => {
  const now = Date.parse('2026-09-01T03:52:00Z')

  it('paints an unanswered offer grey and a held watch purple', () => {
    expect(ghostStateOf(null, now)).toBe('none')
    expect(ghostStateOf(watch({ state: 'holding' }), now)).toBe('holding')
  })

  it('paints a straggler red as it arrives', () => {
    const s = watch({ state: 'draining', trafficCount: 3, lastTrafficAt: '2026-09-01T03:41:00Z' })
    expect(ghostStateOf(s, now)).toBe('broken')
  })

  it('goes back to holding once the ghost can claim an hour of quiet', () => {
    // Round 55 draws exactly this: 22:41 red, and the same watch with
    // three stragglers behind it purple again at 03:52. The alarm is the
    // arrival, not the history.
    const s = watch({ state: 'draining', trafficCount: 3, lastTrafficAt: '2026-08-31T23:48:00Z' })
    expect(ghostStateOf(s, now)).toBe('holding')
  })

  it('paints a watch nothing can feed with the same alarm as a straggler', () => {
    // Two different failures, one ink: to the operator both mean this
    // retired range is not safely quiet. The tally says which it is.
    expect(ghostStateOf(watch({ state: 'broken', covered: false }), now)).toBe('broken')
  })

  it('paints nothing once the watch has retired -- the ghost has left the map', () => {
    expect(ghostStateOf(watch({ state: 'retired' }), now)).toBe('none')
  })
})

describe('the quiet clock', () => {
  it('counts from the last straggler, not from when the watch was created', () => {
    const w = watch({ lastTrafficAt: '2026-08-31T23:48:00Z' })
    const now = Date.parse('2026-09-01T03:52:00Z')
    expect(hoursOfWindow(w, now)).toBe('4 h of 6 h')
  })

  it('counts from creation while nothing has ever straggled', () => {
    expect(hoursOfWindow(watch(), Date.parse('2026-09-01T00:07:00Z'))).toBe('2 h of 6 h')
  })

  it('never claims more quiet than the window asks for', () => {
    expect(hoursOfWindow(watch(), Date.parse('2026-09-02T00:00:00Z'))).toBe('6 h of 6 h')
  })

  it('says when the watch retires if nothing else arrives', () => {
    expect(retiresAt(watch({ lastTrafficAt: '2026-08-31T23:48:00Z' }))).toMatch(/:48$/)
  })
})

describe('the tally lines', () => {
  it('says what left, and how many were on it, before anything is watched', () => {
    expect(ghostTally('none', null, offer(), Date.now())).toMatch(/^gone at \d\d:\d\d · 3 hosts were here$/)
    expect(ghostNote('none', null, offer(), Date.now())).toMatch(/^gone from the router · \d\d:\d\d$/)
  })

  it('counts the quiet while the watch holds', () => {
    const now = Date.parse('2026-09-01T03:52:00Z')
    const w = watch({ lastTrafficAt: '2026-08-31T23:48:00Z' })
    expect(ghostTally('holding', w, null, now)).toMatch(/holding · 4 h of 6 h$/)
    expect(ghostNote('holding', w, null, now)).toMatch(/watch holding · quiet 4 h of 6 h$/)
  })

  it('counts the lines once a straggler has broken it', () => {
    const w = watch({ state: 'draining', trafficCount: 3, lastTrafficAt: '2026-08-31T22:41:00Z' })
    expect(ghostTally('broken', w, null, Date.now())).toMatch(/BROKEN · 3 lines$/)
    expect(ghostNote('broken', w, null, Date.now())).toMatch(/WATCH BROKEN · 3 lines since$/)
  })

  it('says the other reason for broken rather than borrowing a line count it has not got', () => {
    const w = watch({ state: 'broken', covered: false, trafficCount: 0 })
    expect(ghostTally('broken', w, null, Date.now())).toMatch(/BROKEN · nothing logs this range$/)
  })

  it('counts a ghost beside its borough s districts, not among them', () => {
    expect(boroughLabel('RB5009 BOROUGH', 4, 1)).toBe('RB5009 BOROUGH · 4 DISTRICTS · 1 GHOST')
    expect(boroughLabel('RB5009 BOROUGH', 4, 0)).toBe('RB5009 BOROUGH · 4 DISTRICTS')
    expect(boroughLabel('HAP BOROUGH', 1, 2)).toBe('HAP BOROUGH · 1 DISTRICT · 2 GHOSTS')
  })
})

describe('the receipt', () => {
  it('names the count, the span it was measured over, and who it caught', () => {
    const r = receipt({ addresses: [{ address: '10.0.70.14', name: 'garage-cam', count: 3 }] })
    expect(receiptSentence(r)).toBe(
      'in the last 24 h a watch here would have caught 3 lines — all from 10.0.70.14, last named garage-cam',
    )
  })

  it('says the span it actually covered on a young instance, not a flattering one', () => {
    const r = receipt({ start: '2026-08-31T19:07:00Z', emissionCount: 1, addresses: [{ address: '10.0.70.14', count: 1 }] })
    expect(receiptSentence(r)).toBe('in the last 3 h a watch here would have caught 1 line — all from 10.0.70.14')
  })

  it('drops to the count and the span where the replay kept no addresses', () => {
    expect(receiptSentence(receipt())).toBe('in the last 24 h a watch here would have caught 3 lines')
  })

  it('reports an empty replay as empty rather than saying nothing', () => {
    expect(receiptSentence(receipt({ emissionCount: 0 }))).toBe('in the last 24 h a watch here would have caught nothing')
  })
})

describe('the offer', () => {
  it('leads with what left', () => {
    expect(offerHeadline(offer())).toBe('Garage has left the router')
    expect(offerHeadline(offer({ name: '' }))).toBe('garage has left the router')
  })

  it('says what the push stopped carrying', () => {
    expect(offerSummary(offer())).toMatch(/^the \d\d:\d\d push no longer carries it — the address and 3 leases are gone$/)
    expect(offerSummary(offer({ lastKnown: [] }))).toMatch(/the address is gone$/)
  })
})

describe('the straggler', () => {
  const s = {
    address: '10.0.70.14',
    peer: '10.0.20.9',
    protocol: 'tcp',
    port: 554,
    interface: 'iot',
    rule: '#26',
    action: 'accept',
    at: '2026-08-31T22:41:00Z',
  }
  const broken = watch({ state: 'draining', trafficCount: 3, lastTrafficAt: s.at, lastStraggler: s })

  it('says the finding in one line: legal traffic, dead range', () => {
    expect(stragglerHeadline(broken, s)).toMatch(/^a retired range talking · 554\/tcp · 3 lines since \d\d:\d\d · last \d\d:\d\d$/)
  })

  it('names what the address was, where it arrived, and what judged it', () => {
    expect(stragglerProvenance(broken, s)).toBe(`was garage-cam until ${formatDayMonth('2026-08-01T09:00:00Z')} (dhcp lease, rb5009) · arrived on iot · accepted by #26`)
  })

  it('shortens rather than guessing where no push supplied a clause', () => {
    const bare = { ...s, interface: '', rule: '', address: '10.0.70.99' }
    expect(stragglerProvenance(broken, bare)).toBe('')
    // And with no name ever pushed, it will not call it a device.
    expect(stragglerReading(broken, bare)).toBe('')
    expect(stragglerReading(broken, s)).toBe('a device with a static address the move did not touch')
  })

  it('draws the callout as the pair, then the enrichment', () => {
    const { head, detail } = stragglerCallout(broken, s, 'nas')
    expect(head).toBe('STRAGGLER · 10.0.70.14 → nas · tcp/554')
    expect(detail).toBe(`was ‘garage-cam’ until ${formatDayMonth('2026-08-01T09:00:00Z')} · in: iot · 3×`)
  })

  it('says broken with its line count on the watchlist row', () => {
    expect(watchlistRowText(broken, Date.parse('2026-08-31T22:45:00Z'))).toBe('broken · 3 lines')
    expect(watchlistRowText(watch({ lastTrafficAt: '2026-08-31T23:48:00Z' }), Date.parse('2026-09-01T03:52:00Z'))).toBe(
      'holding · 4 h of 6 h',
    )
  })
})

describe('retirement', () => {
  it('leaves one note line saying what happened', () => {
    const w = watch({ state: 'retired', retiredAt: '2026-09-01T04:11:00Z' })
    expect(retiredNote(w)).toMatch(
      /^Garage · 10\.0\.70\.0\/24 retired at \d\d:\d\d — quiet for 6 h — the ghost has left the map and the watch is closed$/,
    )
  })

  it('offers undo only while the server says the hour is still open', () => {
    const w = watch({ state: 'retired', retiredAt: '2026-09-01T04:11:00Z', undoableUntil: '2026-09-01T05:11:00Z' })
    expect(undoable(w, Date.parse('2026-09-01T04:30:00Z'))).toBe(true)
    expect(undoable(w, Date.parse('2026-09-01T05:12:00Z'))).toBe(false)
    expect(undoable(watch(), Date.now())).toBe(false)
  })

  it('states the whole force-remove contract before it is pressed', () => {
    const now = Date.parse('2026-09-01T03:52:00Z')
    const c = forceRemoveContract(watch({ lastTrafficAt: '2026-08-31T23:48:00Z' }), now)
    expect(c).toContain('The ghost leaves the map now')
    expect(c).toContain('4 h of 6 h of quiet still to go')
    expect(c).toContain('can only be forgotten from there')
  })
})
