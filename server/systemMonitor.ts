import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { cpus, freemem, loadavg, totalmem, uptime } from 'node:os'

const execFileAsync = promisify(execFile)

export interface SystemInfo {
  cpu: {
    usage: number
    cores: number
    model: string
  }
  memory: {
    total: number
    used: number
    available: number
    usagePercent: number
  }
  disk: {
    total: number
    used: number
    available: number
    usagePercent: number
    mount: string
  }
  loadAvg: number[]
  uptime: number
}

async function getCpuUsage(): Promise<number> {
  try {
    const { stdout } = await execFileAsync('top', ['-l', '1', '-n', '0', '-s', '0'])
    const cpuLine = stdout.split('\n').find(line => line.startsWith('CPU usage:'))
    if (!cpuLine) return 0

    const idleMatch = cpuLine.match(/([\d.]+)%\s*idle/)
    if (!idleMatch) return 0

    return Math.round((100 - parseFloat(idleMatch[1])) * 100) / 100
  } catch {
    return 0
  }
}

async function getDiskUsage(): Promise<SystemInfo['disk']> {
  const { stdout } = await execFileAsync('df', ['-k', '/'])
  const lines = stdout.trim().split('\n')
  // Header: Filesystem 1024-blocks Used Available Capacity ...
  // Data line follows
  const parts = lines[1].trim().split(/\s+/)

  const totalKb = parseInt(parts[1], 10)
  const usedKb = parseInt(parts[2], 10)
  const availableKb = parseInt(parts[3], 10)
  const capacityStr = parts[4] // e.g. "45%"

  return {
    total: totalKb * 1024,
    used: usedKb * 1024,
    available: availableKb * 1024,
    usagePercent: parseInt(capacityStr, 10),
    mount: parts[parts.length - 1],
  }
}

export async function getSystemInfo(): Promise<SystemInfo> {
  const [cpuUsage, disk] = await Promise.all([getCpuUsage(), getDiskUsage()])

  const totalMem = totalmem()
  const freeMem = freemem()
  const usedMem = totalMem - freeMem

  return {
    cpu: {
      usage: cpuUsage,
      cores: cpus().length,
      model: cpus()[0]?.model ?? 'Unknown',
    },
    memory: {
      total: totalMem,
      used: usedMem,
      available: freeMem,
      usagePercent: Math.round((usedMem / totalMem) * 10000) / 100,
    },
    disk,
    loadAvg: loadavg(),
    uptime: uptime(),
  }
}
