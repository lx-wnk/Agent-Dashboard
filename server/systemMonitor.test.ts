import type { SystemInfo } from './systemMonitor'
import * as os from 'node:os'
import { beforeAll, describe, expect, it } from 'vitest'
import { getSystemInfo } from './systemMonitor'

// systemMonitor.ts uses named imports from node:os (live ESM bindings) and
// promisify(execFile) captured at module-init time.  Neither can be reliably
// intercepted via vi.mock in Vitest's Node environment without modifying the
// source module.
//
// These tests are integration tests: they run the real `top` and `df` commands
// and the real os module, then verify the *shape*, *types*, and *runtime
// constraints* of the returned SystemInfo object.  The machine must be macOS
// (per project requirements).  A single shared result is fetched once in
// beforeAll to avoid repeated ~2.5s `top` invocations.

describe('getSystemInfo', () => {
  let info: SystemInfo

  beforeAll(async () => {
    info = await getSystemInfo()
  }, 15_000) // allow up to 15s for top + df to complete

  it('resolves to an object with all required top-level keys', () => {
    expect(info).toHaveProperty('cpu')
    expect(info).toHaveProperty('memory')
    expect(info).toHaveProperty('disk')
    expect(info).toHaveProperty('loadAvg')
    expect(info).toHaveProperty('uptime')
  })

  it('cpu.usage is a finite number between 0 and 100', () => {
    expect(typeof info.cpu.usage).toBe('number')
    expect(Number.isFinite(info.cpu.usage)).toBe(true)
    expect(info.cpu.usage).toBeGreaterThanOrEqual(0)
    expect(info.cpu.usage).toBeLessThanOrEqual(100)
  })

  it('cpu.cores matches the actual number of logical CPUs on the host', () => {
    expect(info.cpu.cores).toBe(os.cpus().length)
  })

  it('cpu.model matches the first CPU entry model from the host', () => {
    expect(info.cpu.model).toBe(os.cpus()[0]?.model ?? 'Unknown')
  })

  it('memory.total matches os.totalmem()', () => {
    // totalmem() is a constant for the lifetime of the process
    expect(info.memory.total).toBe(os.totalmem())
  })

  it('memory.used = memory.total - memory.available', () => {
    expect(info.memory.used).toBe(info.memory.total - info.memory.available)
  })

  it('memory.usagePercent is between 0 and 100', () => {
    expect(info.memory.usagePercent).toBeGreaterThanOrEqual(0)
    expect(info.memory.usagePercent).toBeLessThanOrEqual(100)
  })

  it('memory.usagePercent equals Math.round((used/total)*10000)/100', () => {
    const expected = Math.round((info.memory.used / info.memory.total) * 10000) / 100
    expect(info.memory.usagePercent).toBe(expected)
  })

  it('disk.total, used, available are positive byte counts', () => {
    expect(info.disk.total).toBeGreaterThan(0)
    expect(info.disk.used).toBeGreaterThan(0)
    expect(info.disk.available).toBeGreaterThanOrEqual(0)
  })

  it('disk.total > disk.used', () => {
    expect(info.disk.total).toBeGreaterThan(info.disk.used)
  })

  it('disk.usagePercent is between 0 and 100', () => {
    expect(info.disk.usagePercent).toBeGreaterThanOrEqual(0)
    expect(info.disk.usagePercent).toBeLessThanOrEqual(100)
  })

  it('disk.mount is a non-empty string', () => {
    expect(typeof info.disk.mount).toBe('string')
    expect(info.disk.mount.length).toBeGreaterThan(0)
  })

  it('loadAvg is an array of 3 finite numbers', () => {
    expect(Array.isArray(info.loadAvg)).toBe(true)
    expect(info.loadAvg).toHaveLength(3)
    for (const val of info.loadAvg) {
      expect(typeof val).toBe('number')
      expect(Number.isFinite(val)).toBe(true)
    }
  })

  it('uptime is a positive number in seconds', () => {
    expect(info.uptime).toBeGreaterThan(0)
    expect(Number.isFinite(info.uptime)).toBe(true)
  })
})
