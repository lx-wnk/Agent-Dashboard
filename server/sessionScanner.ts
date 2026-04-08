import { readdir, readFile, stat } from 'node:fs/promises'
import { homedir } from 'node:os'
import { basename, join } from 'node:path'
import { headRead, parseJsonlLines } from './jsonlParser.js'
import { estimateCost } from './pricing.js'
import { scanProcesses } from './processScanner.js'

export interface SessionInfo {
  sessionId: string
  projectPath: string // decoded from directory name
  projectName: string // basename of projectPath
  lastModified: string // ISO timestamp
  model: string | null
  firstPrompt: string | null
  totalInputTokens: number
  totalOutputTokens: number
  costEstimate: number
  isRunning: boolean // true if a matching PID is currently alive
}

const CLAUDE_PROJECTS_DIR = join(homedir(), '.claude', 'projects')
const SESSION_META_DIR = join(homedir(), '.claude', 'usage-data', 'session-meta')
const MAX_SESSIONS = 100

/**
 * Decode an encoded project directory name back to an absolute path.
 *
 * Claude Code encodes paths by replacing `/` and `_` with `-`, which is
 * ambiguous on decode. We restore the leading `/` and replace remaining
 * `-` with `/`. This won't be perfect for every edge case (e.g. directory
 * names that originally contained `-`), but it gives a reasonable
 * human-readable project path most of the time.
 */
function decodeProjectDir(encoded: string): string {
  // On macOS, paths look like -Users-username-code-project
  // Restore leading `/` and try to rebuild the path
  if (encoded.startsWith('-')) {
    return `/${encoded.slice(1).replace(/-/g, '/')}`
  }
  return encoded
}

function encodePath(absolutePath: string): string {
  return absolutePath.replace(/[/_]/g, '-')
}

interface HeadInfo {
  model: string | null
  cwd: string | null
}

function extractHeadInfo(entries: any[]): HeadInfo {
  let model: string | null = null
  let cwd: string | null = null

  for (const entry of entries) {
    if (!model && entry.message?.model) {
      model = entry.message.model
    }
    if (!cwd && entry.cwd) {
      cwd = entry.cwd
    }
    if (model && cwd)
      break
  }

  return { model, cwd }
}

interface SessionMetaRaw {
  inputTokens: number
  outputTokens: number
  firstPrompt: string | null
  model: string | null
}

async function readSessionMeta(sessionId: string): Promise<SessionMetaRaw | null> {
  try {
    const metaPath = join(SESSION_META_DIR, `${sessionId}.json`)
    const raw = await readFile(metaPath, 'utf-8')
    const data = JSON.parse(raw)
    return {
      inputTokens: data.input_tokens || 0,
      outputTokens: data.output_tokens || 0,
      firstPrompt: data.first_prompt || null,
      model: data.model || null,
    }
  }
  catch {
    return null
  }
}

interface JsonlFileEntry {
  sessionId: string
  filePath: string
  projectDirEncoded: string
  mtime: Date
}

export async function getSessions(): Promise<SessionInfo[]> {
  // 1. Scan project directories
  let projectDirs: string[]
  try {
    const entries = await readdir(CLAUDE_PROJECTS_DIR, { withFileTypes: true })
    projectDirs = entries
      .filter(e => e.isDirectory())
      .map(e => e.name)
  }
  catch {
    return []
  }

  // 2. Collect all .jsonl files with their mtime
  const allFiles: JsonlFileEntry[] = []
  await Promise.all(
    projectDirs.map(async (dirName) => {
      const dirPath = join(CLAUDE_PROJECTS_DIR, dirName)
      try {
        const entries = await readdir(dirPath, { withFileTypes: true })
        const jsonlEntries = entries.filter(e => e.isFile() && e.name.endsWith('.jsonl'))
        await Promise.all(
          jsonlEntries.map(async (e) => {
            try {
              const filePath = join(dirPath, e.name)
              const s = await stat(filePath)
              allFiles.push({
                sessionId: e.name.replace('.jsonl', ''),
                filePath,
                projectDirEncoded: dirName,
                mtime: s.mtime,
              })
            }
            catch {
              // stat failed, skip
            }
          }),
        )
      }
      catch {
        // readdir failed, skip
      }
    }),
  )

  // 3. Sort by mtime descending, limit to MAX_SESSIONS
  allFiles.sort((a, b) => b.mtime.getTime() - a.mtime.getTime())
  const limited = allFiles.slice(0, MAX_SESSIONS)

  // 4. Get running processes to determine isRunning
  let runningCwds: Set<string>
  try {
    const processes = await scanProcesses()
    runningCwds = new Set(processes.map(p => encodePath(p.cwd)))
  }
  catch {
    runningCwds = new Set()
  }

  // 5. Build SessionInfo for each file
  const sessions = await Promise.all(
    limited.map(async (entry): Promise<SessionInfo> => {
      // Read session-meta for token counts and first prompt
      const meta = await readSessionMeta(entry.sessionId)

      // Head-read the JSONL for model and cwd
      let headInfo: HeadInfo = { model: null, cwd: null }
      try {
        const headRaw = await headRead(entry.filePath)
        const headParsed = parseJsonlLines(headRaw)
        headInfo = extractHeadInfo(headParsed)
      }
      catch {
        // head-read failed, proceed with nulls
      }

      // Determine model: prefer head-read, fall back to session-meta
      const model = headInfo.model || meta?.model || null

      // Determine project path: prefer cwd from JSONL, fall back to decoded dir name
      const projectPath = headInfo.cwd || decodeProjectDir(entry.projectDirEncoded)

      const inputTokens = meta?.inputTokens || 0
      const outputTokens = meta?.outputTokens || 0

      return {
        sessionId: entry.sessionId,
        projectPath,
        projectName: basename(projectPath),
        lastModified: entry.mtime.toISOString(),
        model,
        firstPrompt: meta?.firstPrompt || null,
        totalInputTokens: inputTokens,
        totalOutputTokens: outputTokens,
        costEstimate: estimateCost({ inputTokens, outputTokens }, model),
        isRunning: runningCwds.has(entry.projectDirEncoded),
      }
    }),
  )

  return sessions
}
