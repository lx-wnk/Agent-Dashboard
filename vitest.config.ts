import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

// Mark Bun built-ins (e.g. `bun:sqlite`) as external so Vite/Vitest does not
// try to bundle them. Bun resolves these natively at runtime.
const bunBuiltinsPlugin = {
  name: 'externalize-bun-builtins',
  enforce: 'pre' as const,
  resolveId(id: string) {
    if (id.startsWith('bun:'))
      return { id, external: true }
  },
}

export default defineConfig({
  plugins: [vue(), bunBuiltinsPlugin],
  resolve: {
    alias: {
      '@': new URL('./src', import.meta.url).pathname,
    },
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts', 'server/**/*.test.ts'],
    environmentMatchGlobs: [
      ['server/**/*.test.ts', 'node'],
    ],
    server: {
      deps: {
        external: [/^bun:/],
      },
    },
  },
})
