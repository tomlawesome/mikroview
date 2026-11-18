// SPDX-License-Identifier: AGPL-3.0-only

import type { ParsedCidr } from './addressMatch'

// networks.home's IPv4 prefixes (#1481), held in $state so every derived
// value and template that called isPublicIp recomputes when the set
// loads -- it arrives from /api/networks after some state may already
// have been classified. Lives here rather than in format.ts because
// runes only compile in .svelte.ts modules.
export const homeNetworks = $state({ v4: [] as ParsedCidr[] })
