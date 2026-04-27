import { homedir } from 'node:os'
import { join } from 'node:path'
import { Router } from 'express'
import { getSystemInfo } from '../systemMonitor.js'

interface SystemRouterDeps {
  serverDir: string
}

export function createSystemRouter({ serverDir }: SystemRouterDeps): Router {
  const router = Router()

  // Dashboard config (script path, home dir)
  router.get('/config', (_req, res) => {
    const home = homedir()
    const scriptAbsolute = join(serverDir, '..', 'scripts', 'claude-with-channel.sh')
    const scriptPath = scriptAbsolute.startsWith(home)
      ? `~${scriptAbsolute.slice(home.length)}`
      : scriptAbsolute
    res.json({ scriptPath, homedir: home })
  })

  router.get('/system', async (_req, res) => {
    try {
      const info = await getSystemInfo()
      res.json(info)
    }
    catch (err) {
      console.error('Error fetching system info:', err)
      res.status(500).json({ error: 'Failed to fetch system info' })
    }
  })

  return router
}
