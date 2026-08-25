// SPDX-License-Identifier: AGPL-3.0-only

// The country control's own sentinel value, distinct from any real
// ISO 3166-1 alpha-2 code (which is always exactly two letters) and from
// '' (the field's own "no filter" convention) -- selects "this side has
// an address but its country could not be determined" rather than
// leaving that bucket of rows with no way to reach them from the bar
// (#438's owner-ratified country section: "say so rather than silently
// omitting the row").
export const UNKNOWN_COUNTRY = 'unknown'

// matchesCountry needs to know whether this side even has an address,
// not just whether a country code is present -- an event with no source
// address at all (e.g. a rule matching purely on interface) has nothing
// to say "unknown" about, and showing it under "Unknown" would conflate
// "not determined" with "not applicable". See state.svelte.ts's
// srcCountryOptions/dstCountryOptions, which build the select's option
// list under the same rule.
export function matchesCountry(hasAddress: boolean, country: string | undefined, filterValue: string): boolean {
  if (!filterValue) return true
  if (filterValue === UNKNOWN_COUNTRY) return hasAddress && !country
  return (country ?? '').toUpperCase() === filterValue.toUpperCase()
}
