import type { Mock } from 'bun:test'
import type { PipelineTask } from '../../src/types.js'
import { beforeEach, describe, expect, it, mock } from 'bun:test'

import { appendAudit } from '../db/auditRepo.js'
import { createTaskPermission, listTaskPermissions } from '../db/permissionsRepo.js'
import { getTaskById } from '../db/tasksRepo.js'
import { bulkGrantKonzeptPermissions } from './approvalUtils.js'

mock.module('../db/tasksRepo.js', () => ({ getTaskById: mock() }))
mock.module('../db/permissionsRepo.js', () => ({
  listTaskPermissions: mock(() => []),
  createTaskPermission: mock(),
}))
mock.module('../db/auditRepo.js', () => ({ appendAudit: mock() }))

const mockGetTaskById = getTaskById as unknown as Mock<typeof getTaskById>
const mockListTaskPermissions = listTaskPermissions as unknown as Mock<typeof listTaskPermissions>
const mockCreateTaskPermission = createTaskPermission as unknown as Mock<typeof createTaskPermission>
const mockAppendAudit = appendAudit as unknown as Mock<typeof appendAudit>

beforeEach(() => {
  mockGetTaskById.mockReset()
  mockListTaskPermissions.mockReset()
  mockCreateTaskPermission.mockReset()
  mockAppendAudit.mockReset()
  mockListTaskPermissions.mockReturnValue([])
})

describe('bulkGrantKonzeptPermissions', () => {
  it('reads toolRequests from task.metadata.konzeptOutput', () => {
    mockGetTaskById.mockReturnValue({
      id: 'task-1',
      metadata: {
        konzeptOutput: {
          toolRequests: [{ tool: 'Read', pattern: null, reason: 'read files' }],
        },
      },
    } as unknown as PipelineTask)

    bulkGrantKonzeptPermissions('task-1')

    expect(mockCreateTaskPermission).toHaveBeenCalledWith(
      expect.objectContaining({ tool: 'Read', granted: true }),
    )
  })

  it('does nothing when task has no toolRequests', () => {
    mockGetTaskById.mockReturnValue({ id: 'task-1', metadata: {} } as unknown as PipelineTask)
    bulkGrantKonzeptPermissions('task-1')
    expect(mockCreateTaskPermission).not.toHaveBeenCalled()
  })

  it('skips unknown tools', () => {
    mockGetTaskById.mockReturnValue({
      id: 'task-1',
      metadata: { konzeptOutput: { toolRequests: [{ tool: 'NotARealTool', pattern: null, reason: 'x' }] } },
    } as unknown as PipelineTask)
    bulkGrantKonzeptPermissions('task-1')
    expect(mockCreateTaskPermission).not.toHaveBeenCalled()
  })

  it('skips duplicate: already-granted same tool+pattern', () => {
    mockGetTaskById.mockReturnValue({
      id: 'task-1',
      metadata: {
        konzeptOutput: {
          toolRequests: [{ tool: 'Read', pattern: 'src/**', reason: 'read source' }],
        },
      },
    } as unknown as PipelineTask)
    mockListTaskPermissions.mockReturnValue([
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

    expect(mockCreateTaskPermission).not.toHaveBeenCalled()
  })

  it('writes audit entry on success', () => {
    mockGetTaskById.mockReturnValue({
      id: 'task-1',
      metadata: {
        konzeptOutput: {
          toolRequests: [{ tool: 'Read', pattern: null, reason: 'r' }],
        },
      },
    } as unknown as PipelineTask)

    bulkGrantKonzeptPermissions('task-1')

    expect(mockAppendAudit).toHaveBeenCalledTimes(1)
    expect(mockAppendAudit).toHaveBeenCalledWith(
      expect.objectContaining({ action: 'bulk_granted_tool_permissions', taskId: 'task-1' }),
    )
  })

  it('is a no-op when task does not exist', () => {
    mockGetTaskById.mockReturnValue(null)

    bulkGrantKonzeptPermissions('missing-task')

    expect(mockCreateTaskPermission).not.toHaveBeenCalled()
    expect(mockAppendAudit).not.toHaveBeenCalled()
  })

  it('skips null entries in toolRequests array', () => {
    mockGetTaskById.mockReturnValue({
      id: 'task-1',
      metadata: {
        konzeptOutput: {
          toolRequests: [null, { tool: 'Read', pattern: null, reason: 'r' }],
        },
      },
    } as unknown as PipelineTask)

    bulkGrantKonzeptPermissions('task-1')

    expect(mockCreateTaskPermission).toHaveBeenCalledTimes(1)
    expect(mockCreateTaskPermission).toHaveBeenCalledWith(
      expect.objectContaining({ tool: 'Read' }),
    )
  })
})
