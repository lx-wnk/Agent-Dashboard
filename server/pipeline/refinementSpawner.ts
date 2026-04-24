import type { RefinementTurn } from '../db/refinementTurnsRepo.js'
import type { Readable } from 'node:stream'
import { spawn } from 'node:child_process'

export const REFINEMENT_SYSTEM_PROMPT = `You are a ticket refinement assistant that helps software teams create well-defined tasks through structured dialogue. Work through exactly four phases in strict order. Never skip phases.

**Phase 1: ANALYSE**
Ask for: working directory (cwd), source branch, target branch, problem description, complexity estimate. Ask ONE question at a time. When you have all required information, end your message with exactly: __phase_done: analyse

**Phase 2: SPEC**
Write a refined title, description, success criteria (bullet list), assumptions, out-of-scope. Present it and accept feedback. When the spec is accepted by the user, end with: __phase_done: spec

**Phase 3: UMSETZUNGSKONZEPT**
Break down the implementation into numbered steps. List all tool permissions needed. For each tool: name, optional glob pattern, reason. Common tools: Read, Write, Edit, Glob, Grep, Bash. Bash always needs a pattern (e.g. "npm run *"). Never include "Bash(git push *)". Present and accept feedback. When accepted, end with: __phase_done: umsetzungskonzept

**Phase 4: APPROVAL**
Summarise the complete spec and plan. Ask the user to confirm. When confirmed, output ONLY this JSON block and nothing after it:
\`\`\`json
{
  "refinedTitle": "...",
  "refinedDescription": "...",
  "successCriteria": ["..."],
  "assumptions": ["..."],
  "outOfScope": ["..."],
  "cwd": "...",
  "sourceBranch": "...",
  "targetBranch": "...",
  "steps": [{"n": 1, "description": "..."}],
  "toolRequests": [{"tool": "...", "pattern": "...", "reason": "..."}]
}
\`\`\`
Then end with: __phase_done: approval`

export function serializeHistory(turns: RefinementTurn[]): string {
  if (turns.length === 0)
    return ''
  const lines = turns.map(t =>
    `${t.role === 'user' ? 'Human' : 'Assistant'}: ${t.content}`,
  )
  return `Previous conversation:\n${lines.join('\n\n')}\n\n`
}

export interface SpawnRefinementResult {
  stdout: Readable
  waitForExit: () => Promise<void>
}

export function spawnRefinementTurn(
  message: string,
  history: RefinementTurn[],
  cwd: string,
): SpawnRefinementResult {
  const historyBlock = serializeHistory(history)
  const fullPrompt = `${historyBlock}Human: ${message}\n\nContinue the conversation as the assistant. Follow the phase instructions exactly.`

  const child = spawn('claude', [
    '-p', fullPrompt,
    '--system-prompt', REFINEMENT_SYSTEM_PROMPT,
    '--permission-mode', 'default',
  ], {
    cwd,
    stdio: ['ignore', 'pipe', 'pipe'],
  })

  child.stderr?.on('data', () => { /* drain to prevent pipe buffer fill */ })
  child.stderr?.on('error', () => { /* swallow EPIPE on exit */ })

  const waitForExit = (): Promise<void> =>
    new Promise((resolve, reject) => {
      child.on('close', (code, signal) =>
        code === 0
          ? resolve()
          : reject(new Error(`refinement spawn exited code=${code} signal=${signal}`)),
      )
      child.on('error', reject)
    })

  if (!child.stdout)
    throw new Error('refinement spawn: stdout pipe missing')
  return { stdout: child.stdout, waitForExit }
}
