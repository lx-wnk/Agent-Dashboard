import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e',
  // All specs share one stateful backend on :13120; run serially so concurrent
  // specs don't race on shared server/render state (flaky otherwise).
  workers: 1,
  fullyParallel: false,
  // The stubbed specs are deterministic; the real-backend integration specs
  // (create/delete projects against the shared server) can flake transiently,
  // so allow one retry to keep the gate stable without masking hard failures.
  retries: 1,
  webServer: {
    // Build-if-stale, then serve the real Go binary (embedded SPA + live /api)
    // on 13120 — not `pnpm dev` (vite on 5173, which never satisfies the :13120
    // wait). A cold build is allowed for by the timeout; warm runs skip it.
    command: 'bash scripts/e2e-server.sh',
    port: 13120,
    reuseExistingServer: true,
    timeout: 180_000,
  },
  use: {
    baseURL: 'http://localhost:13120',
  },
})
