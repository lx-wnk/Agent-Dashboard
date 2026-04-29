import { execFile } from 'node:child_process'
import { readdir, readFile, stat, unlink } from 'node:fs/promises'
import { join } from 'node:path'
import process from 'node:process'
import { promisify } from 'node:util'
import { DISCOVERY_DIR, WHITESPACE_RE } from './paths.js'

const execFileAsync = promisify(execFile)

interface ChannelCacheEntry {
  mtimeMs: number
  pid: number | null // resolved Claude ancestor PID
}

const channelCache = new Map<string, ChannelCacheEntry>()

interface DiscoveryEntry {
  port: number
  channelPid: number
  parentPid: number
  cwd?: string
  token: string
  startedAt: string
}

export interface ChannelInfo {
  port: number
  token: string
  cwd?: string
}

function isAlive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  }
  catch {
    return false
  }
}

/**
 * Walk up the process tree from `startPid` to find the nearest ancestor
 * whose command ends with `/claude` or equals `claude`.
 * Returns the ancestor PID or null if not found within 5 levels.
 */
async function findClaudeAncestor(startPid: number): Promise<number | null> {
  let currentPid = startPid
  for (let depth = 0; depth < 5; depth++) {
    try {
      // ps -p <pid> -o ppid=,comm= outputs the ppid and comm of <pid>
      const { stdout } = await execFileAsync('ps', ['-p', String(currentPid), '-o', 'ppid=,comm='])
      const trimmed = stdout.trim()
      if (!trimmed)
        return null

      const parts = trimmed.split(WHITESPACE_RE)
      const ppid = Number.parseInt(parts[0], 10)
      const comm = parts.slice(1).join(' ')

      if (Number.isNaN(ppid) || ppid <= 1)
        return null

      // comm belongs to currentPid, so return currentPid when it matches
      if (comm.endsWith('/claude') || comm === 'claude')
        return currentPid

      currentPid = ppid
    }
    catch {
      return null
    }
  }
  return null
}

export async function getChannelMap(): Promise<Map<number, ChannelInfo>> {
  const result = new Map<number, ChannelInfo>()

  let files: string[]
  try {
    files = (await readdir(DISCOVERY_DIR)).filter(f => f.endsWith('.json'))
  }
  catch {
    return result
  }

  await Promise.all(files.map(async (file) => {
    const filePath = join(DISCOVERY_DIR, file)
    try {
      const raw = await readFile(filePath, 'utf-8')
      const entry: DiscoveryEntry = JSON.parse(raw)

      if (!isAlive(entry.parentPid)) {
        await unlink(filePath).catch(() => {})
        channelCache.delete(filePath)
        return
      }

      const info: ChannelInfo = { port: entry.port, token: entry.token, cwd: entry.cwd }

      // mtime cache: avoid forking `ps` per discovery file when the file
      // hasn't changed since the last walk. Discovery files are written
      // once at MCP server startup, so the mtime almost never changes.
      let claudePid: number | null = null
      try {
        const fileStat = await stat(filePath)
        const cached = channelCache.get(filePath)
        if (cached && cached.mtimeMs === fileStat.mtimeMs) {
          claudePid = cached.pid
        }
        else {
          // Try to find the actual claude process by walking up the tree.
          // process.ppid in the MCP server points to the tsx/node wrapper,
          // not the claude process itself. We need the claude PID to match
          // against what processScanner reports.
          claudePid = await findClaudeAncestor(entry.parentPid)
          channelCache.set(filePath, { mtimeMs: fileStat.mtimeMs, pid: claudePid })
        }
      }
      catch {
        claudePid = await findClaudeAncestor(entry.parentPid)
      }

      if (claudePid !== null) {
        result.set(claudePid, info)
      }
      else {
        // Fallback: use the parentPid directly (works if claude spawns
        // the MCP server without an intermediate wrapper)
        result.set(entry.parentPid, info)
      }
    }
    catch {
      // Malformed file, skip
    }
  }))

  return result
}
