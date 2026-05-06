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

// Safe baseline auto-merged into every konzept stage's permissions. Covers the
// common file-op + Bash patterns the implementation agent almost always needs
// (test, lint, typecheck, basic git operations) so the konzept stage cannot
// under-enumerate itself into a permission re-request loop. Excludes
// `git push*`, `curl*`, `wget*` and similar — those still require explicit
// per-task grant.
const KONZEPT_BASELINE: PermissionTemplateEntry[] = [
  { tool: 'Read' },
  { tool: 'Write' },
  { tool: 'Edit' },
  { tool: 'MultiEdit' },
  { tool: 'Glob' },
  { tool: 'Grep' },
  { tool: 'LS' },
  { tool: 'TodoRead' },
  { tool: 'TodoWrite' },
  { tool: 'Bash', pattern: 'pnpm test*' },
  { tool: 'Bash', pattern: 'pnpm typecheck*' },
  { tool: 'Bash', pattern: 'pnpm lint*' },
  { tool: 'Bash', pattern: 'pnpm build*' },
  { tool: 'Bash', pattern: 'pnpm install*' },
  { tool: 'Bash', pattern: 'git status*' },
  { tool: 'Bash', pattern: 'git diff*' },
  { tool: 'Bash', pattern: 'git log*' },
  { tool: 'Bash', pattern: 'git show*' },
  { tool: 'Bash', pattern: 'git add*' },
  { tool: 'Bash', pattern: 'git commit*' },
  { tool: 'Bash', pattern: 'git checkout*' },
  { tool: 'Bash', pattern: 'git branch*' },
  { tool: 'Bash', pattern: 'git stash*' },
  { tool: 'Bash', pattern: 'git restore*' },
  { tool: 'Bash', pattern: 'git switch*' },
]

export const PERMISSION_TEMPLATES = {
  feature_implementation: FEATURE_IMPLEMENTATION,
  research_only: RESEARCH_ONLY,
  test_only: TEST_ONLY,
  review_only: REVIEW_ONLY,
  konzept_baseline: KONZEPT_BASELINE,
} as const

export const DEFAULT_KONZEPT_BASELINE_TEMPLATE: PermissionTemplateName = 'konzept_baseline'

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
