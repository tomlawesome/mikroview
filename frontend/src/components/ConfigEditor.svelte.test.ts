// SPDX-License-Identifier: AGPL-3.0-only
//
// #1347: the full-screen config editor over a mocked API -- checking
// after a pause in typing, Carry forward, Show/Hide secrets, the
// download's password-again path, snapshots, and the keys.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/svelte'
import { flushSync } from 'svelte'

vi.mock('../lib/api', () => ({
  openConfigEditor: vi.fn(),
  validateConfig: vi.fn(),
  fetchConfigSnapshots: vi.fn(),
  carryForwardConfig: vi.fn(),
  createConfigSnapshot: vi.fn(),
  deleteConfigSnapshot: vi.fn(),
  downloadConfig: vi.fn(),
  fetchConfigSnapshot: vi.fn(),
  revealConfigSecrets: vi.fn(),
}))
vi.mock('../lib/export', () => ({ saveBlob: vi.fn() }))

import {
  carryForwardConfig,
  createConfigSnapshot,
  deleteConfigSnapshot,
  downloadConfig,
  fetchConfigSnapshot,
  fetchConfigSnapshots,
  openConfigEditor,
  revealConfigSecrets,
  validateConfig,
} from '../lib/api'
import { saveBlob } from '../lib/export'
import { configEditorState, VALIDATE_PAUSE_MS } from '../lib/configEditor.svelte'
import type { ConfigEditorOpen, ConfigSnapshotSummary } from '../lib/types'
import ConfigEditor from './ConfigEditor.svelte'

const TEXT = [
  '# MikroView configuration -- keep these four lines; MikroView reads them.',
  '# schema: 7',
  '# written-by: mikroview v0.6.1',
  '# layout: 1',
  'listen:',
  '  http: ":8080"',
  'oidc:',
  '  clientSecret: <<secret:oidc.clientSecret>>',
  '  cookieSecret: <<secret:oidc.cookieSecret>>',
].join('\n')

function opened(overrides: Partial<ConfigEditorOpen> = {}): ConfigEditorOpen {
  return {
    text: TEXT,
    path: '/etc/mikroview/config.yaml',
    changedSinceStart: false,
    header: { schema: 7, writtenBy: 'mikroview v0.6.1', layout: 1 },
    schemaGuess: 7,
    runningVersion: 'v0.7.0',
    runningSchema: 8,
    ...overrides,
  }
}

const SNAPS: ConfigSnapshotSummary[] = [
  { id: 's1', when: '2026-09-27T10:00:00Z', by: 'tom', schema: 7, why: 'manual', note: 'before the move' },
  { id: 's2', when: '2026-09-26T10:00:00Z', by: 'tom', schema: 7, why: 'before-carry-forward' },
]

async function settle() {
  for (let i = 0; i < 6; i++) await Promise.resolve()
  flushSync()
}

async function openEditor(overrides: Partial<ConfigEditorOpen> = {}) {
  vi.mocked(openConfigEditor).mockResolvedValue(opened(overrides))
  await configEditorState.open('pw')
  render(ConfigEditor)
  await settle()
}

function textarea(): HTMLTextAreaElement {
  return screen.getByLabelText('config.yaml') as HTMLTextAreaElement
}

async function type(value: string) {
  await fireEvent.input(textarea(), { target: { value } })
}

beforeEach(() => {
  vi.clearAllMocks()
  configEditorState.reset()
  vi.mocked(validateConfig).mockResolvedValue({ problems: [] })
  vi.mocked(fetchConfigSnapshots).mockResolvedValue([])
})

afterEach(() => {
  vi.useRealTimers()
})

