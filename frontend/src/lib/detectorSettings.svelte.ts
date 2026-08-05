import { fetchDetectorSettings, updateDetectorSettings } from './api'
import type { DetectorScope, DetectorSettings } from './types'

// Live per-detector on/off + scope settings (see internal/detect.Settings)
// -- admin-only, mirrors flags.svelte.ts's shape.
class DetectorSettingsState {
  list = $state<DetectorSettings[]>([])

  async refresh() {
    this.list = await fetchDetectorSettings()
  }

  async update(name: string, enabled: boolean, scope: DetectorScope): Promise<string | null> {
    const err = await updateDetectorSettings(name, enabled, scope)
    if (!err) await this.refresh()
    return err
  }
}

export const detectorSettingsState = new DetectorSettingsState()
