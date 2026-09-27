// SPDX-License-Identifier: AGPL-3.0-only
//
// #1347: the Engine Room's Config card -- what it says before and after
// the editor has been opened, and that opening asks for the password
// inline and hands it to POST /api/config/editor/open.

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  openConfigEditor: vi.fn(),
  configEditorSummary: vi.fn(),
  validateConfig: vi.fn(async () => ({ problems: [] })),
  fetchConfigSnapshots: vi.fn(async () => ({ snapshots: [], keep: 5 })),
  carryForwardConfig: vi.fn(),
  createConfigSnapshot: vi.fn(),
  deleteConfigSnapshot: vi.fn(),
  downloadConfig: vi.fn(),
  fetchConfigSnapshot: vi.fn(),
  revealConfigSecrets: vi.fn(),
}))
vi.mock('../lib/export', () => ({ saveBlob: vi.fn() }))

import { configEditorSummary, fetchConfigSnapshots, openConfigEditor } from '../lib/api'
import { configEditorState } from '../lib/configEditor.svelte'
import type { ConfigEditorOpen, ConfigEditorSummary } from '../lib/types'
import ConfigCard from './ConfigCard.svelte'

function opened(overrides: Partial<ConfigEditorOpen> = {}): ConfigEditorOpen {
  return {
    text: '# MikroView configuration\nlisten:\n  http: ":8080"\n',
    path: '/etc/mikroview/config.yaml',
    changedSinceStart: false,
    header: { schema: 7, writtenBy: 'mikroview v0.6.1', layout: 1 },
    schemaGuess: 7,
    runningVersion: 'v0.7.0',
    runningSchema: 8,
    ...overrides,
  }
}

function summary(overrides: Partial<ConfigEditorSummary> = {}): ConfigEditorSummary {
  return {
    path: '/etc/mikroview/config.yaml',
    header: { schema: 7, writtenBy: 'mikroview v0.6.1', layout: 1 },
    schemaGuess: 7,
    runningVersion: 'v0.7.0',
    runningSchema: 8,
    snapshotCount: 2,
    changedSinceStart: false,
    ...overrides,
  }
}

async function settle() {
  for (let i = 0; i < 4; i++) await Promise.resolve()
  flushSync()
}

function rowValue(label: string): string {
  const row = Array.from(document.querySelectorAll('.orow')).find(
    (r) => r.querySelector(':scope > span')?.textContent?.trim() === label,
  )
  return (row?.querySelector('.ov')?.textContent ?? '').replace(/\s+/g, ' ').trim()
}

describe('ConfigCard (#1347)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    configEditorState.reset()
    vi.mocked(fetchConfigSnapshots).mockResolvedValue({ snapshots: [], keep: 5 })
    vi.mocked(configEditorSummary).mockResolvedValue(summary())
  })

  it('shows the snapshot count and the last-kept count from the server', async () => {
    vi.mocked(fetchConfigSnapshots).mockResolvedValue({
      keep: 3,
      snapshots: [
        { id: 'a', when: '2026-09-27T10:00:00Z', by: 'tom', schema: 7, version: 'mikroview v0.6.1', why: 'manual' },
        { id: 'b', when: '2026-09-26T10:00:00Z', by: 'tom', schema: 7, version: '', why: 'before-carry-forward' },
      ],
    })
    render(ConfigCard)
    await settle()
    expect(rowValue('snapshots')).toBe('2 kept (the last 3 are kept)')
  })

  it('shows the file, header and schema from the summary route before the editor is opened', async () => {
    vi.mocked(configEditorSummary).mockResolvedValue(summary({ header: null, schemaGuess: 4, runningSchema: 8 }))
    render(ConfigCard)
    await settle()
    expect(configEditorSummary).toHaveBeenCalled()
    expect(rowValue('file')).toBe('/etc/mikroview/config.yaml')
    expect(rowValue('written by')).toBe('no header — written before v0.7')
    expect(rowValue('schema')).toBe('looks like 4 · this version reads 8')
    expect(screen.getByRole('button', { name: 'Open the editor' })).toBeTruthy()
  })

  it('says so when the summary route fails, rather than pretending nothing is known', async () => {
    vi.mocked(configEditorSummary).mockResolvedValue('the server could not do that (500)')
    render(ConfigCard)
    await settle()
    expect(rowValue('file')).toBe('unknown — the server did not answer')
  })

  it('asks for the password inline and opens the editor with it', async () => {
    vi.mocked(openConfigEditor).mockResolvedValue(opened())
    render(ConfigCard)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'Open the editor' }))
    const field = screen.getByLabelText('Password') as HTMLInputElement
    const go = screen.getByRole('button', { name: 'Open' }) as HTMLButtonElement
    expect(go.disabled).toBe(true)
    await fireEvent.input(field, { target: { value: 'hunter2' } })
    await fireEvent.click(go)
    await settle()

    expect(openConfigEditor).toHaveBeenCalledWith('hunter2')
    expect(configEditorState.visible).toBe(true)
    expect(configEditorState.text).toContain('listen:')
    expect(rowValue('file')).toBe('/etc/mikroview/config.yaml')
    expect(rowValue('written by')).toBe('mikroview v0.6.1')
    expect(rowValue('schema')).toBe('7 · this version reads 8')
    expect(screen.queryByLabelText('Password')).toBeNull()
  })

  it('shows a refused password under the field and stays closed', async () => {
    vi.mocked(openConfigEditor).mockResolvedValue('wrong password')
    render(ConfigCard)
    await settle()

    await fireEvent.click(screen.getByRole('button', { name: 'Open the editor' }))
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'nope' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Open' }))
    await settle()

    expect(screen.getByRole('alert').textContent).toBe('wrong password')
    expect(configEditorState.visible).toBe(false)
  })

  it('says a headerless file was written before v0.7, with the guessed schema', async () => {
    vi.mocked(openConfigEditor).mockResolvedValue(opened({ header: null, schemaGuess: 3 }))
    await configEditorState.open('pw')
    configEditorState.close()
    render(ConfigCard)
    await settle()
    expect(rowValue('written by')).toBe('no header — written before v0.7')
    expect(rowValue('schema')).toBe('looks like 3 · this version reads 8')
  })
})
