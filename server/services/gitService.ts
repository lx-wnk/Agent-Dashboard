import type { GitStatus, GitStatusLastCommit } from '../../src/types.js'
import { execFile as execFileCb } from 'node:child_process'
import { promisify } from 'node:util'

const execFile = promisify(execFileCb)

const AHEAD_RE = /\[ahead (\d+)/
const BEHIND_RE = /behind (\d+)/
const BRANCH_RE = /^## ([^.]+)/
const NO_BRANCH_RE = /^## HEAD \(no branch\)/

function parseAheadBehind(line: string): { ahead: number, behind: number } {
  const aheadMatch = line.match(AHEAD_RE)
  const behindMatch = line.match(BEHIND_RE)
  return {
    ahead: aheadMatch ? Number.parseInt(aheadMatch[1], 10) : 0,
    behind: behindMatch ? Number.parseInt(behindMatch[1], 10) : 0,
  }
}

function parseBranch(line: string): string {
  if (NO_BRANCH_RE.test(line))
    return 'HEAD'
  const match = line.match(BRANCH_RE)
  return match ? match[1] : 'unknown'
}

export async function getGitStatus(cwd: string): Promise<GitStatus> {
  const defaultStatus: GitStatus = {
    branch: 'unknown',
    aheadCount: 0,
    behindCount: 0,
    staged: [],
    unstaged: [],
    untracked: [],
    lastCommit: null,
    remoteUrl: null,
  }

  let porcelainOutput = ''
  try {
    const { stdout } = await execFile('git', ['status', '--porcelain=v1', '-b'], { cwd })
    porcelainOutput = stdout
  }
  catch {
    // Not a git repo or git not available — return defaults
    return defaultStatus
  }

  const lines = porcelainOutput.split('\n')
  const headerLine = lines[0] ?? ''
  const branch = parseBranch(headerLine)
  const { ahead, behind } = parseAheadBehind(headerLine)

  const staged: string[] = []
  const unstaged: string[] = []
  const untracked: string[] = []

  for (const line of lines.slice(1)) {
    if (line.length < 2)
      continue
    const xy = line.slice(0, 2)
    const file = line.slice(3)
    if (xy === '??') {
      untracked.push(file)
      continue
    }
    const x = xy[0] // index (staged)
    const y = xy[1] // working tree (unstaged)
    if (x !== ' ' && x !== '?')
      staged.push(file)
    if (y !== ' ' && y !== '?')
      unstaged.push(file)
  }

  // Get last commit info: hash, shortHash, subject, author, date (5 lines)
  let lastCommit: GitStatusLastCommit | null = null
  try {
    const { stdout: logOutput } = await execFile(
      'git',
      ['log', '-1', '--format=%H%n%h%n%s%n%an%n%ai'],
      { cwd },
    )
    const parts = logOutput.trim().split('\n')
    if (parts.length >= 5) {
      lastCommit = {
        hash: parts[0],
        shortHash: parts[1],
        message: parts[2],
        author: parts[3],
        date: parts[4],
      }
    }
  }
  catch {
    // No commits yet or git error — leave lastCommit as null
  }

  // Get remote URL
  let remoteUrl: string | null = null
  try {
    const { stdout: remoteOutput } = await execFile('git', ['remote', 'get-url', 'origin'], { cwd })
    remoteUrl = remoteOutput.trim() || null
  }
  catch {
    // No remote named "origin"
  }

  return {
    branch,
    aheadCount: ahead,
    behindCount: behind,
    staged,
    unstaged,
    untracked,
    lastCommit,
    remoteUrl,
  }
}

export async function runGitAction(cwd: string, action: 'fetch' | 'pull'): Promise<string> {
  if (action === 'fetch') {
    const { stdout } = await execFile('git', ['fetch', '--prune'], { cwd })
    return stdout
  }
  // action === 'pull'
  const { stdout } = await execFile('git', ['pull', '--ff-only'], { cwd })
  return stdout
}
