// SPDX-License-Identifier: AGPL-3.0-only

import { fetchHealthz } from './api'

/** The GeoIP setup section, for the "no database" messaging's own link
 *  (#1198) -- the country filter's disabled row and the ingest card's
 *  line both point here rather than each holding the URL themselves. */
export const GEOIP_DOCS_URL =
  'https://github.com/tomlawesome/mikroview/blob/main/docs/configuration.md#geoip-country-flags-optional'

// Whether the running server has a GeoIP country database open (#1198).
// Two surfaces need this to explain missing flags rather than just show
// them blank -- FilterBar's country selects (a disabled explainer row)
// and EngineRoom's ingest card (one line) -- and both read this same
// cached value rather than each fetching /api/healthz on their own.
//
// null until the first fetch resolves, so neither surface flashes "no
// database" before it actually knows -- only an explicit false triggers
// the messaging. Fetched once and cached like versionState: this only
// changes on a restart, never from anything the UI does.
class GeoipState {
  enabled = $state<boolean | null>(null)
  private loaded = false

  async ensureLoaded() {
    if (this.loaded) return
    this.loaded = true
    const healthz = await fetchHealthz()
    this.enabled = healthz.geoip
  }
}

export const geoipState = new GeoipState()
