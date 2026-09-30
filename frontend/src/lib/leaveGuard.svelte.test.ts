// SPDX-License-Identifier: AGPL-3.0-only
//
// #1363: the shared beforeunload prompt that replaced five separate
// listeners. Each of the five components still pins its own on/off
// timing (PasskeysOverlay.svelte.test.ts and siblings); this covers the
// store itself -- multiple holders, and ConfigEditor's `edited` folding
// in without it ever calling hold()/release().

import { beforeEach, describe, expect, it } from 'vitest'

import { configEditorState } from './configEditor.svelte'
import { leaveGuard } from './leaveGuard.svelte'

function dispatchBeforeUnload(): Event {
  const evt = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(evt)
  return evt
}

beforeEach(() => {
  leaveGuard.release('a')
  leaveGuard.release('b')
  configEditorState.text = ''
  configEditorState.saved = ''
})

describe('leaveGuard', () => {
  it('prompts on beforeunload while at least one id is held', () => {
    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)

    leaveGuard.hold('a')
    expect(dispatchBeforeUnload().defaultPrevented).toBe(true)

    leaveGuard.release('a')
    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)
  })

  it('keeps prompting while any other id is still held', () => {
    leaveGuard.hold('a')
    leaveGuard.hold('b')
    leaveGuard.release('a')

    expect(dispatchBeforeUnload().defaultPrevented).toBe(true)

    leaveGuard.release('b')
    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)
  })

  it("folds ConfigEditor's edited flag in without it calling hold/release itself", () => {
    configEditorState.saved = 'a'
    configEditorState.text = 'a'
    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)

    configEditorState.text = 'a b'
    expect(dispatchBeforeUnload().defaultPrevented).toBe(true)

    configEditorState.text = configEditorState.saved
    expect(dispatchBeforeUnload().defaultPrevented).toBe(false)
  })
})
