import type { OutputMessage, SessionMeta, TokenUsage } from '../src/types.js'
import { Buffer } from 'node:buffer'
import { open, readdir, readFile, stat } from 'node:fs/promises'
import { homedir } from 'node:os'
import { join } from 'node:path'

export type TokenUsageData = TokenUsage
export type { SessionMeta }

export interface SessionData {
  sessionId: string
  projectPath: string
  entrypoint: 'cli' | 'desktop' | 'unknown'
  lastActivity: string
  currentAction: string | null
  lastTools: string[]
  tasks: { id: string, subject: string, status: string }[]
  subagents: SubAgentData[]
  tokenUsage: TokenUsageData
  model: string | null
  codeVersion: string | null
  conversationTurns: number
  toolCounts: Record<string, number>
  lastOutput: string | null
  meta: SessionMeta | null
}

export interface SubAgentData {
  id: string
  type: string
  status: 'active' | 'completed'
  currentAction: string | null
  sessionFile: string
}

const CLAUDE_PROJECTS_DIR = join(homedir(), '.claude', 'projects')
const SESSION_META_DIR = join(homedir(), '.claude', 'usage-data', 'session-meta')
const TAIL_BYTES = 32768 // read last 32KB
const HEAD_BYTES = 8192 // read first 8KB for model/version

function encodePath(absolutePath: string): string {
  // Claude Code encodes both / and _ as -
  return absolutePath.replace(/[/_]/g, '-')
}

async function tailRead(filePath: string): Promise<string> {
  const handle = await open(filePath, 'r')
  try {
    const fileStat = await handle.stat()
    const size = fileStat.size
    const readSize = Math.min(TAIL_BYTES, size)
    const buffer = Buffer.alloc(readSize)
    await handle.read(buffer, 0, readSize, size - readSize)
    return buffer.toString('utf-8')
  }
  finally {
    await handle.close()
  }
}

export async function headRead(filePath: string): Promise<string> {
  const handle = await open(filePath, 'r')
  try {
    const fileStat = await handle.stat()
    const readSize = Math.min(HEAD_BYTES, fileStat.size)
    const buffer = Buffer.alloc(readSize)
    await handle.read(buffer, 0, readSize, 0)
    return buffer.toString('utf-8')
  }
  finally {
    await handle.close()
  }
}

export function parseJsonlLines(raw: string): any[] {
  const lines = raw.split('\n').filter(l => l.trim())
  const parsed: any[] = []
  for (const line of lines) {
    try {
      parsed.push(JSON.parse(line))
    }
    catch {
      // partial first line from tail-read, skip
    }
  }
  return parsed
}

function extractSessionInfo(entries: any[]): Partial<SessionData> {
  let sessionId = ''
  let entrypoint: SessionData['entrypoint'] = 'unknown'
  let currentAction: string | null = null
  let model: string | null = null
  let codeVersion: string | null = null
  let conversationTurns = 0
  const lastTools: string[] = []
  const tasks: SessionData['tasks'] = []
  const toolCounts: Record<string, number> = {}
  let lastOutput: string | null = null
  const tokenUsage: TokenUsageData = {
    inputTokens: 0,
    outputTokens: 0,
    cacheCreationTokens: 0,
    cacheReadTokens: 0,
  }

  for (const entry of entries) {
    if (entry.sessionId)
      sessionId = entry.sessionId
    if (entry.version)
      codeVersion = entry.version
    if (entry.entrypoint) {
      entrypoint = entry.entrypoint === 'cli'
        ? 'cli'
        : entry.entrypoint === 'desktop'
          ? 'desktop'
          : 'unknown'
    }

    // Count user turns
    if (entry.type === 'user') {
      conversationTurns++
    }

    // Extract data from assistant messages
    if (entry.type === 'assistant') {
      // Token usage from message.usage
      const usage = entry.message?.usage
      if (usage) {
        tokenUsage.inputTokens += usage.input_tokens || 0
        tokenUsage.outputTokens += usage.output_tokens || 0
        tokenUsage.cacheCreationTokens += usage.cache_creation_input_tokens || 0
        tokenUsage.cacheReadTokens += usage.cache_read_input_tokens || 0
      }

      // Model info
      if (entry.message?.model) {
        model = entry.message.model
      }

      // Tool uses
      const content = entry.message?.content
      if (Array.isArray(content)) {
        for (const block of content) {
          if (block.type === 'tool_use' && block.name) {
            // Track tool counts
            toolCounts[block.name] = (toolCounts[block.name] || 0) + 1

            if (!lastTools.includes(block.name)) {
              lastTools.push(block.name)
              if (lastTools.length > 10)
                lastTools.shift()
            }
            currentAction = `${block.name}${block.input?.command ? `: ${String(block.input.command).substring(0, 120)}` : ''}`
          }
          else if (block.type === 'text' && block.text) {
            const text = block.text.trim()
            if (text.length > 0) {
              currentAction = text.substring(0, 300)
              lastOutput = text.substring(0, 500)
            }
          }

          // Task tracking
          if (block.type === 'tool_use' && block.name === 'TaskCreate' && block.input) {
            tasks.push({
              id: block.id || String(tasks.length + 1),
              subject: block.input.subject || 'Unknown',
              status: 'pending',
            })
          }
          if (block.type === 'tool_use' && block.name === 'TaskUpdate' && block.input) {
            const task = tasks.find(t => t.id === block.input.taskId)
            if (task && block.input.status) {
              task.status = block.input.status
            }
          }
        }
      }
    }
  }

  return {
    sessionId,
    entrypoint,
    currentAction,
    lastTools,
    tasks,
    tokenUsage,
    model,
    codeVersion,
    conversationTurns,
    toolCounts,
    lastOutput,
  }
}

