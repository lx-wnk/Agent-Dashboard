import type { PipelineTask, StageRun, TaskPermission } from '../../src/types.js'
import type { SpawnAgentOptions } from './agentSpawner.js'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import {
  buildAllowList,
  buildSpawnArgs,
  buildSpawnEnv,
  shouldCleanSettingsFile,
} from './agentSpawner.js'

function makeTask(overrides: Partial<PipelineTask> = {}): PipelineTask {
  return {
    id: 't-1',
    slug: 'fix-login',
    title: 'Fix login bug',
    description: null,
    cwd: '/tmp/project',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'implementation',
    parentTaskId: null,
    maxIterations: 20,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 1800,
    createdAt: '2026-04-12T10:00:00Z',
    updatedAt: '2026-04-12T10:00:00Z',
    metadata: null,
    silverBullet: false,
    priority: 'medium',
    userId: null,
    ...overrides,
  }
}

function makeStageRun(): StageRun {
  return {
    id: 'sr-1',
    taskId: 't-1',
    stage: 'implementation',
    sessionId: null,
    sessionName: 'fix-login-implementation-iter-0',
    pid: null,
    status: 'pending',
    startedAt: null,
    endedAt: null,
    iteration: 0,
    output: null,
    tokensUsed: 0,
    costCents: 0,
  }
}

function permission(overrides: Partial<TaskPermission>): TaskPermission {
  return {
    id: 'p-x',
    taskId: 't-1',
    tool: 'Bash',
    pattern: null,
    granted: true,
    preApproved: true,
    requestedAt: '2026-04-12T10:00:00Z',
    decidedAt: '2026-04-12T10:00:00Z',
    decidedBy: 'user',
    expiresAt: null,
    ...overrides,
  }
}

const baseOpts: SpawnAgentOptions = {
  task: makeTask(),
  stageRun: makeStageRun(),
  prompt: 'implement the thing',
  permissions: [],
}

describe('buildAllowList', () => {
  it('emits Tool(pattern) for permissions with patterns', () => {
    const list = buildAllowList([
      permission({ tool: 'Bash', pattern: 'npm *' }),
      permission({ tool: 'Bash', pattern: 'git status' }),
    ], false)
    expect(list).toEqual(['Bash(npm *)', 'Bash(git status)'])
  })

  it('emits bare Tool when pattern is null', () => {
    const list = buildAllowList([
      permission({ tool: 'Read', pattern: null }),
      permission({ tool: 'Grep', pattern: null }),
    ], false)
    expect(list).toEqual(['Read', 'Grep'])
  })

  it('filters out denied permissions', () => {
    const list = buildAllowList([
      permission({ tool: 'Read', granted: true }),
      permission({ tool: 'WebFetch', granted: false }),
    ], false)
    expect(list).toEqual(['Read'])
    expect(list).not.toContain('WebFetch')
  })

  it('returns an empty array when all permissions are denied', () => {
    const list = buildAllowList([
      permission({ tool: 'Bash', granted: false }),
      permission({ tool: 'WebFetch', granted: false }),
    ], false)
    expect(list).toEqual([])
  })

  it('blocks git push patterns even when granted', () => {
    const list = buildAllowList([
      permission({ tool: 'Bash', pattern: 'git push *' }),
      permission({ tool: 'Bash', pattern: 'git push origin main' }),
      permission({ tool: 'Bash', pattern: 'git commit -m *' }),
    ], false)
    expect(list).toEqual(['Bash(git commit -m *)'])
    expect(list.some(e => e.includes('git push'))).toBe(false)
  })

  it('prepends channel tools when enableChannel is true', () => {
    const list = buildAllowList([permission({ tool: 'Read', pattern: null })], true)
    expect(list[0]).toBe('mcp__dashboard-channel__dashboard_reply')
    expect(list[1]).toBe('mcp__dashboard-channel__request_permission')
    expect(list).toContain('Read')
  })
})

describe('buildSpawnArgs', () => {
  it('always includes the -p prompt arg', () => {
    const args = buildSpawnArgs({ ...baseOpts, prompt: 'do x' })
    expect(args).toContain('-p')
    expect(args).toContain('do x')
  })

  it('includes --resume when resumeSessionId is set', () => {
    const args = buildSpawnArgs({ ...baseOpts, resumeSessionId: 'sess-abc' })
    const idx = args.indexOf('--resume')
    expect(idx).toBeGreaterThanOrEqual(0)
    expect(args[idx + 1]).toBe('sess-abc')
  })

  it('omits --resume when resumeSessionId is not set', () => {
    const args = buildSpawnArgs(baseOpts)
    expect(args).not.toContain('--resume')
  })

  it('always forces --permission-mode default to prevent plan-mode drift', () => {
    const args = buildSpawnArgs(baseOpts)
    const idx = args.indexOf('--permission-mode')
    expect(idx).toBeGreaterThanOrEqual(0)
    expect(args[idx + 1]).toBe('default')
  })

  it('includes --model when set', () => {
    const args = buildSpawnArgs({ ...baseOpts, model: 'claude-opus-4-6' })
    const idx = args.indexOf('--model')
    expect(args[idx + 1]).toBe('claude-opus-4-6')
  })

  it('includes --system-prompt when set and caps at 10000 chars', () => {
    const longPrompt = 'x'.repeat(12000)
    const args = buildSpawnArgs({ ...baseOpts, systemPrompt: longPrompt })
    const idx = args.indexOf('--system-prompt')
    expect(args[idx + 1]?.length).toBe(10000)
  })

  it('orders args as --resume, -p, --model, --system-prompt', () => {
    const args = buildSpawnArgs({
      ...baseOpts,
      resumeSessionId: 'sess',
      model: 'claude-haiku-4-5',
      systemPrompt: 'you are an agent',
    })
    // --resume comes first, then -p, then --model, then --system-prompt
    expect(args.indexOf('--resume')).toBeLessThan(args.indexOf('-p'))
    expect(args.indexOf('-p')).toBeLessThan(args.indexOf('--model'))
    expect(args.indexOf('--model')).toBeLessThan(args.indexOf('--system-prompt'))
  })
})

