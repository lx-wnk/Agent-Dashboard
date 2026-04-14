import { execFile } from 'node:child_process'
import { existsSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { basename, dirname, join } from 'node:path'
import process from 'node:process'
import { promisify } from 'node:util'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { createWorktree, currentBranch, isGitRepo, removeWorktree, resolveWorktreeRoot } from './worktreeManager.js'

const execFileAsync = promisify(execFile)

let tmpDir: string
let repoDir: string

async function initRepo(path: string) {
  await execFileAsync('git', ['init', '-b', 'main', path])
  await execFileAsync('git', ['-C', path, 'config', 'user.email', 'test@example.com'])
  await execFileAsync('git', ['-C', path, 'config', 'user.name', 'Test'])
  writeFileSync(join(path, 'README.md'), 'hello')
  await execFileAsync('git', ['-C', path, 'add', '.'])
  await execFileAsync('git', ['-C', path, 'commit', '-m', 'init'])
}

beforeEach(async () => {
  tmpDir = mkdtempSync(join(tmpdir(), 'worktree-mgr-test-'))
  repoDir = join(tmpDir, 'repo')
  await initRepo(repoDir)
})

afterEach(() => {
  rmSync(tmpDir, { recursive: true, force: true })
})

describe('isGitRepo', () => {
  it('returns true for an initialized repo', async () => {
    expect(await isGitRepo(repoDir)).toBe(true)
  })

  it('returns false for a non-git directory', async () => {
    const nonGit = join(tmpDir, 'not-a-repo')
    const { mkdirSync } = await import('node:fs')
    mkdirSync(nonGit)
    expect(await isGitRepo(nonGit)).toBe(false)
  })

  it('returns false for a non-existent path', async () => {
    expect(await isGitRepo(join(tmpDir, 'nope'))).toBe(false)
  })
})

describe('currentBranch', () => {
  it('returns the current branch for a fresh repo', async () => {
    const branch = await currentBranch(repoDir)
    expect(branch).toBe('main')
  })

  it('returns null for a detached HEAD', async () => {
    const { stdout } = await execFileAsync('git', ['-C', repoDir, 'rev-parse', 'HEAD'])
    const sha = stdout.trim()
    await execFileAsync('git', ['-C', repoDir, 'checkout', '--detach', sha])
    const branch = await currentBranch(repoDir)
    expect(branch).toBeNull()
  })

  it('returns null for a non-git directory', async () => {
    expect(await currentBranch(tmpDir)).toBeNull()
  })
})

describe('removeWorktree', () => {
  it('is a no-op when the worktree path does not exist', async () => {
    await expect(removeWorktree(repoDir, join(tmpDir, 'missing'))).resolves.toBeUndefined()
  })
})

describe('resolveWorktreeRoot', () => {
  const originalEnv = process.env.DASHBOARD_WORKTREE_ROOT

  afterEach(() => {
    if (originalEnv === undefined)
      delete process.env.DASHBOARD_WORKTREE_ROOT
    else
      process.env.DASHBOARD_WORKTREE_ROOT = originalEnv
  })

  it('defaults to <repo>-worktrees next to the source repo', () => {
    const cwd = '/Users/me/code/my-repo'
    expect(resolveWorktreeRoot(cwd)).toBe('/Users/me/code/my-repo-worktrees')
  })

  it('honors DASHBOARD_WORKTREE_ROOT when set', () => {
    process.env.DASHBOARD_WORKTREE_ROOT = '/opt/worktrees'
    expect(resolveWorktreeRoot('/Users/me/code/my-repo')).toBe('/opt/worktrees')
  })

  it('ignores an empty DASHBOARD_WORKTREE_ROOT', () => {
    process.env.DASHBOARD_WORKTREE_ROOT = '   '
    expect(resolveWorktreeRoot('/a/b/c')).toBe('/a/b/c-worktrees')
  })
})

describe('createWorktree', () => {
  it('creates a worktree at <repo>-worktrees/<slug> by default', async () => {
    const path = await createWorktree({ cwd: repoDir, slug: 'feat-one' })
    const expectedRoot = join(dirname(repoDir), `${basename(repoDir)}-worktrees`)
    expect(path).toBe(join(expectedRoot, 'feat-one'))
    expect(existsSync(path)).toBe(true)
    // Must not land inside the old ~/.claude/ root.
    expect(path.includes('.claude/dashboard-worktrees')).toBe(false)
    // Cleanup: remove the worktree so the repoDir rm in afterEach works cleanly.
    await removeWorktree(repoDir, path, { force: true })
  })

  it('honors DASHBOARD_WORKTREE_ROOT when set', async () => {
    const customRoot = mkdtempSync(join(tmpdir(), 'custom-worktrees-'))
    const original = process.env.DASHBOARD_WORKTREE_ROOT
    process.env.DASHBOARD_WORKTREE_ROOT = customRoot
    try {
      const path = await createWorktree({ cwd: repoDir, slug: 'feat-two' })
      expect(path).toBe(join(customRoot, 'feat-two'))
      expect(existsSync(path)).toBe(true)
      await removeWorktree(repoDir, path, { force: true })
    }
    finally {
      if (original === undefined)
        delete process.env.DASHBOARD_WORKTREE_ROOT
      else
        process.env.DASHBOARD_WORKTREE_ROOT = original
      rmSync(customRoot, { recursive: true, force: true })
    }
  })
})