async function readSessionMeta(sessionId: string): Promise<SessionMeta | null> {
  try {
    const metaPath = join(SESSION_META_DIR, `${sessionId}.json`)
    const raw = await readFile(metaPath, 'utf-8')
    const data = JSON.parse(raw)
    return {
      inputTokens: data.input_tokens || 0,
      outputTokens: data.output_tokens || 0,
      linesAdded: data.lines_added || 0,
      linesRemoved: data.lines_removed || 0,
      filesModified: data.files_modified || 0,
      gitCommits: data.git_commits || 0,
      toolErrors: data.tool_errors || 0,
      usesMcp: data.uses_mcp || false,
      firstPrompt: data.first_prompt || null,
    }
  }
  catch {
    return null
  }
}

export async function findSessionForProject(cwd: string): Promise<SessionData | null> {
  const encoded = encodePath(cwd)
  const projectDir = join(CLAUDE_PROJECTS_DIR, encoded)

  let dirStat
  try {
    dirStat = await stat(projectDir)
  }
  catch {
    return null
  }
  if (!dirStat.isDirectory())
    return null

  // Find newest .jsonl file in this project directory
  const entries = await readdir(projectDir, { withFileTypes: true })
  const jsonlEntries = entries.filter(e => e.isFile() && e.name.endsWith('.jsonl'))
  const jsonlFiles = await Promise.all(
    jsonlEntries.map(async (e) => {
      const s = await stat(join(projectDir, e.name))
      return { name: e.name, mtime: s.mtime }
    }),
  )

  if (jsonlFiles.length === 0)
    return null

  jsonlFiles.sort((a, b) => b.mtime.getTime() - a.mtime.getTime())
  const newestFile = jsonlFiles[0]
  const sessionFilePath = join(projectDir, newestFile.name)

  const raw = await tailRead(sessionFilePath)
  const parsed = parseJsonlLines(raw)
  const info = extractSessionInfo(parsed)

  // If model/version not found in tail, check head of file
  if (!info.model || !info.codeVersion) {
    const headRaw = await headRead(sessionFilePath)
    const headParsed = parseJsonlLines(headRaw)
    for (const entry of headParsed) {
      if (!info.model && entry.message?.model)
        info.model = entry.message.model
      if (!info.codeVersion && entry.version)
        info.codeVersion = entry.version
      if (info.model && info.codeVersion)
        break
    }
  }

  // Find subagents
  const sessionId = newestFile.name.replace('.jsonl', '')
  const subagentDir = join(projectDir, sessionId, 'subagents')
  const subagents = await findSubagents(subagentDir)

  // Read aggregated session metadata (has full token counts, git stats, etc.)
  const meta = await readSessionMeta(sessionId)

  // Use session-meta token counts if available (they cover the full session, not just last 16KB)
  const tokenUsage = info.tokenUsage || { inputTokens: 0, outputTokens: 0, cacheCreationTokens: 0, cacheReadTokens: 0 }
  if (meta && (meta.inputTokens > tokenUsage.inputTokens || meta.outputTokens > tokenUsage.outputTokens)) {
    tokenUsage.inputTokens = meta.inputTokens
    tokenUsage.outputTokens = meta.outputTokens
  }

  return {
    sessionId: info.sessionId || sessionId,
    projectPath: cwd,
    entrypoint: info.entrypoint || 'unknown',
    lastActivity: newestFile.mtime.toISOString(),
    currentAction: info.currentAction ?? null,
    lastOutput: info.lastOutput ?? null,
    lastTools: info.lastTools || [],
    tasks: info.tasks || [],
    subagents,
    tokenUsage,
    model: info.model || null,
    codeVersion: info.codeVersion || null,
    conversationTurns: info.conversationTurns || 0,
    toolCounts: info.toolCounts || {},
    meta,
  }
}

