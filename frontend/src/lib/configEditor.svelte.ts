// SPDX-License-Identifier: AGPL-3.0-only

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
} from './api'
import { saveBlob } from './export'
import type {
  ConfigChange,
  ConfigEditorOpen,
  ConfigEditorProblem,
  ConfigSnapshotSummary,
} from './types'

// The config editor (#1347): one module-lifetime store behind both the
// Engine Room's Config card and the full-screen editor App.svelte mounts
// over the shell, the way wizardState sits behind the setup wizard.
//
// MikroView never writes the config file. Everything here ends in a
// download the operator puts in place themselves.

/** How long typing has to pause before the text is checked. */
export const VALIDATE_PAUSE_MS = 600

const PLACEHOLDER = /<<secret:([^>\s]+)>>/g

/** What the inline password field is asking for. */
export type PasswordReason = 'open' | 'download' | 'reveal'

/** What the card shows about the running file, kept after the editor closes. */
export interface ConfigFileSummary {
  path: string
  header: ConfigEditorOpen['header']
  schemaGuess: number
  runningVersion: string
  runningSchema: number
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

class ConfigEditorState {
  /** The full-screen editor is showing. */
  visible = $state(false)
  /** The last open's answer, less the text: null until the editor has been opened. */
  file = $state<ConfigFileSummary | null>(null)
  changedSinceStart = $state(false)

  text = $state('')
  /** The text as it was last opened, loaded, downloaded or snapshotted --
   *  anything else counts as edits that closing would lose. */
  saved = $state('')
  edited = $derived(this.text !== this.saved)

  problems = $state<ConfigEditorProblem[]>([])
  checking = $state(false)
  checkError = $state<string | null>(null)

  changes = $state<ConfigChange[]>([])
  carrying = $state(false)
  /** A headerless file's guessed schema has been confirmed for Carry forward. */
  guessConfirmed = $state(false)

  revealed = $state(false)
  revealing = $state(false)
  downloading = $state(false)

  /** The last action's failure, shown in the top bar. */
  error = $state<string | null>(null)
  /** The last action's plain success line ("Snapshot kept."). */
  notice = $state<string | null>(null)

  /** Set while the editor's inline password field is showing. */
  passwordFor = $state<PasswordReason | null>(null)
  passwordError = $state<string | null>(null)
  unlocking = $state(false)

  snapshots = $state<ConfigSnapshotSummary[]>([])
  snapshotsLoaded = $state(false)
  snapshotsError = $state<string | null>(null)
  snapshotting = $state(false)

  // The revealed secret values. Deliberately not $state and never
  // logged: nothing renders the map itself, only the text it is
  // substituted into, and it is dropped on close and on sign-out.
  #secrets: Record<string, string> = {}
  // For each line a Show changed: the revealed line -> the line as it
  // was with its placeholder. Hide puts back only lines still reading
  // exactly as revealed, so a secret the operator has edited stays as
  // they typed it.
  #revealedLines = new Map<string, string>()
  #afterUnlock: (() => Promise<void>) | null = null
  #timer: ReturnType<typeof setTimeout> | null = null
  #checkSeq = 0

  /** A headerless file's guess must be confirmed before Carry forward. */
  get needsGuessConfirm(): boolean {
    return !!this.file && this.file.header === null && !this.guessConfirmed
  }

  // --- opening ----------------------------------------------------------

  /** POST /api/config/editor/open with the password; on success the editor shows. */
  async open(password: string): Promise<string | null> {
    const res = await openConfigEditor(password)
    if (typeof res === 'string') return res
    this.#clearSecrets()
    this.file = {
      path: res.path,
      header: res.header,
      schemaGuess: res.schemaGuess,
      runningVersion: res.runningVersion,
      runningSchema: res.runningSchema,
    }
    this.changedSinceStart = res.changedSinceStart
    this.text = res.text
    this.saved = res.text
    this.problems = []
    this.changes = []
    this.checkError = null
    this.guessConfirmed = false
    this.error = null
    this.notice = null
    this.passwordFor = null
    this.passwordError = null
    this.visible = true
    void this.validateNow()
    void this.refreshSnapshots()
    return null
  }

  // --- typing and checking ----------------------------------------------

  setText(text: string) {
    this.text = text
    this.notice = null
    this.scheduleValidate()
  }

