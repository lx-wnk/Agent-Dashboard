/**
 * Spawns a detached Claude CLI process for an agent-driven pipeline
 * stage. Called by the generic `createAgentStage` factory in
 * stageHandlers.ts — each real agent-stage handler invokes
 * `spawnStageAgent` and returns `{ kind: 'async_running', pid }` so
 * the orchestrator's driver loop can later finalize the stage via
 * completionDetector when the PID exits.
 *
 * Side effects: writes `.claude/settings.json` into the task's cwd or
 * worktree (containing pre-approved tool allow-list), injects the
 * dashboard-channel MCP config when enabled, and sets
 * `DASHBOARD_STAGE_RUN_ID` / `DASHBOARD_TASK_ID` env vars so the
 * channel's permission-request tool can post back to the right run.
 */
import type { ChildProcess } from 'node:child_process'
import type { PipelineTask, StageRun, TaskPermission } from '../../src/types.js'
import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import process from 'node:process'
import consola from 'consola'
import { buildDashboardChannelMcpConfig } from '../channelConfig.js'
import { SYSTEM_PROMPT_MAX_CHARS } from '../constants.js'

const GIT_PUSH_RE = /\bgit push\b/i

export interface SpawnAgentOptions {
  task: PipelineTask
  stageRun: StageRun
  prompt: string
  systemPrompt?: string
  model?: string
  permissions: TaskPermission[]
  enableChannel?: boolean
  resumeSessionId?: string | null
  /** Short-lived MCP token for this stage run (raw `mcp_<hex>` value). */
  mcpToken?: string
  /** MCP endpoint URL, e.g. "http://127.0.0.1:13120/api/mcp". */
  mcpUrl?: string
}

export interface SpawnResult {
  child: ChildProcess
  pid: number
  cwd: string
  settingsPath: string | null
  /**
   * Removes the `.claude/settings.json` file iff it was created by this
   * spawn (i.e. carries the `_dashboardManaged: true` stamp). Idempotent
   * and safe to call when no file was written. Callers should invoke this
   * after the stage run is finalized so a user-authored settings file is
   * never overwritten or deleted.
   */
  cleanup: () => void
}

// Channel MCP tools are always pre-approved when the dashboard channel is injected.
// Without these entries the spawned agent hits Claude Code's permission gate on first
// dashboard_reply call and stalls waiting for approval that never surfaces in the UI.
const CHANNEL_ALLOW = [
  'mcp__dashboard-channel__dashboard_reply',
  'mcp__dashboard-channel__request_permission',
]

/**
 * Convert TaskPermission rows into the Claude Code `permissions.allow`
 * array format. Denied permissions are filtered out. Pure function —
 * exported for testing.
 */
export function buildAllowList(permissions: TaskPermission[], enableChannel = true): string[] {
  const allow: string[] = enableChannel ? [...CHANNEL_ALLOW] : []
  for (const p of permissions) {
    if (!p.granted)
      continue
    // Block git push regardless of what was granted — stage agents may commit
    // but must never push; pushes must be triggered by the user.
    if (p.tool === 'Bash' && p.pattern && GIT_PUSH_RE.test(p.pattern))
      continue
    allow.push(p.pattern ? `${p.tool}(${p.pattern})` : p.tool)
  }
  return allow
}

/**
 * Build the argv for `claude` given the spawn options. Pure function —
 * exported for testing without mocking child_process.
 */
export function buildSpawnArgs(opts: SpawnAgentOptions): string[] {
  const args: string[] = []
  if (opts.resumeSessionId)
    args.push('--resume', opts.resumeSessionId)
  args.push('-p', opts.prompt)
  // Force exec mode. Pipeline spawns must never inherit plan-mode from
  // user settings / hooks / session state — ExitPlanMode requires
  // interactive confirmation which headless workers cannot supply, so a
  // drifted plan-mode run simply dies without writing its JSON block.
  args.push('--permission-mode', 'default')
  if (opts.model)
    args.push('--model', opts.model)
  if (opts.systemPrompt)
    args.push('--system-prompt', opts.systemPrompt.slice(0, SYSTEM_PROMPT_MAX_CHARS))
  return args
}

/**
 * Build the env block that agents receive — injects stage_run and task
 * IDs so the channel's request_permission tool can post back to the
 * right task. Pure function — exported for testing.
 */
export function buildSpawnEnv(opts: SpawnAgentOptions): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {
    ...process.env,
    DASHBOARD_STAGE_RUN_ID: opts.stageRun.id,
    DASHBOARD_TASK_ID: opts.task.id,
  }
  if (opts.mcpToken)
    env.DASHBOARD_MCP_TOKEN = opts.mcpToken
  if (opts.mcpUrl)
    env.DASHBOARD_MCP_URL = opts.mcpUrl
  return env
}

/**
 * Write a .claude/settings.json into the worktree (or cwd) with the
 * pre-approved tool permissions converted to Claude Code allowlist format.
 *
 * Primary path: creates settings.json when absent.
 * Fallback path: when settings.json already exists (user-authored), merges
 * only our new allow entries into settings.local.json and tracks them under
 * `_dashboardManagedAllows` for selective cleanup. Claude Code concatenates
 * allow arrays across both files at runtime, so permissions are additive.
 *
 * Returns `{ path, wrote, isLocal }` so the caller can choose the right
 * cleanup strategy.
 */
