// SPDX-License-Identifier: AGPL-3.0-only
//
// The atlas overlay's one piece of state. Under the Atlas identity
// (owner ratification, 2026-08-29) the app has no persistent chrome:
// pages are the site, and navigation lives behind one gesture -- the
// wordmark (or `m`) opens the atlas, a full-screen navigator drawn
// from the same boundary model the fall renders. #485's Map replaces
// the overlay's chart when it lands; this state object is the socket.

class AtlasNavState {
  open = $state(false)

  toggle() {
    this.open = !this.open
  }
}

export const atlasNav = new AtlasNavState()
