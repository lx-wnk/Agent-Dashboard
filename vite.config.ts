import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    // HMR WebSocket runs on the Express httpServer (shared port, see server/index.ts)
    proxy: {
      '/api': 'http://localhost:13120'
    }
  }
})
