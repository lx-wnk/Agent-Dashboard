import express from 'express'
import { createServer as createViteServer } from 'vite'
import { getAgents } from './agentMerger.js'

const PORT = 3120

async function start() {
  const app = express()

  // API routes (before Vite middleware)
  app.get('/api/agents', async (_req, res) => {
    try {
      const agents = await getAgents()
      res.json(agents)
    } catch (err) {
      console.error('Error fetching agents:', err)
      res.status(500).json({ error: 'Failed to fetch agents' })
    }
  })

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
