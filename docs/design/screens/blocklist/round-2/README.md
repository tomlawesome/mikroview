# Blocklist builder — round 2 (#1360): the catalogue, the version, the first-run tail

Round 1 (`../round-1/`) drew the guided command builder; the owner
settled its shape on the issue (9b, 11a, 12a, 13, 14a, 15, 17a, 18).
Round 2 builds on those and does not reopen them. Two mock-ups, one
data story, eight screenshots in `shots/` (`capture.mjs` remakes
them).

| | Scene | File |
|---|---|---|
| **The page** — from Settings ▸ drop list, or the wizard's ledger | rb5009 on RouterOS 7.24.4 | `builder.html` · `shots/builder-01-7.24.4.png` |
| | the same router on 7.19.4: the choices and the block differ | `builder.html?ros=7.19.4` · `shots/builder-02-7.19.4.png` |
| | a router below MikroView's floor (7.12.1) | `builder.html?ros=7.12.1` · `shots/builder-03-below-floor.png` |
| | the "considered and left out" record, open | `builder.html?more=1` · `shots/builder-04-left-out-open.png` |
| | a list's Undo, open | `builder.html?undo=1` · `shots/builder-05-undo-open.png` |
| **The first-run tail** — after the wizard's five steps | Where setup stands, with the tail offered | `tail.html` · `shots/tail-01-where-setup-stands.png` |
| | the tail's stage: the builder in the wizard's frame | `tail.html?scene=build` · `shots/tail-02-the-stage.png` |
| | Where setup stands once the paste landed | `tail.html?scene=after` · `shots/tail-03-after-the-paste.png` |

The research behind the catalogue is `catalogue.md`: every candidate,
its size, terms, cadence and overlap, with sources.

## What changed since round 1, and why

