// SPDX-License-Identifier: AGPL-3.0-only

import { fetchNameProvenance, upsertEntity, deleteEntity } from './api'
import { authState } from './auth.svelte'
import { lookupPort } from './commonPorts'
import { appState } from './state.svelte'
import { toastState } from './toast.svelte'
import type { NameProvenance } from './types'

// The kinds of token this editor can rename. Narrower than EntityType on
// purpose: these are the three that a live row actually shows a label
// for, and each one has its own title, scope sentence and identity line
// below. An arbitrary entity type has none of those, so it has no
// business opening this.
export type EditableTokenType = 'host' | 'port' | 'rule'

interface Anchor {
  x: number
  y: number
}

// Copy per token type, kept in one table rather than spread through the
// component: the strings for a type are a set, and the design (issue
// #413) specifies them as one.
const COPY: Record<EditableTokenType, { title: string; scope: (key: string) => string }> = {
  host: {
    title: 'Name this host',
    scope: (key) => `Applies everywhere ${key} appears, in every view.`,
  },
  port: {
    title: 'Label this port',
    scope: (key) => `Applies to port ${key} everywhere, every protocol.`,
  },
  rule: {
    title: 'Name this rule',
    scope: (key) => `Applies to every event logged with prefix “${key}”.`,
  },
}

// Drives the single NameEditorPopover instance mounted at the app root
// (see App.svelte) -- the same singleton-plus-trigger shape as
// lib/ipLookup.svelte.ts and lib/routerLookup.svelte.ts, so only one
// editor can ever be open and the pencil rendered per row per token
// owns no state of its own.
//
// The load-before-you-offer-a-field order is the point of this class.
// open() shows the popover immediately in a loading state and asks
// GET /api/naming/provenance what would actually happen to an edit
// here; only once that answers does the field appear, and only if the
// answer was yes. Prefilling and enabling an input first, then
// disabling it when the response lands, would present a writable field
// for the exact moment somebody is most likely to start typing into it.
class NameEditorState {
  anchor = $state<Anchor | null>(null)
  type = $state<EditableTokenType>('host')
  // key is the RAW value -- the IP, the port as a decimal string, the
  // raw log prefix. Identity, never display: it is what the entity is
  // keyed by and what filters and copies keep using.
  key = $state('')
  // device scopes the router-pushed name layer, and only host names
  // have one (internal/naming.Resolver.Host). '' for ports and rules.
  device = $state('')

  loading = $state(false)
  error = $state<string | null>(null)
  saving = $state(false)
  provenance = $state<NameProvenance | null>(null)
  // draft is the text in the input. Only ever meaningful once
  // provenance has landed and said the edit is allowed.
  draft = $state('')

  private requestId = 0

  // Whether this operator gets a pencil at all. Admins only, and the
  // control is absent rather than disabled for everyone else: #439
  // named a control that cannot act the lying-affordance class, which
  // is the same failure this editor exists to remove, so shipping one
  // on every row of a viewer's screen would be self-defeating.
  //
  // Lives here rather than in EditNameButton so the callers that must
  // skip building the button at all can ask the same question the
  // button itself asks. A row carries up to five of these and the live
  // view renders up to MAX_RENDERED_ROWS rows, so "renders nothing" is
  // not the same as "costs nothing" -- for every session that never
  // sees a pencil, the component is now never created either.
  get available(): boolean {
    return authState.state === 'authenticated' && authState.role === 'admin'
  }

  get title(): string {
    return COPY[this.type].title
  }

  get scopeLine(): string {
    return COPY[this.type].scope(this.key)
  }

  // The gate. Null provenance means "not known yet", which is not the
  // same as "allowed" -- an editor that defaulted to editable while
  // loading would offer the field for the moment before it knows.
  get editable(): boolean {
    return this.provenance?.editable === true
  }

