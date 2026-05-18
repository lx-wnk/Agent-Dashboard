import process from 'node:process'
import { fileURLToPath, URL } from 'node:url'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import { VitePWA } from 'vite-plugin-pwa'

const DASHBOARD_PORT = process.env.DASHBOARD_PORT || '13120'

export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  plugins: [
    tailwindcss(),
    vue(),
    // PWA intent: offline capability is limited to precached static assets only;
    // HTML navigation fallback is not handled (no NavigationRoute in injectManifest strategy).
    // API calls (SSE, /api/*) always require a live server connection.
    // injectManifest strategy is used so src/sw.ts can include custom Background Sync logic.
    VitePWA({
      // Use 'prompt' so the user controls when a new service worker activates.
      // This prevents stale cached assets from silently replacing a running session.
      registerType: 'prompt',
      // injectManifest: custom SW at src/sw.ts handles Background Sync + precaching.
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.ts',
      includeAssets: ['favicon.ico', 'apple-touch-icon.png', 'icon-192.png', 'icon-512.png'],
      manifest: {
        name: 'Agent Dashboard',
        short_name: 'Agents',
        description: 'Real-time monitoring dashboard for Claude Code agents',
        theme_color: '#1e293b',
        background_color: '#0f172a',
        display: 'standalone',
        start_url: '/',
        icons: [
          {
            src: '/icon-192.png',
            sizes: '192x192',
            type: 'image/png',
          },
          {
            src: '/icon-512.png',
            sizes: '512x512',
            type: 'image/png',
          },
          {
            src: '/icon-512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'any maskable',
          },
        ],
      },
      injectManifest: {
        // Raise the per-file limit to 5 MB to avoid Workbox warnings on large chunks.
        maximumFileSizeToCacheInBytes: 5 * 1024 * 1024,
        globPatterns: ['**/*.{js,css,html,ico,woff2}'],
      },
    }),
  ],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['vue'],
          charts: ['d3'],
        },
      },
    },
  },
  server: {
    port: 5173,
    // Use 127.0.0.1 explicitly — on dual-stack IPv6 systems 'localhost' may resolve
    // to ::1 first, causing ECONNREFUSED when the server only binds to 127.0.0.1.
    proxy: {
      '/api': {
        target: `http://127.0.0.1:${DASHBOARD_PORT}`,
        changeOrigin: true,
        ws: true,
        configure: (proxy) => {
          proxy.on('error', (err) => {
            if ((err as NodeJS.ErrnoException).code === 'ECONNREFUSED') return
            console.error('[vite proxy]', err)
          })
        },
      },
      '/auth': {
        target: `http://127.0.0.1:${DASHBOARD_PORT}`,
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on('error', (err) => {
            if ((err as NodeJS.ErrnoException).code === 'ECONNREFUSED') return
            console.error('[vite proxy]', err)
          })
        },
      },
    },
  },
})