  scheduleValidate() {
    if (this.#timer !== null) clearTimeout(this.#timer)
    this.#timer = setTimeout(() => {
      this.#timer = null
      void this.validateNow()
    }, VALIDATE_PAUSE_MS)
  }

  #cancelValidate() {
    if (this.#timer !== null) clearTimeout(this.#timer)
    this.#timer = null
    // Bumped so an answer already in flight for older text is dropped.
    this.#checkSeq++
  }

  async validateNow() {
    const seq = ++this.#checkSeq
    this.checking = true
    try {
      const res = await validateConfig(this.text)
      if (seq !== this.#checkSeq) return
      this.problems = res.problems
      this.checkError = null
    } catch (err) {
      if (seq !== this.#checkSeq) return
      this.checkError = errorText(err)
    } finally {
      if (seq === this.#checkSeq) this.checking = false
    }
  }

  // --- carry forward ----------------------------------------------------

  async carryForward(): Promise<void> {
    this.error = null
    this.notice = null
    this.carrying = true
    try {
      const res = await carryForwardConfig(this.text)
      if (typeof res === 'string') {
        this.error = `Could not carry this forward: ${res}`
        return
      }
      this.#cancelValidate()
      this.checking = false
      // The server answers with placeholders back in place, so whatever
      // Show had revealed is masked again.
      this.#revealedLines.clear()
      this.revealed = false
      this.text = res.text
      this.changes = res.changes
      this.problems = res.problems
      this.checkError = null
      this.guessConfirmed = true
      // The server kept a snapshot of the text as it was before this.
      void this.refreshSnapshots()
    } finally {
      this.carrying = false
    }
  }

  // --- secrets ----------------------------------------------------------

  async showSecrets(): Promise<void> {
    this.error = null
    this.revealing = true
    try {
      const res = await revealConfigSecrets()
      if (typeof res === 'string') {
        this.error = `Could not show the secrets: ${res}`
        return
      }
      if ('reauth' in res) {
        this.#askPassword('reveal', () => this.showSecrets())
        return
      }
      this.#secrets = { ...res.secrets }
      this.#revealedLines.clear()
      const lines = this.text.split('\n')
      for (let i = 0; i < lines.length; i++) {
        const masked = lines[i]
        if (!masked.includes('<<secret:')) continue
        const shown = masked.replace(PLACEHOLDER, (m, key: string) =>
          Object.prototype.hasOwnProperty.call(this.#secrets, key) ? this.#secrets[key] : m,
        )
        if (shown !== masked) {
          this.#revealedLines.set(shown, masked)
          lines[i] = shown
        }
      }
      const next = lines.join('\n')
      // Revealing is not an edit: saved moves with the text when it had
      // not been edited, so closing straight after Show asks nothing.
      if (this.saved === this.text) this.saved = next
      this.text = next
      this.revealed = true
    } finally {
      this.revealing = false
    }
  }

  hideSecrets() {
    const wasSaved = this.saved === this.text
    const lines = this.text.split('\n').map((line) => this.#revealedLines.get(line) ?? line)
    const next = lines.join('\n')
    if (wasSaved) this.saved = next
    this.text = next
    this.#revealedLines.clear()
    this.revealed = false
  }

  #clearSecrets() {
    this.#secrets = {}
    this.#revealedLines.clear()
    this.revealed = false
  }

  // --- the password, again ------------------------------------------------

  #askPassword(reason: PasswordReason, retry: () => Promise<void>) {
    this.passwordFor = reason
    this.passwordError = null
    this.#afterUnlock = retry
  }

  /** Asks for the password inline, for the setup-only screen's first open. */
  askToOpen() {
    this.passwordFor = 'open'
    this.passwordError = null
    this.#afterUnlock = null
  }

  cancelPassword() {
    this.passwordFor = null
    this.passwordError = null
    this.#afterUnlock = null
  }

  /**
   * Answers the inline password field. For 'open' this is the first
   * open; otherwise the unlock lapsed, so the same open call renews it
   * and the action that asked is tried again -- keeping the editor's
   * text, not the fresh copy the open call sends back.
   */
  async submitPassword(password: string): Promise<void> {
    const reason = this.passwordFor
    if (!reason) return
    this.unlocking = true
    this.passwordError = null
    try {
      if (reason === 'open') {
        const err = await this.open(password)
        if (err) this.passwordError = err
        return
      }
      const res = await openConfigEditor(password)
      if (typeof res === 'string') {
        this.passwordError = res
        return
      }
      this.changedSinceStart = res.changedSinceStart
      const retry = this.#afterUnlock
      this.passwordFor = null
      this.#afterUnlock = null
      if (retry) await retry()
    } finally {
      this.unlocking = false
    }
  }

  // --- download ---------------------------------------------------------

  #fallbackName(): string {
    return this.file ? `config.v${this.file.runningVersion.replace(/^v/, '')}.yaml` : 'config.yaml'
  }

  async download(): Promise<void> {
    await this.#download(this.text, true)
  }

  async #download(text: string, fromEditor: boolean): Promise<void> {
    this.error = null
    this.notice = null
    this.downloading = true
    try {
      const res = await downloadConfig(text)
      if (typeof res === 'string') {
        this.error = `Could not download: ${res}`
        return
      }
      if ('reauth' in res) {
        this.#askPassword('download', () => this.#download(text, fromEditor))
        return
      }
      const name = res.filename ?? this.#fallbackName()
      saveBlob(name, res.blob)
      if (fromEditor && this.text === text) this.saved = text
      this.notice = `Downloaded ${name}. Keep your previous file beside it rather than overwriting it.`
    } finally {
      this.downloading = false
    }
  }

  // --- snapshots --------------------------------------------------------

  async refreshSnapshots(): Promise<void> {
    try {
      const list = await fetchConfigSnapshots()
      this.snapshots = [...list].sort((a, b) => (a.when < b.when ? 1 : a.when > b.when ? -1 : 0))
      this.snapshotsError = null
    } catch (err) {
      this.snapshotsError = errorText(err)
    } finally {
      this.snapshotsLoaded = true
    }
  }

  async takeSnapshot(note: string): Promise<boolean> {
    this.error = null
    this.notice = null
    this.snapshotting = true
    const text = this.text
    try {
      const err = await createConfigSnapshot(text, note.trim())
      if (err) {
        this.error = `Could not keep a snapshot: ${err}`
        return false
      }
      if (this.text === text) this.saved = text
      this.notice = 'Snapshot kept.'
      await this.refreshSnapshots()
      return true
    } finally {
      this.snapshotting = false
    }
  }

  async loadSnapshot(id: string): Promise<void> {
    this.error = null
    this.notice = null
    const snap = await fetchConfigSnapshot(id)
    if (typeof snap === 'string') {
      this.error = `Could not load that snapshot: ${snap}`
      return
    }
    this.#cancelValidate()
    this.#revealedLines.clear()
    this.revealed = false
    this.text = snap.text
    this.saved = snap.text
    this.changes = []
    this.notice = 'Snapshot loaded into the editor.'
    void this.validateNow()
  }

  async downloadSnapshot(id: string): Promise<void> {
    this.error = null
    this.notice = null
    const snap = await fetchConfigSnapshot(id)
    if (typeof snap === 'string') {
      this.error = `Could not read that snapshot: ${snap}`
      return
    }
    // Through the same download route as the editor's own text, so the
    // server puts the secret values back in place of the placeholders.
    await this.#download(snap.text, false)
  }

  async deleteSnapshot(id: string): Promise<void> {
    this.error = null
    this.notice = null
    const err = await deleteConfigSnapshot(id)
    if (err) {
      this.error = `Could not delete that snapshot: ${err}`
      return
    }
    await this.refreshSnapshots()
  }

  // --- closing ----------------------------------------------------------

  /** Hides the editor and drops its text and any revealed secrets. The
   *  card's summary of the file stays. */
  close() {
    this.#cancelValidate()
    this.#clearSecrets()
    this.visible = false
    this.text = ''
    this.saved = ''
    this.problems = []
    this.changes = []
    this.checking = false
    this.checkError = null
    this.error = null
    this.notice = null
    this.cancelPassword()
  }

  // #1083: every admin-only module-lifetime store is cleared on sign-out.
  reset() {
    this.close()
    this.file = null
    this.changedSinceStart = false
    this.guessConfirmed = false
    this.snapshots = []
    this.snapshotsLoaded = false
    this.snapshotsError = null
  }
}

export const configEditorState = new ConfigEditorState()
