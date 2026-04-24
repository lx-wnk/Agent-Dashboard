import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../db/tasksRepo', () => ({ getTaskById: vi.fn() }))
vi.mock('../db/permissionsRepo', () => ({
  listTaskPermissions: vi.fn(() => []),
  createTaskPermission: vi.fn(),
}))
vi.mock('../db/auditRepo', () => ({ appendAudit: vi.fn() }))

import { getTaskById } from '../db/tasksRepo'
import { createTaskPermission } from '../db/permissionsRepo'
import { bulkGrantKonzeptPermissions } from './approvalUtils'

beforeEach(() => vi.clearAllMocks())

describe('bulkGrantKonzeptPermissions', () => {
  it('reads toolRequests from task.metadata', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: {
        toolRequests: [{ tool: 'Read', pattern: null, reason: 'read files' }],
      },
    } as any)

    bulkGrantKonzeptPermissions('task-1')

    expect(vi.mocked(createTaskPermission)).toHaveBeenCalledWith(
      expect.objectContaining({ tool: 'Read', granted: true }),
    )
  })

  it('does nothing when task has no toolRequests', () => {
    vi.mocked(getTaskById).mockReturnValue({ id: 'task-1', metadata: {} } as any)
    bulkGrantKonzeptPermissions('task-1')
    expect(vi.mocked(createTaskPermission)).not.toHaveBeenCalled()
  })

  it('skips unknown tools', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: { toolRequests: [{ tool: 'NotARealTool', pattern: null, reason: 'x' }] },
    } as any)
    bulkGrantKonzeptPermissions('task-1')
    expect(vi.mocked(createTaskPermission)).not.toHaveBeenCalled()
  })
})
