import type { StageRun } from '../types'
import { describe, expect, it } from 'vitest'
import { activeRuntime, formatDuration } from './taskFormat'

function run(startedAt: string | null, endedAt: string | null): StageRun {
  return { startedAt, endedAt } as StageRun
}

describe('formatDuration', () => {
  it('formats hours and minutes', () => {
    expect(formatDuration(3_660_000)).toBe('1h 1m')
  })
  it('formats minutes and seconds', () => {
    expect(formatDuration(125_000)).toBe('2m 5s')
  })
  it('formats seconds only', () => {
    expect(formatDuration(5_000)).toBe('5s')
  })
})

describe('activeRuntime', () => {
  it('sums execution time across stage runs, excluding idle gaps', () => {
    const runs = [
      run('2026-06-16T10:00:00Z', '2026-06-16T10:01:00Z'),
      run('2026-06-16T10:30:00Z', '2026-06-16T10:31:30Z'),
    ]
    expect(activeRuntime(runs)).toBe('2m 30s')
  })

  it('ignores runs that never started', () => {
    expect(activeRuntime([run(null, null)])).toBe('—')
  })

  it('returns — for no stage runs', () => {
    expect(activeRuntime([])).toBe('—')
  })
})
