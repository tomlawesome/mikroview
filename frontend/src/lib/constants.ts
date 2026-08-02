// How many events the client keeps in memory for live filtering. Deeper
// history is fetched on demand via "load older" against the server's
// much larger retained buffer, not held in the browser.
export const MAX_CLIENT_EVENTS = 5000

// How many rows are actually rendered in the DOM at once. Kept well below
// MAX_CLIENT_EVENTS so scrolling stays smooth without needing a
// virtual-scroll library.
export const MAX_RENDERED_ROWS = 800
