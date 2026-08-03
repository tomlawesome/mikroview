import { lookupIp } from './api'
import type { ReputationResult } from './types'

interface Anchor {
  ip: string
  x: number
  y: number
}

// Drives the single IpLookupPopover instance mounted at the app root (see
// App.svelte). Kept as one shared singleton rather than per-row state so
// only one popover can ever be open at a time, and so the trigger button
// (rendered once per row, per IP) doesn't need to own any lookup state
// itself -- it just calls open() with its own screen position.
class IpLookupState {
  anchor = $state<Anchor | null>(null)
  result = $state<ReputationResult | null>(null)
  loading = $state(false)
  error = $state<string | null>(null)

  private requestId = 0

  open(ip: string, rect: DOMRect) {
    this.anchor = { ip, x: rect.left, y: rect.bottom }
    this.result = null
    this.error = null
    this.loading = true

    const id = ++this.requestId
    lookupIp(ip).then(
      (r) => {
        if (id !== this.requestId) return
        this.result = r
        this.loading = false
      },
      () => {
        if (id !== this.requestId) return
        this.error = 'Lookup failed'
        this.loading = false
      },
    )
  }

  close() {
    this.anchor = null
  }
}

export const ipLookupState = new IpLookupState()
