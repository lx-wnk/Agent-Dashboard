import { execFile } from 'node:child_process'
import { readlink } from 'node:fs/promises'
import { platform } from 'node:os'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

const LSOF_PATH_RE = /\nn(.+)/
const WHITESPACE_RE = /\s+/
const IS_LINUX = platform() === 'linux'

export interface ProcessInfo {
  pid: number
  cwd: string
  uptime: number
  command: string
}

export function parseElapsedTime(etime: string): number {
  // Format: [[dd-]hh:]mm:ss
  const parts = etime.trim().replace(/-/g, ':').split(':').reverse()
  const seconds = Number.parseInt(parts[0] || '0', 10)
  const minutes = Number.parseInt(parts[1] || '0', 10)
  const hours = Number.parseInt(parts[2] || '0', 10)
  const days = Number.parseInt(parts[3] || '0', 10)
  return days * 86400 + hours * 3600 + minutes * 60 + seconds
}

async function getCwdLinux(pid: number): Promise<string | null> {
  try {
    return await readlink(`/proc/${pid}/cwd`)
  }
  catch {
    return null
  }
}

async function getCwdMac(pid: number): Promise<string | null> {
  try {
    const { stdout } = await execFileAsync('lsof', ['-a', '-d', 'cwd', '-p', String(pid), '-Fn'])
    const match = stdout.match(LSOF_PATH_RE)
    return match ? match[1] : null
  }
  catch {
    return null
  }
}

const getCwd = IS_LINUX ? getCwdLinux : getCwdMac

export async function scanProcesses(): Promise<ProcessInfo[]> {
  const { stdout } = await execFileAsync('ps', ['-eo', 'pid,etime,comm'])
  const lines = stdout.trim().split('\n').slice(1) // skip header

  const claudeLines = lines.filter((line) => {
    const comm = line.trim().split(WHITESPACE_RE).slice(2).join(' ')
    return comm.endsWith('/claude') || comm === 'claude'
  })

  const parsed = claudeLines.map((line) => {
    const parts = line.trim().split(WHITESPACE_RE)
    return {
      pid: Number.parseInt(parts[0], 10),
      etime: parts[1],
      command: parts.slice(2).join(' '),
    }
  })

  const withCwd = await Promise.all(
    parsed.map(async p => ({ ...p, cwd: await getCwd(p.pid) })),
  )

  return withCwd
    .filter(p => p.cwd && p.cwd !== '/')
    .map(p => ({
      pid: p.pid,
      cwd: p.cwd!,
      uptime: parseElapsedTime(p.etime),
      command: p.command,
    }))
}
