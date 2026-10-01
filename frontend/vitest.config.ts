// Pin the suite's timezone before anything formats a date (#741). The
// foot line renders wall-clock times through the browser's local zone,
// so three of its assertions passed on a BST machine and failed in CI's
// UTC container -- a check that reports green for whoever wrote it and
// red for everyone else. UTC matches CI, so local runs now agree with it.
process.env.TZ = 'UTC'

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
  // Standalone from vite.config.ts (see above), so its own `define` for
  // #1363's baked-in build id needs repeating here -- tests never set
  // VERSION, so this is always "dev:local", the same default a plain
  // `npm run build` gets.
  define: {
    __MIKROVIEW_VERSION__: JSON.stringify('dev:local'),
  },
  plugins: [svelte(), svelteTesting()],
  test: {
    environment: 'jsdom',
    include: [
      'src/**/*.{test,spec}.{ts,js}',
      'guards/**/*.{test,spec}.{ts,js}',
      // scripts/ is plain Node .mjs (no jsdom/Svelte needed), but
      // perf-compare.mjs's comparison rule is a pure function worth
      // unit-testing same as anything else -- no reason to stand up a
      // separate runner just for one file.
      'scripts/**/*.{test,spec}.mjs',
    ],
    // #1333, the frontend half: a ratchet at measured values, same
    // reasoning as scripts/coverage-floor.py's Go floors -- never lower
    // to make a change pass; a change that would need that needs more
    // tests. 'text' prints the per-file table in the job log; no other
    // reporter is wired to anything yet, so nothing else is added.
    // See docs/testing.md for how these were measured.
    coverage: {
      provider: 'v8',
      reporter: ['text'],
      // Measured 2026-09-27 against the node:26-alpine image `npm test --
      // --coverage` (statements 82.4%, branches 69.28%, functions 82.34%,
      // lines 84.58%), rounded down. See docs/testing.md for the exact
      // command.
      thresholds: {
        lines: 84,
        statements: 82,
        functions: 82,
        branches: 69,
      },
    },
  },
})