  // Where the displayed name comes from, in one sentence, always
  // showing the raw value. This is the identity line the design calls
  // for, and on a router-named token it is also the refusal: it names
  // the table and the device to go and change instead.
  get identityLine(): string {
    const p = this.provenance
    if (!p) return this.key
    switch (p.source) {
      case 'entity':
        return `${this.key} — currently “${p.name}”, from your label`
      case 'config':
        return `${this.key} — currently “${p.name}”, from config.yaml`
      case 'router-dhcp-lease':
        return `${this.key} — currently “${p.name}”, from a DHCP lease`
      case 'router-dns-static':
        return `${this.key} — currently “${p.name}”, from a DNS static entry`
      case 'router-wireguard-peer':
        return `${this.key} — currently “${p.name}”, from a WireGuard peer comment`
      case 'router':
        return `${this.key} — currently “${p.name}”, pushed by the router`
      default:
        return this.wellKnown
          ? `${this.key} — well-known: ${this.wellKnown}`
          : `${this.key} — no name yet`
    }
  }

  // Why the field is not there, and what to do instead. Spelled out
  // rather than left to a disabled input, because a greyed-out box with
  // no explanation reads as a bug.
  get refusal(): string | null {
    const p = this.provenance
    if (!p || p.editable) return null
    const where = p.router ? `“${p.router}”` : 'the router'
    const shadowed = p.label ? ` Your label “${p.label}” is saved, but it is not what is shown.` : ''
    return (
      'RouterOS supplies this name, and RouterOS wins — a name set here would be stored and never displayed.' +
      ` Change it on ${where}, in the table above.${shadowed}`
    )
  }

  // The well-known service name for a port, from the frontend's own
  // table -- the backend has no equivalent (internal/naming.Resolver.
  // Port has no config fallback at all), so the suggestion for an
  // unlabelled port is derived here.
  get wellKnown(): string {
    if (this.type !== 'port') return ''
    return lookupPort(Number(this.key))?.[0]?.name ?? ''
  }

  open(type: EditableTokenType, key: string, device: string, rect: DOMRect) {
    this.type = type
    this.key = key
    this.device = device
    this.provenance = null
    this.error = null
    this.draft = ''
    this.loading = true
    // Hold the stream for as long as this is open -- an editor anchored
    // to a row that new arrivals keep pushing down is unusable. Guarded
    // on anchor so re-opening for another token does not take a second
    // hold that nothing will release; paired with close(), which every
    // exit path goes through.
    if (this.anchor === null) appState.holdStream()
    this.anchor = { x: rect.left, y: rect.bottom }

    const id = ++this.requestId
    fetchNameProvenance(type, key, device).then(
      (p) => {
        if (id !== this.requestId) return
        this.provenance = p
        // Prefill with the best derivation already available: the
        // operator's own label, else the name in use, else the
        // well-known service name for a port. The common case is
        // confirming a suggestion, not typing.
        this.draft = p.label || p.name || this.wellKnown
        this.loading = false
      },
      () => {
        if (id !== this.requestId) return
        // Deliberately leaves provenance null, so `editable` stays
        // false and no field is offered: with the answer unknown, an
        // edit might be one that does nothing, and offering it anyway
        // is the failure this whole editor is built to avoid.
        this.error = 'Could not check where this name comes from'
        this.loading = false
      },
    )
  }

  async save() {
    if (!this.editable || this.saving) return
    const label = this.draft.trim()
    const { type, key } = this

    this.saving = true
    // An emptied field removes the label rather than saving an empty
    // one, so the raw value shows again. A 404 from delete means there
    // was no label to begin with, which is the requested end state, not
    // a failure.
    const err = label === '' ? await deleteEntity(type, key) : await upsertEntity({ type, key, label })
    this.saving = false
    if (err && !(label === '' && err.includes('not found'))) {
      this.error = err
      return
    }

    // Rewrite what is already on screen. The server will resolve every
    // later event against the entity that now exists, but the rows
    // being looked at right now were named at ingest and would
    // otherwise keep the old name until they aged out.
    appState.relabel(type, key, label)
    toastState.show(label === '' ? `Name removed for ${key}` : `“${label}” saved for ${key}`)
    this.close()
  }

  close() {
    if (this.anchor === null) return
    this.anchor = null
    this.provenance = null
    this.error = null
    // Invalidates any in-flight provenance request, so a slow response
    // cannot land on a popover that has since been reopened for a
    // different token.
    this.requestId++
    this.loading = false
    appState.releaseStream()
  }
}

export const nameEditorState = new NameEditorState()
