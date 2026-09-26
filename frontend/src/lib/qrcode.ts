// SPDX-License-Identifier: AGPL-3.0-only
//
// Draws a TOTP enrolment URI as a QR code (#1249), onto a <canvas> the
// caller already has in the DOM -- not an <img src="data:...">, which
// main.go's CSP (`Content-Security-Policy: default-src 'self'`, no
// img-src override) would refuse: a data: URI is not 'self'. Canvas
// drawing is plain script writing pixels into an element already on the
// page, so it never touches that policy at all.
//
// A Svelte action rather than a component: AuthenticatorOverlay owns the
// canvas element and its layout (sitting beside the text secret, per the
// issue's "always" -- see that component), this just fills it in.

import QRCode from 'qrcode'

/**
 * qrCode(node, uri) renders `uri` into the canvas node, and re-renders
 * whenever the action is called again with a new uri (Svelte's own
 * update contract for a `use:` directive whose argument changes). Errors
 * (a malformed uri, which should never happen against this app's own
 * server) are left on the canvas as whatever qrcode already drew -- the
 * text secret beside it is the same information in a form that never
 * depends on this succeeding.
 */
export function qrCode(node: HTMLCanvasElement, uri: string) {
  function render(value: string) {
    // margin: 1 keeps the code's quiet zone tight -- the default (4
    // modules) reads fine on paper but wastes space in a dialog this
    // narrow. width is the bitmap drawn, not the size shown: each caller's
    // CSS scales it down to its own tile (120px, 88px), which keeps the
    // modules sharp on a high-density screen.
    QRCode.toCanvas(node, value, { margin: 1, width: 176 })
      .then(() => {
        // #1364: toCanvas also sets an inline width and height of the
        // drawn size, which beats the caller's CSS -- the code spilled
        // out of its tile and over the secret text beneath it.
        node.style.removeProperty('width')
        node.style.removeProperty('height')
      })
      .catch(() => {
        // Nothing sensible to show in place of a QR code that failed to
        // draw -- the secret text beside it (AuthenticatorOverlay's own)
        // still lets enrolment finish by typing it in instead of scanning.
      })
  }

  render(uri)

  return {
    update(next: string) {
      render(next)
    },
  }
}
