// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { composeCommand, reachComposeInput, refusingCommentFor } from './compose'
import type { PolicyEdge } from './policy.svelte'
import type { ReachStrand } from './reach'

const base = {
  hostIp: '10.0.20.31',
  direction: 'out' as const,
  target: '10.0.40.10',
  port: 445,
  proto: 'tcp',
  mode: 'allow' as const,
  hostName: 'cam-porch',
  targetName: 'nas',
}

describe('composeCommand', () => {
  it('drafts the allow with the host as source, logged and named', () => {
    const cmd = composeCommand({ ...base, placeBefore: 'iot-to-lan-drop' })
    expect(cmd).toContain('src-address=10.0.20.31 dst-address=10.0.40.10')
    expect(cmd).toContain('protocol=tcp dst-port=445 action=accept log=yes')
    expect(cmd).toContain('log-prefix="cam-porch-nas-445"')
    expect(cmd).toContain('place-before=[find comment="iot-to-lan-drop"]')
  })

  it('an inbound strand swaps the ends', () => {
    const cmd = composeCommand({ ...base, direction: 'in' })
    expect(cmd).toContain('src-address=10.0.40.10 dst-address=10.0.20.31')
  })

  it('the named block drops, still logged, and takes no place-before', () => {
    const cmd = composeCommand({ ...base, mode: 'block', placeBefore: 'iot-to-lan-drop' })
    expect(cmd).toContain('action=drop log=yes')
    expect(cmd).toContain('comment="named block: cam-porch → nas :445"')
    expect(cmd).not.toContain('place-before')
  })

  it('a subnet scope rides as-is', () => {
    const cmd = composeCommand({ ...base, target: '10.0.40.0/24' })
    expect(cmd).toContain('dst-address=10.0.40.0/24')
  })

  it('names stay a safe slug in the prefix', () => {
    const cmd = composeCommand({ ...base, hostName: 'Weird "Host"!', targetName: 'the internet' })
    expect(cmd).toMatch(/log-prefix="weird-host-the-internet-445"/)
  })

  it('escapes a hostname carrying a RouterOS command substitution into the comment', () => {
    const cmd = composeCommand({ ...base, hostName: 'x$[/system reset-configuration]"\\', placeBefore: 'iot-to-lan-drop' })
    expect(cmd).not.toBeNull()
    // The dangerous run must be escaped in the printed comment: every
    // `$[` in the line is preceded by the escaping backslash, so none
    // of them are left for RouterOS to expand when the line is pasted.
    expect(cmd).not.toMatch(/(?<!\\)\$\[/)
    expect(cmd).toContain('x\\$[/system reset-configuration]\\"\\\\')
  })

  it('escapes a quote in placeBefore\'s own comment match', () => {
    const cmd = composeCommand({ ...base, placeBefore: 'drop "iot"' })
    expect(cmd).not.toBeNull()
    expect(cmd).toContain('place-before=[find comment="drop \\"iot\\""]')
  })

  it('refuses to compose when hostIp is not an address or CIDR', () => {
    const cmd = composeCommand({ ...base, hostIp: '1.2.3.4 dst-address=0.0.0.0/0' })
    expect(cmd).toBeNull()
  })

  it('refuses to compose when target is not an address or CIDR', () => {
    const cmd = composeCommand({ ...base, target: '10.0.40.10; /system reset-configuration' })
    expect(cmd).toBeNull()
  })

  it('refuses to compose when the protocol is not a plain RouterOS protocol name or number', () => {
    expect(composeCommand({ ...base, proto: 'tcp action=drop dst-port=0 place-before=[find]' })).toBeNull()
    expect(composeCommand({ ...base, proto: 'tcp;' })).toBeNull()
    expect(composeCommand({ ...base, proto: '' })).toBeNull()
    expect(composeCommand({ ...base, proto: 'ipv6-icmp' })).toContain('protocol=ipv6-icmp ')
    expect(composeCommand({ ...base, proto: '47' })).toContain('protocol=47 ')
  })

  it('refuses to compose when the port is not a whole number in range', () => {
    expect(composeCommand({ ...base, port: 1.5 })).toBeNull()
    expect(composeCommand({ ...base, port: 70000 })).toBeNull()
    expect(composeCommand({ ...base, port: '22 action=drop' as unknown as number })).toBeNull()
  })

  it('refuses to compose when a name carries a line break or control character', () => {
    expect(composeCommand({ ...base, hostName: 'nas\n/system reset-configuration' })).toBeNull()
    expect(composeCommand({ ...base, targetName: 'x\u2028y' })).toBeNull()
    expect(composeCommand({ ...base, placeBefore: 'drop\r' })).toBeNull()
  })

  it('still composes for a valid IPv4 address', () => {
    const cmd = composeCommand({ ...base, hostIp: '10.0.20.31', target: '10.0.40.10' })
    expect(cmd).toContain('src-address=10.0.20.31 dst-address=10.0.40.10')
  })

  it('still composes for a valid IPv4 CIDR target', () => {
    const cmd = composeCommand({ ...base, target: '10.0.40.0/24' })
    expect(cmd).toContain('dst-address=10.0.40.0/24')
  })

  it('still composes for a valid IPv6 address', () => {
    const cmd = composeCommand({ ...base, hostIp: 'fe80::1', target: '2001:db8::1' })
    expect(cmd).toContain('src-address=fe80::1 dst-address=2001:db8::1')
  })
})

describe('refusingCommentFor', () => {
  const edge = (over: Partial<PolicyEdge>): PolicyEdge => ({
    key: 'a|b',
    from: 'a',
    to: 'b',
    accepted: false,
    refused: true,
    acceptPorts: [],
    refusePorts: [],
    comment: 'the drop',
    ruleCount: 1,
    logged: false,
    ...over,
  })

  it('returns the refusing rule comment for the pair', () => {
    expect(refusingCommentFor([edge({})], 'a', 'b')).toBe('the drop')
  })

  it('nothing without a refusal or a comment', () => {
    expect(refusingCommentFor([edge({ refused: false })], 'a', 'b')).toBeUndefined()
    expect(refusingCommentFor([edge({ comment: '' })], 'a', 'b')).toBeUndefined()
    expect(refusingCommentFor([], 'a', 'b')).toBeUndefined()
  })
})

describe('reachComposeInput (#868: one strand-to-command translation for both views)', () => {
  const strand = (over: Partial<ReachStrand> = {}): ReachStrand => ({
    key: 'vlan-srv|out|blocked',
    counterpart: 'vlan-srv',
    outcome: 'blocked',
    direction: 'out',
    peers: ['nas'],
    peerAddrs: ['10.0.40.10'],
    ports: [445],
    portHits: [{ port: 445, n: 14, proto: 'tcp' }],
    count: 14,
    weight: 14,
    refusedBy: 'iot-to-lan-drop',
    ...over,
  })

  const ctx = {
    hostIp: '10.0.20.31',
    hostName: 'cam-porch',
    zoneId: 'vlan-iot',
    wanInterface: 'ether1',
    zones: [{ id: 'vlan-srv', cidr: '10.0.40.0/24', name: 'Servers' }],
    edges: [] as PolicyEdge[],
  }

  it('drafts the busiest port, host-scoped, allow, with no overrides', () => {
    const input = reachComposeInput(strand(), ctx)
    expect(input).toEqual({
      hostIp: '10.0.20.31',
      direction: 'out',
      target: '10.0.40.10',
      port: 445,
      proto: 'tcp',
      mode: 'allow',
      hostName: 'cam-porch',
      targetName: 'nas',
      placeBefore: undefined,
    })
  })

  it('nothing to draft from: no port hit and no free-typed one, or no far-side address', () => {
    expect(reachComposeInput(strand({ portHits: [] }), ctx)).toBeNull()
    expect(reachComposeInput(strand({ peerAddrs: [] }), ctx)).toBeNull()
  })

  it('a chosen port overrides the default busiest one', () => {
    const s = strand({ portHits: [{ port: 445, n: 14, proto: 'tcp' }, { port: 22, n: 2, proto: 'tcp' }] })
    expect(reachComposeInput(s, ctx, { port: 22 })?.port).toBe(22)
  })

  it('a free-typed port wins over both the default and a chosen one', () => {
    expect(reachComposeInput(strand(), ctx, { port: 22, free: '8080' })?.port).toBe(8080)
  })

  it('block mode carries through', () => {
    expect(reachComposeInput(strand(), ctx, { mode: 'block' })?.mode).toBe('block')
  })

  it('subnet scope targets the zone\'s cidr and names the zone', () => {
    const input = reachComposeInput(strand(), ctx, { scope: 'subnet' })
    expect(input?.target).toBe('10.0.40.0/24')
    expect(input?.targetName).toBe('Servers')
  })

  it('the internet counterpart falls back to the WAN interface and "the internet"', () => {
    // The refusing edge sits on the iot->WAN pair, so placeBefore only
    // resolves if the counterpart really became ctx.wanInterface.
    const edges: PolicyEdge[] = [
      { key: 'vlan-iot|ether1', from: 'vlan-iot', to: 'ether1', accepted: false, refused: true, acceptPorts: [], refusePorts: [], comment: 'iot-to-wan-drop', ruleCount: 1, logged: false },
    ]
    const input = reachComposeInput(strand({ counterpart: 'internet', peers: [] }), { ...ctx, edges })
    expect(input?.targetName).toBe('the internet')
    expect(input?.placeBefore).toBe('iot-to-wan-drop')
  })

  it('places the allow before the pushed table\'s own refusing rule on this pair', () => {
    const edges: PolicyEdge[] = [
      { key: 'vlan-iot|vlan-srv', from: 'vlan-iot', to: 'vlan-srv', accepted: false, refused: true, acceptPorts: [], refusePorts: [], comment: 'iot-to-lan-drop', ruleCount: 1, logged: false },
    ]
    expect(reachComposeInput(strand(), { ...ctx, edges })?.placeBefore).toBe('iot-to-lan-drop')
  })

  it('a strand and its default draft print the exact same line composeCommand builds directly', () => {
    const input = reachComposeInput(strand(), ctx)!
    const direct = composeCommand({ hostIp: '10.0.20.31', direction: 'out', target: '10.0.40.10', port: 445, proto: 'tcp', mode: 'allow', hostName: 'cam-porch', targetName: 'nas' })
    expect(composeCommand(input)).toBe(direct)
  })
})
