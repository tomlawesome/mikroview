<script lang="ts">
  // SPDX-License-Identifier: AGPL-3.0-only
  //
  // The conditions bar (#829, settings round 8 -- the ratified drawing is
  // docs/design/screens/settings/round-8/conditions-editor.html, and this
  // is a port of its markup and CSS rather than a reading of it).
  //
  // GitLab's filter bar with the decoration taken off. One box: the
  // stream's own .fbox, --bg-elevated under one hairline, --accent and a
  // soft ring while it is the thing being worked in. One quiet pill per
  // condition -- field and verb as dim words, the value bold in the ink
  // the app already gives that thing everywhere else -- and nothing
  // joining the pills but a gap. The list opens inside the same box under
  // one hairline; there is never a second edge.
  //
  // Why the input is always mounted, in the same place, whatever step the
  // bar is on: the bar is one focus stop (round 7's keyboard model), and
  // an input that moved between DOM parents as the step changed would be
  // remounted by Svelte and lose the caret mid-word. The step changes what
  // is drawn around it, never where it is.
  //
  // Unfinished is --now, never --alarm. A half-written condition is not an
  // error -- it is a line someone is in the middle of -- so only the value
  // goes amber, with its caret, and the drawer's own amber line beside the
  // waiting pills says which line to finish.
  import {
    conditionFrom,
    editText,
    fieldSpec,
    inkFor,
    narrowFields,
    narrowValues,
    valueText,
    verbFor,
    verbLabel,
  } from '../lib/conditionTokens'
  import type { DefinitionCondition } from '../lib/types'

  let {
    conditions = $bindable<DefinitionCondition[]>([]),
    unfinishedLine = $bindable(''),
    readonly = false,
    onchange,
  }: {
    conditions: DefinitionCondition[]
    // Set while a half-written token is in the bar; see the effect below.
    unfinishedLine?: string
    readonly?: boolean
    // Called whenever the committed conditions change, so the drawer can
    // work out whether Save has anything to send.
    onchange?: (next: DefinitionCondition[]) => void
  } = $props()

  // Which step the half-written token is on. 'field' until a field is
  // chosen, 'value' after -- there is no third step, because the verb is
  // read off the shape of what is being typed rather than asked for
  // separately (see lib/conditionTokens.parseValue).
  let step = $state<'field' | 'value'>('field')
  let pendingField = $state('')
  // A verb chosen off the list before anything was typed. Only ever wins
  // over a bare single value; see conditionFrom.
  let pinned = $state<string | undefined>(undefined)
  let typed = $state('')
  let highlight = $state(0)
  let focused = $state(false)
  let input = $state<HTMLInputElement | null>(null)

  // The rows the list is showing, and what each one does when taken.
  // Derived rather than held, so nothing can leave a stale list open over
  // a field that has since changed.
  const rows = $derived.by(() => {
    if (step === 'field') {
      return narrowFields(typed).map((f) => ({
        value: f.label,
        hint: f.hint,
        ink: f.ink,
        field: f.field,
      }))
    }
    const spec = fieldSpec(pendingField)
    if (!spec) return []
    return narrowValues(spec, typed).map((r) => ({
      value: r.value,
      hint: r.hint,
      ink: spec.ink,
      field: '',
    }))
  })

  const open = $derived(focused && rows.length > 0 && !readonly)

  // The verb the unfinished token shows. It updates as the value is
  // typed, which is the whole point of reading the operator off the
  // shape: the token says `between` the moment a dash appears.
  const pendingVerb = $derived(step === 'value' ? verbFor(pendingField, typed, pinned) : '')
  const pendingLabel = $derived(fieldSpec(pendingField)?.label ?? '')
  const pendingInk = $derived(fieldSpec(pendingField)?.ink ?? 'var(--accent)')

  // What is stopping this bar from being finished, in the words the
  // drawer's amber line uses -- one line naming the line to finish, not a
  // description of a rule. Empty means nothing is.
  //
  // Reported upward rather than drawn here, because the thing it governs
  // is not in this component: the drawer greys try and save and puts this
  // sentence beside them (round 7, "Unfinished is --now, never --alarm").
  $effect(() => {
    if (step !== 'value') {
      unfinishedLine = ''
      return
    }
    const built = conditionFrom(pendingField, typed, pinned)
    unfinishedLine = 'error' in built ? `finish the ${pendingLabel} line` : ''
  })

  $effect(() => {
    // Reset the highlight whenever the list's contents change under it,
    // so enter never takes a row the operator cannot see.
    void rows
    highlight = 0
  })

  function focus() {
    if (readonly) return
    input?.focus()
  }

  function chooseField(field: string) {
    pendingField = field
    step = 'value'
    pinned = undefined
    typed = ''
    input?.focus()
  }

  function takeRow(index: number) {
    const row = rows[index]
    if (!row) return
    if (step === 'field') {
      chooseField(row.field)
      return
    }
    // At the value step with nothing typed the rows are the field's
    // verbs; with something typed they are values. Which one this is
    // decides whether taking it pins a comparison or finishes the token.
    if (typed.trim() === '') {
      const spec = fieldSpec(pendingField)
      const verb = spec?.verbs.find((v) => v.label === row.value)
      if (verb) {
        pinned = verb.operator
        input?.focus()
        return
      }
    }
    typed = row.value
    commit()
  }

  // commit turns the half-written token into a condition, or leaves it
  // alone. Leaving it alone is not a failure state to report here: the
  // amber line is already saying which line to finish, and a second
  // complaint on every keystroke would be noise.
  function commit() {
    if (step !== 'value') return
    const built = conditionFrom(pendingField, typed, pinned)
    if ('error' in built) return
    // One condition per field is the engine's rule (compileConditions
    // refuses a duplicate), so a second line on the same field replaces
    // the first rather than being added beside it and refused on save.
    const next = conditions.filter((c) => c.field !== built.field)
    conditions = [...next, built]
    onchange?.(conditions)
    liftedBack = null
    step = 'field'
    pendingField = ''
    pinned = undefined
    typed = ''
    input?.focus()
  }

  // commitPending is commit()'s other caller (#1075): the drawer's Save
  // reads only the committed `conditions`, so a token still sitting in
  // `typed` -- never finished with Enter or a list pick -- would
  // otherwise be sent to the server as if the operator never wrote it.
  // Called from the drawer just before it reads the conditions back out.
  // A token that parses commits itself, the same path Enter takes; one
  // that does not is left alone, because unfinishedLine (the $effect
  // above) is already carrying the reason and the drawer's amber line is
  // already showing it -- this only reports whether Save may proceed.
  export function commitPending(): boolean {
    if (step !== 'value') return true
    const built = conditionFrom(pendingField, typed, pinned)
    if ('error' in built) return false
    commit()
    return true
  }

  function remove(field: string) {
    conditions = conditions.filter((c) => c.field !== field)
    onchange?.(conditions)
    input?.focus()
  }

  // Picking a token up puts it back into the bar as the half-written one,
  // so changing a condition is the same gesture as writing it. The token
  // leaves the committed list while it is being edited; abandoning the
  // edit with esc puts it back, because esc "leaves the token as it was".
  let liftedBack: DefinitionCondition | null = null
  function lift(c: DefinitionCondition) {
    if (readonly) return
    liftedBack = c
    conditions = conditions.filter((x) => x.field !== c.field)
    pendingField = c.field
    pinned = c.operator
    typed = editText(c)
    step = 'value'
    input?.focus()
  }

  function abandon() {
    if (liftedBack) {
      conditions = [...conditions.filter((c) => c.field !== liftedBack!.field), liftedBack]
      onchange?.(conditions)
    }
    liftedBack = null
    step = 'field'
    pendingField = ''
    pinned = undefined
    typed = ''
  }

  function stepBack() {
    if (step !== 'value') return
    // Back to the field step with the field's own name in the box, which
    // is what was typed to get here -- so shift-tab reads as undoing the
    // last choice rather than as clearing the line.
    typed = pendingLabel
    step = 'field'
    pendingField = ''
    pinned = undefined
  }

  function onkey(e: KeyboardEvent) {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        highlight = rows.length === 0 ? 0 : (highlight + 1) % rows.length
        return
      case 'ArrowUp':
        e.preventDefault()
        highlight = rows.length === 0 ? 0 : (highlight - 1 + rows.length) % rows.length
        return
      case 'Enter':
        e.preventDefault()
        if (step === 'value' && typed.trim() !== '') {
          commit()
          return
        }
        takeRow(highlight)
        return
      case 'Escape':
        e.preventDefault()
        abandon()
        return
      case 'Tab':
        // Shift-tab steps back a step rather than out of the bar, but
        // only while there is a step to go back to -- otherwise the bar
        // would be a focus trap.
        if (e.shiftKey && step === 'value') {
          e.preventDefault()
          stepBack()
        }
        return
      case 'Backspace':
        if (typed === '' && step === 'value') {
          e.preventDefault()
          stepBack()
        }
        return
    }
  }
