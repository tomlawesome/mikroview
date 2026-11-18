// SPDX-License-Identifier: AGPL-3.0-only
//
// Shared by the dialog-keyboard tests (#1539 C2/C3): a focused trigger to
// return to, and a probe that a dialog traps Tab the way lib/focusTrap.ts
// does. Test-only; nothing in the app imports it.
import { expect } from 'vitest'

const FOCUSABLE =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

/** A real button outside the dialog, focused -- what opened the dialog. */
export function focusedTrigger(label = 'opener'): HTMLButtonElement {
  const b = document.createElement('button')
  b.type = 'button'
  b.textContent = label
  document.body.appendChild(b)
  b.focus()
  expect(document.activeElement).toBe(b)
  return b
}

function tab(from: HTMLElement, shiftKey: boolean): KeyboardEvent {
  const e = new KeyboardEvent('keydown', { key: 'Tab', shiftKey, bubbles: true, cancelable: true })
  from.dispatchEvent(e)
  return e
}

/** Tab off the last control wraps to the first; Shift+Tab off the first wraps to the last. */
export function expectTabWraps(dialog: HTMLElement) {
  const items = [...dialog.querySelectorAll<HTMLElement>(FOCUSABLE)]
  expect(items.length).toBeGreaterThan(1)
  const first = items[0]
  const last = items[items.length - 1]

  last.focus()
  expect(tab(last, false).defaultPrevented).toBe(true)
  expect(document.activeElement).toBe(first)

  first.focus()
  expect(tab(first, true).defaultPrevented).toBe(true)
  expect(document.activeElement).toBe(last)
}

/** Focus is somewhere inside the dialog (the node itself counts). */
export function expectFocusInside(dialog: HTMLElement) {
  expect(dialog.contains(document.activeElement)).toBe(true)
}
