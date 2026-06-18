import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e',
  webServer: {
    command: 'pnpm dev',
    port: 13120,
    reuseExistingServer: true,
  },
  use: {
    baseURL: 'http://localhost:13120',
  },
})
