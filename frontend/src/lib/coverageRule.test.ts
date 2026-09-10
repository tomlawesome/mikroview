import { describe, expect, it } from 'vitest'
import { boundaryCoverage, edgeCoverage } from './coverageRule'
import type { PolicyEdge } from './policy.svelte'

const edge = (from: string, to: string, logged: boolean): PolicyEdge =>
  ({ key: from + '|' + to, from, to, accepted: true, refused: false, acceptPorts: [], refusePorts: [], comment: '', ruleCount: 1, logged }) as PolicyEdge

describe('the coverage rule', () => {
  it('reads one direction: logged, else declared quiet, else dark', () => {
    expect(edgeCoverage(edge('a', 'b', true), new Set())).toBe('logged')
    expect(edgeCoverage(edge('a', 'b', false), new Set(['a|b']))).toBe('quiet')
    expect(edgeCoverage(edge('a', 'b', false), new Set())).toBe('dark')
    // A declaration on the other direction says nothing about this one.
    expect(edgeCoverage(edge('a', 'b', false), new Set(['b|a']))).toBe('dark')
  })

  it('reads a whole lane from every direction that names it', () => {
    const both = [edge('lan', 'ether1', false), edge('ether1', 'lan', false)]
    expect(boundaryCoverage('lan', both, new Set(['lan|ether1', 'ether1|lan']))).toBe('quiet')
    expect(boundaryCoverage('lan', both, new Set(['lan|ether1']))).toBe('dark')
    expect(boundaryCoverage('lan', both, new Set())).toBe('dark')
  })

  it('lets anything logging on the lane light it', () => {
    const mixed = [edge('lan', 'ether1', true), edge('ether1', 'lan', false)]
    expect(boundaryCoverage('lan', mixed, new Set())).toBe('logged')
  })

  it('calls a lane the pushed table never names dark', () => {
    expect(boundaryCoverage('vlan-guest', [edge('lan', 'ether1', true)], new Set(['vlan-guest|ether1']))).toBe('dark')
  })
})