describe('the text and the Problems rail', () => {
  it('shows the running config with a line-number gutter and the file facts', async () => {
    await openEditor()
    expect(textarea().value).toBe(TEXT)
    expect(document.querySelectorAll('.gutter .ln').length).toBe(9)
    expect(screen.getByText('/etc/mikroview/config.yaml')).toBeTruthy()
    expect(document.querySelector('.facts')?.textContent).toMatch(/schema 7, written by mikroview v0\.6\.1/)
    expect(document.querySelector('.facts')?.textContent).toMatch(/this MikroView is v0\.7\.0, schema 8/)
    expect(screen.queryByText(/changed on disk/)).toBeNull()
  })

  it('says so when the file changed on disk since start', async () => {
    await openEditor({ changedSinceStart: true })
    expect(screen.getByText(/has changed on disk since MikroView started/)).toBeTruthy()
  })

  it('checks the text only once typing pauses, and lists what comes back', async () => {
    await openEditor()
    vi.useFakeTimers()
    vi.mocked(validateConfig).mockClear()
    vi.mocked(validateConfig).mockResolvedValue({
      problems: [
        { line: 6, key: 'listen.htp', severity: 'error', message: 'listen.htp is not a setting MikroView knows' },
        { line: 2, key: '', severity: 'warning', message: 'schema 7 is older than this version' },
      ],
    })

    await type(TEXT + '\nx')
    await vi.advanceTimersByTimeAsync(300)
    await type(TEXT + '\nxy')
    await vi.advanceTimersByTimeAsync(VALIDATE_PAUSE_MS - 1)
    expect(validateConfig).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(validateConfig).toHaveBeenCalledTimes(1)
    expect(validateConfig).toHaveBeenCalledWith(TEXT + '\nxy')
    await settle()

    const rows = Array.from(document.querySelectorAll('.plist button.row')).map((b) => b.textContent?.replace(/\s+/g, ' ').trim())
    expect(rows).toEqual([
      'line 6 must fix listen.htp is not a setting MikroView knows',
      'line 2 check schema 7 is older than this version',
    ])
    expect(document.querySelector('.gutter .ln.error')?.textContent).toBe('6')
    expect(document.querySelector('.gutter .ln.warning')?.textContent).toBe('2')
  })

  it('moves the caret to a problem\'s line when it is clicked', async () => {
    vi.mocked(validateConfig).mockResolvedValue({
      problems: [{ line: 6, key: 'listen.http', severity: 'error', message: 'bad port' }],
    })
    await openEditor()
    await fireEvent.click(screen.getByText('bad port'))
    const offset = TEXT.split('\n').slice(0, 5).join('\n').length + 1
    expect(textarea().selectionStart).toBe(offset)
    expect(document.activeElement).toBe(textarea())
  })

  it('says plainly when there is nothing wrong, and when the check itself failed', async () => {
    await openEditor()
    expect(screen.getByText(/No problems found/)).toBeTruthy()
    vi.mocked(validateConfig).mockRejectedValue(new Error('the server could not do that (500)'))
    await configEditorState.validateNow()
    flushSync()
    expect(screen.getByText('Could not check the text: the server could not do that (500)')).toBeTruthy()
  })
})

