// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'

// jsdom has no matchMedia, which lib/viewport.svelte.ts reads at module
// load somewhere down the import chain.
vi.hoisted(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
})

import { wizardState } from './wizard.svelte'
import { wizardRun } from './wizardRun.svelte'
import type { SetupStatus } from './types'

// A2-F2 (#1365): wizardRun.certificateProblem is caStep's blocked reading
// for the address the wizard is using, or null when there is nothing to say.

function statusWith(over: Partial<SetupStatus['instance']>): SetupStatus {
  return {
    instance: {
      tlsEnabled: true,
      hosts: ['192.0.2.15'],
      effectiveHosts: ['192.0.2.15'],
      certificateCoversAddress: true,
      addressHost: '192.0.2.15',
      syslogPort: '6514',
      syslogEnabled: true,
      address: '192.0.2.15:8080',
      addressCandidates: [],
      backupTransport: 'sftp',
      ...over,
    },
    sources: [],
    devices: [],
    marks: [],
    witnesses: [],
    pushKinds: ['filter-rule'],
  }
}

const UNCOVERED = { hosts: [], effectiveHosts: ['localhost', '127.0.0.1'], certificateCoversAddress: false }

describe('wizardRun.certificateProblem', () => {
  beforeEach(() => {
    wizardState.reset()
    wizardRun.reset()
  })

  it('is null without a status, null when covered, and caStep’s blocked reading when not', () => {
    wizardState.status = null
    expect(wizardRun.certificateProblem).toBeNull()

    wizardState.address = '192.0.2.15:8080'
    wizardState.status = statusWith({})
    expect(wizardRun.certificateProblem).toBeNull()

    wizardState.status = statusWith(UNCOVERED)
    expect(wizardRun.certificateProblem).toEqual({
      state: 'blocked',
      detail: expect.stringContaining('MikroView\'s certificate does not cover 192.0.2.15'),
      pasteBlock: 'hosts: ["localhost", "127.0.0.1", "192.0.2.15"]',
    })

    // An empty address is not a mismatch to report.
    wizardState.address = ''
    wizardState.status = statusWith(UNCOVERED)
    expect(wizardRun.certificateProblem).toBeNull()
  })
})
