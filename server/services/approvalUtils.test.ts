import type { Mock } from 'bun:test'
import type { PipelineTask } from '../../src/types.js'
import { beforeEach, describe, expect, it, mock } from 'bun:test'

import { appendAudit } from '../db/auditRepo.js'
import { getPipelineConfig } from '../db/notificationConfigRepo.js'
import { createTaskPermission, listTaskPermissions } from '../db/permissionsRepo.js'
import { getTaskById } from '../db/tasksRepo.js'
import { bulkGrantKonzeptPermissions, isDangerousBashPattern, validatePermissionEntry } from './approvalUtils.js'

mock.module('../db/tasksRepo.js', () => ({ getTaskById: mock() }))
mock.module('../db/permissionsRepo.js', () => ({
  listTaskPermissions: mock(() => []),
  createTaskPermission: mock(),
}))
mock.module('../db/auditRepo.js', () => ({ appendAudit: mock() }))
mock.module('../db/notificationConfigRepo.js', () => ({
  getPipelineConfig: mock(() => null),
  setPipelineConfig: mock(),
  getPipelineConfigNumber: mock((_k: string, fallback: number) => fallback),
}))

const mockGetTaskById = getTaskById as unknown as Mock<typeof getTaskById>
const mockListTaskPermissions = listTaskPermissions as unknown as Mock<typeof listTaskPermissions>
const mockCreateTaskPermission = createTaskPermission as unknown as Mock<typeof createTaskPermission>
const mockAppendAudit = appendAudit as unknown as Mock<typeof appendAudit>
const mockGetPipelineConfig = getPipelineConfig as unknown as Mock<typeof getPipelineConfig>

