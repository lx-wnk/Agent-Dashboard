import express from 'express'
import { createServer as createViteServer } from 'vite'

const PORT = 3120

async function start() {
  const app = express()

  // Vite dev middleware
  const vite = await createViteServer({
    server: { middlewareMode: true },
    appType: 'spa',
  })
  app.use(vite.middlewares)

  app.listen(PORT, () => {
    console.log(`Claude Agent Overview running at http://localhost:${PORT}`)
  })
}

start()
