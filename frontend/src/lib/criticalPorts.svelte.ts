import { fetchCriticalPorts } from './api'

// The configured control-port list (issue #34's tracking tab) --
// fetched once and cached, unlike detectorSettingsState which re-fetches
// on every open: this list only changes on a config reload, not from
// anything the UI itself does.
class CriticalPortsState {
  ports = $state<number[]>([])
  private loaded = false

  async ensureLoaded() {
    if (this.loaded) return
    this.loaded = true
    this.ports = await fetchCriticalPorts()
  }
}

export const criticalPortsState = new CriticalPortsState()
