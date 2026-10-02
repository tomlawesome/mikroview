// SPDX-License-Identifier: AGPL-3.0-only

// The blocklist builder's components (#1360), behind one dynamic import:
// the page (App.svelte) and the wizard's first-run tail (Wizard.svelte)
// both load this module the first time they need it, so the builder
// ships as one chunk outside the entry bundle.
export { default as Builder } from './Builder.svelte'
export { default as BuilderBody } from './BuilderBody.svelte'
