import { appendAudit } from '../db/auditRepo.js'
import { createTaskPermission, listTaskPermissions } from '../db/permissionsRepo.js'
import { listPresets, upsertPreset } from '../db/permissionPresetsRepo.js'
import { getTaskById } from '../db/tasksRepo.js'

// Kept here (not in src/types.ts) because it is a runtime enforcement concern
// for pipeline permission gates, not a shared type contract.
export const ALLOWED_TOOLS = new Set([
  'Bash',
  'Read',
  'Write',
  'Edit',
  'MultiEdit',
  'Glob',
  'Grep',
  'LS',
  'WebFetch',
  'WebSearch',
  'Task',
  'Agent',
  'TodoRead',
  'TodoWrite',
  'NotebookRead',
  'NotebookEdit',
])

export function bulkGrantKonzeptPermissions(taskId: string): void {
  const task = getTaskById(taskId)
  if (!task)
    return
  const metadata = task?.metadata as Record<string, unknown> | null
  const konzeptOutput = metadata?.konzeptOutput as Record<string, unknown> | undefined
  const rawRequests = konzeptOutput?.toolRequests ?? metadata?.toolRequests
  if (!Array.isArray(rawRequests))
    return

  const existing = listTaskPermissions(taskId)
  let granted = 0
  for (const req of rawRequests) {
    if (typeof req !== 'object' || req === null)
      continue
    const r = req as Record<string, unknown>
    const tool = typeof r.tool === 'string' ? r.tool.trim() : null
    const patternTrimmed = typeof r.pattern === 'string' ? r.pattern.trim() : ''
    const pattern = patternTrimmed ? patternTrimmed : null
    if (!tool || !ALLOWED_TOOLS.has(tool))
      continue
    const alreadyGranted = existing.some(p => p.tool === tool && (p.pattern ?? null) === pattern && p.granted)
    if (alreadyGranted)
      continue
    createTaskPermission({ taskId, tool, pattern, granted: true, preApproved: true, decidedBy: 'user' })
    granted++
  }

  appendAudit({
    taskId,
    actor: 'user',
    action: 'bulk_granted_tool_permissions',
    details: { source: 'konzept_metadata_toolRequests', count: granted },
  })
}

/**
 * Applies (user, project) preset permissions onto a task. Skips entries
 * that are already granted on the task and tools outside ALLOWED_TOOLS.
 * Returns the number of newly-created grants.
 */
export function applyPresetPermissions(
  taskId: string,
  userId: string | null,
  projectCwd: string,
): number {
  const presets = listPresets(userId, projectCwd)
  if (presets.length === 0)
    return 0

  const existing = listTaskPermissions(taskId)
  let granted = 0
  for (const entry of presets) {
    if (!ALLOWED_TOOLS.has(entry.tool))
      continue
    const pattern = entry.pattern
    const alreadyGranted = existing.some(
      p => p.tool === entry.tool && (p.pattern ?? null) === pattern && p.granted,
    )
    if (alreadyGranted)
      continue
    createTaskPermission({
      taskId,
      tool: entry.tool,
      pattern,
      granted: true,
      preApproved: true,
      decidedBy: 'user',
    })
    granted++
  }

  if (granted > 0) {
    appendAudit({
      taskId,
      actor: 'user',
      action: 'applied_permission_presets',
      details: { projectCwd, userId, count: granted },
    })
  }

  return granted
}

/**
 * Persists the task's currently-granted tool permissions as presets for
 * the (user, project) pair. Idempotent: re-confirming a task with the same
 * grants is a no-op. Skips denied/revoked entries (granted === false).
 */
export function saveGrantsToPresets(
  taskId: string,
  userId: string | null,
  projectCwd: string,
): void {
  const grants = listTaskPermissions(taskId)
  for (const p of grants) {
    if (p.granted !== true)
      continue
    upsertPreset(userId, projectCwd, p.tool, p.pattern ?? null)
  }
}
