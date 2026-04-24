import { appendAudit } from '../db/auditRepo.js'
import { createTaskPermission, listTaskPermissions } from '../db/permissionsRepo.js'
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
  const rawRequests = (task.metadata as Record<string, unknown> | null)?.toolRequests
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
