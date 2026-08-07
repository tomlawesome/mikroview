import { fetchHealthz } from './api'

// The running server's build version (main.version's short commit SHA,
// "dev" for a plain local build) -- fetched once and cached, same
// "changes only on restart, not from anything the UI does" reasoning
// criticalPortsState already documents. GET /api/healthz is reachable
// with no auth and no session regardless of deployment state, so this
// works identically for every viewer.
class VersionState {
  version = $state('')
  private loaded = false

  async ensureLoaded() {
    if (this.loaded) return
    this.loaded = true
    const healthz = await fetchHealthz()
    this.version = healthz.version
  }
}

export const versionState = new VersionState()