function writeSettingsFile(
  cwd: string,
  permissions: TaskPermission[],
  enableChannel: boolean,
): { path: string | null, wrote: boolean, isLocal: boolean } {
  const allow = buildAllowList(permissions, enableChannel)
  if (allow.length === 0)
    return { path: null, wrote: false, isLocal: false }

  const settingsDir = join(cwd, '.claude')
  const settingsPath = join(settingsDir, 'settings.json')

  if (!existsSync(settingsPath)) {
    mkdirSync(settingsDir, { recursive: true })
    const settings = { permissions: { allow }, _dashboardManaged: true }
    writeFileSync(settingsPath, JSON.stringify(settings, null, 2))
    return { path: settingsPath, wrote: true, isLocal: false }
  }

  // settings.json is user-authored — merge into settings.local.json instead.
  // Claude Code concatenates permissions.allow arrays across scopes, so our
  // entries are additive and the user's file is never touched.
  const localPath = join(settingsDir, 'settings.local.json')
  consola.info(
    `[agentSpawner] ${settingsPath} already exists — merging allow-list into settings.local.json.`,
  )

  let existing: Record<string, unknown> = {}
  if (existsSync(localPath)) {
    try {
      existing = JSON.parse(readFileSync(localPath, 'utf8')) as Record<string, unknown>
    }
    catch {}
  }

  const existingAllow: string[] = Array.isArray((existing?.permissions as Record<string, unknown>)?.allow)
    ? (existing.permissions as Record<string, unknown>).allow as string[]
    : []

  const newEntries = allow.filter(e => !existingAllow.includes(e))
  if (newEntries.length === 0)
    return { path: localPath, wrote: false, isLocal: true }

  const merged = {
    ...existing,
    permissions: {
      ...((existing.permissions as Record<string, unknown> | undefined) ?? {}),
      allow: [...existingAllow, ...newEntries],
    },
    _dashboardManagedAllows: newEntries,
  }
  mkdirSync(settingsDir, { recursive: true })
  writeFileSync(localPath, JSON.stringify(merged, null, 2))
  return { path: localPath, wrote: true, isLocal: true }
}

/**
 * Returns true iff `settingsPath` exists and carries the
 * `_dashboardManaged: true` stamp. Used by callers that need to decide
 * whether to delete a leftover settings.json without holding the
 * SpawnResult closure (e.g. orchestrator finalization after restart).
 */
export function shouldCleanSettingsFile(settingsPath: string): boolean {
  if (!existsSync(settingsPath))
    return false
  try {
    const parsed = JSON.parse(readFileSync(settingsPath, 'utf8')) as Record<string, unknown>
    return parsed._dashboardManaged === true
  }
  catch {
    return false
  }
}

/**
 * Removes dashboard-managed allow entries from settings.local.json.
 * Reads `_dashboardManagedAllows`, strips those entries from
 * `permissions.allow`, then removes the tracking key. Deletes the file if
 * nothing remains. Called by orchestrator finalization and SpawnResult.cleanup
 * when the fallback local-merge path was used.
 */
export function cleanupLocalSettingsEntries(localPath: string): void {
  if (!existsSync(localPath))
    return
  let parsed: Record<string, unknown>
  try {
    parsed = JSON.parse(readFileSync(localPath, 'utf8')) as Record<string, unknown>
  }
  catch {
    return
  }
  const managed = Array.isArray(parsed._dashboardManagedAllows)
    ? parsed._dashboardManagedAllows as string[]
    : []
  if (managed.length === 0)
    return

  delete parsed._dashboardManagedAllows
  const perms = parsed.permissions as Record<string, unknown> | undefined
  if (perms && Array.isArray(perms.allow)) {
    const cleaned = (perms.allow as string[]).filter(e => !managed.includes(e))
    if (cleaned.length === 0)
      delete perms.allow
    else
      perms.allow = cleaned
    if (Object.keys(perms).length === 0)
      delete parsed.permissions
  }
  try {
    if (Object.keys(parsed).length === 0)
      unlinkSync(localPath)
    else
      writeFileSync(localPath, JSON.stringify(parsed, null, 2))
  }
  catch { /* already gone or read-only */ }
}

/**
 * Spawn a detached Claude agent process for the given stage.
 * The orchestrator records the returned PID on the stage_run and later
 * watches for channel replies that carry session metadata.
 */
export function spawnStageAgent(opts: SpawnAgentOptions): SpawnResult {
  const cwd = opts.task.worktreePath || opts.task.cwd
  const enableChannel = opts.enableChannel !== false
  const { path: settingsPath, wrote: wroteSettingsFile, isLocal } = writeSettingsFile(
    cwd,
    opts.permissions,
    enableChannel,
  )

  const args = buildSpawnArgs(opts)

  if (enableChannel) {
    args.push('--mcp-config', buildDashboardChannelMcpConfig())
  }

  const child = spawn('claude', args, {
    cwd,
    detached: true,
    stdio: ['ignore', 'ignore', 'pipe'],
    env: buildSpawnEnv(opts),
  })

  // CRITICAL: drain stderr so the OS pipe buffer (typically 64 KB) cannot
  // fill and block a long-running detached agent. We don't store it here —
  // the dashboard's existing spawnStore path is responsible for buffering.
  child.stderr?.on('data', () => { /* drain */ })
  child.stderr?.on('error', () => { /* child exit may trigger EPIPE */ })

  child.unref()

  child.on('error', (err) => {
    consola.error(`[agentSpawner] spawn failed: ${err.message}`)
  })

  return {
    child,
    pid: child.pid ?? 0,
    cwd,
    settingsPath,
    cleanup: () => {
      if (!wroteSettingsFile || !settingsPath)
        return
      if (isLocal) {
        cleanupLocalSettingsEntries(settingsPath)
      }
      else {
        // Only delete settings.json if it still carries our stamp — guards
        // against deleting a file the user has since replaced with their own.
        if (!shouldCleanSettingsFile(settingsPath))
          return
        try {
          unlinkSync(settingsPath)
        }
        catch { /* already gone, race with another cleanup, or read-only fs */ }
      }
    },
  }
}
