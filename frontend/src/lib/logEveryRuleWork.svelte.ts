// SPDX-License-Identifier: AGPL-3.0-only
//
// The operator's in-progress work on Log every rule (#435/#1134): the
// export they pasted or dropped, and what Analyse/Render made of it.
// Module-lifetime state, not component state -- Deck.svelte unmounts a
// card's scene whenever it scrolls more than one card from the active
// one (lib/deckMount.ts's mount rule), which used to throw away
// whatever the operator had pasted, and any rendered-but-not-yet-
// downloaded result, the moment they scrolled elsewhere and back. This
// is still exactly the ephemerality LogEveryRule.svelte's own note
// promises -- never written to disk, localStorage or a log, gone the
// moment the tab closes or reloads -- just no longer tied to one
// component instance's mount lifetime within that session.
import type { TuneLoggingAnalyseResponse, TuneLoggingRenderResponse } from './types'

class LogEveryRuleWorkState {
  device = $state('')
  exportText = $state('')
  // What the drop zone says it is holding. A dropped or chosen file is
  // named by the file; a paste has no name of its own, so it says so.
  exportName = $state('')
  result = $state<TuneLoggingAnalyseResponse | null>(null)
  selected = $state<Set<number>>(new Set())
  // The non-dark group starts collapsed (contract §6): "the rest shown
  // collapsed, unticked".
  showOther = $state(false)
  renderResult = $state<TuneLoggingRenderResponse | null>(null)
  // Whether the rendered result has been downloaded or copied at least
  // once -- what the beforeunload guard reads. Cleared whenever a fresh
  // render arrives, since that is a new unsaved result.
  resultSaved = $state(false)

  // A new or edited export invalidates whatever was derived from the
  // old one -- an analyse result, a render, and the guard around it all
  // describe text that is no longer what is in the box.
  resetDownstream() {
    this.result = null
    this.selected = new Set()
    this.showOther = false
    this.renderResult = null
    this.resultSaved = false
  }

  /** Clears the export itself too, not just what was derived from it.
   * LogEveryRule.svelte calls this when a nav request names a
   * different router, since this module's lifetime would otherwise
   * carry one router's export into another's view. Tests call it for
   * the same reason across renders. */
  reset() {
    this.device = ''
    this.exportText = ''
    this.exportName = ''
    this.resetDownstream()
  }
}

export const logEveryRuleWorkState = new LogEveryRuleWorkState()
