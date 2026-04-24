import type { PipelineTask } from '../../src/types.js'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../db/tasksRepo.js', () => ({ getTaskById: vi.fn() }))
vi.mock('../db/permissionsRepo.js', () => ({
  listTaskPermissions: vi.fn(() => []),
  createTaskPermission: vi.fn(),
}))
vi.mock('../db/auditRepo.js', () => ({ appendAudit: vi.fn() }))

import { getTaskById } from '../db/tasksRepo.js'
import { createTaskPermission, listTaskPermissions } from '../db/permissionsRepo.js'
import { appendAudit } from '../db/auditRepo.js'
import { bulkGrantKonzeptPermissions } from './approvalUtils.js'

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(listTaskPermissions).mockReturnValue([])
})

describe('bulkGrantKonzeptPermissions', () => {
  it('reads toolRequests from task.metadata', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: {
        toolRequests: [{ tool: 'Read', pattern: null, reason: 'read files' }],
      },
    } as unknown as PipelineTask)

    bulkGrantKonzeptPermissions('task-1')

    expect(vi.mocked(createTaskPermission)).toHaveBeenCalledWith(
      expect.objectContaining({ tool: 'Read', granted: true }),
    )
  })

  it('does nothing when task has no toolRequests', () => {
    vi.mocked(getTaskById).mockReturnValue({ id: 'task-1', metadata: {} } as unknown as PipelineTask)
    bulkGrantKonzeptPermissions('task-1')
    expect(vi.mocked(createTaskPermission)).not.toHaveBeenCalled()
  })

  it('skips unknown tools', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: { toolRequests: [{ tool: 'NotARealTool', pattern: null, reason: 'x' }] },
    } as unknown as PipelineTask)
    bulkGrantKonzeptPermissions('task-1')
    expect(vi.mocked(createTaskPermission)).not.toHaveBeenCalled()
  })

  it('skips duplicate: already-granted same tool+pattern', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: {
        toolRequests: [{ tool: 'Read', pattern: 'src/**', reason: 'read source' }],
      },
    } as unknown as PipelineTask)
    vi.mocked(listTaskPermissions).mockReturnValue([
      {
        id: 'perm-1',
        taskId: 'task-1',
        tool: 'Read',
        pattern: 'src/**',
        granted: true,
        preApproved: true,
        decidedBy: 'user',
        createdAt: '2026-01-01T00:00:00.000Z',
      } as unknown as ReturnType<typeof listTaskPermissions>[number],
    ])

    bulkGrantKonzeptPermissions('task-1')

    expect(vi.mocked(createTaskPermission)).not.toHaveBeenCalled()
  })

  it('writes audit entry on success', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: {
        toolRequests: [{ tool: 'Read', pattern: null, reason: 'r' }],
      },
    } as unknown as PipelineTask)

    bulkGrantKonzeptPermissions('task-1')

    expect(vi.mocked(appendAudit)).toHaveBeenCalledTimes(1)
    expect(vi.mocked(appendAudit)).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'bulk_granted_tool_permissions', taskId: 'task-1' }),
    )
  })

  it('is a no-op when task does not exist', () => {
    vi.mocked(getTaskById).mockReturnValue(null)

    bulkGrantKonzeptPermissions('missing-task')

    expect(vi.mocked(createTaskPermission)).not.toHaveBeenCalled()
    expect(vi.mocked(appendAudit)).not.toHaveBeenCalled()
  })

  it('skips null entries in toolRequests array', () => {
    vi.mocked(getTaskById).mockReturnValue({
      id: 'task-1',
      metadata: {
        toolRequests: [null, { tool: 'Read', pattern: null, reason: 'r' }],
      },
    } as unknown as PipelineTask)

    bulkGrantKonzeptPermissions('task-1')

    expect(vi.mocked(createTaskPermission)).toHaveBeenCalledTimes(1)
    expect(vi.mocked(createTaskPermission)).toHaveBeenCalledWith(
      expect.objectContaining({ tool: 'Read' }),
    )
  })
})
