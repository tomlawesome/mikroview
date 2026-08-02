# MikroView branding

## Mark: "Pulse Shield"

A shield badge with a live traffic waveform cut through the middle — the
mark is meant to represent what the product actually does (trace
accepted/blocked connections in real time), not generic network/firewall
iconography. The green/red terminal dots on the waveform echo the
accept/blocked color coding used throughout the live view itself.

## Files

| File | Use |
|---|---|
| `logo-mark.svg` | Icon only. Source of truth for all other exports. |
| `logo-lockup-dark.svg` | Icon + wordmark, light text — for dark backgrounds. |
| `logo-lockup-light.svg` | Icon + wordmark, dark text — for light backgrounds. |
| `logo-mark-512.png` | Square PNG, transparent — GitHub org/repo avatar, etc. |
| `favicon-16.png` / `favicon-32.png` | Browser tab favicon (also see `frontend/public/favicon.svg`, the SVG favicon used by default). |
| `apple-touch-icon.png` | 180×180, opaque background — iOS home-screen/bookmark icon. |
| `social-preview.png` | 1280×640 — GitHub repo social preview image. |

Regenerate the PNGs from the SVG source with `tools/screenshots/render-brand.mjs`
(`node render-brand.mjs`, requires the Playwright + Chromium already set up
for `tools/screenshots/capture.mjs`).

The in-app logo (`frontend/src/components/LogoMark.svelte` /
`LogoLockup.svelte`) is a separate hand-kept copy of the same path data,
since it's rendered inline as a Svelte component rather than loaded as a
file — keep both in sync if the mark ever changes.

## Colors

| Token | Hex | Use |
|---|---|---|
| Badge fill | `#2f6fe0` | The shield's solid fill — the brand's signature color. |
| Badge outline | `#1a4fc4` | 1px stroke on the shield edge. |
| UI accent | `#4d9fff` | Lighter blue used for links/focus/"View" in the wordmark *in-app*; matches `--accent` in `frontend/src/app.css`. |
| Accept | `#3ecf7e` | Also `--accept` in the app's design tokens. |
| Drop | `#f5a623` | Also `--drop`. |
| Reject | `#ef4444` | Also `--reject`. |

The mark's own colors (badge fill/outline/waveform) are fixed across
light and dark themes — only the *surrounding* UI (wordmark text color,
backgrounds) adapts per theme. See `frontend/src/app.css` for the full
token set the live app itself uses.

## Wordmark

Set in the system sans-serif stack (`-apple-system, Segoe UI, system-ui,
Roboto, sans-serif`) at bold weight — no custom typeface, deliberately,
to keep the in-app lockup dependency-free (no webfont loading) and
consistent with the rest of the UI's typography.
