import { defineConfig, minimal2023Preset } from '@vite-pwa/assets-generator/config'

// Source is the SVG (brand/logo-mark.svg), not the pre-rendered PNG --
// vector source means every generated size is rasterized crisply from
// scratch, no upscaling artifacts. See brand/BRANDING.md: logo-mark.svg
// is this project's documented source of truth for icon exports: this
// generates PWA-specific sizes (including a maskable variant with the
// safe-zone padding Android's adaptive-icon masking needs) alongside
// the hand-maintained exports already in brand/, not replacing them.
export default defineConfig({
  preset: minimal2023Preset,
  images: ['../brand/logo-mark.svg'],
})
