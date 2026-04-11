import { execFile } from 'node:child_process'
import { readFile } from 'node:fs/promises'
import { cpus, freemem, loadavg, totalmem, uptime } from 'node:os'
import { promisify } from 'node:util'
import { IS_LINUX } from './platform'

const execFileAsync = promisify(execFile)

const CPU_IDLE_RE = /([\d.]+)%\s*idle/
const WHITESPACE_RE = /\s+/

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

// Previous /proc/stat snapshot for delta-based CPU calculation on Linux
let prevCpuIdle = 0
let prevCpuTotal = 0

async function getCpuUsageLinux(): Promise<number> {
  try {
    const stat = await readFile('/proc/stat', 'utf-8')
    const cpuLine = stat.split('\n').find(l => l.startsWith('cpu '))
    if (!cpuLine)
      return 0
    // cpu  user nice system idle iowait irq softirq steal
    const parts = cpuLine.trim().split(WHITESPACE_RE).slice(1).map(Number)
    const idle = parts[3] + (parts[4] || 0) // idle + iowait
    const total = parts.reduce((a, b) => a + b, 0)

    if (prevCpuTotal === 0) {
      prevCpuIdle = idle
      prevCpuTotal = total
      return 0
    }

    const deltaIdle = idle - prevCpuIdle
    const deltaTotal = total - prevCpuTotal
    prevCpuIdle = idle
    prevCpuTotal = total

    if (deltaTotal === 0)
      return 0
    return Math.round((1 - deltaIdle / deltaTotal) * 10000) / 100
  }
  catch {
    return 0
  }
}

async function getCpuUsageMac(): Promise<number> {
  try {
    const { stdout } = await execFileAsync('top', ['-l', '1', '-n', '0', '-s', '0'])
    const cpuLine = stdout.split('\n').find(line => line.startsWith('CPU usage:'))
    if (!cpuLine)
      return 0

    const idleMatch = cpuLine.match(CPU_IDLE_RE)
    if (!idleMatch)
      return 0

    return Math.round((100 - Number.parseFloat(idleMatch[1])) * 100) / 100
  }
  catch {
    return 0
  }
}

async function getDiskUsage(): Promise<SystemInfo['disk']> {
  const { stdout } = await execFileAsync('df', ['-k', '/'])
  const lines = stdout.trim().split('\n')
  const parts = lines[1].trim().split(WHITESPACE_RE)

  const totalKb = Number.parseInt(parts[1], 10)
  const usedKb = Number.parseInt(parts[2], 10)
  const availableKb = Number.parseInt(parts[3], 10)
  const capacityStr = parts[4] // e.g. "45%"

  return {
    total: totalKb * 1024,
    used: usedKb * 1024,
    available: availableKb * 1024,
    usagePercent: Number.parseInt(capacityStr, 10),
    mount: parts[parts.length - 1],
  }
}

export async function getSystemInfo(): Promise<SystemInfo> {
  const getCpuUsage = IS_LINUX ? getCpuUsageLinux : getCpuUsageMac
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
