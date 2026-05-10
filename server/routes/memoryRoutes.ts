import { readdir, readFile, writeFile } from 'node:fs/promises'
import { homedir } from 'node:os'
import { join, resolve } from 'node:path'
import { Router } from 'express'

const CLAUDE_ROOT = resolve(join(homedir(), '.claude'))

function safePath(encoded: string): string | null {
  const decoded = decodeURIComponent(encoded)
  const resolved = resolve(CLAUDE_ROOT, decoded)
  if (!resolved.startsWith(`${CLAUDE_ROOT}/`) && resolved !== CLAUDE_ROOT)
    return null
  return resolved
}

export function createMemoryRouter(): ReturnType<typeof Router> {
  const router = Router()

  router.get('/memory', async (_req, res) => {
    try {
      const projectsDir = join(CLAUDE_ROOT, 'projects')
      const projects = await readdir(projectsDir).catch(() => [])
      const files: Array<{ path: string, name: string }> = []

      for (const project of projects) {
        const memDir = join(projectsDir, project, 'memory')
        const entries = await readdir(memDir).catch(() => [])
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

  router.put('/memory/{*encoded}', async (req, res) => {
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
