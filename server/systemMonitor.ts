import { readFile, statfs } from 'node:fs/promises'
import { cpus, freemem, loadavg, totalmem, uptime } from 'node:os'
import { WHITESPACE_RE } from './paths.js'
import { IS_LINUX } from './platform.js'

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

// Previous os.cpus() snapshot for delta-based CPU calculation on macOS
let prevMacCpus: ReturnType<typeof cpus> | null = null

async function getCpuUsageMac(): Promise<number> {
  const currentCpus = cpus()

  if (!prevMacCpus) {
    prevMacCpus = currentCpus
    return 0
  }

  let totalDelta = 0
  let idleDelta = 0

  for (let i = 0; i < currentCpus.length; i++) {
    const curr = currentCpus[i].times
    const prev = prevMacCpus[i]?.times ?? curr
    const currTotal = curr.user + curr.nice + curr.sys + curr.idle + curr.irq
    const prevTotal = prev.user + prev.nice + prev.sys + prev.idle + prev.irq
    totalDelta += currTotal - prevTotal
    idleDelta += curr.idle - prev.idle
  }

  prevMacCpus = currentCpus

  if (totalDelta === 0)
    return 0
  return Math.round((1 - idleDelta / totalDelta) * 10000) / 100
}

async function getDiskUsage(): Promise<SystemInfo['disk']> {
  const stats = await statfs('/')
  const total = stats.blocks * stats.bsize
  const available = stats.bavail * stats.bsize
  const used = total - stats.bfree * stats.bsize

  return {
    total,
    used,
    available,
    usagePercent: Math.round((used / total) * 10000) / 100,
    mount: '/',
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
