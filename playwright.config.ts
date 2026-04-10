import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  webServer: {
    command: 'npm run dev',
    port: 13120,
    reuseExistingServer: true,
  },
  use: {
    baseURL: 'http://localhost:13120',
  },
})