beforeEach(() => {
  mockGetTaskById.mockReset()
  mockListTaskPermissions.mockReset()
  mockCreateTaskPermission.mockReset()
  mockAppendAudit.mockReset()
  mockGetPipelineConfig.mockReset()
  mockListTaskPermissions.mockReturnValue([])
  mockGetPipelineConfig.mockReturnValue(null)
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

  it('skips unknown tools (konzept side; baseline still applies)', () => {
    mockGetTaskById.mockReturnValue({
      id: 'task-1',
      metadata: {
        skipBaseline: true, // isolate konzept-side behavior
        konzeptOutput: { toolRequests: [{ tool: 'NotARealTool', pattern: null, reason: 'x' }] },
      },
    } as unknown as PipelineTask)
    bulkGrantKonzeptPermissions('task-1')
    expect(mockCreateTaskPermission).not.toHaveBeenCalled()
  })

  it('skips konzept duplicate when same tool+pattern already granted (baseline opted out)', () => {
    mockGetTaskById.mockReturnValue({
      id: 'task-1',
      metadata: {
        skipBaseline: true,
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

    // baseline tools are also granted on top — Read is in the baseline so it
    // would normally appear once (deduplicated against the konzept output).
    // We assert it was called at least once with Read; baseline merge
    // (covered separately) handles the rest.
    const readCalls = mockCreateTaskPermission.mock.calls.filter(c => c[0].tool === 'Read')
    expect(readCalls.length).toBe(1)
  })

  describe('baseline merge (A3)', () => {
    it('grants the konzept_baseline template entries even when toolRequests is empty', () => {
      mockGetTaskById.mockReturnValue({
        id: 'task-base',
        metadata: { konzeptOutput: { toolRequests: [] } },
      } as unknown as PipelineTask)

      bulkGrantKonzeptPermissions('task-base')

      const tools = mockCreateTaskPermission.mock.calls.map(c => c[0].tool)
      expect(tools).toContain('Read')
      expect(tools).toContain('Edit')
      expect(tools).toContain('Glob')
      // Baseline includes safe Bash patterns
      const bashPatterns = mockCreateTaskPermission.mock.calls
        .filter(c => c[0].tool === 'Bash')
        .map(c => c[0].pattern)
      expect(bashPatterns).toContain('pnpm test*')
      expect(bashPatterns).toContain('git commit*')
    })

    it('does NOT grant a baseline entry that is already present in task_permissions', () => {
      mockGetTaskById.mockReturnValue({
        id: 'task-dup',
        metadata: { konzeptOutput: { toolRequests: [] } },
      } as unknown as PipelineTask)
      mockListTaskPermissions.mockReturnValue([
        {
          id: 'perm-existing',
          taskId: 'task-dup',
          tool: 'Read',
          pattern: null,
          granted: true,
          preApproved: true,
          decidedBy: 'user',
          createdAt: '2026-01-01T00:00:00.000Z',
        } as unknown as ReturnType<typeof listTaskPermissions>[number],
      ])

      bulkGrantKonzeptPermissions('task-dup')

      const readCalls = mockCreateTaskPermission.mock.calls.filter(c => c[0].tool === 'Read')
      expect(readCalls.length).toBe(0)
    })

    it('konzept-emitted entry takes precedence — baseline does not duplicate it', () => {
      mockGetTaskById.mockReturnValue({
        id: 'task-precedence',
        metadata: {
          konzeptOutput: {
            // Konzept already emitted Read; baseline must not re-emit
            toolRequests: [{ tool: 'Read', pattern: null, reason: 'konzept-said' }],
          },
        },
      } as unknown as PipelineTask)

      bulkGrantKonzeptPermissions('task-precedence')

      const readCalls = mockCreateTaskPermission.mock.calls.filter(c => c[0].tool === 'Read')
      expect(readCalls.length).toBe(1)
    })

    it('skips baseline entirely when task.metadata.skipBaseline=true (per-task opt-out)', () => {
      mockGetTaskById.mockReturnValue({
        id: 'task-optout',
        metadata: {
          skipBaseline: true,
          konzeptOutput: { toolRequests: [{ tool: 'Read', pattern: null, reason: 'r' }] },
        },
      } as unknown as PipelineTask)

      bulkGrantKonzeptPermissions('task-optout')

      const tools = mockCreateTaskPermission.mock.calls.map(c => c[0].tool)
      expect(tools).toEqual(['Read'])
    })
  })

  describe('pipeline_config defaultPermissionTemplate (A4)', () => {
    it('uses the configured template name when defaultPermissionTemplate is set', () => {
      mockGetPipelineConfig.mockImplementation((key: string) =>
        key === 'defaultPermissionTemplate' ? 'research_only' : null,
      )
      mockGetTaskById.mockReturnValue({
        id: 'task-cfg',
        metadata: { konzeptOutput: { toolRequests: [] } },
      } as unknown as PipelineTask)

      bulkGrantKonzeptPermissions('task-cfg')

      const tools = mockCreateTaskPermission.mock.calls.map(c => c[0].tool)
      // research_only includes WebSearch but no Bash
      expect(tools).toContain('WebSearch')
      expect(tools).not.toContain('Bash')
    })

    it('falls back to konzept_baseline when defaultPermissionTemplate is unknown', () => {
      mockGetPipelineConfig.mockImplementation((key: string) =>
        key === 'defaultPermissionTemplate' ? 'not_a_template' : null,
      )
      mockGetTaskById.mockReturnValue({
        id: 'task-bad-cfg',
        metadata: { konzeptOutput: { toolRequests: [] } },
      } as unknown as PipelineTask)

      bulkGrantKonzeptPermissions('task-bad-cfg')

      const bashPatterns = mockCreateTaskPermission.mock.calls
        .filter(c => c[0].tool === 'Bash')
        .map(c => c[0].pattern)
      expect(bashPatterns).toContain('pnpm test*')
    })
  })
})

describe('isDangerousBashPattern (busy-wait polling guards)', () => {
  it('flags `until [...]; do sleep N; done` polling loop', () => {
    expect(isDangerousBashPattern('until [ -e /tmp/flag ]; do sleep 5; done')).toBe(true)
  })

  it('flags `while [...]; do sleep N; done` polling loop', () => {
    expect(isDangerousBashPattern('while [ ! -f /tmp/x ]; do sleep 30; done')).toBe(true)
  })

  it('flags loop where sleep follows on a separate line', () => {
    expect(isDangerousBashPattern('until grep ready /tmp/log\ndo\n  sleep 10\ndone')).toBe(true)
  })

  it('keeps existing dangerous patterns blocked', () => {
    expect(isDangerousBashPattern('curl https://example.com')).toBe(true)
    expect(isDangerousBashPattern('python -c "print(1)"')).toBe(true)
  })

  it('does not over-block legitimate patterns', () => {
    expect(isDangerousBashPattern('pnpm test')).toBe(false)
    expect(isDangerousBashPattern('git status')).toBe(false)
  })

  it('validatePermissionEntry rejects polling-loop Bash patterns', () => {
    const result = validatePermissionEntry('Bash', 'until [ -e /tmp/x ]; do sleep 5; done')
    expect(result.ok).toBe(false)
    expect(result.reason).toMatch(/dangerous-pattern/)
  })
})
