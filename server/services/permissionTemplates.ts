/**
 * Predefined permission sets for common task profiles. MCP `create_task` and
 * `manage_task` accept `template: <name>` as shorthand for an explicit
 * permissions array.
 *
 * Templates are the union of:
 *   1. AGENTS.md "Pipeline Permissions" defaults (broad project baseline)
 *   2. Task-shape-specific tools (e.g. test-only excludes Write outside test dirs)
 *
 * All entries pass through validatePermissionEntry at apply time, so dangerous
 * patterns are still blocked even when introduced via a template.
 */

export interface PermissionTemplateEntry {
  tool: string
  pattern?: string | null
}

const FEATURE_IMPLEMENTATION: PermissionTemplateEntry[] = [
  { tool: 'Read' },
  { tool: 'Write' },
  { tool: 'Edit' },
  { tool: 'MultiEdit' },
  { tool: 'Glob' },
  { tool: 'Grep' },
  { tool: 'LS' },
  { tool: 'Bash' },
  { tool: 'WebFetch' },
  { tool: 'TodoRead' },
  { tool: 'TodoWrite' },
]

const RESEARCH_ONLY: PermissionTemplateEntry[] = [
  { tool: 'Read' },
  { tool: 'Glob' },
  { tool: 'Grep' },
  { tool: 'LS' },
  { tool: 'WebFetch' },
  { tool: 'WebSearch' },
  { tool: 'TodoRead' },
  { tool: 'TodoWrite' },
]

const TEST_ONLY: PermissionTemplateEntry[] = [
  { tool: 'Read' },
  { tool: 'Glob' },
  { tool: 'Grep' },
  { tool: 'LS' },
  { tool: 'Bash', pattern: 'pnpm test*' },
  { tool: 'Bash', pattern: 'pnpm lint*' },
  { tool: 'Bash', pattern: 'pnpm typecheck*' },
  { tool: 'Bash', pattern: 'bun test*' },
  { tool: 'TodoRead' },
  { tool: 'TodoWrite' },
]

const REVIEW_ONLY: PermissionTemplateEntry[] = [
  { tool: 'Read' },
  { tool: 'Glob' },
  { tool: 'Grep' },
  { tool: 'LS' },
  { tool: 'Bash', pattern: 'git log*' },
  { tool: 'Bash', pattern: 'git diff*' },
  { tool: 'Bash', pattern: 'git show*' },
  { tool: 'Bash', pattern: 'git status*' },
  { tool: 'TodoRead' },
  { tool: 'TodoWrite' },
]

export const PERMISSION_TEMPLATES = {
  feature_implementation: FEATURE_IMPLEMENTATION,
  research_only: RESEARCH_ONLY,
  test_only: TEST_ONLY,
  review_only: REVIEW_ONLY,
} as const

export type PermissionTemplateName = keyof typeof PERMISSION_TEMPLATES

export function isPermissionTemplate(name: string): name is PermissionTemplateName {
  return name in PERMISSION_TEMPLATES
}

export function resolveTemplate(name: PermissionTemplateName): PermissionTemplateEntry[] {
  return [...PERMISSION_TEMPLATES[name]]
}

export function listTemplateNames(): PermissionTemplateName[] {
  return Object.keys(PERMISSION_TEMPLATES) as PermissionTemplateName[]
}
