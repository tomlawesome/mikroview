// SPDX-License-Identifier: AGPL-3.0-only

import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { installCancellationGuard } from './lib/cancelled'
import { registerServiceWorker } from './lib/serviceWorker'

// Before anything can fetch: #1298's one place where a poll cut off by
// leaving the page is dropped instead of being printed as an error.
installCancellationGuard()

// #1314: ours rather than vite-plugin-pwa's injected registerSW.js, so
// the registration promise has a handler -- see lib/serviceWorker.ts.
// Only in a built app: there is no generated worker to register in dev
// (devOptions is off), and the plugin injected its script into the
// built index.html only, so this matches what shipped.
if (import.meta.env.PROD) registerServiceWorker()

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
