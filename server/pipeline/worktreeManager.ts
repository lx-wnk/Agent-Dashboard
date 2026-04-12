import { execFile } from 'node:child_process'
import { existsSync } from 'node:fs'
import { mkdir } from 'node:fs/promises'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

export const WORKTREE_ROOT = join(homedir(), '.claude', 'dashboard-worktrees')

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
  if (!(await isGitRepo(opts.cwd)))
    throw new Error(`${opts.cwd} is not a git repository — cannot create worktree`)

  await mkdir(WORKTREE_ROOT, { recursive: true })
  const path = join(WORKTREE_ROOT, opts.slug)

  if (existsSync(path))
    throw new Error(`Worktree path already exists: ${path}`)

  const args = ['-C', opts.cwd, 'worktree', 'add', path]
  if (opts.branch)
    args.push(opts.branch)
  await execFileAsync('git', args)
  return path
}

/**
 * Remove a previously created worktree. Uses --force to delete even if
 * there are uncommitted changes (caller is responsible for preserving them).
 */
export async function removeWorktree(cwd: string, worktreePath: string): Promise<void> {
  if (!existsSync(worktreePath))
    return
  await execFileAsync('git', ['-C', cwd, 'worktree', 'remove', '--force', worktreePath])
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
