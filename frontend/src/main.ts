// SPDX-License-Identifier: AGPL-3.0-only

import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { installCancellationGuard } from './lib/cancelled'
import { onFreshnessSignal } from './lib/api'
import { onUnauthorizedClaim } from './lib/auth.svelte'
import { freshnessState } from './lib/freshness.svelte'
import { registerServiceWorker } from './lib/serviceWorker'

// Before anything can fetch: #1298's one place where a poll cut off by
// leaving the page is dropped instead of being printed as an error.
installCancellationGuard()

// #1363: api.ts cannot import freshness.svelte.ts itself (that file
// imports fetchHealthz from api.ts), so this is where the two meet.
onFreshnessSignal(() => freshnessState.checkOnErrorSignal())
// And auth.svelte.ts cannot either (freshness.svelte.ts imports it): a
// 401 mid-session asks freshness first whether it is an upgrade's
// restart, so a busy tab gets the banner rather than a reload (7a).
onUnauthorizedClaim(() => freshnessState.claimUnauthorized())

// #1314: ours rather than vite-plugin-pwa's injected registerSW.js, so
// the registration promise has a handler -- see lib/serviceWorker.ts.
// Only in a built app: there is no generated worker to register in dev
// (devOptions is off), and the plugin injected its script into the
// built index.html only, so this matches what shipped.
//
// #1363: the registration this returns is what lets a later safe
// reload hand off to a new worker before it reloads -- see
// freshnessState.reloadNow().
if (import.meta.env.PROD) {
  void registerServiceWorker().then((registration) => freshnessState.setRegistration(registration))
}

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
