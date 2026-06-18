import { describe, expect, it } from 'vitest'
import { AGENT_STATUSES } from '../types'
import { STATUS_ORDER } from './agentSort'

describe('sTATUS_ORDER', () => {
  it('is a record of every AgentStatus mapped to its index', () => {
    AGENT_STATUSES.forEach((status, index) => {
      expect(STATUS_ORDER[status]).toBe(index)
    })
  })

  it('has the same number of entries as AGENT_STATUSES', () => {
    expect(Object.keys(STATUS_ORDER).length).toBe(AGENT_STATUSES.length)
  })

  it('active has a lower order value than waiting', () => {
    expect(STATUS_ORDER.active).toBeLessThan(STATUS_ORDER.waiting)
  })

  it('waiting has a lower order value than idle', () => {
    expect(STATUS_ORDER.waiting).toBeLessThan(STATUS_ORDER.idle)
  })

  it('active sorts before idle', () => {
    expect(STATUS_ORDER.active).toBeLessThan(STATUS_ORDER.idle)
  })

  it('all order values are non-negative integers', () => {
    Object.values(STATUS_ORDER).forEach((val) => {
      expect(val).toBeGreaterThanOrEqual(0)
      expect(Number.isInteger(val)).toBe(true)
    })
  })

  it('all order values are unique (no ties)', () => {
    const values = Object.values(STATUS_ORDER)
    const unique = new Set(values)
    expect(unique.size).toBe(values.length)
  })

  it('can be used to sort agents by status correctly', () => {
    const statuses: Array<'active' | 'waiting' | 'idle'> = ['idle', 'active', 'waiting']
    const sorted = [...statuses].sort((a, b) => STATUS_ORDER[a] - STATUS_ORDER[b])
    expect(sorted).toEqual(['active', 'waiting', 'idle'])
  })
})
