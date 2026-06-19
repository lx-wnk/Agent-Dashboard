import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { actionLabel, actionVariant, renderableActions, runAction } from '../useRunAction'

describe('useRunAction', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  const okResponse = () => Promise.resolve({ ok: true } as Response)
  const errResponse = (msg: string) =>
    Promise.resolve({
      ok: false,
      json: () => Promise.resolve({ error: msg }),
    } as unknown as Response)

  describe('runAction URL routing', () => {
    it.each([
      ['advance', '/api/tasks/t1/advance'],
      ['retry', '/api/tasks/t1/retry'],
      ['resume', '/api/tasks/t1/resume'],
      ['cancel', '/api/tasks/t1/cancel'],
      ['hold', '/api/tasks/t1/hold'],
      ['approve_all_pending', '/api/tasks/t1/approve-all-pending'],
      ['approve_spec', '/api/refine/t1/confirm'],
    ])('action %s hits %s', async (action, url) => {
      vi.mocked(fetch).mockImplementation(okResponse)
      await runAction('t1', action)
      expect(fetch).toHaveBeenCalledWith(url, expect.objectContaining({ method: 'POST' }))
    })

    it('throws on unknown action', async () => {
      await expect(runAction('t1', 'unknown_action')).rejects.toThrow('Unknown action')
    })

    it('throws with server error message on non-2xx', async () => {
      vi.mocked(fetch).mockImplementation(() => errResponse('not allowed'))
      await expect(runAction('t1', 'cancel')).rejects.toThrow('not allowed')
    })
  })

  describe('actionLabel', () => {
    it.each([
      ['advance', 'Advance →'],
      ['retry', 'Retry Stage'],
      ['resume', 'Resume'],
      ['cancel', 'Cancel Task'],
      ['hold', 'Hold'],
      ['approve_all_pending', 'Approve All Pending'],
      ['approve_spec', 'Approve Spec'],
    ])('%s → %s', (action, label) => {
      expect(actionLabel(action)).toBe(label)
    })

    it('falls back to the action name for unknown actions', () => {
      expect(actionLabel('mystery_action')).toBe('mystery_action')
    })
  })

  describe('actionVariant', () => {
    it.each([
      ['advance', 'primary'],
      ['retry', 'info'],
      ['resume', 'secondary'],
      ['cancel', 'danger'],
      ['hold', 'secondary'],
      ['approve_all_pending', 'info'],
      ['approve_spec', 'primary'],
    ])('%s → %s', (action, variant) => {
      expect(actionVariant(action)).toBe(variant)
    })
  })

  describe('renderableActions', () => {
    it('returns empty array for undefined', () => {
      expect(renderableActions(undefined)).toEqual([])
    })

    it('filters out actions without meta (e.g. open_pr if not mapped)', () => {
      const actions = [
        { action: 'advance', enabled: true, primary: true },
        { action: 'open_pr', enabled: true, primary: false },
        { action: 'cancel', enabled: false, reason: 'terminal', primary: false },
      ]
      const result = renderableActions(actions)
      expect(result.map(a => a.action)).toEqual(['advance', 'cancel'])
    })
  })
})
