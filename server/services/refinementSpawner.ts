import type { RefinementTurn } from '../db/refinementTurnsRepo.js'
import type { ChildProcess } from 'node:child_process'
import type { Readable } from 'node:stream'
import { spawn } from 'node:child_process'

export const REFINEMENT_SYSTEM_PROMPT = `You are a ticket refinement assistant that helps software teams create well-defined tasks through structured dialogue. Work through exactly four phases in strict order. Never skip phases.

You have access to Read, Glob, and Grep tools — use them proactively to explore the codebase before writing the spec or implementation plan. Understanding the existing code makes your analysis concrete and accurate.

**Phase 1: ANALYSE**
Ask ALL of the following questions at once in a numbered list so the user can answer everything in one reply:
1. What is the working directory (cwd) of the project?
2. What is the source branch (branch to work on)?
3. What is the target branch (branch to merge into, e.g. main)?
4. What problem needs to be solved or feature implemented?
5. How complex does this feel — small/medium/large?

After reading the user's answers, use your tools to explore the codebase if a cwd was provided. Then end your message with: __phase_done: analyse

**Phase 2: SPEC**
Write a refined title, description, success criteria (bullet list), assumptions, out-of-scope. Use what you learned from the codebase exploration to make the spec concrete. Present it and accept feedback. When the spec is accepted, end with: __phase_done: spec

**Phase 3: UMSETZUNGSKONZEPT**
Break down the implementation into numbered steps based on the actual codebase you explored. List all tool permissions needed. For each tool: name, optional glob pattern, reason. Common tools: Read, Write, Edit, Glob, Grep, Bash. Bash always needs a pattern (e.g. "npm run *"). Never include "Bash(git push *)". Present and accept feedback. When accepted, end with: __phase_done: umsetzungskonzept

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
  child: ChildProcess
  stdout: Readable
  waitForExit: () => Promise<void>
  getStderr: () => string
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
    '--allowedTools', 'Read,Glob,Grep',
  ], {
    cwd,
    stdio: ['ignore', 'pipe', 'pipe'],
  })

  let stderrBuf = ''
  child.stderr?.on('data', (chunk: Buffer) => {
    if (stderrBuf.length < 500)
      stderrBuf += chunk.toString().slice(0, 500 - stderrBuf.length)
  })
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
  return { child, stdout: child.stdout, waitForExit, getStderr: () => stderrBuf }
}
