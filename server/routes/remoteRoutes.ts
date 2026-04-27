import type { Router } from 'express'
import { Router as createRouter } from 'express'
import {
  createRemoteRegistration,
  deleteRemoteRegistration,
  listRemoteRegistrationsForUser,
} from '../db/remoteRegistrationsRepo.js'

const REMOTE_TIMEOUT_MS = 5000

const BLOCKED_HOSTNAMES = new Set(['localhost', '127.0.0.1', '::1', '0.0.0.0'])
const LOOPBACK_RE = /^127\.\d+\.\d+\.\d+$/
const LINK_LOCAL_RE = /^169\.254\.\d+\.\d+$/

function isSafeRemoteUrl(raw: string): boolean {
  try {
    const u = new URL(raw)
    if (u.protocol !== 'http:' && u.protocol !== 'https:')
      return false
    const host = u.hostname.toLowerCase()
    if (BLOCKED_HOSTNAMES.has(host))
      return false
    // IPv4 loopback (127.x.x.x) and link-local (169.254.x.x)
    if (LOOPBACK_RE.test(host) || LINK_LOCAL_RE.test(host))
      return false
    // IPv6 loopback full form
    if (host === '[::1]')
      return false
    return true
  }
  catch {
    return false
  }
}

async function testRemoteConnection(url: string, bearerKey: string | null): Promise<boolean> {
  try {
    const controller = new AbortController()
    const timeout = setTimeout(() => controller.abort(), REMOTE_TIMEOUT_MS)
    const res = await fetch(`${url}/api/agents`, {
      signal: controller.signal,
      headers: bearerKey ? { Authorization: `Bearer ${bearerKey}` } : {},
    })
    clearTimeout(timeout)
    return res.ok
  }
  catch {
    return false
  }
}

export function createRemoteRouter(): Router {
  const router = createRouter()

  router.get('/', (req, res) => {
    const registrations = listRemoteRegistrationsForUser(req.user!.id)
    // Strip bearerKey from responses — never send tokens to the browser
    res.json(registrations.map(({ bearerKey: _bearerKey, ...r }) => r))
  })

  router.post('/', async (req, res) => {
    const { url, name, bearerKey } = req.body ?? {}
    if (!url || typeof url !== 'string') {
      res.status(400).json({ error: 'url is required' })
      return
    }
    if (!isSafeRemoteUrl(url)) {
      res.status(400).json({ error: 'url must be an http/https address pointing to an external host' })
      return
    }
    const ok = await testRemoteConnection(url, typeof bearerKey === 'string' ? bearerKey : null)
    const reg = createRemoteRegistration({
      userId: req.user!.id,
      url,
      name: typeof name === 'string' ? name : null,
      bearerKey: typeof bearerKey === 'string' ? bearerKey : null,
    })
    const { bearerKey: _bearerKey, ...safeReg } = reg
    res.status(201).json({ ...safeReg, connectionOk: ok })
  })

  router.delete('/:id', (req, res) => {
    const deleted = deleteRemoteRegistration(req.params.id, req.user!.id)
    if (!deleted) {
      res.status(404).json({ error: 'Not found' })
      return
    }
    res.status(204).end()
  })

  return router
}
