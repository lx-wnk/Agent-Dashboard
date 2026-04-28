import { readdir, stat } from 'node:fs/promises'
import { basename, join } from 'node:path'
import { encodePath, headRead, parseJsonlLines, readSessionMeta, tailRead } from './jsonlParser.js'
import { CLAUDE_PROJECTS_DIR } from './paths.js'
import { estimateCost } from './pricing.js'
import { scanProcesses } from './processScanner.js'

export interface SessionInfo {
  sessionId: string
  projectPath: string // decoded from directory name
  projectName: string // basename of projectPath
  lastModified: string // ISO timestamp
  model: string | null
  firstPrompt: string | null
  lastResponse: string | null
  totalInputTokens: number
  totalOutputTokens: number
  costEstimate: number
  isRunning: boolean // true if a matching PID is currently alive
}

const MAX_SESSIONS = 100

export function decodeProjectDir(encoded: string): string {
  // Return the raw encoded string — decoding is lossy since `/`, `.`, `_`
  // are all encoded as `-`. The call site already prefers headInfo.cwd
  // (the actual filesystem path from JSONL); this fallback is only reached
  // for sessions with unreadable JSONL where showing the encoded form is
  // better than fabricating a wrong path.
  return encoded
}

function extractLastAssistantText(entries: any[]): string | null {
  let last: string | null = null
  for (const entry of entries) {
    if (entry.type === 'assistant' && Array.isArray(entry.message?.content)) {
      const text = entry.message.content
        .filter((c: any) => c.type === 'text')
        .map((c: any) => (c.text as string))
        .join('')
        .trim()
      if (text)
        last = text
    }
  }
  return last ? last.slice(0, 1000) : null
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

      // Head-read the JSONL for model and cwd; tail-read for last assistant response
      let headInfo: HeadInfo = { model: null, cwd: null }
      let lastResponse: string | null = null
      try {
        const [headRaw, tailRaw] = await Promise.all([
          headRead(entry.filePath),
          tailRead(entry.filePath),
        ])
        const headParsed = parseJsonlLines(headRaw)
        headInfo = extractHeadInfo(headParsed)
        lastResponse = extractLastAssistantText(parseJsonlLines(tailRaw))
      }
      catch {
        // read failed, proceed with nulls
      }

      // Determine model from head-read of JSONL
      const model = headInfo.model || null

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
        lastResponse,
        totalInputTokens: inputTokens,
        totalOutputTokens: outputTokens,
        costEstimate: estimateCost({ inputTokens, outputTokens, cacheCreationTokens: 0, cacheReadTokens: 0 }, model),
        isRunning: runningCwds.has(entry.projectDirEncoded),
      }
    }),
  )

  return sessions
}