</script>

<!-- The box is a click target for the input inside it, which is what the
     drawing's `cursor: text` bar is; it is not itself interactive, and
     every actual control in it (the input, each token's ×, each list row)
     is a real focusable element. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div class="box" class:on={focused && !readonly} class:ro={readonly}>
  <div class="bar" onclick={focus}>
    {#each conditions as c (c.field)}
      {@const spec = fieldSpec(c.field)}
      <span class="tok" style="--vi: {inkFor(c)}">
        {#if readonly}
          <span class="f">{spec?.label ?? c.field}</span>
          <span class="o">{verbLabel(c.operator)}</span>
          <span class="tv">{valueText(c)}</span>
        {:else}
          <button type="button" class="pick" onclick={(e) => { e.stopPropagation(); lift(c) }}>
            <span class="f">{spec?.label ?? c.field}</span>
            <span class="o">{verbLabel(c.operator)}</span>
            <span class="tv">{valueText(c)}</span>
          </button>
          <button
            type="button"
            class="tx"
            aria-label="remove the {spec?.label ?? c.field} condition"
            onclick={(e) => { e.stopPropagation(); remove(c.field) }}>×</button
          >
        {/if}
      </span>
    {/each}

    {#if !readonly}
      <span class="tok wait" class:bare={step === 'field'} style="--vi: {pendingInk}">
        {#if step === 'value'}
          <span class="f">{pendingLabel}</span>
          <span class="o">{pendingVerb}</span>
        {/if}
        <input
          bind:this={input}
          class="tv"
          class:acc={step === 'field'}
          type="text"
          autocomplete="off"
          spellcheck="false"
          aria-label={step === 'field' ? 'add a condition' : `the ${pendingLabel} value`}
          style="width: {Math.max(typed.length, 1)}ch"
          bind:value={typed}
          onkeydown={onkey}
          onfocus={() => (focused = true)}
          onblur={() => (focused = false)}
        />
        {#if step === 'value'}
          <button
            type="button"
            class="tx"
            aria-label="drop the {pendingLabel} line"
            onmousedown={(e) => e.preventDefault()}
            onclick={abandon}>×</button
          >
        {/if}
      </span>
      {#if step === 'field' && typed === ''}
        <span class="placeholder">click to add a condition</span>
      {/if}
    {/if}
  </div>

  {#if open}
    <div class="dd" role="listbox" aria-label={step === 'field' ? 'fields' : 'values'}>
      {#each rows as row, i (row.value)}
        <div
          class="ddrow"
          class:hl={i === highlight}
          style="--vi: {row.ink}"
          role="option"
          aria-selected={i === highlight}
          tabindex="-1"
          onmousedown={(e) => e.preventDefault()}
          onclick={() => takeRow(i)}
        >
          <b>{row.value}</b><span class="hint">{row.hint}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  /* FilterBar.svelte's .fbox: the one boxed input on the whole surface. */
  .box {
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 7px;
  }
  .box.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 11%, transparent);
  }
  /* To a viewer the box goes entirely -- the same pills, read-only. */
  .box.ro {
    background: none;
    border-color: transparent;
  }
  .bar {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 10px;
    align-items: center;
    min-height: 34px;
    padding: 5px 11px;
    font: 12px var(--font-mono);
    color: var(--fg-muted);
    cursor: text;
  }
  .box.ro .bar {
    padding-left: 0;
    cursor: default;
  }
  .bar .placeholder {
    color: var(--fg-dim);
    font-size: 11.5px;
  }

  /* GitLab's token, undressed: field, verb and value in one quiet pill on
     one wash, no segments. --bg-hover is the pill's only wash -- hovering
     a token does not change it, hovering its × brightens the ×. */
  .tok {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 5px 3px 9px;
    border-radius: 4px;
    background: var(--bg-hover);
    font: 11px var(--font-mono);
    white-space: nowrap;
    line-height: 15px;
  }
  .tok .f {
    color: var(--fg-muted);
  }
  .tok .o {
    color: var(--fg-dim);
  }
  .tok .tv {
    color: var(--vi);
    font-weight: 700;
  }
  .tok .tx {
    cursor: pointer;
    color: var(--fg-dim);
    padding: 0 3px;
    font-size: 11px;
    background: none;
    border: none;
    font-family: inherit;
    line-height: inherit;
  }
  .tok .tx:hover {
    color: var(--fg);
  }
  /* Picking a token up to change it is the same gesture as writing it,
     so the whole pill is the button -- drawn as the pill, never as a
     control with an edge of its own. */
  .pick {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: inherit;
    cursor: pointer;
  }

  /* Unfinished is --now, the engine room's time colour, never --alarm.
     Only the value goes amber -- the pill stays the pill. */
  .tok.wait .tv {
    color: var(--now);
    caret-color: var(--now);
  }
  /* At the field step there is no token yet, only what is being typed. */
  .tok.bare {
    background: none;
    padding: 0;
  }
  .tok.bare .tv {
    font: 700 11px var(--font-mono);
    color: var(--fg);
    caret-color: var(--accent);
  }
  input.tv {
    background: none;
    border: none;
    outline: none;
    padding: 0;
    min-width: 1ch;
    font: 700 11px var(--font-mono);
  }

  /* The list: rows of text inside the same box, under one hairline. No
     breadcrumb above, no note below -- the token being built shows the
     step. */
  .dd {
    border-top: 1px solid var(--hair-2);
    padding: 5px 0 6px;
  }
  .ddrow {
    display: flex;
    gap: 14px;
    align-items: baseline;
    padding: 5px 13px;
    font: 11.5px var(--font-mono);
    color: var(--fg);
    cursor: pointer;
  }
  .ddrow b {
    font-weight: 700;
    color: var(--vi);
    min-width: 96px;
  }
  .ddrow .hint {
    color: var(--fg-dim);
    font-size: 10.5px;
  }
  .ddrow:hover {
    background: var(--bg-hover);
  }
  .ddrow.hl {
    background: var(--bg-hover);
    box-shadow: inset 2px 0 0 var(--accent);
  }
</style>
