// SPDX-License-Identifier: AGPL-3.0-only

import { fetchHealthz } from './api'

// Setup-only mode (#1347): when the config file is refused at start-up,
// the server answers only sign-in, /api/healthz and the config editor's
// routes, and every other /api call is a 503. GET /api/healthz says so
// as `mode: "setup-only"`, and needs no session, so it is read once at
// boot beside authState.check() -- the same one-shot healthz read as
// versionState and geoipState.
//
// App.svelte holds the shell back until this has answered (`loaded`),
// so a refused config never mounts the deck, its polls or the socket:
// each of those would only meet a 503. A healthz that cannot be read at
// all counts as a normal start -- the shell's own connection handling
// is what reports an unreachable server.
class ServerModeState {
  setupOnly = $state(false)
  loaded = $state(false)
  #loading: Promise<void> | null = null

  load(): Promise<void> {
    if (!this.#loading) {
      this.#loading = (async () => {
        try {
          const healthz = await fetchHealthz()
          this.setupOnly = healthz.mode === 'setup-only'
        } catch {
          this.setupOnly = false
        } finally {
          this.loaded = true
        }
      })()
    }
    return this.#loading
  }
}

export const serverModeState = new ServerModeState()
