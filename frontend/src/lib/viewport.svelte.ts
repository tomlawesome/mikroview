// SPDX-License-Identifier: AGPL-3.0-only

// Reactive viewport-width breakpoint (issue #85): a single shared
// matchMedia listener, rather than every component that needs to know
// "are we at phone width" wiring its own resize handler. 700px matches
// the breakpoint issue #85 itself specifies -- narrow enough that the
// desktop LiveTable grid's fixed-width columns (time/device/action/...)
// no longer fit without horizontal scrolling, which is the actual
// layout problem this exists to detect, not a generic "mobile" guess.
const MOBILE_BREAKPOINT = 700

class ViewportState {
  isMobile = $state(window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`).matches)

  constructor() {
    const mq = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`)
    mq.addEventListener('change', (e) => {
      this.isMobile = e.matches
    })
  }
}

export const viewportState = new ViewportState()
