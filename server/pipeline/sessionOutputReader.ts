/**
 * Reads the final structured JSON output a stage agent produced in its
 * session JSONL, so the orchestrator can finalize a completed stage run.
 *
 * Contract with stagePrompts.ts: every agent-driven stage prompt tells
 * the agent to wrap its final answer in a ```json ... ``` fenced block.
 * This module locates that block in the last assistant turn and parses it.
 */
import { readdir, realpath, stat } from 'node:fs/promises'
import { join } from 'node:path'
import { encodePath, parseJsonlLines, tailRead } from '../jsonlParser.js'
import { CLAUDE_PROJECTS_DIR } from '../paths.js'

const JSON_BLOCK_RE_G = /```json\b([\s\S]*?)```/gi
const JSONL_SUFFIX_RE = /\.jsonl$/

/**
 * Claude CLI resolves symlinks before encoding the cwd into
 * `~/.claude/projects/{encoded}`. If we encode the raw (symlink) path
 * while Claude CLI encodes the realpath, Dashboard and CLI look at two
 * different project directories and lookup silently fails. This helper
 * is the single source of truth for the directory we should look in.
 *
 * Falls back to the un-resolved path if `realpath` throws — typical in
 * tests where the directory does not exist on disk.
 */
export async function resolvedProjectDir(cwd: string): Promise<string> {
  let resolved = cwd
  try {
    resolved = await realpath(cwd)
  }
  catch {
    // path may not exist (tests, deleted worktree) — fall through
  }
  return join(CLAUDE_PROJECTS_DIR, encodePath(resolved))
}

export interface JsonlEntry {
  type?: string
  message?: {
    role?: string
    content?: Array<{ type?: string, text?: string }> | string
  }
}

/**
 * Locate the session JSONL file for a given cwd + sessionId. Returns the
 * absolute path, or null if the file does not exist yet.
 */
export async function resolveSessionFile(cwd: string, sessionId: string): Promise<string | null> {
  const projectDir = await resolvedProjectDir(cwd)
  const path = join(projectDir, `${sessionId}.jsonl`)
  try {
    await stat(path)
    return path
  }
  catch {
    return null
  }
}

/**
 * Extract the last ```json ... ``` fenced block from an assistant text
 * blob and JSON.parse it. Returns null on any structural failure so the
 * caller can decide how to react (retry, fail, etc.).
 *
 * We pick the LAST block (not the first) because agents sometimes include
 * an example JSON block earlier in their prose before emitting the final
 * answer block — the final block is the contractual output.
 */
export function extractJsonBlock(text: string): Record<string, unknown> | null {
  const matches = [...text.matchAll(JSON_BLOCK_RE_G)]
  const match = matches.at(-1)
  if (!match)
    return null
  try {
    const parsed = JSON.parse(match[1].trim())
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
      return null
    return parsed as Record<string, unknown>
  }
  catch {
    return null
  }
}

/**
 * Walk the parsed JSONL entries from the tail backwards, find the last
 * assistant text turn, and return its concatenated text content. Tool_use
 * blocks are ignored — we only care about the agent's prose output.
 */
export function lastAssistantText(entries: JsonlEntry[]): string | null {
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i]
    if (entry.type !== 'assistant')
      continue
    const content = entry.message?.content
    if (!Array.isArray(content))
      continue
    const parts = content
      .filter(b => b.type === 'text' && typeof b.text === 'string')
      .map(b => b.text as string)
    if (parts.length > 0)
      return parts.join('\n')
  }
  return null
}

/**
 * Fallback sessionId discovery: when a stage_run has no sessionId attached
 * (the production spawn path does not currently round-trip it back), scan
 * the encoded project directory for the newest .jsonl file whose mtime
 * is at or after `afterIso`. This avoids grabbing a pre-existing session
 * that belonged to a different (user-started) agent in the same cwd.
 */
export async function findNewestSessionId(
  cwd: string,
  afterIso: string | null,
): Promise<string | null> {
  const projectDir = await resolvedProjectDir(cwd)

  let entries
  try {
    entries = await readdir(projectDir, { withFileTypes: true })
  }
  catch {
    return null
  }

  const afterMs = afterIso ? new Date(afterIso).getTime() : 0
  const candidates: Array<{ sessionId: string, mtimeMs: number }> = []

  for (const entry of entries) {
    if (!entry.isFile() || !entry.name.endsWith('.jsonl'))
      continue
    const fullPath = join(projectDir, entry.name)
    try {
      const s = await stat(fullPath)
      if (s.mtimeMs < afterMs)
        continue
      candidates.push({
        sessionId: entry.name.replace(JSONL_SUFFIX_RE, ''),
        mtimeMs: s.mtimeMs,
      })
    }
    catch {
      continue
    }
  }

  if (candidates.length === 0)
    return null
  candidates.sort((a, b) => b.mtimeMs - a.mtimeMs)
  return candidates[0].sessionId
}

/**
 * Top-level helper: resolve file → tail read → parse lines → find last
 * assistant text → extract JSON block.
 *
 * Returns a struct so callers can distinguish three outcomes:
 * - `{ output: {...}, rawText }`  — JSON block found and parsed
 * - `{ output: null, rawText: string }` — agent wrote prose but no parseable
 *   JSON block (typically: permission wall, tool refusal, user-directed
 *   prose response). The raw text is surfaced in the stage_run failure
 *   output so the user can see what the agent actually said instead of a
 *   mechanical "no parseable json output" message.
 * - `{ output: null, rawText: null }` — no session, no assistant turn,
 *   nothing to show. Hard failure with no additional context.
 */
export interface StageOutputRead {
  output: Record<string, unknown> | null
  rawText: string | null
}

export async function readLastStageJsonOutput(
  cwd: string,
  sessionId: string,
): Promise<StageOutputRead> {
  const filePath = await resolveSessionFile(cwd, sessionId)
  if (!filePath)
    return { output: null, rawText: null }

  let raw: string
  try {
    raw = await tailRead(filePath)
  }
  catch {
    return { output: null, rawText: null }
  }

  const entries = parseJsonlLines(raw) as JsonlEntry[]
  const text = lastAssistantText(entries)
  if (!text)
    return { output: null, rawText: null }

  return { output: extractJsonBlock(text), rawText: text }
}
