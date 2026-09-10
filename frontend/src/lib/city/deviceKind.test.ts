// SPDX-License-Identifier: AGPL-3.0-only
//
// What these tests are, and what they are not.
//
// Every expectation below asserts a LABELLING rule: which shape the
// city draws for a host, given a name, a tag, or the ports and peers
// the log stream already carried. None of them asserts that a host IS
// the thing its shape depicts. The ratified record is explicit -- "a
// wrong shape is a labelling defect, never a data claim"
// (docs/design/screens/city/DESIGN.md) -- so a failure here means the
// city drew the wrong picture, not that mikroview identified a device
// wrongly. Mikroview identifies nothing: it never probes, and every
// input to these rules is something an operator typed or something that
// arrived in the log stream.
//
// That is also why the fallback matters more than any single rule: when
// nothing matches, the answer is the puck, which depicts nothing.

import { describe, expect, it } from 'vitest'
import { deviceKindFor, deviceKindVerdict } from './deviceKind'
import { DEVICE_KINDS } from './devices'

describe('deviceKindFor: the entities register wins', () => {
  it('takes a kind:<type> tag an operator recorded', () => {
    expect(deviceKindFor({ name: 'tv-lounge', tags: ['kind:server'] })).toBe('server')
  })

  it('takes a device:<type> tag too', () => {
    expect(deviceKindFor({ name: 'anything', tags: ['device:laptop'] })).toBe('laptop')
  })

  it('takes a bare kind name as a tag', () => {
    expect(deviceKindFor({ name: 'anything', tags: ['camera'] })).toBe('camera')
  })

  it('ignores tags that are not a kind, and falls through to the name', () => {
    expect(deviceKindFor({ name: 'nas', tags: ['upstairs', 'noisy'] })).toBe('server')
  })

  it('outranks the name, the router flag and the traffic shape', () => {
    const v = deviceKindVerdict({
      name: 'cam-porch',
      tags: ['kind:puck'],
      isRouter: true,
      traffic: { servedPorts: [554], talkedToBy: 40 },
    })
    expect(v).toMatchObject({ kind: 'puck', source: 'register' })
  })
})

describe('deviceKindFor: interfaces, tunnels and routers', () => {
  it('draws an interface or tunnel end as the gateway post', () => {
    expect(deviceKindVerdict({ name: 'wg0', isGateway: true })).toMatchObject({
      kind: 'post',
      source: 'gateway',
    })
  })

  it('draws the primary router as the plain router', () => {
    expect(deviceKindFor({ name: 'rb5009', isRouter: true, isPrimaryRouter: true })).toBe('router')
  })

  it('draws a second, downstream router with antennas', () => {
    expect(deviceKindFor({ name: 'hap-ax3', isRouter: true })).toBe('router-ant')
  })
})

describe('deviceKindFor: the name rules', () => {
  const cases: [string, string][] = [
    // interface and tunnel names, before anything else reads them
    ['ether1', 'post'],
    ['wg0', 'post'],
    ['l2tp', 'post'],
    ['bridge', 'post'],
    // camera
    ['cam-porch', 'camera'],
    ['camera_drive', 'camera'],
    ['IPCam-Yard', 'camera'],
    ['cctv-2', 'camera'],
    // server
    ['nas', 'server'],
    ['pihole', 'server'],
    ['dns-2', 'server'],
    ['media-server', 'server'],
    ['srv-backup', 'server'],
    ['unifi', 'server'],
    ['synology-01', 'server'],
    ['proxmox', 'server'],
    // phone
    ['phone-tom', 'phone'],
    ['iphone-anna', 'phone'],
    ['pixel-7', 'phone'],
    ['galaxy-s24', 'phone'],
    // tv
    ['tv-lounge', 'tv'],
    ['lounge.tv', 'tv'],
    ['chromecast-kitchen', 'tv'],
    ['roku', 'tv'],
    // laptop
    ['laptop-anna', 'laptop'],
    ['macbook-pro', 'laptop'],
    ['thinkpad-x1', 'laptop'],
    ['chromebook', 'laptop'],
    // workstation
    ['tom-desktop', 'workstation'],
    ['pc-bench', 'workstation'],
    ['workstation-3', 'workstation'],
    ['imac-studio', 'workstation'],
    // switch
    ['switch-hall', 'switch'],
    ['sw-office', 'switch'],
    ['crs326', 'switch'],
    // access point / second router
    ['ap-loft', 'router-ant'],
    ['wifi-garden', 'router-ant'],
    ['hap-ax3', 'router-ant'],
    ['cap-ac', 'router-ant'],
    // router
    ['router', 'router'],
    ['rb5009', 'router'],
    ['edgerouter-x', 'router'],
    ['pfsense', 'router'],
    ['gw', 'router'],
  ]

  for (const [name, kind] of cases) {
    it(`draws ${name} as the ${kind}`, () => {
      expect(deviceKindVerdict({ name })).toMatchObject({ kind, source: 'name' })
    })
  }

  it('matches a whole segment, not any substring', () => {
    // "tvheadend" is a recording server, not a television, and "atv" is
    // not a word this table claims to know: neither may become a TV on
    // a bare substring.
    expect(deviceKindFor({ name: 'atv' })).toBe('puck')
  })

  it('is case- and separator-insensitive', () => {
    expect(deviceKindFor({ name: 'CAM_Porch.01' })).toBe('camera')
  })
})