- **The catalogue is researched and curated** (owner, 13). Fourteen
  candidates measured and read; three pass: Spamhaus DROP (with DROPv6
  folded in as "IPv6 too"), Emerging Threats compromised IPs, CINS
  Army. The bar is in `catalogue.md`: terms stated by the source that
  let anyone use the list, fits a router, safe to drop blind, adds
  something, alive. DShield (round 1's third card) goes: its licence is
  non-commercial, and MikroView cannot know whether the operator is a
  business. The eleven lists left out are on the page too, folded under
  the cards with one line each — the owner asked for research, and the
  operator deserves to see why the list they read about elsewhere is
  not here.
- **The "where" choice is gone** (owner, 11a): raw prerouting, placed
  first, is a fact in the hint and the block. The block gains part 1,
  the push updated once for every list, so the raw rules' counters and
  each list's count reach MikroView.
- **The choices follow the router's version** (owner, 15). The page
  says what it writes for ("written for RouterOS 7.24.4 — what rb5009
  reported at 14:32") and shows only the choices that version has. On
  7.24 Emerging Threats offers **weekdays** (the scheduler's `days`,
  new in 7.24; the list is rebuilt on weekdays); on 7.19 it does not,
  and the block sets `http-max-redirect-count=2` by hand (the default
  since 7.22) and skips bad lines with an `:onerror` flag rather than
  `:continue` (7.22). Version-dependent tokens wear the time ink in the
  block. Below 7.18 the page refuses with the upgrade commands and
  waits for the push after the reboot (owner, 18: no fallback).
- **Spamhaus in JSON.** Spamhaus now recommends `drop_v4.json` and
  says the text files "in time … will be deprecated". The script reads
  the JSON lines with `:deserialize from=json` (7.13, so every
  supported router) and keeps the SBL id and the credit in each
  entry's comment, as the terms ask.
- **One log prefix per list, both directions** (owner, 12a):
  `D|drop|et` on the "from" and the "to" rule alike. MikroView reads
  the direction from the line's addresses, as it does for every drop.
- **The first-run tail** (owner, 14a). Below.
- Round 1's caution box about routers below 7.13 is gone: the floor
  covers it. Its data story moved to the wizard's (rb5009 on 7.24.4,
  push every 20 minutes) so the two mock-ups share one router.
- Two defects from round 1 fixed in passing: the ledger's Undo
  rendered as a boxed button where the ratified wizard draws a link,
  and the primary button (Copy, Finish) never filled with the accent.
  Both were the frame's generic button rule outranking the two
  classes; the ratified wizard's look is restored.

## The first-run tail — the design call

The wizard's five steps end on **Where setup stands**. On first run
only, the ledger gains a sixth row and the rail a sixth entry:

- **The rail:** `+ Block known-bad addresses · optional · first run
  only` — a plus in a dashed ring, not a number, because it is an offer
  rather than a step; muted, because its receipt is a fact rather than
  a proof. Never locked: it opens from the ledger and nowhere earlier.
- **The ledger:** a sixth row, `Known-bad addresses · not blocked yet
  — rb5009 lets them in, and MikroView flags them from Spamhaus DROP
  and Emerging Threats`, with **Set it up** where the other rows have
  Undo. The foot gains **Block known-bad addresses** beside Finish;
  Finish stays the primary, because the tail is optional.
- **The stage** is the builder page itself, in the wizard's frame —
  the same two columns, the same block, so what the operator learns
  here is what they find later under Settings. Foot: Back · Not now ·
  Copy · Finish. Not now sets the row aside ("not now · Settings ▸
  drop list"); Finish is allowed at any time, and the ledger keeps
  watching the router.
- **After the paste** the row is a proof like the others: `Known-bad
  addresses dropped · Spamhaus DROP 1,692 held · Emerging Threats 633
  held · loaded 14:07 · confirmed by the push 14:23 · rules fired 3`,
  with Undo; the track gains a `2 lists` station and the bar a chip.
  The wizard's ledger stays green throughout (owner, wizard round 9);
  the lists' own inks live on the builder's body.
- Only a first run offers it. Add another router lands on the fleet
  and does not; Run setup… reopens the ledger without the row once
  the tail has been taken or set aside.

## Calls made in this round, for the record

- CINS Army is **off by default**: 15,000 single hosts is a minute or
  more of address-list adds on a small router, and a home router with
  nothing exposed drops inbound scanners already. On for routers that
  expose a service.
- The per-list **log the drops** toggle stays (12a sets the default,
  yes, and the prefix); an operator with a noisy WAN may want one list
  quiet.
- **How MikroView learns what the router holds:** the push. Part 1 of
  the block updates the push script once so `/ip/firewall/raw` joins
  the rule table it sends and lists named `mv-bl-*` are sent as
  `{list, count, changed}` rather than entry by entry (15,000 entries
  would not fit the 64 KB envelope). The ledger waits for the next
  push, at most 20 minutes; a log line at each refresh would be
  quicker but needs the `script` topic forwarded, which the wizard
  does not do today.
- **Refresh floors per list**: Spamhaus never under 6 h (its terms say
  an hour); Emerging Threats daily, weekdays or weekly; CINS hourly,
  6 h or daily.
- Names: address lists `mv-bl-<list>` (`mv-bl-spamhaus6` for IPv6),
  scripts and schedulers the same, rule comments `mikroview blocklist:
  <list> (from|to)`. Undo removes by those names.

## Gates

- No charts, meters or data palettes on either screen; the dataviz
  validator does not apply. The inks are the wizard's tokens: one per
  list (`--drop`, `--marked`, `--natted`), the push's purple, the time
  amber for what depends on the version.
- Every scene captured at 1600×1000 and looked at; the fixes made
  before this commit: the considered-and-left-out record sat below the
  fold; a list's open undo clipped the ledger; a below-floor router
  showed a list it could not hold; the link buttons above.
- Prototype-only: the `?ros=`, `?more=`, `?undo=`, `?scene=` switches;
  the block's folded script sources (the parse recipes are in
  `catalogue.md`; a build writes them out and tests them on a real
  router at 7.18 and at the newest release). The 7.24 scheduler `days`
  syntax is from the changelog line, not a manual — verify at build.

## Honest gaps

- The Settings ▸ drop list entry point is not drawn: it is one line in
  the existing drop-list group ("Block known-bad addresses…"), no
  design needed.
- Phones: desktop-first by ruling (#635), as the wizard.
- What the fall and the stream show for a `D|drop|<list>` line is
  #1360's last bullet and not this round's; the drop ink and the list
  name in the line are the whole design intent.
- `internal/blocklist` still fetches `drop.txt`; Spamhaus's JSON
  recommendation applies to it too (finding, not this issue).

## Questions for the owner

**A — the catalogue's admission bar.** Three lists pass the strict
bar; a looser one admits more.
- **A1** Keep the bar as written: terms stated by the source that let
  anyone use the list. Three lists. (Recommended.)
- **A2** Also admit "free, but no terms stated" lists, with a "no
  licence stated" fact on the card: blocklist.de `strongips` (386
  hosts, high confidence) and GreenSnow.
- **A3** Also admit non-commercial lists with a "not for business
  use" fact and the operator judging: DShield, Binary Defense.

**B — the first-run tail's shape.**
- **B1** As drawn: a sixth rail row and ledger row, a foot button
  beside Finish, the builder as the stage in the wizard's frame,
  Finish stays primary. (Recommended.)
- **B2** Lighter: no rail row — only the ledger row and the foot
  button, and Set it up opens the Settings page rather than a stage
  in the wizard.
- **B3** Heavier: on first run the tail is the primary action and
  Finish is secondary until it is taken or set aside.

Written by Fable 5.1, 2026-09-30.
