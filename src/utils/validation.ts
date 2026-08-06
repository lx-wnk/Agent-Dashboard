export const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/
export const SLUG_PATTERN_MESSAGE = 'slug must match [a-z0-9][a-z0-9-]{0,63}'

/** Character budget SLUG_RE allows: one leading char plus up to 63 more. */
const SLUG_MAX_CHARS = 64

export function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .slice(0, SLUG_MAX_CHARS)
    .replace(/^-+|-+$/g, '')
}

/**
 * Slug to display for `name`, given whether the user has taken the slug over.
 * `slugTouched` is set by the slug field's own input handler, so an edited slug
 * is never overwritten. Clearing that field unsets the flag and hands the slug
 * back to the name.
 */
export function slugFollowingName(name: string, currentSlug: string, slugTouched: boolean): string {
  return slugTouched ? currentSlug : slugify(name)
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
  return !p.split('/').includes('..')
}

/**
 * Allowed bare command names for spawner commands.
 * Keep in sync with services.DefaultAllowedCommands (server is authoritative).
 */
const ALLOWED_SPAWNER_BARE_NAMES = new Set(['claude', 'claude-code', 'npx'])

/**
 * Advisory client-side pre-check for a spawner command. The SERVER is
 * authoritative: services.ValidateSpawnerCommand resolves symlinks and requires
 * absolute paths to live under a trusted bin directory. The browser cannot
 * resolve realpaths, so this only catches obviously-bad bare names early and
 * lets any absolute path through — the server makes the final decision.
 * - Bare names: must be claude, claude-code, or npx.
 * - Absolute paths: accepted optimistically here; server enforces the trusted-dir rule.
 */
export function isAllowedSpawnerCommand(cmd: string): boolean {
  if (!cmd || !cmd.trim())
    return false
  const trimmed = cmd.trim()
  if (ALLOWED_SPAWNER_BARE_NAMES.has(trimmed))
    return true
  // Absolute paths can't be realpath-resolved client-side; defer to the server.
  return trimmed.startsWith('/')
}
