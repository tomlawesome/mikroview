/// <reference types="vite/client" />

// #1363: the build id baked in by vite.config.ts's `define` -- see
// lib/freshness.svelte.ts, which compares this against /api/healthz's
// answer to notice a server upgrade an already-open tab would otherwise
// never see.
declare const __MIKROVIEW_VERSION__: string
