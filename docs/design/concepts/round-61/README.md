# Round 61 — the forced-enrolment door (#1336)

2026-09-23. M19 forces a second factor on every local account
(#1253): a factorless account signs in with the right password and
then every request 403s — and nothing in the frontend reads
`mustEnrolSecondFactor`, so that account sees no screen at all.
This round designs the screen: what the door says and offers when
the person can go nowhere until they hold a second factor. It must
choose between the two factor kinds (authenticator app, passkeys),
explain the stop to someone who has used neither, and offer no way
out — there deliberately isn't one.

The base is the ratified door carried verbatim (round 29/30, #645):
void ground, the fall rained behind and masked out of the centre,
the amber 1.5px box framing the wordmark, underlined fields, the
quiet outlined pill. `must-change-password` (#1251) and
`pending-factor` (#1249) are this same door with a field swapped;
the three directions differ in **where enrolment happens**, not in
what the door is.

Shared decisions, whichever direction wins:

- **Copy in the #1251/#1249 family**: "Your password was right.
  Signing in here needs a second step as well, and this account
  doesn't have one yet — choose one, set it up, and you're in."
  Enrolment copy is taken from the shipped overlays
  (`AuthenticatorOverlay.svelte` / `PasskeysOverlay.svelte`)
  verbatim wherever a line already exists: the QR-plus-secret row
  (secret always shown as text), "Google Authenticator, 1Password,
  Bitwarden, or similar", "a fingerprint, face or device PIN",
  "this laptop", the signs-out-everywhere-else note, "I have saved
  these", "Your ten recovery codes cover your authenticator app and
  your passkeys alike."
- **The no-way-out is said, quietly**, in the app's mono caption
  voice: "no skipping this one — every account here needs a second
  step before the door opens." No cancel, no skip, no sign-out
  offered — the whole point is that there isn't one.
- **Authenticator app first, passkey second** in every choice list:
  the app works everywhere, IP addresses included; passkeys are
  conditional on the deployment.
- **Passkeys unusable is a named state, never a hidden row**
  (PasskeysOverlay's own rule): the choice stays visible, disabled,
  with one line why — "passkeys need a web address, and you're
  viewing MikroView at 192.168.88.10 — your authenticator app works
  everywhere."
- **Recovery codes are part of the door's flow**: whichever factor
  activates first mints the ten codes, so the door shows them
  before it opens, closed only by "I have saved these" — the same
  no-escape rule the overlay's codes step already has.
- After the codes, the door plays its ordinary entrance beat and
  the app is simply there — no extra confirmation screen.

Data story, all directions: account `meredith` (viewer) on the
instance at `https://view.brandt.example`; the `no-passkey` scene is
the same instance reached at `192.168.88.10`. Fake secret
`GQ4T MNZV G5UW K2LN MFRG YZLB OR2W CZ3F`, the same ten fake
recovery codes everywhere. The QR tile is decorative (a drawn
QR-lookalike, not an encoding of the fake secret).

The dataviz palette validator does not apply: this surface carries
no data palette — the fall strokes are the door's ratified ambience
carried forward, not a chart.

## The directions

### The same door — `same-door.html`

Enrolment inline, in the door's own column: the strongest reading of
"same door, same beat, one field swapped". The choice is two quiet
hairline blocks under the subtitle; picking one swaps the column to
the QR-plus-secret row with an underlined code field (authenticator)
or a single underlined name field (passkey), each with a dim
use-the-other-instead link, exactly the door's existing link-button
idiom. The codes appear on the door in the overlay's two-column
tile. No modal chrome anywhere; the fall never stops raining.

### The porch — `porch.html`

Enrolment routed into the account menu's own overlays, opened over
the door: chrome, copy and controls verbatim from
`AuthenticatorOverlay`/`PasskeysOverlay`, with every close path
removed — no ✕, no Escape, no backdrop click; Cancel becomes "Back"
to the choice, never out. The choice itself is a first modal in the
same chrome. The surface you will meet later under the account menu
is the one that enrols you now, and the build is nearly free — the
overlays already exist. The dimmed wordmark stays visible above the
modal, so the door is still visibly the door.

### Two keys — `two-keys.html`

The door opened out into a short staged walk. A mono thread under
the wordmark names the whole of what stands between you and the app
— `choose · prove it · keep the codes · enter` — so being stopped
reads as being three small steps deep, not locked out. The choice is
two keys side by side (the app's six-digit face, a drawn fingerprint
arc); proving is a two-column stage, QR left, code right. The most
designed of the three; the thread is the piece that would carry
forward if only the idea is liked.

## Scenes (`shots/`)

`<direction>-<scene>.png` (1440×900) and `<direction>-<scene>-phone.png`
(400×820) for each of `same-door`, `porch`, `two-keys` ×
`first`, `totp`, `passkey`, `codes`, `no-passkey` — 30 shots,
captured by `capture.mjs`, all looked at. Fixed before this commit:
the first capture waited 0.7 s, shorter than the fall strokes'
animation delays, so the rain was missing from nearly every shot —
recaptured at 6 s with the rain airborne. The small scene strip at
the bottom left is the mockup's own review navigation, shared by all
three directions; it is not part of the design.

## Owner verdicts

Owner, 2026-09-23, on the batch of three: *"Two keys approved."*

**two keys — accepted.** It is what gets built: the `'must-enrol-factor'`
phase in `frontend/src/lib/auth.svelte.ts`, drawn by `App.svelte` beside
`'must-change-password'` and `'pending-factor'`, ported from
`two-keys.html` markup and CSS rather than from an impression of it.

**the same door — dropped.** **the porch — dropped.** Neither was
commented on. Nothing in either is carried into the build; they stay
here as the record of what was considered.

Decisions inside the accepted direction, ratified with it: recovery
codes are shown on the door before it opens; the passkeys-unusable
state stays visible and disabled with its reason rather than being
hidden; the authenticator app is listed first; and the fact that there
is no way past the door is said in the mono caption line.