describe('deviceKindFor: the traffic-shape rules', () => {
  it('draws a host reached on RTSP as a camera', () => {
    expect(deviceKindVerdict({ name: 'unknown-1', traffic: { servedPorts: [554] } })).toMatchObject({
      kind: 'camera',
      source: 'traffic',
    })
  })

  it('draws a host reached on DNS as a server', () => {
    expect(deviceKindFor({ name: 'unknown-1', traffic: { servedPorts: [53] } })).toBe('server')
  })

  it('draws a host reached on a file or database port as a server', () => {
    expect(deviceKindFor({ name: 'unknown-1', traffic: { servedPorts: [445] } })).toBe('server')
    expect(deviceKindFor({ name: 'unknown-1', traffic: { servedPorts: [5432] } })).toBe('server')
  })

  it('draws a host many others ask for something as a server', () => {
    expect(deviceKindFor({ name: 'unknown-1', traffic: { talkedToBy: 4 } })).toBe('server')
  })

  it('needs enough askers before it says server', () => {
    expect(deviceKindFor({ name: 'unknown-1', traffic: { talkedToBy: 3 } })).toBe('puck')
  })

  it('lets the name win over the traffic shape', () => {
    // Both are guesses; a name is usually something a person chose for
    // the thing itself, so it goes first.
    expect(deviceKindVerdict({ name: 'phone-tom', traffic: { servedPorts: [53] } })).toMatchObject({
      kind: 'phone',
      source: 'name',
    })
  })

  it('ignores the ports a host used, only the ones it answered on', () => {
    // servedPorts is destination ports. A laptop asking a DNS server on
    // 53 never appears here, and so never turns into a server.
    expect(deviceKindFor({ name: 'unknown-1', traffic: { servedPorts: [] } })).toBe('puck')
  })
})

describe('deviceKindFor: the fallback', () => {
  it('is the puck when nothing is known at all', () => {
    expect(deviceKindVerdict({})).toMatchObject({ kind: 'puck', source: 'fallback' })
  })

  it('is the puck for a name no rule recognises', () => {
    expect(deviceKindFor({ name: 'esp-weather' })).toBe('puck')
    expect(deviceKindFor({ name: 'hue-bridge' })).toBe('puck')
    expect(deviceKindFor({ name: 'thermostat' })).toBe('puck')
    expect(deviceKindFor({ name: 'guest-e8b2' })).toBe('puck')
  })

  it('is the puck for a bare IP, which says nothing about a type', () => {
    expect(deviceKindFor({ name: '10.0.30.52' })).toBe('puck')
  })

  it('never guesses from an empty name', () => {
    expect(deviceKindFor({ name: '' })).toBe('puck')
    expect(deviceKindFor({ name: '   ' })).toBe('puck')
  })

  it('always answers with a kind the library can draw', () => {
    const inputs = [{}, { name: 'nas' }, { name: 'zzz', traffic: { talkedToBy: 99 } }]
    for (const input of inputs) {
      expect(DEVICE_KINDS).toContain(deviceKindFor(input))
    }
  })

  it('gives a reason that reads as a guess, never as a finding', () => {
    expect(deviceKindVerdict({ name: 'cam-porch' }).why).toBe('its name contains a camera word')
    expect(deviceKindVerdict({}).why).toBe('nothing mikroview saw suggests a type')
  })
})
