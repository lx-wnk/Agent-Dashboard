import { describe, expect, it } from 'vitest'
import { agentSessionStatusClass, runStatusChipClass, stageChipClass, statusLabel } from './statusColors'

describe('statusLabel', () => {
  it('returns human-readable labels for known statuses', () => {
    expect(statusLabel('active')).toBe('Active')
    expect(statusLabel('waiting')).toBe('Waiting')
    expect(statusLabel('idle')).toBe('Idle')
    expect(statusLabel('completed')).toBe('Completed')
    expect(statusLabel('error')).toBe('Error')
    expect(statusLabel('info')).toBe('Info')
  })

  it('returns the raw string for unknown values (pass-through)', () => {
    expect(statusLabel('unknown-status')).toBe('unknown-status')
    expect(statusLabel('')).toBe('')
    expect(statusLabel('custom')).toBe('custom')
  })
})

describe('stageChipClass', () => {
  it('returns a non-empty string for every known pipeline stage', () => {
    const knownStages = ['on_hold', 'implementation', 'done', 'cancelled']
    knownStages.forEach((stage) => {
      const cls = stageChipClass(stage)
      expect(typeof cls).toBe('string')
      expect(cls.length).toBeGreaterThan(0)
    })
  })

  it('returns a non-empty fallback string for unknown stages', () => {
    const cls = stageChipClass('some-future-stage')
    expect(typeof cls).toBe('string')
    expect(cls.length).toBeGreaterThan(0)
  })

  it('done stage contains green color classes', () => {
    expect(stageChipClass('done')).toContain('green')
  })

  it('cancelled stage contains red color classes', () => {
    expect(stageChipClass('cancelled')).toContain('red')
  })

  it('on_hold stage contains yellow color classes', () => {
    expect(stageChipClass('on_hold')).toContain('yellow')
  })

  it('implementation stage contains blue color classes', () => {
    expect(stageChipClass('implementation')).toContain('blue')
  })
})

describe('runStatusChipClass', () => {
  it('returns a non-empty string for every known run status', () => {
    const knownStatuses = ['running', 'done', 'failed', 'on_hold', 'awaiting_user']
    knownStatuses.forEach((status) => {
      const cls = runStatusChipClass(status)
      expect(typeof cls).toBe('string')
      expect(cls.length).toBeGreaterThan(0)
    })
  })

  it('pending status returns fallback class', () => {
    const cls = runStatusChipClass('pending')
    expect(typeof cls).toBe('string')
    expect(cls.length).toBeGreaterThan(0)
  })

  it('returns a non-empty fallback for truly unknown statuses', () => {
    const cls = runStatusChipClass('truly-unknown-status')
    expect(typeof cls).toBe('string')
    expect(cls.length).toBeGreaterThan(0)
  })

  it('running status contains blue color classes', () => {
    expect(runStatusChipClass('running')).toContain('blue')
  })

  it('done status contains green color classes', () => {
    expect(runStatusChipClass('done')).toContain('green')
  })

  it('failed status contains red color classes', () => {
    expect(runStatusChipClass('failed')).toContain('red')
  })

  it('awaiting_user status contains yellow color classes', () => {
    expect(runStatusChipClass('awaiting_user')).toContain('yellow')
  })

  it('on_hold and awaiting_user return identical classes', () => {
    expect(runStatusChipClass('on_hold')).toBe(runStatusChipClass('awaiting_user'))
  })
})

describe('agentSessionStatusClass', () => {
  it('returns a non-empty string for every known agent status', () => {
    const knownStatuses = ['active', 'waiting', 'idle', 'completed', 'error']
    knownStatuses.forEach((status) => {
      const cls = agentSessionStatusClass(status)
      expect(typeof cls).toBe('string')
      expect(cls.length).toBeGreaterThan(0)
    })
  })

  it('returns a non-empty fallback for unknown statuses', () => {
    const cls = agentSessionStatusClass('unknown')
    expect(typeof cls).toBe('string')
    expect(cls.length).toBeGreaterThan(0)
  })

  it('active status contains green color classes', () => {
    expect(agentSessionStatusClass('active')).toContain('green')
  })

  it('waiting status contains yellow color classes', () => {
    expect(agentSessionStatusClass('waiting')).toContain('yellow')
  })

  it('error status contains red color classes', () => {
    expect(agentSessionStatusClass('error')).toContain('red')
  })
})
