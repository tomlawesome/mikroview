// SPDX-License-Identifier: AGPL-3.0-only

// The chrome's shared "label — reason; reason" composition. The Flags
// count badge and the broken ring (#546) are independent per the record
// -- a row can in principle carry both -- so this takes a list of
// applicable reasons rather than assuming there is exactly one, and joins
// whatever is present rather than one marker overwriting another's label.
// A bare label with no reasons is the common case (most rows carry
// neither).
export function spokenLabel(label: string, bits: string[]): string {
  return bits.length > 0 ? `${label} — ${bits.join('; ')}` : label
}

// The broken ring's whole meaning, in one sentence (#546's ratified
// wording): plain operator language naming the count and the cause, not
// "coverage is no-logging", which is vocabulary the operator never chose.
// It lives here beside spokenLabel because #583 puts the ring on two
// surfaces -- the bottom bar's group and its half-sheet's page row --
// and the record asks for the *same sentence* each time, with only the
// subject narrowing (Expect - Watchlist - the entry itself). One copy
// is what makes that true rather than aspirational.
export function ringReason(brokenCount: number): string {
  return brokenCount === 1
    ? `1 watch can't be checked: the firewall rules it needs aren't being logged`
    : `${brokenCount} watches can't be checked: the firewall rules they need aren't being logged`
}
