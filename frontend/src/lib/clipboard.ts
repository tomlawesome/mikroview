// SPDX-License-Identifier: AGPL-3.0-only
//
// One copy-to-clipboard path for the whole app.
//
// navigator.clipboard.writeText requires a secure context (HTTPS or
// localhost). mikroview serves TLS by default and needs no configuration
// to get it (README.md), but docs/configuration.md's "Running behind a
// reverse proxy" section explicitly documents disabling it
// (MIKROVIEW_TLS_ENABLED=false) for an operator whose own reverse proxy
// terminates TLS instead -- on an isolated management network that is a
// supported deployment, not a misconfiguration, and it leaves the
// Clipboard API unavailable. The legacy path below (a hidden textarea
// plus document.execCommand('copy'), which carries no secure-context
// requirement) is what keeps a copy control doing real work there
// instead of silently failing every time it is clicked.
//
// Extracted from CopyButton.svelte (#439) when #488's Table view needed
// the same behaviour for a whole table of figures rather than one token:
// that control needs its own label and its own affordance, but it must
// not need its own second-guess at the fallback.

function legacyCopy(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  // Off-screen rather than hidden -- a hidden or zero-size element
  // cannot be selected, which execCommand('copy') needs.
  ta.style.position = 'fixed'
  ta.style.top = '-1000px'
  ta.style.left = '-1000px'
  document.body.appendChild(ta)
  ta.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  document.body.removeChild(ta)
  return ok
}

/** Copies `value`, falling back where the Clipboard API is unavailable. */
export async function copyToClipboard(value: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return true
    } catch {
      // fall through to the legacy path below
    }
  }
  return legacyCopy(value)
}