describe('buildSpawnEnv', () => {
  let savedMcpToken: string | undefined
  let savedMcpUrl: string | undefined

  beforeEach(() => {
    savedMcpToken = process.env.DASHBOARD_MCP_TOKEN
    savedMcpUrl = process.env.DASHBOARD_MCP_URL
    delete process.env.DASHBOARD_MCP_TOKEN
    delete process.env.DASHBOARD_MCP_URL
  })

  afterEach(() => {
    if (savedMcpToken !== undefined)
      process.env.DASHBOARD_MCP_TOKEN = savedMcpToken
    else delete process.env.DASHBOARD_MCP_TOKEN
    if (savedMcpUrl !== undefined)
      process.env.DASHBOARD_MCP_URL = savedMcpUrl
    else delete process.env.DASHBOARD_MCP_URL
  })

  it('injects DASHBOARD_STAGE_RUN_ID and DASHBOARD_TASK_ID', () => {
    const env = buildSpawnEnv(baseOpts)
    expect(env.DASHBOARD_STAGE_RUN_ID).toBe('sr-1')
    expect(env.DASHBOARD_TASK_ID).toBe('t-1')
  })

  it('preserves the parent process env', () => {
    const env = buildSpawnEnv(baseOpts)
    // PATH is present on every sane system
    expect(env.PATH).toBeDefined()
  })

  it('injects DASHBOARD_MCP_TOKEN when mcpToken is provided', () => {
    const env = buildSpawnEnv({ ...baseOpts, mcpToken: 'mcp_abc123' })
    expect(env.DASHBOARD_MCP_TOKEN).toBe('mcp_abc123')
  })

  it('omits DASHBOARD_MCP_TOKEN when mcpToken is absent', () => {
    const env = buildSpawnEnv(baseOpts)
    expect(env.DASHBOARD_MCP_TOKEN).toBeUndefined()
  })

  it('injects DASHBOARD_MCP_URL when mcpUrl is provided', () => {
    const env = buildSpawnEnv({ ...baseOpts, mcpUrl: 'http://127.0.0.1:13120/api/mcp' })
    expect(env.DASHBOARD_MCP_URL).toBe('http://127.0.0.1:13120/api/mcp')
  })

  it('omits DASHBOARD_MCP_URL when mcpUrl is absent', () => {
    const env = buildSpawnEnv(baseOpts)
    expect(env.DASHBOARD_MCP_URL).toBeUndefined()
  })
})

describe('shouldCleanSettingsFile', () => {
  let tmp: string
  let settingsPath: string

  beforeEach(() => {
    tmp = mkdtempSync(join(tmpdir(), 'settings-test-'))
    mkdirSync(join(tmp, '.claude'), { recursive: true })
    settingsPath = join(tmp, '.claude', 'settings.json')
  })

  afterEach(() => {
    rmSync(tmp, { recursive: true, force: true })
  })

  it('returns false when the file does not exist', () => {
    expect(shouldCleanSettingsFile(settingsPath)).toBe(false)
  })

  it('returns true for files stamped with _dashboardManaged: true', () => {
    writeFileSync(settingsPath, JSON.stringify({ _dashboardManaged: true, permissions: { allow: [] } }))
    expect(shouldCleanSettingsFile(settingsPath)).toBe(true)
  })

  it('returns false for user-authored files without the stamp', () => {
    writeFileSync(settingsPath, JSON.stringify({ permissions: { allow: ['Read'] } }))
    expect(shouldCleanSettingsFile(settingsPath)).toBe(false)
  })

  it('returns false when the file contains invalid JSON', () => {
    writeFileSync(settingsPath, '{ not json')
    expect(shouldCleanSettingsFile(settingsPath)).toBe(false)
  })

  it('returns false when _dashboardManaged is anything other than true', () => {
    writeFileSync(settingsPath, JSON.stringify({ _dashboardManaged: false }))
    expect(shouldCleanSettingsFile(settingsPath)).toBe(false)
    writeFileSync(settingsPath, JSON.stringify({ _dashboardManaged: 'yes' }))
    expect(shouldCleanSettingsFile(settingsPath)).toBe(false)
  })

  it('does not consider a sibling user file with the stamp inside permissions', () => {
    // Stamp must live at the top level — nested fields don't count.
    writeFileSync(
      settingsPath,
      JSON.stringify({ permissions: { allow: [], _dashboardManaged: true } }),
    )
    expect(shouldCleanSettingsFile(settingsPath)).toBe(false)
  })

  it('leaves the readFileSync semantics deterministic across calls', () => {
    writeFileSync(settingsPath, JSON.stringify({ _dashboardManaged: true }))
    expect(readFileSync(settingsPath, 'utf8')).toContain('_dashboardManaged')
    expect(shouldCleanSettingsFile(settingsPath)).toBe(true)
  })
})
