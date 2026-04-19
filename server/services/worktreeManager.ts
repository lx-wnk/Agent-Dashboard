import { execFile } from 'node:child_process'
import { existsSync } from 'node:fs'
import { mkdir } from 'node:fs/promises'
import { homedir } from 'node:os'
import { basename, dirname, join } from 'node:path'
import process from 'node:process'
import { promisify } from 'node:util'
import { SLUG_RE as SAFE_SLUG_RE } from '../constants.js'

const execFileAsync = promisify(execFile)

/**
 * Legacy worktree root from before the per-repo default. Still searched as
 * a fallback when adopting pre-existing worktrees so older tasks keep
 * working after the move.
 */
export const LEGACY_WORKTREE_ROOT = join(homedir(), '.claude', 'dashboard-worktrees')

/**
 * Resolve the worktree root for a given source repo.
 *
 * Precedence:
 * 1. `DASHBOARD_WORKTREE_ROOT` env var — absolute path, applied to all repos.
 * 2. Sibling default `<repo-parent>/<repo-name>-worktrees` — next to the
 *    source repo, out of `.claude/` so Claude CLI does not gate edits
 *    behind "sensitive file" prompts that detached agents cannot answer.
 */
export function resolveWorktreeRoot(cwd: string): string {
  const envRoot = process.env.DASHBOARD_WORKTREE_ROOT
  if (envRoot && envRoot.trim().length > 0)
    return envRoot.trim()
  return join(dirname(cwd), `${basename(cwd)}-worktrees`)
}


export interface WorktreeOptions {
  cwd: string
  slug: string
  branch?: string | null
}

/**
 * Create a git worktree for a task under the resolved worktree root.
 * Returns the absolute worktree path. Adopts an already-existing worktree
 * at the target path (or at the legacy `~/.claude/dashboard-worktrees/`
 * location) if it is registered with git, so older tasks survive the move.
 */
export async function createWorktree(opts: WorktreeOptions): Promise<string> {
  if (!SAFE_SLUG_RE.test(opts.slug))
    throw new Error(`Invalid worktree slug: ${opts.slug}`)
  if (!(await isGitRepo(opts.cwd)))
    throw new Error(`${opts.cwd} is not a git repository — cannot create worktree`)

  const root = resolveWorktreeRoot(opts.cwd)
  await mkdir(root, { recursive: true })
  const path = join(root, opts.slug)

  // Legacy adoption: a task that ran under the old ~/.claude/dashboard-worktrees
  // root should keep running there instead of forcing a second worktree
  // for the same slug. Only applies when the new path doesn't exist yet.
  if (!existsSync(path)) {
    const legacyPath = join(LEGACY_WORKTREE_ROOT, opts.slug)
    if (existsSync(legacyPath) && await isRegisteredWorktree(opts.cwd, legacyPath))
      return legacyPath
  }

  // Adoption path: if a worktree already exists at this path AND is
  // registered in git worktree list, adopt it (orphan recovery after crash).
  if (existsSync(path)) {
    const isRegistered = await isRegisteredWorktree(opts.cwd, path)
    if (isRegistered)
      return path
    throw new Error(`Path exists but is not a registered git worktree: ${path}`)
  }

  const args = ['-C', opts.cwd, 'worktree', 'add', path]
  if (opts.branch)
    args.push(opts.branch)
  await execFileAsync('git', args)
  return path
}

/**
 * Check whether a given path is tracked in `git worktree list` for the
 * given source repo.
 */
async function isRegisteredWorktree(cwd: string, targetPath: string): Promise<boolean> {
  try {
    const { stdout } = await execFileAsync('git', ['-C', cwd, 'worktree', 'list', '--porcelain'])
    const lines = stdout.split('\n')
    for (const line of lines) {
      if (line.startsWith('worktree ') && line.slice('worktree '.length).trim() === targetPath)
        return true
    }
  }
  catch (e) {
    console.warn('[worktreeManager] git worktree list failed', e)
  }
  return false
}

/**
 * Remove a previously created worktree. By default refuses to delete a
 * worktree with uncommitted changes. Pass `{ force: true }` to override —
 * the caller must acknowledge that user changes will be lost.
 */
export async function removeWorktree(
  cwd: string,
  worktreePath: string,
  opts: { force?: boolean } = {},
): Promise<void> {
  if (!existsSync(worktreePath))
    return

  if (!opts.force && await hasUncommittedChanges(worktreePath)) {
    throw new Error(
      `Refusing to remove worktree with uncommitted changes: ${worktreePath}. Pass { force: true } to override.`,
    )
  }

  const args = ['-C', cwd, 'worktree', 'remove']
  if (opts.force)
    args.push('--force')
  args.push(worktreePath)
  await execFileAsync('git', args)
}

async function hasUncommittedChanges(worktreePath: string): Promise<boolean> {
  try {
    const { stdout } = await execFileAsync('git', ['-C', worktreePath, 'status', '--porcelain'])
    return stdout.trim().length > 0
  }
  catch (e) {
    // If we can't determine state, assume dirty to err on the safe side.
    console.warn('[worktreeManager] git status failed, assuming dirty', e)
    return true
  }
}

/**
 * Check whether a directory is inside a git work tree.
 */
export async function isGitRepo(cwd: string): Promise<boolean> {
  try {
    const { stdout } = await execFileAsync('git', ['-C', cwd, 'rev-parse', '--is-inside-work-tree'])
    return stdout.trim() === 'true'
  }
  catch (e) {
    console.warn('[worktreeManager] git rev-parse (isGitRepo) failed', e)
    return false
  }
}

/**
 * Get the current branch for a cwd. Returns null on detached HEAD or error.
 */
export async function currentBranch(cwd: string): Promise<string | null> {
  try {
    const { stdout } = await execFileAsync('git', ['-C', cwd, 'rev-parse', '--abbrev-ref', 'HEAD'])
    const branch = stdout.trim()
    return branch === 'HEAD' ? null : branch
  }
  catch (e) {
    console.warn('[worktreeManager] git rev-parse (currentBranch) failed', e)
    return null
  }
}
