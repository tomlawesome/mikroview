// SPDX-License-Identifier: AGPL-3.0-only

// Reactive viewport-width breakpoint (issue #85): a single shared
// matchMedia listener, rather than every component that needs to know
// "are we at phone width" wiring its own resize handler. 700px matches
// the breakpoint issue #85 itself specifies -- narrow enough that the
// desktop LiveTable grid's fixed-width columns (time/device/action/...)
// no longer fit without horizontal scrolling, which is the actual
// layout problem this exists to detect, not a generic "mobile" guess.
const MOBILE_BREAKPOINT = 700

// A second, wider breakpoint (#1150): a desktop window narrow enough
// that a wide table's controls no longer fit beside its content, while
// still being nothing like a phone. 1366x768 is a desktop width and
// stays in scope of #635's desktop-first ruling -- what fails there is
// the column count, not the platform. The docket's verdict chips are the
// first user: below this they move into the row's drawer rather than
// being pushed off the right edge.
const NARROW_BREAKPOINT = 1300

class ViewportState {
  isMobile = $state(window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`).matches)
  isNarrow = $state(window.matchMedia(`(max-width: ${NARROW_BREAKPOINT}px)`).matches)

  constructor() {
    const mq = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT}px)`)
    mq.addEventListener('change', (e) => {
      this.isMobile = e.matches
    })
    const narrow = window.matchMedia(`(max-width: ${NARROW_BREAKPOINT}px)`)
    narrow.addEventListener('change', (e) => {
      this.isNarrow = e.matches
    })
  }
}

export const viewportState = new ViewportState()
