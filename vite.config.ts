import process from 'node:process'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import { VitePWA } from 'vite-plugin-pwa'

const DASHBOARD_PORT = process.env.DASHBOARD_PORT || '13120'

export default defineConfig({
  plugins: [
    tailwindcss(),
    vue(),
    // PWA intent: offline capability is limited to static assets + HTML navigation;
    // API calls (SSE, /api/*) always require a live server connection.
    VitePWA({
      // Use 'prompt' so the user controls when a new service worker activates.
      // This prevents stale cached assets from silently replacing a running session.
      registerType: 'prompt',
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
      workbox: {
        // Raise the per-file limit to 5 MB to avoid Workbox warnings on large chunks.
        maximumFileSizeToCacheInBytes: 5 * 1024 * 1024,
        globPatterns: ['**/*.{js,css,html,ico,woff2}'],
        navigateFallback: null,
        // Do not skip waiting — let the user decide when to activate a new SW.
        skipWaiting: false,
        cleanupOutdatedCaches: true,
        runtimeCaching: [
          {
            // NetworkFirst for HTML navigation prevents stale shell after auth expiry.
            urlPattern: /^\/$|\/index\.html$/,
            handler: 'NetworkFirst',
            options: {
              cacheName: 'html-cache',
              networkTimeoutSeconds: 3,
            },
          },
        ],
      },
    }),
  ],
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['vue', 'vue-router'],
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
      },
      '/auth': {
        target: `http://127.0.0.1:${DASHBOARD_PORT}`,
        changeOrigin: true,
      },
    },
  },
})
