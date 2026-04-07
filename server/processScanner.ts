import { execFile } from 'node:child_process'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

export interface ProcessInfo {
  pid: number
  cwd: string
  uptime: number
  command: string
}

function parseElapsedTime(etime: string): number {
  // Format: [[dd-]hh:]mm:ss
  const parts = etime.trim().replace(/-/g, ':').split(':').reverse()
  const seconds = parseInt(parts[0] || '0', 10)
  const minutes = parseInt(parts[1] || '0', 10)
  const hours = parseInt(parts[2] || '0', 10)
  const days = parseInt(parts[3] || '0', 10)
  return days * 86400 + hours * 3600 + minutes * 60 + seconds
}

async function getCwd(pid: number): Promise<string | null> {
  try {
    const { stdout } = await execFileAsync('lsof', ['-a', '-d', 'cwd', '-p', String(pid), '-Fn'])
    // Output format: p<PID>\nfcwd\nn<path>
    const match = stdout.match(/\nn(.+)/)
    return match ? match[1] : null
  } catch {
    return null
  }
}

export async function scanProcesses(): Promise<ProcessInfo[]> {
  const { stdout } = await execFileAsync('ps', ['-eo', 'pid,etime,comm'])
  const lines = stdout.trim().split('\n').slice(1) // skip header

  const claudeLines = lines.filter(line => {
    const comm = line.trim().split(/\s+/).slice(2).join(' ')
    return comm.endsWith('/claude') || comm === 'claude'
  })

  const parsed = claudeLines.map(line => {
    const parts = line.trim().split(/\s+/)
    return {
      pid: parseInt(parts[0], 10),
      etime: parts[1],
      command: parts.slice(2).join(' '),
    }
  })

  const withCwd = await Promise.all(
    parsed.map(async p => ({ ...p, cwd: await getCwd(p.pid) }))
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
