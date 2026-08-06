import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { svelteTesting } from '@testing-library/svelte/vite'

// Deliberately separate from vite.config.ts rather than merging a `test`
// field into it -- vite.config.ts already carries a fair amount of
// PWA-plugin configuration (see its comments), and running vitest never
// needs VitePWA, the dev proxy, or any of that. Keeping this standalone
// means test runs don't pull those in, and app builds are never at risk
// of picking up test-only config by accident.
//
// svelteTesting() (from @testing-library/svelte) handles the three
// pieces of Svelte-5-in-Vitest wiring by hand otherwise needed: it adds
// the `browser` resolve condition (so Svelte's client runtime compiles
// in, not its SSR runtime), registers an afterEach DOM-cleanup hook, and
// marks @testing-library/svelte as non-external for SSR-style bundling.
export default defineConfig({
  plugins: [svelte(), svelteTesting()],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{ts,js}'],
  },
})
