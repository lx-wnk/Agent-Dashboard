import type { TaskPermission } from '../../src/types.js'
import type { PermissionTemplateEntry, PermissionTemplateName } from './permissionTemplates.js'
import { readFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { appendAudit } from '../db/auditRepo.js'
import { getPipelineConfig } from '../db/notificationConfigRepo.js'
import { listPresets, upsertPreset } from '../db/permissionPresetsRepo.js'
import { bulkCreateTaskPermissions, createTaskPermission, listTaskPermissions } from '../db/permissionsRepo.js'
import { getTaskById } from '../db/tasksRepo.js'
import { DEFAULT_KONZEPT_BASELINE_TEMPLATE, isPermissionTemplate, resolveTemplate } from './permissionTemplates.js'

// Matches Bash patterns that could fetch or execute remote code, or that
// implement busy-wait polling loops (which agents have been observed to
// generate as a permission-gate workaround, burning tokens indefinitely).
// Used to block automatic pre-approval of potentially dangerous commands
// from agent-emitted toolRequests. Operators can still grant these
// manually via the UI, where the warning surfaces explicitly.
//
// Polling-loop patterns matched: `until [...]; do sleep N; done`,
// `while [...]; do sleep N; done`, and any while/until paired with a
// sleep call inside the same pattern string.
const DANGEROUS_BASH_RE = /curl|wget|nc\b|netcat|python\s+-c|perl\s+-e|ruby\s+-e|eval\b|base64\s+-d|[;&|`]|\$\(|\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}|\b(?:until|while)\b[\s\S]+?\bsleep\s+\d+/i

export function isDangerousBashPattern(pattern: string): boolean {
  return DANGEROUS_BASH_RE.test(pattern)
}

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

export interface PermissionValidationResult {
  ok: boolean
  reason?: string
}

/**
 * Single source of truth for "can this grant be auto-applied?". Used by:
 *   - bulkGrantKonzeptPermissions / bulkGrantAgentFilePermissions
 *   - applyPresetPermissions
 *   - MCP create_task / manage_task / grant_permission tools
 *   - REST POST /api/tasks (permissions[])
 *
 * Returns ok=false when the entry would be unsafe to silently allow-list.
 * Operators can still force-grant via UI (they see the warning), but every
 * code path that auto-creates permissions MUST check this first.
 */
export function validatePermissionEntry(
  tool: string,
  pattern: string | null | undefined,
): PermissionValidationResult {
  if (typeof tool !== 'string' || tool.trim().length === 0)
    return { ok: false, reason: 'tool name is empty' }
  if (!ALLOWED_TOOLS.has(tool))
    return { ok: false, reason: `tool '${tool}' is not in ALLOWED_TOOLS` }
  if (tool === 'Bash' && pattern && isDangerousBashPattern(pattern))
    return { ok: false, reason: `Bash pattern matches dangerous-pattern allowlist (curl/wget/eval/shell-substitution/etc.): '${pattern}'` }
  return { ok: true }
}

/**
 * Resolves the pipeline-config-driven baseline template. Falls back to
 * `konzept_baseline` when the config key is unset or names a non-existent
 * template. Returns `[]` if (theoretically) the resolved template is empty.
 */
function resolveBaselineTemplate(): PermissionTemplateEntry[] {
  const configured = getPipelineConfig('defaultPermissionTemplate')
  const name = configured && isPermissionTemplate(configured)
    ? configured
    : DEFAULT_KONZEPT_BASELINE_TEMPLATE
  return resolveTemplate(name)
}

export function bulkGrantKonzeptPermissions(taskId: string): void {
  const task = getTaskById(taskId)
  if (!task)
    return
  const metadata = task?.metadata as Record<string, unknown> | null
  const konzeptOutput = metadata?.konzeptOutput as Record<string, unknown> | undefined
  const rawRequests = konzeptOutput?.toolRequests ?? metadata?.toolRequests
  if (!Array.isArray(rawRequests))
    return

  // Konzept-emitted entries take precedence over the baseline. We collect
  // them first, then merge the baseline on top, skipping any (tool, pattern)
  // pair already produced by konzept.
  interface PendingEntry { tool: string, pattern: string | null, source: 'konzept' | 'baseline' }
  const pending: PendingEntry[] = []
  const seen = new Set<string>()
  const keyOf = (tool: string, pattern: string | null): string => `${tool}::${pattern ?? ''}`

  for (const req of rawRequests) {
    if (typeof req !== 'object' || req === null)
      continue
    const r = req as Record<string, unknown>
    const tool = typeof r.tool === 'string' ? r.tool.trim() : null
    const patternTrimmed = typeof r.pattern === 'string' ? r.pattern.trim() : ''
    const pattern = patternTrimmed || null
    if (!tool)
      continue
    const validation = validatePermissionEntry(tool, pattern)
    if (!validation.ok)
      continue
    const k = keyOf(tool, pattern)
    if (seen.has(k))
      continue
    seen.add(k)
    pending.push({ tool, pattern, source: 'konzept' })
  }

  // Merge baseline unless task opts out (per-task override). Baseline entries
  // are already-validated constants, but we run them through
  // validatePermissionEntry anyway so a future baseline change with an unsafe
  // pattern is caught at runtime.
  const skipBaseline = metadata?.skipBaseline === true
  if (!skipBaseline) {
    const baseline = resolveBaselineTemplate()
    for (const entry of baseline) {
      const tool = entry.tool
      const pattern = entry.pattern ?? null
      const validation = validatePermissionEntry(tool, pattern)
      if (!validation.ok)
        continue
      const k = keyOf(tool, pattern)
      if (seen.has(k))
        continue
      seen.add(k)
      pending.push({ tool, pattern, source: 'baseline' })
    }
  }

  const existing = listTaskPermissions(taskId)
  let granted = 0
  let baselineGranted = 0
  let konzeptGranted = 0
  for (const entry of pending) {
    const alreadyGranted = existing.some(p =>
      p.tool === entry.tool && (p.pattern ?? null) === entry.pattern && p.granted,
    )
    if (alreadyGranted)
      continue
    createTaskPermission({
      taskId,
      tool: entry.tool,
      pattern: entry.pattern,
      granted: true,
      preApproved: true,
      decidedBy: 'user',
    })
    granted++
    if (entry.source === 'baseline')
      baselineGranted++
    else
      konzeptGranted++
  }

  appendAudit({
    taskId,
    actor: 'user',
    action: 'bulk_granted_tool_permissions',
    details: {
      source: 'konzept_metadata_toolRequests',
      count: granted,
      konzeptCount: konzeptGranted,
      baselineCount: baselineGranted,
      baselineSkipped: skipBaseline,
    },
  })
}

interface ParsedPermission {
  tool: string
  pattern: string | null
}

// Matches `## Pipeline Permissions` (any level-2 heading, case-insensitive).
const PIPELINE_PERMISSIONS_HEADING_RE = /^##\s+pipeline\s+permissions\s*$/im
// Matches the first fenced code block (```json or ```) after a heading,
// stopping at the next `##` heading.
// `[ \t]*` (not `\s*`) before the newline avoids super-linear backtracking
// against the following `[\s\S]*?` — `\s` would otherwise overlap with `\n`.
const FENCED_BLOCK_RE = /```(?:json)?[ \t]*\n([\s\S]*?)\n```/i
// Matches `Tool(pattern)` where pattern may contain anything except an unbalanced paren.
const TOOL_PATTERN_RE = /^([A-Z]\w*)\(([\s\S]*)\)\s*$/i
// Used to slice an `## Pipeline Permissions` section up to the next `##` heading.
const NEXT_HEADING_RE = /\n##\s+/

function parsePermissionEntry(entry: unknown): ParsedPermission | null {
  if (typeof entry === 'string') {
    const trimmed = entry.trim()
    if (!trimmed)
      return null
    const match = trimmed.match(TOOL_PATTERN_RE)
    if (match) {
      const tool = match[1].trim()
      const pattern = match[2].trim()
      return { tool, pattern: pattern.length > 0 ? pattern : null }
    }
    return { tool: trimmed, pattern: null }
  }
  if (typeof entry === 'object' && entry !== null) {
    const e = entry as Record<string, unknown>
    const tool = typeof e.tool === 'string' ? e.tool.trim() : null
    if (!tool)
      return null
    const patternRaw = typeof e.pattern === 'string' ? e.pattern.trim() : ''
    return { tool, pattern: patternRaw.length > 0 ? patternRaw : null }
  }
  return null
}

export function parseDefaultPermissionsFromFile(filePath: string): ParsedPermission[] {
  let content: string
  try {
    content = readFileSync(filePath, 'utf8')
  }
  catch {
    return []
  }

  const headingMatch = PIPELINE_PERMISSIONS_HEADING_RE.exec(content)
  if (!headingMatch)
    return []

  // Slice from the end of the heading match to the next `##` heading (if any).
  const afterHeading = content.slice(headingMatch.index + headingMatch[0].length)
  const nextHeadingIdx = afterHeading.search(NEXT_HEADING_RE)
  const section = nextHeadingIdx >= 0 ? afterHeading.slice(0, nextHeadingIdx) : afterHeading

  const fencedMatch = FENCED_BLOCK_RE.exec(section)
  if (!fencedMatch)
    return []

  let parsed: unknown
  try {
    parsed = JSON.parse(fencedMatch[1])
  }
  catch {
    return []
  }

  if (typeof parsed !== 'object' || parsed === null)
    return []
  const allow = (parsed as Record<string, unknown>).allow
  if (!Array.isArray(allow))
    return []

  const result: ParsedPermission[] = []
  for (const entry of allow) {
    const p = parsePermissionEntry(entry)
    if (p)
      result.push(p)
  }
  return result
}

export function bulkGrantAgentFilePermissions(taskId: string, cwd: string): void {
  const sources = [
    join(cwd, 'AGENTS.md'),
    join(cwd, 'CLAUDE.md'),
    join(cwd, '.claude', 'CLAUDE.md'),
    join(homedir(), '.claude', 'CLAUDE.md'),
  ]

  const merged: ParsedPermission[] = []
  const usedSources: string[] = []
  for (const src of sources) {
    const entries = parseDefaultPermissionsFromFile(src)
    if (entries.length > 0) {
      merged.push(...entries)
      usedSources.push(src)
    }
  }

  if (merged.length === 0)
    return

  const existing = listTaskPermissions(taskId)
  let granted = 0
  const seen = new Set<string>()
  for (const entry of merged) {
    if (!ALLOWED_TOOLS.has(entry.tool))
      continue
    const key = `${entry.tool}::${entry.pattern ?? ''}`
    if (seen.has(key))
      continue
    seen.add(key)
    const alreadyGranted = existing.some(
      p => p.tool === entry.tool && (p.pattern ?? null) === entry.pattern && p.granted,
    )
    if (alreadyGranted)
      continue
    createTaskPermission({
      taskId,
      tool: entry.tool,
      pattern: entry.pattern,
      granted: true,
      preApproved: true,
      decidedBy: 'user',
    })
    granted++
  }

  appendAudit({
    taskId,
    actor: 'user',
    action: 'bulk_granted_agent_file_permissions',
    details: { sources: usedSources, count: granted },
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

export interface BulkGrantEntry {
  tool: string
  pattern?: string | null
  expiresAt?: string | null
}

export interface BulkGrantResult {
  granted: TaskPermission[]
  skipped: Array<{ tool: string, pattern: string | null, reason: string }>
}

/**
 * Grant a list of permissions to a task in one transaction. Validates each
 * entry against ALLOWED_TOOLS + DANGEROUS_BASH_RE. Skips already-granted
 * (taskId, tool, pattern) tuples idempotently. Used by:
 *   - MCP create_task `permissions[]` and `template`
 *   - MCP manage_task `grant_permissions` action
 *   - REST POST /api/tasks
 *   - Subagent inheritance (inheritParentPermissions)
 */
export function bulkGrantPermissions(
  taskId: string,
  entries: BulkGrantEntry[],
  opts: { decidedBy?: 'user' | 'auto', preApproved?: boolean, source?: string } = {},
): BulkGrantResult {
  const existing = listTaskPermissions(taskId)
  const skipped: BulkGrantResult['skipped'] = []
  const toInsert: Parameters<typeof bulkCreateTaskPermissions>[0] = []

  for (const entry of entries) {
    const tool = entry.tool?.trim?.() ?? ''
    const pattern = entry.pattern && entry.pattern.trim().length > 0 ? entry.pattern.trim() : null
    const validation = validatePermissionEntry(tool, pattern)
    if (!validation.ok) {
      skipped.push({ tool, pattern, reason: validation.reason ?? 'rejected' })
      continue
    }
    const alreadyGranted = existing.some(p => p.tool === tool && (p.pattern ?? null) === pattern && p.granted)
    if (alreadyGranted) {
      skipped.push({ tool, pattern, reason: 'already granted' })
      continue
    }
    toInsert.push({
      taskId,
      tool,
      pattern,
      granted: true,
      preApproved: opts.preApproved ?? true,
      decidedBy: opts.decidedBy ?? 'user',
      expiresAt: entry.expiresAt ?? null,
    })
  }

  const granted = bulkCreateTaskPermissions(toInsert)

  if (granted.length > 0) {
    appendAudit({
      taskId,
      actor: opts.decidedBy === 'auto' ? 'system' : 'user',
      action: 'bulk_grant_permissions',
      details: {
        source: opts.source ?? 'unspecified',
        granted: granted.length,
        skipped: skipped.length,
        skippedDetail: skipped,
      },
    })
  }

  return { granted, skipped }
}

/**
 * Apply a named template to a task (e.g. `feature_implementation`).
 * Wraps bulkGrantPermissions with template-resolution and audit metadata.
 */
export function applyPermissionTemplate(
  taskId: string,
  templateName: PermissionTemplateName,
): BulkGrantResult {
  const entries: PermissionTemplateEntry[] = resolveTemplate(templateName)
  return bulkGrantPermissions(
    taskId,
    entries.map(e => ({ tool: e.tool, pattern: e.pattern ?? null })),
    { source: `template:${templateName}` },
  )
}

export function applyPermissionTemplateByName(
  taskId: string,
  name: string,
): BulkGrantResult | null {
  if (!isPermissionTemplate(name))
    return null
  return applyPermissionTemplate(taskId, name)
}

/**
 * Copy effective (granted, non-expired) permissions from a parent task to a
 * child. Used when an agent spawns a subtask via Task/Agent tool — the child
 * inherits the parent's grant set unless its own `permissions[]` is provided.
 */
export function inheritParentPermissions(
  childTaskId: string,
  parentTaskId: string,
): BulkGrantResult {
  const parentPerms = listTaskPermissions(parentTaskId)
  const entries: BulkGrantEntry[] = parentPerms
    .filter(p => p.granted && (p.expiresAt === null || p.expiresAt > new Date().toISOString()))
    .map(p => ({ tool: p.tool, pattern: p.pattern, expiresAt: p.expiresAt }))
  return bulkGrantPermissions(childTaskId, entries, {
    decidedBy: 'auto',
    source: `inherited_from:${parentTaskId}`,
  })
}
