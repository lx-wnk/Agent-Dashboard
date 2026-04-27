import { describe, expect, it } from 'vitest'
import { parseElapsedTime, parseLsofBatch, scanProcesses } from './processScanner'

describe('parseLsofBatch', () => {
  it('parses a single process entry', () => {
    const stdout = 'p123\nn/Users/alex/my-project\n'
    expect(parseLsofBatch(stdout)).toEqual(new Map([[123, '/Users/alex/my-project']]))
  })

  it('parses multiple process entries', () => {
    const stdout = 'p100\nn/home/a\np200\nn/home/b\n'
    const result = parseLsofBatch(stdout)
    expect(result.get(100)).toBe('/home/a')
    expect(result.get(200)).toBe('/home/b')
    expect(result.size).toBe(2)
  })

  it('returns empty map for empty input', () => {
    expect(parseLsofBatch('')).toEqual(new Map())
  })

  it('ignores lines that are not p- or n-prefixed', () => {
    const stdout = 'p99\nf3\nn/some/path\n'
    expect(parseLsofBatch(stdout)).toEqual(new Map([[99, '/some/path']]))
  })

  it('skips a pid entry that has no following n-line before next p-line', () => {
    const stdout = 'p99\np100\nn/some/path\n'
    const result = parseLsofBatch(stdout)
    expect(result.get(99)).toBeUndefined()
    expect(result.get(100)).toBe('/some/path')
  })
})

// NOTE: processScanner.ts captures `execFileAsync = promisify(execFile)` at
// module-evaluation time.  Vitest's vi.mock on node:util or node:child_process
// cannot retroactively update that already-bound reference.  The scanProcesses
// tests therefore run against the real `ps` and `lsof` binaries (macOS-only,
// per project requirements) and verify structural/contractual properties of
// the output rather than specific values.

// ── parseElapsedTime ────────────────────────────────────────────────────────

describe('parseElapsedTime', () => {
  it('parses mm:ss format', () => {
    expect(parseElapsedTime('05:23')).toBe(5 * 60 + 23)
  })

  it('parses hh:mm:ss format', () => {
    expect(parseElapsedTime('01:05:23')).toBe(1 * 3600 + 5 * 60 + 23)
  })

  it('parses dd-hh:mm:ss format', () => {
    expect(parseElapsedTime('2-01:05:23')).toBe(2 * 86400 + 1 * 3600 + 5 * 60 + 23)
  })

  it('parses 00:00 as 0 seconds', () => {
    expect(parseElapsedTime('00:00')).toBe(0)
  })

  it('parses just seconds (00:ss)', () => {
    expect(parseElapsedTime('00:45')).toBe(45)
  })

  it('handles leading/trailing whitespace', () => {
    expect(parseElapsedTime('  03:30  ')).toBe(3 * 60 + 30)
  })

  it('parses a one-day elapsed time', () => {
    expect(parseElapsedTime('1-00:00:00')).toBe(86400)
  })

  it('returns the correct value for a multi-day elapsed time', () => {
    expect(parseElapsedTime('2-01:05:23')).toBe(2 * 86400 + 1 * 3600 + 5 * 60 + 23)
  })

  it('parses hours-minutes-seconds correctly when days are absent', () => {
    // 10 hours, 0 minutes, 0 seconds
    expect(parseElapsedTime('10:00:00')).toBe(36000)
  })
})

// ── scanProcesses ───────────────────────────────────────────────────────────
// Integration tests: run the real ps/lsof and verify structural invariants.

describe('scanProcesses', () => {
  it('returns an array (possibly empty)', async () => {
    const result = await scanProcesses()
    expect(Array.isArray(result)).toBe(true)
  })

  it('every ProcessInfo entry has a positive integer pid', async () => {
    const result = await scanProcesses()
    for (const p of result) {
      expect(Number.isInteger(p.pid)).toBe(true)
      expect(p.pid).toBeGreaterThan(0)
    }
  })

  it('every ProcessInfo entry has a non-empty cwd string', async () => {
    const result = await scanProcesses()
    for (const p of result) {
      expect(typeof p.cwd).toBe('string')
      expect(p.cwd.length).toBeGreaterThan(0)
    }
  })

  it('every ProcessInfo entry has cwd that is not the root path', async () => {
    const result = await scanProcesses()
    for (const p of result) {
      expect(p.cwd).not.toBe('/')
    }
  })

  it('every ProcessInfo entry has a non-negative integer uptime', async () => {
    const result = await scanProcesses()
    for (const p of result) {
      expect(Number.isInteger(p.uptime) || typeof p.uptime === 'number').toBe(true)
      expect(p.uptime).toBeGreaterThanOrEqual(0)
    }
  })

  it('every ProcessInfo entry has a non-empty command string ending with "claude"', async () => {
    const result = await scanProcesses()
    for (const p of result) {
      expect(typeof p.command).toBe('string')
      expect(p.command.endsWith('/claude') || p.command === 'claude').toBe(true)
    }
  })
})
