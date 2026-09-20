// SPDX-License-Identifier: AGPL-3.0-only

import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { installCancellationGuard } from './lib/cancelled'

// Before anything can fetch: #1298's one place where a poll cut off by
// leaving the page is dropped instead of being printed as an error.
installCancellationGuard()

const app = mount(App, {
  target: document.getElementById('app')!,
})

export default app
