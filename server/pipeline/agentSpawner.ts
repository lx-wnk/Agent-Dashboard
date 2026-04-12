import type { ChildProcess } from 'node:child_process'
import type { PipelineTask, StageRun, TaskPermission } from '../../src/types.js'
import { spawn } from 'node:child_process'
import { mkdirSync, realpathSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import process from 'node:process'

const CHANNEL_DIR = join(dirname(new URL(import.meta.url).pathname), '..', '..', 'channel')
const CHANNEL_SCRIPT = join(CHANNEL_DIR, 'dashboard-channel.ts')
const CHANNEL_TSX = join(CHANNEL_DIR, 'node_modules', '.bin', 'tsx')

export interface SpawnAgentOptions {
  task: PipelineTask
  stageRun: StageRun
  prompt: string
  systemPrompt?: string
  model?: string
  permissions: TaskPermission[]
  enableChannel?: boolean
  resumeSessionId?: string | null
}

export interface SpawnResult {
  child: ChildProcess
  pid: number
  cwd: string
  settingsPath: string | null
}

/**
 * Write a .claude/settings.json into the worktree (or cwd) with the
 * pre-approved tool permissions converted to Claude Code allowlist format.
 */
function writeSettingsFile(cwd: string, permissions: TaskPermission[]): string | null {
  const allow: string[] = []
  for (const p of permissions) {
    if (!p.granted)
      continue
    allow.push(p.pattern ? `${p.tool}(${p.pattern})` : p.tool)
  }
  if (allow.length === 0)
    return null

  const settingsDir = join(cwd, '.claude')
  mkdirSync(settingsDir, { recursive: true })
  const settingsPath = join(settingsDir, 'settings.json')
  const settings = { permissions: { allow } }
  writeFileSync(settingsPath, JSON.stringify(settings, null, 2))
  return settingsPath
}

/**
 * Spawn a detached Claude agent process for the given stage.
 * The orchestrator records the returned PID on the stage_run and later
 * watches for channel replies that carry session metadata.
 */
export function spawnStageAgent(opts: SpawnAgentOptions): SpawnResult {
  const cwd = opts.task.worktreePath || opts.task.cwd
  const settingsPath = writeSettingsFile(cwd, opts.permissions)

  const args: string[] = []
  if (opts.resumeSessionId)
    args.push('--resume', opts.resumeSessionId)
  args.push('-p', opts.prompt)
  if (opts.model)
    args.push('--model', opts.model)
  if (opts.systemPrompt)
    args.push('--system-prompt', opts.systemPrompt.slice(0, 10000))

  if (opts.enableChannel !== false) {
    const mcpConfig = JSON.stringify({
      mcpServers: {
        'dashboard-channel': {
          command: realpathSync(CHANNEL_TSX),
          args: [realpathSync(CHANNEL_SCRIPT)],
        },
      },
    })
    args.push('--mcp-config', mcpConfig)
  }

  const child = spawn('claude', args, {
    cwd,
    detached: true,
    stdio: ['ignore', 'ignore', 'pipe'],
    env: {
      ...process.env,
      DASHBOARD_STAGE_RUN_ID: opts.stageRun.id,
      DASHBOARD_TASK_ID: opts.task.id,
    },
  })

  // CRITICAL: drain stderr so the OS pipe buffer (typically 64 KB) cannot
  // fill and block a long-running detached agent. We don't store it here —
  // the dashboard's existing spawnStore path is responsible for buffering.
  child.stderr?.on('data', () => { /* drain */ })
  child.stderr?.on('error', () => { /* child exit may trigger EPIPE */ })

  child.unref()

  return {
    child,
    pid: child.pid ?? 0,
    cwd,
    settingsPath,
  }
}