async function findSubagents(subagentDir: string): Promise<SubAgentData[]> {
  let entries
  try {
    entries = await readdir(subagentDir, { withFileTypes: true })
  }
  catch {
    return []
  }

  const subagents: SubAgentData[] = []
  for (const entry of entries) {
    if (!entry.isFile() || !entry.name.endsWith('.jsonl'))
      continue

    const filePath = join(subagentDir, entry.name)
    const s = await stat(filePath)
    const raw = await tailRead(filePath)
    const parsed = parseJsonlLines(raw)

    // Extract subagent type from first user message (usually contains the task description)
    let type = 'unknown'
    let currentAction: string | null = null
    for (const e of parsed) {
      if (e.type === 'user' && e.message?.content) {
        const text = typeof e.message.content === 'string'
          ? e.message.content
          : Array.isArray(e.message.content)
            ? e.message.content.find((b: any) => b.type === 'text')?.text || ''
            : ''
        if (text.length > 0) {
          type = text.substring(0, 80)
        }
      }
      if (e.type === 'assistant' && e.message?.content && Array.isArray(e.message.content)) {
        for (const block of e.message.content) {
          if (block.type === 'tool_use') {
            currentAction = block.name
          }
        }
      }
    }

    // Subagent is "active" if file was modified recently (within 60s)
    const age = Date.now() - s.mtime.getTime()
    const status = age < 60000 ? 'active' : 'completed'

    subagents.push({
      id: entry.name.replace('.jsonl', ''),
      type,
      status,
      currentAction,
      sessionFile: filePath,
    })
  }

  return subagents
}

export async function parseFullSession(sessionId: string, lastOnly: boolean = false): Promise<OutputMessage[]> {
  const projectDirs = await readdir(CLAUDE_PROJECTS_DIR, { withFileTypes: true })
  let sessionFilePath: string | null = null

  for (const dir of projectDirs) {
    if (!dir.isDirectory())
      continue
    const candidate = join(CLAUDE_PROJECTS_DIR, dir.name, `${sessionId}.jsonl`)
    try {
      await stat(candidate)
      sessionFilePath = candidate
      break
    }
    catch {
      continue
    }
  }

  if (!sessionFilePath)
    return []

  // Check file size and cap reads to avoid loading 100MB+ files into memory
  const fileStats = await stat(sessionFilePath)
  const MAX_READ_BYTES = 10 * 1024 * 1024 // 10MB cap

  let raw: string
  if (lastOnly) {
    // For last message only, tail read is sufficient
    raw = await tailRead(sessionFilePath)
  }
  else if (fileStats.size <= MAX_READ_BYTES) {
    raw = await readFile(sessionFilePath, 'utf-8')
  }
  else {
    // For large files, read last 10MB
    const handle = await open(sessionFilePath, 'r')
    try {
      const buffer = Buffer.alloc(MAX_READ_BYTES)
      await handle.read(buffer, 0, MAX_READ_BYTES, fileStats.size - MAX_READ_BYTES)
      raw = buffer.toString('utf-8')
    }
    finally {
      await handle.close()
    }
  }

  const entries = parseJsonlLines(raw)
  const messages: OutputMessage[] = []

  for (const entry of entries) {
    // Handle tool results (separate entry type)
    if (entry.type === 'result' && entry.result) {
      messages.push({
        role: 'tool_result',
        content: typeof entry.result === 'string' ? entry.result : JSON.stringify(entry.result).substring(0, 1000),
        timestamp: entry.timestamp,
      })
      continue
    }

    if (entry.type !== 'assistant' || !entry.message?.content)
      continue
    if (!Array.isArray(entry.message.content))
      continue

    for (const block of entry.message.content) {
      if (block.type === 'text' && block.text?.trim()) {
        messages.push({
          role: 'assistant',
          content: block.text.trim(),
          timestamp: entry.timestamp,
        })
      }
      else if (block.type === 'tool_use' && block.name) {
        const filePath = block.input?.file_path || block.input?.path || undefined
        messages.push({
          role: 'tool_call',
          content: block.name,
          timestamp: entry.timestamp,
          toolName: block.name,
          filePath,
        })
      }
    }
  }

  if (lastOnly) {
    const lastAssistant = messages.filter(m => m.role === 'assistant').pop()
    return lastAssistant ? [lastAssistant] : []
  }

  return messages
}
