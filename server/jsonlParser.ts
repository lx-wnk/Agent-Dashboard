import { readdir, stat, open } from 'node:fs/promises'
import { join } from 'node:path'
import { homedir } from 'node:os'

export interface SessionData {
  sessionId: string
  projectPath: string
  entrypoint: 'cli' | 'desktop' | 'unknown'
  lastActivity: string
  currentAction: string | null
  lastTools: string[]
  tasks: { id: string; subject: string; status: string }[]
  subagents: SubAgentData[]
}

export interface SubAgentData {
  id: string
  type: string
  status: 'active' | 'completed'
  currentAction: string | null
  sessionFile: string
}

const CLAUDE_PROJECTS_DIR = join(homedir(), '.claude', 'projects')
const TAIL_BYTES = 16384 // read last 16KB

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
  } finally {
    await handle.close()
  }
}

function parseJsonlLines(raw: string): any[] {
  const lines = raw.split('\n').filter(l => l.trim())
  const parsed: any[] = []
  for (const line of lines) {
    try {
      parsed.push(JSON.parse(line))
    } catch {
      // partial first line from tail-read, skip
    }
  }
  return parsed
}

function extractSessionInfo(entries: any[]): Partial<SessionData> {
  let sessionId = ''
  let entrypoint: SessionData['entrypoint'] = 'unknown'
  let currentAction: string | null = null
  const lastTools: string[] = []
  const tasks: SessionData['tasks'] = []

  for (const entry of entries) {
    if (entry.sessionId) sessionId = entry.sessionId
    if (entry.entrypoint) {
      entrypoint = entry.entrypoint === 'cli' ? 'cli'
        : entry.entrypoint === 'desktop' ? 'desktop'
        : 'unknown'
    }

    // Extract tool uses from assistant messages
    if (entry.type === 'assistant' && entry.message?.content) {
      const content = entry.message.content
      if (Array.isArray(content)) {
        for (const block of content) {
          if (block.type === 'tool_use' && block.name) {
            if (!lastTools.includes(block.name)) {
              lastTools.push(block.name)
              if (lastTools.length > 10) lastTools.shift()
            }
            currentAction = `${block.name}${block.input?.command ? `: ${String(block.input.command).substring(0, 60)}` : ''}`
          } else if (block.type === 'text' && block.text) {
            const text = block.text.trim()
            if (text.length > 0 && text.length < 200) {
              currentAction = text.substring(0, 100)
            }
          }
        }
      }
    }

    // Extract task updates
    if (entry.type === 'assistant' && entry.message?.content) {
      const content = entry.message.content
      if (Array.isArray(content)) {
        for (const block of content) {
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

  return { sessionId, entrypoint, currentAction, lastTools, tasks }
}

export async function findSessionForProject(cwd: string): Promise<SessionData | null> {
  const encoded = encodePath(cwd)
  const projectDir = join(CLAUDE_PROJECTS_DIR, encoded)

  let dirStat
  try {
    dirStat = await stat(projectDir)
  } catch {
    return null
  }
  if (!dirStat.isDirectory()) return null

  // Find newest .jsonl file in this project directory
  const entries = await readdir(projectDir, { withFileTypes: true })
  const jsonlFiles: { name: string; mtime: Date }[] = []

  for (const entry of entries) {
    if (entry.isFile() && entry.name.endsWith('.jsonl')) {
      const filePath = join(projectDir, entry.name)
      const s = await stat(filePath)
      jsonlFiles.push({ name: entry.name, mtime: s.mtime })
    }
  }

  if (jsonlFiles.length === 0) return null

  jsonlFiles.sort((a, b) => b.mtime.getTime() - a.mtime.getTime())
  const newestFile = jsonlFiles[0]
  const sessionFilePath = join(projectDir, newestFile.name)

  const raw = await tailRead(sessionFilePath)
  const parsed = parseJsonlLines(raw)
  const info = extractSessionInfo(parsed)

  // Find subagents
  const sessionId = newestFile.name.replace('.jsonl', '')
  const subagentDir = join(projectDir, sessionId, 'subagents')
  const subagents = await findSubagents(subagentDir)

  return {
    sessionId: info.sessionId || sessionId,
    projectPath: cwd,
    entrypoint: info.entrypoint || 'unknown',
    lastActivity: newestFile.mtime.toISOString(),
    currentAction: info.currentAction,
    lastTools: info.lastTools || [],
    tasks: info.tasks || [],
    subagents,
  }
}

async function findSubagents(subagentDir: string): Promise<SubAgentData[]> {
  let entries
  try {
    entries = await readdir(subagentDir, { withFileTypes: true })
  } catch {
    return []
  }

  const subagents: SubAgentData[] = []
  for (const entry of entries) {
    if (!entry.isFile() || !entry.name.endsWith('.jsonl')) continue

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
