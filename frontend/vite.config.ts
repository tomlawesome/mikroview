import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
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
