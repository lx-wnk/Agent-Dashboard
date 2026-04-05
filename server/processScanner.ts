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

  const processes: ProcessInfo[] = []

  for (const line of claudeLines) {
    const parts = line.trim().split(/\s+/)
    const pid = parseInt(parts[0], 10)
    const etime = parts[1]
    const command = parts.slice(2).join(' ')

    const cwd = await getCwd(pid)
    if (!cwd || cwd === '/') continue // skip processes without a real cwd

    processes.push({
      pid,
      cwd,
      uptime: parseElapsedTime(etime),
      command,
    })
  }

  return processes
}
