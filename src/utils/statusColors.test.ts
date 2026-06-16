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

  it('done stage uses success family tokens', () => {
    expect(stageChipClass('done')).toContain('success')
  })

  it('cancelled stage uses danger family tokens', () => {
    expect(stageChipClass('cancelled')).toContain('danger')
  })

  it('on_hold stage uses warning family tokens', () => {
    expect(stageChipClass('on_hold')).toContain('warning')
  })

  it('implementation stage uses info family tokens', () => {
    expect(stageChipClass('implementation')).toContain('info')
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

  it('running status uses info family tokens', () => {
    expect(runStatusChipClass('running')).toContain('info')
  })

  it('done status uses success family tokens', () => {
    expect(runStatusChipClass('done')).toContain('success')
  })

  it('failed status uses danger family tokens', () => {
    expect(runStatusChipClass('failed')).toContain('danger')
  })

  it('awaiting_user status uses warning family tokens', () => {
    expect(runStatusChipClass('awaiting_user')).toContain('warning')
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

  it('active status uses success family tokens', () => {
    expect(agentSessionStatusClass('active')).toContain('success')
  })

  it('waiting status uses warning family tokens', () => {
    expect(agentSessionStatusClass('waiting')).toContain('warning')
  })

  it('error status uses danger family tokens', () => {
    expect(agentSessionStatusClass('error')).toContain('danger')
  })
})
