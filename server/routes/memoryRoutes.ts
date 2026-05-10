import type express from 'express'
import { readdir, readFile, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { join, resolve, sep as pathSep } from 'node:path'
import { Router } from 'express'
import { isAuthEnabled } from '../auth/requireAuth.js'

const CLAUDE_ROOT = resolve(join(homedir(), '.claude'))

// Only ASCII letters, digits, dots, hyphens, underscores — no path separators
const SAFE_SEGMENT_RE = /^[a-zA-Z0-9._-]+$/

type RejectCrossOrigin = (req: express.Request, res: express.Response) => boolean

// rejectCrossOrigin is optional so tests can call createMemoryRouter() without a dep
export interface MemoryRouterDeps {
  rejectCrossOrigin?: RejectCrossOrigin
}

const noopCsrf: RejectCrossOrigin = () => false

function safePath(encoded: string): string | null {
  const decoded = decodeURIComponent(encoded)
  const parts = decoded.split('/')
  if (parts.length !== 4)
    return null
  const [head, project, memDir, file] = parts
  if (head !== 'projects' || memDir !== 'memory')
    return null
  if (!SAFE_SEGMENT_RE.test(project) || !SAFE_SEGMENT_RE.test(file))
    return null
  if (!file.endsWith('.md') || file === '.md')
    return null
  const resolved = resolve(CLAUDE_ROOT, 'projects', project, 'memory', file)
  const expectedPrefix = resolve(CLAUDE_ROOT, 'projects') + pathSep
  if (!resolved.startsWith(expectedPrefix))
    return null
  return resolved
}

export function createMemoryRouter({ rejectCrossOrigin = noopCsrf }: MemoryRouterDeps = {}): ReturnType<typeof Router> {
  const router = Router()

  router.get('/memory', async (req, res) => {
    if (isAuthEnabled() && req.user && !req.user.isAdmin) {
      res.status(403).json({ error: 'Admin access required' })
      return
    }
    try {
      const projectsDir = join(CLAUDE_ROOT, 'projects')
      const projects = await readdir(projectsDir).catch(() => [] as string[])
      const files: Array<{ path: string, name: string }> = []

      for (const project of projects) {
        const memDir = join(projectsDir, project, 'memory')
        const entries = await readdir(memDir).catch(() => [] as string[])
        for (const entry of entries) {
          if (entry.endsWith('.md')) {
            const relPath = join('projects', project, 'memory', entry)
            files.push({ path: relPath, name: `${project}/${entry}` })
          }
        }
      }
      res.json({ files })
    }
    catch {
      res.status(500).json({ error: 'Failed to list memory files' })
    }
  })

  router.get('/memory/{*encoded}', async (req, res) => {
    const safe = safePath(String(req.params.encoded ?? ''))
    if (!safe) {
      res.status(400).json({ error: 'Path traversal detected' })
      return
    }
    try {
      const content = await readFile(safe, 'utf8')
      res.json({ content })
    }
    catch {
      res.status(404).json({ error: 'File not found' })
    }
  })

  // Mutating route — guard with CSRF check
  router.put('/memory/{*encoded}', (req, res, next) => {
    if (rejectCrossOrigin(req, res))
      return
    next()
  }, async (req, res) => {
    const safe = safePath(String(req.params.encoded ?? ''))
    if (!safe) {
      res.status(400).json({ error: 'Path traversal detected' })
      return
    }
    const { content } = req.body as { content: string }
    if (typeof content !== 'string') {
      res.status(400).json({ error: 'content must be a string' })
      return
    }
    try {
      await writeFile(safe, content, 'utf8')
      res.json({ ok: true })
    }
    catch {
      res.status(500).json({ error: 'Failed to write file' })
    }
  })

  return router
}
