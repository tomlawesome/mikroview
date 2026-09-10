// SPDX-License-Identifier: AGPL-3.0-only
//
// The one shared altitude axis (#869): the 2D map's own three stops,
// the city centred at the join, then the city's own three. One slider
// walks both views; `lib/city/project.ts`'s STOPS stays the city's own
// four, named apart from a camera height, so this is the full seven
// the slider itself walks.
import { STOPS } from './city/project'

export const ALTITUDE_LABELS = ['clients', 'services', 'zones', ...STOPS] as const
export type AltitudeLabel = (typeof ALTITUDE_LABELS)[number]
export type Altitude = 0 | 1 | 2 | 3 | 4 | 5 | 6

/** 'city' is the centre and the default (#869, DESIGN.md "The model"):
 * everything at or past this index is the city's own side of the axis. */
export const CENTRE_ALTITUDE = ALTITUDE_LABELS.indexOf('city') as Altitude

export const isCityAltitude = (a: Altitude): boolean => a >= CENTRE_ALTITUDE
