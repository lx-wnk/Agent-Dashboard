import { appendAudit } from '../db/auditRepo.js'
import { createTaskPermission, listTaskPermissions } from '../db/permissionsRepo.js'
import { getLatestStageRun } from '../db/stageRunsRepo.js'

/**
 * The set of Claude Code tool names that may be granted as task permissions.
 * Kept here (rather than in types.ts) because it is a runtime enforcement
 * concern for pipeline permission gates, not a shared type contract.
 */
export const ALLOWED_TOOLS = new Set([
  'Bash', 'Read', 'Write', 'Edit', 'MultiEdit',
  'Glob', 'Grep', 'LS', 'WebFetch', 'WebSearch',
  'Task', 'Agent', 'TodoRead', 'TodoWrite',
  'NotebookRead', 'NotebookEdit',
])

/**
 * When approval2 is approved, bulk-grant every tool permission declared in
 * the umsetzungskonzept stage output's `toolRequests` array.
 *
 * Skips permissions already granted for the same tool+pattern pair.
 * Silently drops entries with unknown tool names (LLM hallucination guard).
 * Writes a single audit entry regardless of how many grants were created.
 */
export function bulkGrantKonzeptPermissions(taskId: string): void {
  const konzeptRun = getLatestStageRun(taskId, 'umsetzungskonzept')
  const rawRequests = (konzeptRun?.output as Record<string, unknown> | null)?.toolRequests
  if (!Array.isArray(rawRequests))
    return

  const existing = listTaskPermissions(taskId)
  for (const req of rawRequests) {
    if (typeof req !== 'object' || req === null)
      continue
    const r = req as Record<string, unknown>
    const tool = typeof r.tool === 'string' ? r.tool.trim() : null
    const pattern = typeof r.pattern === 'string' && r.pattern.trim() ? r.pattern.trim() : null
    if (!tool || !ALLOWED_TOOLS.has(tool))
      continue
    const alreadyGranted = existing.some(p => p.tool === tool && (p.pattern ?? null) === pattern && p.granted)
    if (alreadyGranted)
      continue
    createTaskPermission({ taskId, tool, pattern, granted: true, preApproved: true, decidedBy: 'user' })
  }

  appendAudit({
    taskId,
    actor: 'user',
    action: 'bulk_granted_tool_permissions',
    details: { source: 'umsetzungskonzept_toolRequests', count: rawRequests.length },
  })
}
