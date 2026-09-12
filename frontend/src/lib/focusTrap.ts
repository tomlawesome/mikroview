// SPDX-License-Identifier: AGPL-3.0-only
//
// Svelte action that keeps Tab/Shift+Tab cycling inside the node it is
// attached to, and moves focus into it as soon as it mounts. #550's
// half-sheet is the first control in this codebase to need a *real*
// trap: on a small screen it is the only way to reach a group's other
// pages, so tabbing out of it must not be possible while it is open.
//
// The dialogs that ask for something rather than merely show it need it
// too (#1182): Change password's two fields were unreachable by
// keyboard, because focus never moved into the dialog and Tab then
// walked the page behind it. Where an overlay is one of several ways to
// reach the same page (EventDetailSheet, ...), letting focus wander is
// still fine -- the trap goes where not being able to leave is the
// point, not on every dialog in the app.

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

function focusables(node: HTMLElement): HTMLElement[] {
  return Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
}

/**
 * trapFocus(node) focuses the first focusable descendant (falling back to
 * the node itself, which callers give tabindex="-1" for exactly this
 * case) and wraps Tab at the last element back to the first, and
 * Shift+Tab at the first back to the last. Restores focus to whatever
 * had it before the trap mounted once the node unmounts, since a sheet
 * that closes should hand focus back to the control that opened it
 * rather than dropping it to the document body.
 */
export function trapFocus(node: HTMLElement) {
  const previouslyFocused = document.activeElement as HTMLElement | null

  const first = focusables(node)[0]
  ;(first ?? node).focus()

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Tab') return
    const items = focusables(node)
    if (items.length === 0) {
      // Nothing tabbable inside -- keep focus on the node itself rather
      // than letting Tab escape to the page behind it.
      e.preventDefault()
      return
    }
    const firstEl = items[0]
    const lastEl = items[items.length - 1]
    if (e.shiftKey && document.activeElement === firstEl) {
      e.preventDefault()
      lastEl.focus()
    } else if (!e.shiftKey && document.activeElement === lastEl) {
      e.preventDefault()
      firstEl.focus()
    }
  }

  node.addEventListener('keydown', onKeydown)

  return {
    destroy() {
      node.removeEventListener('keydown', onKeydown)
      previouslyFocused?.focus()
    },
  }
}
