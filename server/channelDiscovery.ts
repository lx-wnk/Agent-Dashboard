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
 * Build a process-info map from a single `ps -A` invocation.
 * Returns Map<pid, { ppid, comm }> for all visible processes.
 */
async function getProcessMap(): Promise<Map<number, { ppid: number, comm: string }>> {
  const map = new Map<number, { ppid: number, comm: string }>()
  try {
    const { stdout } = await execFileAsync('ps', ['-A', '-o', 'pid=,ppid=,comm='])
    for (const line of stdout.split('\n')) {
      const parts = line.trim().split(WHITESPACE_RE)
      if (parts.length < 3)
        continue
      const pid = Number.parseInt(parts[0], 10)
      const ppid = Number.parseInt(parts[1], 10)
      const comm = parts.slice(2).join(' ')
      if (!Number.isNaN(pid) && !Number.isNaN(ppid))
        map.set(pid, { ppid, comm })
    }
  }
  catch { /* ps unavailable */ }
  return map
}

/**
 * Walk up the process tree (in memory) to find the nearest claude ancestor.
 * Returns the ancestor PID or null if not found within 5 levels.
 */
function findClaudeAncestorInMap(startPid: number, processMap: Map<number, { ppid: number, comm: string }>): number | null {
  let currentPid = startPid
  for (let depth = 0; depth < 5; depth++) {
    const info = processMap.get(currentPid)
    if (!info || info.ppid <= 1)
      return null
    if (info.comm.endsWith('/claude') || info.comm === 'claude')
      return currentPid
    currentPid = info.ppid
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

  const processMap = await getProcessMap()

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

      // mtime cache: avoid re-walking the in-memory process tree per
      // discovery file when the file hasn't changed since the last walk.
      // Discovery files are written once at MCP server startup, so the
      // mtime almost never changes.
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
          claudePid = findClaudeAncestorInMap(entry.parentPid, processMap)
          channelCache.set(filePath, { mtimeMs: fileStat.mtimeMs, pid: claudePid })
        }
      }
      catch {
        claudePid = findClaudeAncestorInMap(entry.parentPid, processMap)
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
