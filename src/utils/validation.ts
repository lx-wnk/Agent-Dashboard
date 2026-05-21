export const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
export const SLUG_PATTERN_MESSAGE = 'slug must match [a-z0-9][a-z0-9-]{0,63}'

export function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

/** Maximum characters allowed in a task description. */
export const MAX_DESCRIPTION_CHARS = 10_000

/**
 * Returns true when `p` is a safe absolute path:
 * - Must begin with "/"
 * - Must not contain ".." segments (traversal guard)
 */
export function isAbsolutePath(p: string): boolean {
  if (!p.startsWith('/'))
    return false
  // Reject any ".." path segment
  return !p.split('/').some(segment => segment === '..')
}

/**
 * Allowed bare command names for spawner commands.
 * SSOT — backend mirrors the same rule.
 */
const ALLOWED_SPAWNER_BARE_NAMES = new Set(['claude', 'claude-code', 'npx'])

/**
 * Returns true when `cmd` is an acceptable spawner command:
 * - Bare names: claude, claude-code, npx
 * - Absolute paths that do NOT reside under /tmp or /var/tmp
 */
export function isAllowedSpawnerCommand(cmd: string): boolean {
  if (!cmd || !cmd.trim())
    return false
  const trimmed = cmd.trim()
  if (ALLOWED_SPAWNER_BARE_NAMES.has(trimmed))
    return true
  if (trimmed.startsWith('/')) {
    // Absolute path — disallow /tmp and /var/tmp trees
    if (trimmed.startsWith('/tmp/') || trimmed === '/tmp')
      return false
    if (trimmed.startsWith('/var/tmp/') || trimmed === '/var/tmp')
      return false
    return true
  }
  return false
}
