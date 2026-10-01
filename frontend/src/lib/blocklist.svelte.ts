// SPDX-License-Identifier: AGPL-3.0-only

// The blocklist builder's state (#1360, BUILD.md part 7): which router
// the page is open for, what the server said about it, what the
// operator has clicked, and the block rendered from those clicks.
//
// The block is re-requested on every click, debounced, and an older
// response landing after a newer one is dropped -- commandsRequestSeq,
// the guard wizard.svelte.ts keeps for the same race.
//
// The push re-set (part 1 of the block) embeds an ingest token. One is
// minted per router per page session, named blocklist-<router>, held
// here for the module's lifetime and never persisted: a bearer
// credential does not go into web storage, so a reload mints afresh
// (wizard.svelte.ts's `token`, same reasoning). Inside the wizard's
// first-run tail the wizard's own token is reused when it is for the
// same router.

import {
  createToken,
  fetchBlocklistBuilder,
  fetchBlocklistCommands,
  type BlocklistBuilder,
  type BlocklistCommands,
} from './api'
import { defaultChoices, requestLists, type CardChoice, type Choices } from './blocklistBuild'
import { copyToClipboard } from './clipboard'
import { wizardState } from './wizard.svelte'

const DEBOUNCE_MS = 120

class BlocklistState {
  // open is the full page (Settings ▸ drop list ▸ "Block known-bad
  // addresses…"); the wizard's tail renders the same body without it.
  open = $state(false)
  device = $state('')
  data = $state<BlocklistBuilder | null>(null)
  error = $state<string | null>(null)
  choices = $state<Choices>({})
  block = $state<BlocklistCommands | null>(null)
  blockError = $state<string | null>(null)
  // current is the rail row last pointed at; undoOpen the ledger row
  // whose Undo lines are showing; showUndoAll the foot's undo for
  // everything.
  current = $state('')
  undoOpen = $state('')
  showUndoAll = $state(false)
  copied = $state(false)

  private tokens = new Map<string, string>()
  private seq = 0
  private timer: ReturnType<typeof setTimeout> | null = null
  // inWizard: the body is on the wizard's tail stage, so the wizard's
  // token for this router is the one to reuse.
  private inWizard = false

  // openFor opens the full page for device.
  async openFor(device: string): Promise<void> {
    this.open = true
    await this.load(device, false)
  }

  close() {
    this.open = false
    this.showUndoAll = false
    this.undoOpen = ''
  }

  // load reads the page for device and resets the clicks to its defaults.
  async load(device: string, inWizard: boolean): Promise<void> {
    this.inWizard = inWizard
    if (device !== this.device) {
      this.data = null
      this.block = null
      this.choices = {}
    }
    this.device = device
    this.copied = false
    try {
      const data = await fetchBlocklistBuilder(device)
      if (this.device !== device) return
      this.data = data
      this.error = null
      this.choices = defaultChoices(data)
      this.current = data.catalogue[0]?.key ?? ''
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e)
      return
    }
    await this.ensureToken()
    await this.render()
  }

  // peek reads the page's data for device without minting a token or
  // rendering a block: the wizard's ledger offers the tail, and names
  // the lists MikroView flags from, before the operator has asked for
  // anything -- no credential is made for an offer nobody took.
  async peek(device: string): Promise<void> {
    if (!device) return
    if (device !== this.device || !this.data) {
      this.device = device
      this.block = null
      try {
        const data = await fetchBlocklistBuilder(device)
        if (this.device !== device) return
        this.data = data
        this.error = null
        this.choices = defaultChoices(data)
        this.current = data.catalogue[0]?.key ?? ''
      } catch (e) {
        this.error = e instanceof Error ? e.message : String(e)
      }
      return
    }
    await this.refresh()
  }

  // refresh re-reads what the router holds without touching the clicks.
  async refresh(): Promise<void> {
    if (!this.device) return
    try {
      const data = await fetchBlocklistBuilder(this.device)
      if (data.device !== this.device) return
      const wasHeld = new Set((this.data?.lists ?? []).filter((l) => l.state === 'held').map((l) => l.key))
      this.data = data
      // A list the router now holds is on; nothing the operator chose
      // is switched off by a push.
      for (const l of data.lists) {
        if (l.state === 'held' && !wasHeld.has(l.key) && this.choices[l.key]) this.choices[l.key] = { ...this.choices[l.key], on: true }
      }
    } catch {
      // The next poll tries again; the page keeps what it has.
    }
  }

  setChoice(key: string, patch: Partial<CardChoice>) {
    const prev = this.choices[key]
    if (!prev) return
    this.choices = { ...this.choices, [key]: { ...prev, ...patch } }
    this.current = key
    this.copied = false
    this.schedule()
  }

  private schedule() {
    if (this.timer) clearTimeout(this.timer)
    this.timer = setTimeout(() => {
      this.timer = null
      void this.render()
    }, DEBOUNCE_MS)
  }

  get token(): string {
    if (this.inWizard && wizardState.token && wizardState.tokenDevice === this.device) return wizardState.token
    return this.tokens.get(this.device) ?? ''
  }

  // address is where part 1's push posts to: the wizard's own answer
  // (#1213, the operator's stored address, else the browser's host --
  // the wizard's own default order).
  get address(): string {
    return wizardState.address || (typeof window !== 'undefined' ? window.location.host : '')
  }

  // ensureToken mints this page session's token for the router, once.
  private async ensureToken(): Promise<void> {
    if (this.token || this.data?.standing !== 'ok') return
    const device = this.device
    try {
      const result = await createToken(`blocklist-${device}`, 'ingest', device)
      if (typeof result === 'string' || !result.value) return
      this.tokens.set(device, result.value)
    } catch {
      // Without a token the block comes back without its push part,
      // and says so (blocked: no-token).
    }
  }

  // render asks the server for the block the current clicks describe.
  async render(): Promise<void> {
    const data = this.data
    if (!data || data.standing !== 'ok') return
    const seq = ++this.seq
    const result = await fetchBlocklistCommands({
      device: this.device,
      token: this.token || undefined,
      address: this.address || undefined,
      lists: requestLists(data, this.choices),
    })
    if (seq !== this.seq) return
    if (typeof result === 'string') {
      this.blockError = result
      return
    }
    this.blockError = null
    this.block = result
  }

  async copy(): Promise<boolean> {
    if (!this.block?.copyText) return false
    const ok = await copyToClipboard(this.block.copyText)
    this.copied = ok
    return ok
  }

  toggleUndo(key: string) {
    this.undoOpen = this.undoOpen === key ? '' : key
  }

  toggleUndoAll() {
    this.showUndoAll = !this.showUndoAll
  }

  // reset is for tests and sign-out.
  reset() {
    this.open = false
    this.device = ''
    this.data = null
    this.error = null
    this.choices = {}
    this.block = null
    this.blockError = null
    this.current = ''
    this.undoOpen = ''
    this.showUndoAll = false
    this.copied = false
    this.tokens.clear()
    this.seq = 0
    if (this.timer) clearTimeout(this.timer)
    this.timer = null
  }
}

export const blocklistState = new BlocklistState()
