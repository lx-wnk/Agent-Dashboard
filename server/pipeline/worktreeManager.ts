import { execFile } from 'node:child_process'
import { existsSync } from 'node:fs'
import { mkdir } from 'node:fs/promises'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

export const WORKTREE_ROOT = join(homedir(), '.claude', 'dashboard-worktrees')

const SAFE_SLUG_RE = /^[a-z0-9][a-z0-9-]{0,63}$/

export interface WorktreeOptions {
  cwd: string
  slug: string
  branch?: string | null
}

/**
 * Create a git worktree for a task under {WORKTREE_ROOT}/{slug}.
 * Falls back to the source cwd if the repo is not a git repository.
 * Returns the absolute worktree path.
 */
export async function createWorktree(opts: WorktreeOptions): Promise<string> {
  if (!SAFE_SLUG_RE.test(opts.slug))
    throw new Error(`Invalid worktree slug: ${opts.slug}`)
  if (!(await isGitRepo(opts.cwd)))
    throw new Error(`${opts.cwd} is not a git repository — cannot create worktree`)

  await mkdir(WORKTREE_ROOT, { recursive: true })
  const path = join(WORKTREE_ROOT, opts.slug)

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
  catch {
    /* ignore */
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
  catch {
    // If we can't determine state, assume dirty to err on the safe side.
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
  catch {
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
  catch {
    return null
  }
}
