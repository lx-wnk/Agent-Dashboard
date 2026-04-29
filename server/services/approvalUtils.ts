import { readFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
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

interface ParsedPermission {
  tool: string
  pattern: string | null
}

// Matches `## Pipeline Permissions` (any level-2 heading, case-insensitive).
const PIPELINE_PERMISSIONS_HEADING_RE = /^##\s+pipeline\s+permissions\s*$/im
// Matches the first fenced code block (```json or ```) after a heading,
// stopping at the next `##` heading.
const FENCED_BLOCK_RE = /```(?:json)?\s*\n([\s\S]*?)\n```/i
// Matches `Tool(pattern)` where pattern may contain anything except an unbalanced paren.
const TOOL_PATTERN_RE = /^([A-Za-z][\w]*)\(([\s\S]*)\)\s*$/

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
  const nextHeadingIdx = afterHeading.search(/\n##\s+/)
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
