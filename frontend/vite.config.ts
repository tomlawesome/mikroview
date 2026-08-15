import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { VitePWA } from 'vite-plugin-pwa'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    svelte(),
    VitePWA({
      // Installability only -- deliberately not caching any live data.
      // mikroview is fundamentally a live WebSocket tail; there's
      // nothing meaningful to show "offline," so the generated service
      // worker precaches just the static app shell (HTML/JS/CSS/icons)
      // and nothing from /api/* or /api/ws. Don't "helpfully" add
      // runtime caching for API responses later -- that would mean
      // showing stale, possibly-misleading security data as if it were
      // live.
      registerType: 'autoUpdate',
      injectRegister: 'auto',
      manifest: {
        name: 'MikroView',
        short_name: 'MikroView',
        description:
          'Real-time RouterOS firewall live view — see every connection accepted, dropped, or rejected, and by which rule.',
        start_url: '/',
        display: 'standalone',
        // The brand's signature shield-fill blue (see brand/BRANDING.md)
        // for theme_color -- background_color matches the app's own
        // default dark theme (--bg in src/app.css), since that's what
        // renders during the brief splash/load before the app's own
        // styling takes over.
        theme_color: '#2f6fe0',
        background_color: '#0b0e14',
        icons: [
          { src: 'pwa-192x192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512x512.png', sizes: '512x512', type: 'image/png' },
          { src: 'maskable-icon-512x512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        // The default precache glob already only matches the built app
        // shell (dist/**/*.{js,css,html,svg,png,ico}) -- no API routes
        // exist as static files to accidentally sweep in. Explicit here
        // so that stays true if the build output ever changes shape.
        globPatterns: ['**/*.{js,css,html,svg,png,ico}'],
        // The comment above guarded *caching*, but navigations are a
        // separate code path: the generated worker's navigateFallback
        // answers any typed URL with the cached shell, so an operator
        // visiting /api/stats in the address bar got the frontend
        // instead of JSON -- while the UI's own fetch() calls, which are
        // not navigations, passed through fine. Server-owned paths must
        // reach the server even when typed: /api/* (curl-able JSON, per
        // docs/configuration.md's API reference) and /ca.crt (the one
        // path a router or reverse proxy fetches directly).
        navigateFallbackDenylist: [/^\/api(\/|$)/, /^\/ca\.crt$/],
      },
    }),
  ],
  server: {
    proxy: {
      // wss/https + secure: false -- the backend serves TLS by default
      // now (see internal/config.TLS), including in local dev (`make
      // dev-backend`), with a self-signed local-CA certificate the dev
      // proxy has no reason to validate against a real trust store.
      '/api/ws': {
        target: 'wss://localhost:8080',
        ws: true,
        secure: false,
      },
      '/api': {
        target: 'https://localhost:8080',
        secure: false,
      },
    },
  },
})
