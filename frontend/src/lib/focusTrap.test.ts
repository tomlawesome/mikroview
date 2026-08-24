// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, afterEach } from 'vitest'
import { trapFocus } from './focusTrap'

// Built with createElement rather than an innerHTML assignment --
// guards/injection-sinks.test.ts forbids that sink across src/ regardless
// of whether the string is a literal, so test fixtures follow the same
// rule as app code.
function mountTrap(labels: string[]) {
  const node = document.createElement('div')
  for (const label of labels) {
    const button = document.createElement('button')
    button.id = label
    button.textContent = label
    node.appendChild(button)
  }
  document.body.appendChild(node)
  const action = trapFocus(node)
  return { node, destroy: action?.destroy ?? (() => {}) }
}

afterEach(() => {
  document.body.replaceChildren()
})

describe('trapFocus', () => {
  it('focuses the first focusable descendant on mount', () => {
    const { node } = mountTrap(['a', 'b'])
    expect(document.activeElement).toBe(node.querySelector('#a'))
  })

  it('wraps Tab from the last item back to the first', () => {
    const { node } = mountTrap(['a', 'b'])
    const b = node.querySelector('#b') as HTMLButtonElement
    b.focus()
    const evt = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    node.dispatchEvent(evt)
    expect(evt.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(node.querySelector('#a'))
  })

  it('wraps Shift+Tab from the first item back to the last', () => {
    const { node } = mountTrap(['a', 'b'])
    const a = node.querySelector('#a') as HTMLButtonElement
    a.focus()
    const evt = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true })
    node.dispatchEvent(evt)
    expect(evt.defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(node.querySelector('#b'))
  })

  it('leaves an ordinary Tab in the middle of the list alone', () => {
    const { node } = mountTrap(['a', 'b', 'c'])
    const b = node.querySelector('#b') as HTMLButtonElement
    b.focus()
    const evt = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    node.dispatchEvent(evt)
    expect(evt.defaultPrevented).toBe(false)
  })

  it('keeps focus put when nothing inside is focusable', () => {
    // Falls back to the node itself, which callers give tabindex="-1" --
    // set before trapFocus runs, since its initial focus() call is
    // synchronous.
    const node = document.createElement('div')
    node.tabIndex = -1
    document.body.appendChild(node)
    trapFocus(node)

    const evt = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    node.dispatchEvent(evt)
    expect(evt.defaultPrevented).toBe(true)
  })

  it('restores focus to whatever had it before the trap mounted, on destroy', () => {
    const opener = document.createElement('button')
    document.body.appendChild(opener)
    opener.focus()

    const { destroy } = mountTrap(['a'])
    expect(document.activeElement).not.toBe(opener)

    destroy()
    expect(document.activeElement).toBe(opener)
  })
})
