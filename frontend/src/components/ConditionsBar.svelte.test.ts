// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/svelte'
import ConditionsBar from './ConditionsBar.svelte'
import type { DefinitionCondition } from '../lib/types'

const GARAGE: DefinitionCondition[] = [
  { field: 'sourceAddress', operator: 'inCIDR', values: ['10.0.70.0/24'] },
  { field: 'action', operator: 'equals', values: ['drop'] },
]

function bar(conditions: DefinitionCondition[] = []) {
  const out = render(ConditionsBar, { conditions })
  return out
}

async function settle() {
  await new Promise((r) => setTimeout(r, 0))
}

describe('the bar draws one quiet pill per condition', () => {
  it('reads field, verb and value, with the verbs round 8 shortened', () => {
    bar(GARAGE)
    expect(screen.getByText('source')).toBeTruthy()
    expect(screen.getByText('within')).toBeTruthy()
    expect(screen.getByText('10.0.70.0/24')).toBeTruthy()
    expect(screen.getByText('is')).toBeTruthy()
    expect(screen.getByText('drop')).toBeTruthy()
  })

  it('gives every token a real × button, so removal has a keyboard path', async () => {
    const { component } = bar(GARAGE)
    void component
    const x = screen.getByRole('button', { name: 'remove the action condition' })
    await fireEvent.click(x)
    await settle()
    expect(screen.queryByText('drop')).toBeNull()
    // The other token is untouched: removing one condition is not
    // clearing the bar.
    expect(screen.getByText('10.0.70.0/24')).toBeTruthy()
  })

  it('says what an empty bar is for rather than sitting blank', () => {
    bar([])
    expect(screen.getByText('click to add a condition')).toBeTruthy()
  })

  it('to a viewer the tokens lose their × and the box goes entirely', () => {
    render(ConditionsBar, { conditions: GARAGE, readonly: true })
    expect(screen.queryByRole('button', { name: /remove the/ })).toBeNull()
    expect(screen.queryByRole('textbox')).toBeNull()
  })
})

describe('typing narrows', () => {
  it('takes fourteen fields down to two, in the drawing s3 shows', async () => {
    bar([])
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await fireEvent.input(input, { target: { value: 'dst' } })
    await settle()
    const rows = screen.getAllByRole('option')
    expect(rows).toHaveLength(2)
    expect(rows.map((r) => r.querySelector('b')?.textContent)).toEqual(['destination', 'port'])
  })

  it('closes the list when nothing matches, rather than showing everything', async () => {
    bar([])
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await fireEvent.input(input, { target: { value: 'zzz' } })
    await settle()
    expect(screen.queryAllByRole('option')).toHaveLength(0)
  })

  it('offers the field its verbs once it is taken, then values as they are typed', async () => {
    bar([])
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await fireEvent.input(input, { target: { value: 'port' } })
    await settle()
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()
    // The verbs, before anything is typed.
    expect(screen.getByText('between')).toBeTruthy()
    await fireEvent.input(input, { target: { value: '1-' } })
    await settle()
    const rows = screen.getAllByRole('option')
    expect(rows).toHaveLength(2)
    expect(rows[0].textContent).toContain('1-1024')
  })
})

describe('the keyboard model round 7 set out', () => {
  it('takes the highlighted row on enter and advances a step', async () => {
    bar([])
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await fireEvent.input(input, { target: { value: 'action' } })
    await settle()
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()
    // The half-written token now names the field and the step it is on.
    expect(screen.getByLabelText('the action value')).toBeTruthy()
  })

  it('commits a typed value straight from the bar', async () => {
    bar([])
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await fireEvent.input(input, { target: { value: 'action' } })
    await settle()
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()
    await fireEvent.input(input, { target: { value: 'drop' } })
    await settle()
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()
    expect(screen.getByText('drop')).toBeTruthy()
    // And the bar is back at the field step, ready for the next line.
    expect(screen.getByLabelText('add a condition')).toBeTruthy()
  })

  it('moves the highlight with the arrows', async () => {
    bar([])
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await settle()
    const first = screen.getAllByRole('option')[0]
    expect(first.getAttribute('aria-selected')).toBe('true')
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    await settle()
    const rows = screen.getAllByRole('option')
    expect(rows[0].getAttribute('aria-selected')).toBe('false')
    expect(rows[1].getAttribute('aria-selected')).toBe('true')
  })

  it('steps back a step on shift-tab rather than out of the bar', async () => {
    bar([])
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await fireEvent.input(input, { target: { value: 'action' } })
    await settle()
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()
    await fireEvent.keyDown(input, { key: 'Tab', shiftKey: true })
    await settle()
    expect(screen.getByLabelText('add a condition')).toBeTruthy()
  })

  it('leaves the token as it was on esc', async () => {
    bar(GARAGE)
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    // Pick the action token up to change it, then abandon the change.
    await fireEvent.click(screen.getAllByRole('button', { name: /action/ })[0])
    await settle()
    await fireEvent.input(input, { target: { value: 'accept' } })
    await settle()
    await fireEvent.keyDown(input, { key: 'Escape' })
    await settle()
    expect(screen.getByText('drop')).toBeTruthy()
    expect(screen.queryByText('accept')).toBeNull()
  })
})

describe('the unfinished line', () => {
  it('names the line to finish, and clears once it is', async () => {
    const seen: string[] = []
    render(ConditionsBar, {
      conditions: [],
      // The parent reads this to grey try and save and say which line to
      // finish beside them; here it is captured instead.
      get unfinishedLine() {
        return seen[seen.length - 1] ?? ''
      },
      set unfinishedLine(v: string) {
        seen.push(v)
      },
    } as never)
    const input = screen.getByRole('textbox')
    await fireEvent.focus(input)
    await fireEvent.input(input, { target: { value: 'port' } })
    await settle()
    await fireEvent.keyDown(input, { key: 'Enter' })
    await settle()
    await fireEvent.input(input, { target: { value: '1-' } })
    await settle()
    expect(seen).toContain('finish the port line')
    await fireEvent.input(input, { target: { value: '1-1024' } })
    await settle()
    expect(seen[seen.length - 1]).toBe('')
  })
})
