import type { PipelineTask, StageRun } from '../../src/types.js'

/**
 * Stage prompts: build the system prompt + user prompt for each agent-driven
 * pipeline stage. Keeps prompt engineering isolated from the orchestrator.
 */

export interface PromptBundle {
  systemPrompt: string
  userPrompt: string
}

const SHARED_CONTEXT = `You are an agent working inside a structured task pipeline. A human orchestrator will review your output at specific stages. Be concise, actionable, and honest about uncertainty. When you produce structured output, wrap it in a fenced \`\`\`json ... \`\`\` block for the orchestrator to parse.`

export function pruefungPrompt(task: PipelineTask): PromptBundle {
  return {
    systemPrompt: SHARED_CONTEXT,
    userPrompt: `## Task: ${task.title}\n\n${task.description || '(no description provided)'}\n\n## Working Directory\n${task.cwd}\n\n## Your Job: Feasibility Check\n\nAnalyze the task and produce a short feasibility report. Answer:\n1. Is this task well-defined or does it need refinement?\n2. What are the obvious risks or unknowns?\n3. Rough complexity estimate (XS / S / M / L / XL).\n4. Any immediate blockers?\n\nRespond with a \`\`\`json\`\`\` block containing {"wellDefined": bool, "risks": string[], "complexity": "XS"|"S"|"M"|"L"|"XL", "blockers": string[], "recommendation": "proceed"|"refine"|"reject"}.`,
  }
}

export function planningPrompt(task: PipelineTask, prevOutput: unknown): PromptBundle {
  return {
    systemPrompt: SHARED_CONTEXT,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Previous Stage Output\n\`\`\`json\n${JSON.stringify(prevOutput, null, 2)}\n\`\`\`\n\n## Your Job: Breakdown\n\nDecompose the task into concrete subtasks. List:\n- Files likely to be touched\n- External dependencies or APIs involved\n- Order of operations\n- Acceptance criteria\n\nRespond with a \`\`\`json\`\`\` block: {"subtasks": [{"id": string, "title": string, "files": string[]}], "acceptanceCriteria": string[]}.`,
  }
}

export function umsetzungskonzeptPrompt(task: PipelineTask, prevOutput: unknown): PromptBundle {
  return {
    systemPrompt: SHARED_CONTEXT,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Plan From Previous Stage\n\`\`\`json\n${JSON.stringify(prevOutput, null, 2)}\n\`\`\`\n\n## Your Job: Implementation Plan + Tool Inventory\n\nProduce the concrete implementation plan AND a complete list of tool permissions you will need during umsetzung. Be exhaustive — any missing tool will force a mid-run ON HOLD pause.\n\nRespond with a \`\`\`json\`\`\` block: {"steps": [{"n": number, "description": string}], "toolRequests": [{"tool": string, "pattern": string|null, "reason": string}]}. \n\nCommon tools: Read, Grep, Glob, Write, Edit, Bash. For Bash, always include a pattern (e.g. "npm *", "git status"). Do NOT request Bash(git push *) unless absolutely necessary. Do NOT request WebFetch unless you know you need network access.`,
  }
}

export function umsetzungPrompt(task: PipelineTask, prevOutput: unknown, feedback?: string): PromptBundle {
  const systemPrompt = `${SHARED_CONTEXT}\n\nYou are the Opus orchestrator for this task's implementation phase. Use the Task tool to dispatch subagents for parallel work when beneficial. Commit your work via git when done. Call dashboard_reply when you need to communicate status. Use request_permission if you discover a tool need that was not pre-approved.`

  const feedbackBlock = feedback
    ? `\n\n## Review Feedback From Previous Iteration\n${feedback}\n\nAddress this feedback in your next attempt.`
    : ''

  return {
    systemPrompt,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Approved Plan\n\`\`\`json\n${JSON.stringify(prevOutput, null, 2)}\n\`\`\`${feedbackBlock}\n\n## Your Job: Implement\n\nWork step-by-step through the approved plan. Commit each logical change via git. When finished, write a short summary to dashboard_reply and stop.`,
  }
}

export function selbstreviewPrompt(task: PipelineTask, umsetzungOutput: unknown): PromptBundle {
  return {
    systemPrompt: SHARED_CONTEXT,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Implementation Output\n\`\`\`json\n${JSON.stringify(umsetzungOutput, null, 2)}\n\`\`\`\n\n## Your Job: Self-Review\n\nReview the implementation against:\n1. Original task requirements — are they all met?\n2. Security — any injection, XSS, SQL, auth bypass, secrets leaked?\n3. Code quality — DRY violations, dead code, missing error handling?\n4. Test coverage — are the changes tested?\n\nRespond with a \`\`\`json\`\`\` block: {"passed": bool, "findings": [{"severity": "high"|"medium"|"low", "description": string, "file": string|null}], "summary": string}.`,
  }
}

export function finalisierungPrompt(task: PipelineTask, stageRuns: StageRun[]): PromptBundle {
  const history = stageRuns.map(r => `${r.stage} (iter ${r.iteration}): ${r.status}`).join('\n')
  return {
    systemPrompt: SHARED_CONTEXT,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Stage History\n${history}\n\n## Your Job: Final Report\n\nProduce a user-facing summary of what was done. Include:\n- Short insights or lessons learned\n- Known open todos or caveats\n- Concrete test steps the user can run to verify the change\n\nRespond with a \`\`\`json\`\`\` block: {"summary": string, "insights": string[], "openTodos": string[], "testPlan": string[]}.`,
  }
}
