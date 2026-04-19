/**
 * Spawns an ad-hoc Claude CLI side session to help the user investigate
 * a task that landed in a failed stage_run. Unlike `agentSpawner`, this
 * is NOT part of the pipeline state machine:
 *
 *  - no stage_run row is created
 *  - no channel MCP, no per-stage allow-list (the user is attending the
 *    session themselves and can grant tools interactively)
 *  - the prompt carries a rich context block with the task identity,
 *    failure details, and pointers to the relevant session JSONLs
 *
 * The spawned process shows up in the dashboard's normal agent-monitoring
 * view (processScanner finds it by cwd + `claude` command) and the user
 * chats with it via the existing AgentModal prompt input, which resumes
 * the same session id.
 */
import type { PipelineTask, StageRun } from '../../src/types.js'
import { spawn } from 'node:child_process'
import { realpathSync } from 'node:fs'
import process from 'node:process'

export interface AnalysisSpawnOptions {
  task: PipelineTask
  failedRun: StageRun
  /** Pre-computed diagnostic blob shown to the agent (errors, paths). */
  errorSummary: string
  /** Session JSONL paths the agent should read for root-cause analysis. */
  sessionLogPaths: string[]
}

export interface AnalysisSpawnResult {
  pid: number
  cwd: string
}

/**
 * Build the system+user prompt the analysis agent starts with. Pure
 * function — exported for testing and so tests can snapshot the exact
 * framing without spawning a real process.
 */
export function buildAnalysisPrompt(opts: AnalysisSpawnOptions): string {
  const { task, failedRun, errorSummary, sessionLogPaths } = opts
  const logsBlock = sessionLogPaths.length > 0
    ? sessionLogPaths.map(p => `- ${p}`).join('\n')
    : '(none found)'

  return [
    '# Failure Analysis Session',
    '',
    'You are attached to a failed pipeline task as an independent analysis',
    'session. You are NOT part of the pipeline state machine — your job is',
    'to help the human understand what went wrong and decide what to do next.',
    '',
    '## Task',
    `- id: ${task.id}`,
    `- slug: ${task.slug}`,
    `- title: ${task.title}`,
    `- current stage: ${task.currentStage}`,
    `- worktree: ${task.worktreePath ?? '(none)'}`,
    `- cwd: ${task.cwd}`,
    task.description ? `\n## Description\n${task.description}` : '',
    '',
    '## Failed Stage Run',
    `- stage: ${failedRun.stage}`,
    `- iteration: ${failedRun.iteration}`,
    `- stage_run id: ${failedRun.id}`,
    `- session id: ${failedRun.sessionId ?? '(not attached)'}`,
    `- started: ${failedRun.startedAt ?? '—'}`,
    `- ended: ${failedRun.endedAt ?? '—'}`,
    '',
    '## Error Summary',
    errorSummary,
    '',
    '## Relevant Session Logs (JSONL files on disk)',
    logsBlock,
    '',
    '## What to do',
    '1. Read the session JSONL(s) above and any task-relevant files to',
    '   identify what actually went wrong.',
    '2. Report to the human in plain language: root cause, what is still',
    '   salvageable, and a recommendation (retry as-is, edit something',
    '   first, split the task, or abandon it).',
    '3. If the human asks you to adjust the task itself, you may:',
    `   - curl the dashboard: POST http://127.0.0.1:${process.env.DASHBOARD_PORT ?? '13120'}/api/tasks/${task.id}`,
    '     with Content-Type: application/json to patch editable fields',
    '     (title, description, priority, max_iterations, etc.).',
    '   - Edit files under the worktree directly.',
    '',
    'Start by reading the newest session JSONL from the list above.',
  ].filter(Boolean).join('\n')
}

/**
 * Spawn a detached interactive-mode analysis agent in the task's worktree
 * (or cwd). Uses `claude -p <prompt>` — the CLI processes the prompt,
 * writes a session JSONL, and the user continues the conversation from
 * the dashboard's existing AgentModal by resuming the same session id.
 */
export function spawnAnalysisAgent(opts: AnalysisSpawnOptions): AnalysisSpawnResult {
  const rawCwd = opts.task.worktreePath || opts.task.cwd
  // Resolve symlinks before spawning so the Claude CLI's own realpath
  // resolution and our dashboard lookups encode the same directory —
  // the exact class of bug that necessitated sessionOutputReader's
  // resolvedProjectDir helper.
  const cwd = (() => {
    try {
      return realpathSync(rawCwd)
    }
    catch {
      return rawCwd
    }
  })()

  const prompt = buildAnalysisPrompt(opts)

  const child = spawn('claude', ['-p', prompt, '--permission-mode', 'acceptEdits'], {
    cwd,
    detached: true,
    stdio: ['ignore', 'ignore', 'pipe'],
    env: {
      ...process.env,
      DASHBOARD_TASK_ID: opts.task.id,
      DASHBOARD_ANALYSIS: '1',
    },
  })

  // Drain stderr to prevent the OS pipe buffer from blocking the detached
  // child on long-running investigations.
  child.stderr?.on('data', () => { /* drain */ })
  child.stderr?.on('error', () => { /* EPIPE on exit is fine */ })
  child.unref()

  return { pid: child.pid ?? 0, cwd }
}
