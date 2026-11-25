// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

// The real library sets the canvas's inline width and height to the
// drawn size (node_modules/qrcode/lib/renderer/canvas.js) -- jsdom has
// no 2D context to draw with, so the stub does just that part.
vi.mock('qrcode', () => ({
  default: {
    toCanvas: vi.fn(async (canvas: HTMLCanvasElement, _text: string, opts: { width: number }) => {
      canvas.style.width = `${opts.width}px`
      canvas.style.height = `${opts.width}px`
    }),
  },
}))

import { qrCode } from './qrcode'

describe('qrCode', () => {
  // #1364: the library's inline 176px beat each component's CSS size, so
  // the code spilled out of its tile and covered the secret beneath it.
  it('leaves the displayed size to the CSS', async () => {
    const canvas = document.createElement('canvas')
    qrCode(canvas, 'otpauth://totp/MikroView:alice?secret=ABCD')
    await vi.waitFor(() => expect(canvas.style.width).toBe(''))
    expect(canvas.style.height).toBe('')
  })

  it('leaves it to the CSS after an update too', async () => {
    const canvas = document.createElement('canvas')
    const action = qrCode(canvas, 'otpauth://totp/MikroView:alice?secret=ABCD')
    await vi.waitFor(() => expect(canvas.style.width).toBe(''))
    action.update('otpauth://totp/MikroView:alice?secret=EFGH')
    await vi.waitFor(() => expect(canvas.style.width).toBe(''))
    expect(canvas.style.height).toBe('')
  })
})
