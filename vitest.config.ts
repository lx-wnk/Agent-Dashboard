import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  test: {
    environment: 'jsdom',
    // Server tests transitively import `bun:sqlite` (via server/db/client.ts).
    // Vitest runs under Node's ESM loader, which cannot resolve the `bun:`
    // protocol — even when marked external. Server tests are run separately
    // via `bun test` (see the `test:server` npm script).
    include: ['src/**/*.test.ts'],
  },
})