describe('Carry forward', () => {
  it('replaces the text with the server\'s and lists each change above the problems', async () => {
    await openEditor()
    vi.mocked(carryForwardConfig).mockResolvedValue({
      text: '# schema: 8\nlisten:\n  syslog: ":5514"\n',
      changes: [
        { kind: 'renamed', key: 'listen.syslogUdp', to: 'listen.syslog', line: 3, note: 'renamed in v0.2.0' },
        { kind: 'removed', key: 'store.maxEvents', to: '', line: 0, note: 'no longer used' },
      ],
      problems: [{ line: 3, key: 'listen.syslog', severity: 'warning', message: 'check the port' }],
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Carry forward' }))
    await settle()

    expect(carryForwardConfig).toHaveBeenCalledWith(TEXT)
    expect(textarea().value).toBe('# schema: 8\nlisten:\n  syslog: ":5514"\n')
    const changes = Array.from(document.querySelectorAll('.changes button.row')).map((b) =>
      b.textContent?.replace(/\s+/g, ' ').trim(),
    )
    expect(changes).toEqual([
      'renamed listen.syslogUdp → listen.syslog renamed in v0.2.0',
      'removed store.maxEvents no longer used',
    ])
    expect(screen.getByText(/What Carry forward changed \(2\)/)).toBeTruthy()
    expect(screen.getByText('check the port')).toBeTruthy()
    // The server kept a snapshot before changing anything.
    expect(fetchConfigSnapshots).toHaveBeenCalledTimes(2)
  })

  it('asks to confirm the guessed schema first for a file with no header', async () => {
    await openEditor({ header: null, schemaGuess: 3 })
    vi.mocked(carryForwardConfig).mockResolvedValue({ text: 'x', changes: [], problems: [] })
    expect(document.querySelector('.facts')?.textContent).toMatch(/no header — written before v0\.7 · looks like schema 3/)

    await fireEvent.click(screen.getByRole('button', { name: 'Carry forward' }))
    expect(carryForwardConfig).not.toHaveBeenCalled()
    expect(screen.getByText(/it looks like schema\s+3/)).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Carry forward from schema 3' }))
    await settle()
    expect(carryForwardConfig).toHaveBeenCalledTimes(1)
  })

  it('shows a refusal in the top bar and leaves the text alone', async () => {
    await openEditor()
    vi.mocked(carryForwardConfig).mockResolvedValue('the text is not YAML')
    await fireEvent.click(screen.getByRole('button', { name: 'Carry forward' }))
    await settle()
    expect(screen.getByRole('alert').textContent).toBe('Could not carry this forward: the text is not YAML')
    expect(textarea().value).toBe(TEXT)
  })
})

describe('secrets', () => {
  it('shows the values in place of the placeholders and hides them again, keeping an edited one', async () => {
    await openEditor()
    vi.mocked(revealConfigSecrets).mockResolvedValue({
      secrets: { 'oidc.clientSecret': 'abc123', 'oidc.cookieSecret': 'cookie-value' },
    })

    await fireEvent.click(screen.getByRole('button', { name: 'Show secrets' }))
    await settle()
    expect(textarea().value).toContain('  clientSecret: abc123')
    expect(textarea().value).toContain('  cookieSecret: cookie-value')
    expect(textarea().value).not.toContain('<<secret:')
    // Showing is not an edit.
    expect(configEditorState.edited).toBe(false)

    await type(textarea().value.replace('cookie-value', 'a-new-cookie'))
    await fireEvent.click(screen.getByRole('button', { name: 'Hide secrets' }))
    flushSync()

    expect(textarea().value).toContain('  clientSecret: <<secret:oidc.clientSecret>>')
    expect(textarea().value).toContain('  cookieSecret: a-new-cookie')
    expect(textarea().value).not.toContain('abc123')
    expect(screen.getByRole('button', { name: 'Show secrets' })).toBeTruthy()
  })

  it('round-trips to the exact original text when nothing was edited', async () => {
    await openEditor()
    vi.mocked(revealConfigSecrets).mockResolvedValue({
      secrets: { 'oidc.clientSecret': 'abc123', 'oidc.cookieSecret': 'c' },
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Show secrets' }))
    await settle()
    await fireEvent.click(screen.getByRole('button', { name: 'Hide secrets' }))
    flushSync()
    expect(textarea().value).toBe(TEXT)
  })
})

describe('Download', () => {
  it('saves the server\'s file under the server\'s name', async () => {
    await openEditor()
    const blob = new Blob(['x'])
    vi.mocked(downloadConfig).mockResolvedValue({ filename: 'config.v0.7.0.yaml', blob })
    await fireEvent.click(screen.getByRole('button', { name: 'Download' }))
    await settle()
    expect(downloadConfig).toHaveBeenCalledWith(TEXT)
    expect(saveBlob).toHaveBeenCalledWith('config.v0.7.0.yaml', blob)
    expect(screen.getByRole('status').textContent).toMatch(/Downloaded config\.v0\.7\.0\.yaml\. Keep your previous file beside it/)
  })

  it('asks for the password again when the unlock has lapsed, then retries with the same text', async () => {
    await openEditor()
    await type(TEXT + '\n# edited')
    const blob = new Blob(['x'])
    vi.mocked(downloadConfig).mockResolvedValueOnce({ reauth: true }).mockResolvedValueOnce({ filename: null, blob })
    vi.mocked(openConfigEditor).mockClear()
    vi.mocked(openConfigEditor).mockResolvedValue(opened({ text: 'a fresh copy that must not replace the edits' }))

    await fireEvent.click(screen.getByRole('button', { name: 'Download' }))
    await settle()
    expect(saveBlob).not.toHaveBeenCalled()
    expect(screen.getByText(/more than 15 minutes .* Enter it again to download/)).toBeTruthy()

    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'hunter2' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await settle()

    expect(openConfigEditor).toHaveBeenCalledWith('hunter2')
    expect(downloadConfig).toHaveBeenCalledTimes(2)
    expect(downloadConfig).toHaveBeenLastCalledWith(TEXT + '\n# edited')
    // No filename from the server: named for the running version.
    expect(saveBlob).toHaveBeenCalledWith('config.v0.7.0.yaml', blob)
    expect(textarea().value).toBe(TEXT + '\n# edited')
    expect(screen.queryByLabelText('Password')).toBeNull()
  })

  it('keeps asking when the password is wrong', async () => {
    await openEditor()
    vi.mocked(downloadConfig).mockResolvedValue({ reauth: true })
    vi.mocked(openConfigEditor).mockResolvedValue('wrong password')
    await fireEvent.click(screen.getByRole('button', { name: 'Download' }))
    await settle()
    await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'nope' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Continue' }))
    await settle()
    expect(screen.getByText('wrong password')).toBeTruthy()
    expect(downloadConfig).toHaveBeenCalledTimes(1)
  })

  it('Ctrl+S downloads', async () => {
    await openEditor()
    vi.mocked(downloadConfig).mockResolvedValue({ filename: 'c.yaml', blob: new Blob(['x']) })
    await fireEvent.keyDown(window, { key: 's', ctrlKey: true })
    await settle()
    expect(downloadConfig).toHaveBeenCalledTimes(1)
  })
})

describe('snapshots', () => {
  it('lists them with when, by, why and schema', async () => {
    vi.mocked(fetchConfigSnapshots).mockResolvedValue(SNAPS)
    await openEditor()
    const rows = Array.from(document.querySelectorAll('.snap .sline .dim')).map((el) => el.textContent?.trim())
    expect(rows).toEqual(['· tom · kept by hand · schema 7', '· tom · before Carry forward · schema 7'])
    expect(screen.getByText('before the move')).toBeTruthy()
  })

  it('deletes only on the second click, and a click elsewhere disarms', async () => {
    vi.mocked(fetchConfigSnapshots).mockResolvedValue(SNAPS)
    vi.mocked(deleteConfigSnapshot).mockResolvedValue(null)
    await openEditor()

    const first = () => document.querySelectorAll('.snap .del')[0] as HTMLButtonElement
    await fireEvent.click(first())
    expect(first().textContent?.trim()).toBe('Delete for good?')
    expect(deleteConfigSnapshot).not.toHaveBeenCalled()

    await fireEvent.click(document.body)
    expect(first().textContent?.trim()).toBe('Delete')

    await fireEvent.click(first())
    await fireEvent.click(first())
    await settle()
    expect(deleteConfigSnapshot).toHaveBeenCalledWith('s1')
  })

  it('keeps a snapshot of the current text with an optional note', async () => {
    vi.mocked(createConfigSnapshot).mockResolvedValue(null)
    await openEditor()
    await type(TEXT + '\n# mine')
    await fireEvent.click(screen.getByRole('button', { name: 'Snapshot' }))
    await fireEvent.input(screen.getByLabelText('Note (optional)'), { target: { value: ' trying the move ' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Keep snapshot' }))
    await settle()
    expect(createConfigSnapshot).toHaveBeenCalledWith(TEXT + '\n# mine', 'trying the move')
    expect(screen.getByRole('status').textContent).toBe('Snapshot kept.')
    expect(configEditorState.edited).toBe(false)
  })

  it('loads one into the editor, asking first when there are unsaved edits', async () => {
    vi.mocked(fetchConfigSnapshots).mockResolvedValue(SNAPS)
    vi.mocked(fetchConfigSnapshot).mockResolvedValue({ ...SNAPS[0], text: 'from the snapshot' })
    await openEditor()
    await type(TEXT + '\n# unsaved')

    await fireEvent.click(screen.getAllByRole('button', { name: 'Load into editor' })[0])
    expect(fetchConfigSnapshot).not.toHaveBeenCalled()
    expect(screen.getByText(/haven't been downloaded or kept as a snapshot/)).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Load snapshot' }))
    await settle()
    expect(fetchConfigSnapshot).toHaveBeenCalledWith('s1')
    expect(textarea().value).toBe('from the snapshot')
  })

  it('downloads one through the download route, so the server fills in the secrets', async () => {
    vi.mocked(fetchConfigSnapshots).mockResolvedValue(SNAPS)
    vi.mocked(fetchConfigSnapshot).mockResolvedValue({ ...SNAPS[1], text: 'old text' })
    const blob = new Blob(['x'])
    vi.mocked(downloadConfig).mockResolvedValue({ filename: 'config.v0.7.0.yaml', blob })
    await openEditor()
    await fireEvent.click(screen.getAllByRole('button', { name: 'Download' })[1])
    await settle()
    expect(downloadConfig).toHaveBeenCalledWith('old text')
    expect(saveBlob).toHaveBeenCalledWith('config.v0.7.0.yaml', blob)
    expect(textarea().value).toBe(TEXT)
  })
})

describe('closing', () => {
  it('Escape closes at once when nothing was edited', async () => {
    await openEditor()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(configEditorState.visible).toBe(false)
  })

  it('Escape asks first when there are edits, and Keep editing keeps them', async () => {
    await openEditor()
    await type(TEXT + '\n# edit')
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(configEditorState.visible).toBe(true)
    expect(screen.getByText(/Close the editor and lose them\?/)).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }))
    expect(configEditorState.visible).toBe(true)
    expect(textarea().value).toBe(TEXT + '\n# edit')

    await fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Close without saving' }))
    expect(configEditorState.visible).toBe(false)
    expect(configEditorState.text).toBe('')
  })
})
