import { execFile } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { currentBranch, isGitRepo, removeWorktree } from './worktreeManager.js'

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
