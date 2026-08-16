// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import {
  caStep,
  caTrustCommands,
  certificateCovers,
  hostname,
  portOf,
  pushBlock,
  pushScript,
  pushStep,
  rulesStep,
  syslogCommands,
  syslogStep,
} from './setupsteps'
import type { SetupStatus } from './types'

function status(over: Partial<SetupStatus> = {}): SetupStatus {
  return {
    instance: { tlsEnabled: true, hosts: ['192.0.2.10'], syslogPort: ':6514', syslogEnabled: true },
    sources: [],
    devices: [],
    pushKinds: ['filter-rule', 'address-list', 'dhcp-lease', 'arp'],
    ...over,
  }
}

describe('address handling', () => {
  it('strips ports, including from IPv6 literals', () => {
    expect(hostname('192.0.2.10:8080')).toBe('192.0.2.10')
    expect(hostname('192.0.2.10')).toBe('192.0.2.10')
    expect(hostname('[2001:db8::1]:8080')).toBe('2001:db8::1')
  })

  it('takes the port off a listen address', () => {
    expect(portOf(':6514')).toBe('6514')
    expect(portOf('0.0.0.0:6514')).toBe('6514')
  })
})

describe('certificate cover check', () => {
  it('accepts an address the certificate names', () => {
    expect(certificateCovers(status(), '192.0.2.10:8080')).toBe(true)
  })

  // The failure the owner actually hit: reaching MikroView by an address
  // tls.hosts does not list. It surfaces three steps later as
  // "name verification failed", pointing at the router.
  it('rejects an address the certificate does not name', () => {
    expect(certificateCovers(status(), '192.0.2.99:8080')).toBe(false)
  })

  it('falls back to localhost/127.0.0.1 when tls.hosts is unset', () => {
    const s = status({ instance: { tlsEnabled: true, hosts: [], syslogPort: ':6514', syslogEnabled: true } })
    expect(certificateCovers(s, '127.0.0.1:8080')).toBe(true)
    expect(certificateCovers(s, '192.168.1.5:8080')).toBe(false)
  })

  it('is not a question when TLS is off', () => {
    const s = status({ instance: { tlsEnabled: false, hosts: [], syslogPort: ':6514', syslogEnabled: true } })
    expect(certificateCovers(s, 'anything:8080')).toBe(true)
  })
})

describe('generated commands', () => {
  // The wizard must never emit a placeholder: a saved script still
  // containing <mikroview-host> fails much later, somewhere else.
  const placeholders = /<[a-z-]+>/

  it('fills in the address for the CA fetch', () => {
    const cmd = caTrustCommands('192.0.2.10:8080')
    expect(cmd).toContain('https://192.0.2.10:8080/ca.crt')
    expect(cmd).not.toMatch(placeholders)
  })

  it('uses the configured syslog port, not an assumed one', () => {
    expect(syslogCommands('192.0.2.10:8080', ':16514')).toContain('remote-port=16514')
  })

  it('sends syslog to the host without the web port', () => {
    const cmd = syslogCommands('192.0.2.10:8080', ':6514')
    expect(cmd).toContain('remote=192.0.2.10')
    expect(cmd).not.toContain('remote=192.0.2.10:8080')
  })

  it('embeds the token in every push block', () => {
    const script = pushScript('192.0.2.10:8080', 'tok-123', ['filter-rule', 'arp'])
    expect(script.match(/Bearer tok-123/g)).toHaveLength(2)
    expect(script).not.toMatch(placeholders)
  })

  it('renames RouterOS fields to mikroview names', () => {
    const block = pushBlock('h', 't', 'filter-rule')
    expect(block).toContain('"logPrefix"=($v->"log-prefix")')
    expect(block).toContain('"srcAddressList"=($v->"src-address-list")')
    // #408's fields. connection-state is a set, passed through as the
    // array RouterOS sends rather than joined by the script.
    expect(block).toContain('"connectionState"=($v->"connection-state")')
    expect(block).toContain('"inInterface"=($v->"in-interface")')
    expect(block).toContain('"outInterface"=($v->"out-interface")')
    // The wrapping that makes it a list of records rather than one
    // merged map -- silently wrong without it.
    expect(block).toContain('{$rec}')
  })

  it('reports the router version on every block, on the payload not a record', () => {
    const script = pushScript('h', 't', ['filter-rule', 'arp'])
    expect(script.match(/"routerosVersion"=\[\/system\/resource get version\]/g)).toHaveLength(2)
    // On the envelope beside kind/page/pages -- never inside the
    // per-record map, which describes a rule and not the router.
    expect(script).not.toContain('"routerosVersion"=[/system/resource get version]; "comment"')
  })

  it('gives each block its own variables, since they share one script', () => {
    const script = pushScript('h', 't', ['filter-rule', 'arp'])
    expect(script).toContain('ruleRecs')
    expect(script).toContain('arpRecs')
  })

  it('emits nothing for a kind it does not know', () => {
    expect(pushBlock('h', 't', 'not-a-kind')).toBe('')
  })
})

describe('step status', () => {
  it('blocks the CA step when the certificate cannot cover the address', () => {
    const s = caStep(status(), '192.0.2.99:8080')
    expect(s.state).toBe('blocked')
    expect(s.detail).toContain('tls.hosts')
  })

  it('reports the CA step done once something has fetched it', () => {
    const s = caStep(status({ sources: [{ source: '192.0.2.1', caFetchedAt: '2026-08-13T00:00:00Z' }] }), '192.0.2.10')
    expect(s.state).toBe('done')
  })

  it('blocks the syslog step when the listener is switched off', () => {
    const s = syslogStep(status({ instance: { tlsEnabled: true, hosts: [], syslogPort: '', syslogEnabled: false } }))
    expect(s.state).toBe('blocked')
    expect(s.detail).toContain('listen.syslogTls')
  })

  // Connected but silent is a real, common state -- and it is not the
  // same as "cannot reach me", which is why the connection is observed
  // separately from events.
  it('separates connected-but-no-events from no connection', () => {
    const connected = status({ sources: [{ source: '1.2.3.4', syslogFirstSeenAt: '2026-08-13T00:00:00Z' }] })
    expect(syslogStep(connected).state).toBe('done')
    expect(rulesStep(connected).state).toBe('waiting')
    expect(rulesStep(connected).detail).toContain('log=yes')
  })

  // Events with no decoded action look completely healthy on every
  // other measure. This is the state the wizard exists to name.
  it('flags events arriving with no decoded action', () => {
    const s = rulesStep(
      status({ devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 40, decodedActions: 0 }] }),
    )
    expect(s.state).toBe('partial')
    expect(s.detail).toContain('log-prefix')
  })

  it('reports partial tagging honestly', () => {
    const s = rulesStep(
      status({ devices: [{ device: 'r', configured: true, sourceIp: '1.2.3.4', events: 10, decodedActions: 4 }] }),
    )
    expect(s.state).toBe('partial')
    expect(s.detail).toContain('4 of 10')
  })

  it('names which push blocks are missing rather than just "incomplete"', () => {
    const s = pushStep(
      status({
        devices: [
          {
            device: 'r',
            configured: true,
            sourceIp: '1.2.3.4',
            events: 1,
            decodedActions: 1,
            pushedKinds: { 'filter-rule': '2026-08-13T00:00:00Z' },
          },
        ],
      }),
    )
    expect(s.state).toBe('partial')
    expect(s.detail).toContain('address-list')
    expect(s.detail).toContain('dhcp-lease')
  })
})
