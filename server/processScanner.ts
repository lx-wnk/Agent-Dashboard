import { execFile } from 'node:child_process'
import { readlink } from 'node:fs/promises'
import { promisify } from 'node:util'
import { WHITESPACE_RE } from './paths.js'
import { IS_LINUX } from './platform.js'

const execFileAsync = promisify(execFile)

export interface ProcessInfo {
  pid: number
  cwd: string
  uptime: number
  command: string
}

export function parseElapsedTime(etime: string): number {
  const parts = etime.trim().replace(/-/g, ':').split(':').reverse()
  const seconds = Number.parseInt(parts[0] || '0', 10)
  const minutes = Number.parseInt(parts[1] || '0', 10)
  const hours = Number.parseInt(parts[2] || '0', 10)
  const days = Number.parseInt(parts[3] || '0', 10)
  return days * 86400 + hours * 3600 + minutes * 60 + seconds
}

/**
 * Parse `lsof -a -d cwd -p pid1,pid2 -Fn` output into a pid→cwd map.
 * Each process block: `p{pid}` line followed by `n{path}` line.
 */
export function parseLsofBatch(stdout: string): Map<number, string> {
  const result = new Map<number, string>()
  let currentPid: number | null = null
  for (const line of stdout.split('\n')) {
    if (line.startsWith('p')) {
      currentPid = Number.parseInt(line.slice(1), 10)
    }
    else if (line.startsWith('n') && currentPid !== null) {
      result.set(currentPid, line.slice(1))
      currentPid = null
    }
  }
  return result
}

async function getCwdsLinux(pids: number[]): Promise<Map<number, string>> {
  const result = new Map<number, string>()
  await Promise.all(
    pids.map(async (pid) => {
      try {
        const cwd = await readlink(`/proc/${pid}/cwd`)
        result.set(pid, cwd)
      }
      catch {
        // process may have exited
      }
    }),
  )
  return result
}

async function getCwdsMac(pids: number[]): Promise<Map<number, string>> {
  if (pids.length === 0)
    return new Map()
  try {
    const { stdout } = await execFileAsync('lsof', [
      '-a', '-d', 'cwd', '-p', pids.join(','), '-Fn',
    ])
    return parseLsofBatch(stdout)
  }
  catch {
    return new Map()
  }
}

const getCwds = IS_LINUX ? getCwdsLinux : getCwdsMac

export async function scanProcesses(): Promise<ProcessInfo[]> {
  const { stdout } = await execFileAsync('ps', ['-eo', 'pid,etime,comm'])
  const lines = stdout.trim().split('\n').slice(1)

  const parsed = lines
    .filter((line) => {
      const comm = line.trim().split(WHITESPACE_RE).slice(2).join(' ')
      return comm.endsWith('/claude') || comm === 'claude'
    })
    .map((line) => {
      const parts = line.trim().split(WHITESPACE_RE)
      return {
        pid: Number.parseInt(parts[0], 10),
        etime: parts[1],
        command: parts.slice(2).join(' '),
      }
    })

  const cwdMap = await getCwds(parsed.map(p => p.pid))

  return parsed
    .map(p => ({ ...p, cwd: cwdMap.get(p.pid) ?? null }))
    .filter(p => p.cwd && p.cwd !== '/')
    .map(p => ({
      pid: p.pid,
      cwd: p.cwd!,
      uptime: parseElapsedTime(p.etime),
      command: p.command,
    }))
}
