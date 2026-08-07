// SPDX-License-Identifier: AGPL-3.0-only

// The Worker entry point. A thin shim over matchingIds on purpose:
// anything running in here is unkillable short of terminating the whole
// Worker, so there should be as little of it as possible.

import { matchingIds, type MatchCandidate } from './ruleMatcher'

self.onmessage = (e: MessageEvent) => {
  const { id, pattern, candidates } = e.data as {
    id: number
    pattern: string
    candidates: MatchCandidate[]
  }
  try {
    self.postMessage({ id, ids: matchingIds(pattern, candidates) })
  } catch {
    // An invalid pattern, not a slow one -- the caller treats it the way
    // it always has: the rule filter is ignored.
    self.postMessage({ id, invalid: true })
  }
}
