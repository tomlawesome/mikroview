# #995 — ingest-loss banner family (mockup)

Supersedes `../syslog-full-77/` (owner verdict: banners "horribly verbose").
Frame, scene grammar and the Settings panel carried forward; copy re-cut to
one line per banner. The lines are the owner's own (2026-09-06 rewrite),
label-first — the condition named before its numbers — verbatim, no trailing
punctuation.

## Severity mapping (owner-ruled, 2026-09-06)

| Signal | Tier | Why |
|---|---|---|
| `Dropped` | red | Records reached MikroView and were binned — permanent loss. |
| `RejectedConfigured` | **orange** | Owner ruling: red is reserved for lines MikroView received and then binned; a locked-out declared router is a warning, not a critical. |
| `Rejected` (undeclared) | yellow | The flood defence working; not the operator's routers. |
| `Oversized` | yellow | Partial loss; the real message is "a non-RouterOS sender is aimed at the syslog port". |
| `wsDropped` | cyan | Owner decision: info tier; the events are still logged. |

Severity is also readable without hue: the stack is always in severity
order, the collapsed bar leads with the worst signal, and every label
names its condition before its numbers.

### Superseded

First draft (7809ec0) proposed `RejectedConfigured` as red — same
permanent loss, more insidious because the stream looks healthy. The
owner ruled orange; closed, not overlooked.

## Banner copy (exact — the owner's words, not to be edited)

- red — `Ingest queue full: 812 log lines lost`
- orange — `Syslog slots full: 43 refused (branch-e4a1 locked out)`
- yellow — `Undeclared sources: 2,867 connections refused`
- yellow — `Non-RouterOS sender: 7 oversized messages received from <ip> were truncated`
- cyan — `Slow browser tab: 156 events not shown (but still logged)`

Counts, router name and IP substituted at render time; singular when a count
is 1. The cyan line's "(but still logged)" is the reassurance that earns its
info tier — nothing is lost, the tab just fell behind.

Each line is set in two tiers (owner ruling, 2026-09-06): the label before
the colon is the heading — bold, dominant, read first — and the detail after
it sits at text weight, subordinate. Weight is the only cue; no size,
opacity or spacing changes. The copy itself is untouched.

## Capture-host warning: weights and `system-ui`

The app ships no webfont; `--font-sans` ends in `system-ui`. On the owner's
Windows desktop that resolves to Segoe UI, which has real weights. On this
Linux capture host it resolves to a single-face font: weights 400/600/700
render byte-identically, which once led to a wrong conclusion here that
bold "does not render". It renders for the actual reader; it was only
invisible in the shots. The mockup therefore inserts `'Liberation Sans'`
(installed here, full Regular+Bold, Arial-class metrics — the nearest
Segoe stand-in on this host) after `'Segoe UI'` in its own copy of the
stack, as a capture shim only; Segoe still wins on Windows, and the app's
real token carries no such entry. Do not repeat the weight measurement on
this host without that shim.

Real-loss banners carry a small `details` link into the Settings readout;
that is the "what to do", instead of prose.

## Several at once

One bar's height, always: the bar wears the worst severity, leads with that
signal's message, and a `+N more` chip expands the full stack in severity
order (scene 4 collapsed, scene 5 expanded). Re-collapses when counters go
quiet. Healthy is silent: no banner at all (scene 1); the counters idle in
Settings in muted ink (scene 6).

## Colour tokens and validation

- red = `--alarm #ff5470`, cyan = `--log #38bdf8` — the app's own tokens.
- **Neither orange nor yellow exists in app.css for this meaning** (`--drop`
  and `--now` name a firewall verdict and a time colour). Two tokens are
  minted openly here, **proposed, not settled — awaiting the owner's ruling
  on adding palette tokens**: `--warn #f28a1e` (orange) and `--caution
  #e8e04d` (yellow).
- The orange↔yellow pair was the hard part. With orange occupied by the
  ruling, the first-draft pair failed the dataviz validator's
  normal-vision floor: `#f5a623`↔`#e8d44d` ΔE 10.9 normal (floor 15),
  7.6 deutan. No yellow separates from amber `#f5a623` (best found: 14.9
  normal, with deutan collapsing to 7.8), so the orange moved too — off
  amber toward true orange. Shipped pair `#f28a1e`↔`#e8e04d`: **ΔE 20.4
  normal, 15.1 deutan, 21.2 tritan — PASS** (before: 10.9 / 7.6 / 11.8).
- Full five-tier set `#ff5470,#f28a1e,#e8e04d,#38bdf8` on `--bg`, all
  pairs: worst is red↔orange at ΔE 15.1 normal / 8.9 deutan — above both
  floors; contrast all ≥3:1. The validator's overall verdict still prints
  FAILED from its lightness-band check alone, which is scoped to
  categorical chart marks, not lone status inks (its own scope note); the
  app's shipped palette sits outside that band too.

## Data story

router `branch-e4a1` · 43 turned away · 24/24 slots · 38/s ·
Dropped 812 · Rejected (undeclared) 2,867 · Oversized 7 from `10.20.3.9` ·
wsDropped 156. Multi-fire scenes are the flood scenario (61/s, drops 9%).

Shots in `shots/`, one per scene plus `full-page.png`.
