import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const DASHBOARD_PORT = process.env.DASHBOARD_PORT || '13120'

export default defineConfig({
  plugins: [vue()],
  server: {
    // HMR WebSocket runs on the Express httpServer (shared port, see server/index.ts)
    proxy: {
      '/api': `http://localhost:${DASHBOARD_PORT}`
    }
  }
})
