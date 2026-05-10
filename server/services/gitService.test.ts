import { describe, expect, it } from 'bun:test'
import { getGitStatus, runGitAction } from './gitService.js'

const REPO_CWD = process.cwd()

describe('getGitStatus', () => {
  it('returns a valid GitStatus object for the repo', async () => {
    const status = await getGitStatus(REPO_CWD)

    expect(typeof status.branch).toBe('string')
    expect(status.branch.length).toBeGreaterThan(0)

    expect(typeof status.aheadCount).toBe('number')
    expect(typeof status.behindCount).toBe('number')
    expect(status.aheadCount).toBeGreaterThanOrEqual(0)
    expect(status.behindCount).toBeGreaterThanOrEqual(0)

    expect(Array.isArray(status.staged)).toBe(true)
    expect(Array.isArray(status.unstaged)).toBe(true)
    expect(Array.isArray(status.untracked)).toBe(true)

    // The repo has at least one commit
    expect(status.lastCommit).not.toBeNull()
    if (status.lastCommit) {
      expect(typeof status.lastCommit.hash).toBe('string')
      expect(status.lastCommit.hash.length).toBe(40)
      expect(typeof status.lastCommit.shortHash).toBe('string')
      expect(typeof status.lastCommit.message).toBe('string')
      expect(typeof status.lastCommit.author).toBe('string')
      expect(typeof status.lastCommit.date).toBe('string')
    }
  })

  it('returns defaults for a non-git directory', async () => {
    const status = await getGitStatus('/tmp')

    expect(status.branch).toBe('unknown')
    expect(status.aheadCount).toBe(0)
    expect(status.behindCount).toBe(0)
    expect(status.staged).toEqual([])
    expect(status.unstaged).toEqual([])
    expect(status.untracked).toEqual([])
    expect(status.lastCommit).toBeNull()
    expect(status.remoteUrl).toBeNull()
  })
})

describe('runGitAction', () => {
  it('fetch returns a string without throwing', async () => {
    const output = await runGitAction(REPO_CWD, 'fetch')
    expect(typeof output).toBe('string')
  })
})
