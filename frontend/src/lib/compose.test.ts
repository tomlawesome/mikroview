// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from 'vitest'
import { composeCommand, refusingCommentFor } from './compose'
import type { PolicyEdge } from './policy.svelte'

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
