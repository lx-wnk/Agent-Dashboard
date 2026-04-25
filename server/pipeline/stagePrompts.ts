/**
 * Per-stage prompt builders consumed by `createAgentStage` in
 * stageHandlers.ts. Each function returns a `{ systemPrompt, userPrompt }`
 * bundle that the agent spawner passes to the Claude CLI. Keeping prompt
 * engineering isolated from the orchestrator lets prompts evolve without
 * touching the state machine.
 *
 * Contract: every userPrompt ends with a `\`\`\`json\`\`\`` fenced block
 * describing the expected output schema. The orchestrator's
 * completionDetector parses that block and runs `validateStageOutput`
 * against it — keep the schema description and the validator in sync.
 */
import type { PipelineTask, StageRun, TaskFeedback } from '../../src/types.js'

export interface PromptBundle {
  systemPrompt: string
  userPrompt: string
}

const SHARED_CONTEXT = `You are an agent working inside a structured task pipeline. A human orchestrator will review your output at specific stages. Be concise, actionable, and honest about uncertainty. When you produce structured output, wrap it in a fenced \`\`\`json ... \`\`\` block for the orchestrator to parse.`

/**
 * Format unresolved user-feedback entries into a prompt prefix that the
 * agent reads BEFORE the regular task prompt. This block is the entire
 * user-feedback contract: how it's phrased determines whether the agent
 * actually addresses past concerns or treats them as background noise.
 *
 * Pure function — exported for testing.
 */
export function buildUserFeedbackPrefix(feedbacks: TaskFeedback[]): string {
  if (feedbacks.length === 0)
    return ''
  const count = feedbacks.length
  const header = count === 1
    ? `## Reviewer Feedback (1 outstanding item)`
    : `## Reviewer Feedback (${count} outstanding items, oldest first)`
  const items = feedbacks
    .map((f, i) => {
      const tag = i === count - 1 ? ' **[most recent]**' : ''
      return `**${f.iteration}.**${tag} ${f.feedback}`
    })
    .join('\n\n')
  return `${header}\n\nA human reviewer rejected your prior output on this stage. Address the items below in your next attempt. Each item below blocks approval until resolved.\n\n${items}\n\n**Acknowledgement contract:** in your output, briefly state how each numbered item was addressed (one sentence each is fine). The reviewer uses this to verify nothing was silently skipped.\n\n---\n\n`
}

export function umsetzungPrompt(task: PipelineTask, prevOutput: unknown, feedback?: string): PromptBundle {
  const systemPrompt = `${SHARED_CONTEXT}

You are the Opus orchestrator for this task's implementation phase. Use the Task tool to dispatch subagents for parallel work when beneficial. Commit your work via git when done — but NEVER git push; pushing is always the user's responsibility. Call dashboard_reply when you need to communicate status.

## Permission handling — CRITICAL

The tools you need were pre-approved from the konzept refinement chat's toolRequests. If you try a tool and it is denied (permission error / interactive prompt), you MUST:
1. Call the \`request_permission\` MCP tool with the exact tool name and pattern (e.g. tool="Bash", pattern="npm run *").
2. Stop immediately after calling it — do NOT write prose asking the user, do NOT continue guessing alternatives.
3. The task will pause on_hold. The user will grant or deny the request and resume you.

Never write a message like "please grant me write permission to X" — that message cannot be acted upon. Always use request_permission instead.`

  const feedbackBlock = feedback
    ? `\n\n## Review Feedback From Previous Iteration\n${feedback}\n\nAddress this feedback in your next attempt.`
    : ''

  return {
    systemPrompt,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Konzept (spec, plan, toolRequests)\n\`\`\`json\n${JSON.stringify(prevOutput, null, 2)}\n\`\`\`${feedbackBlock}\n\n## Your Job: Implement\n\nWork step-by-step through the konzept plan. Commit each logical change via git.\n\nWhen finished, produce a \`\`\`json\`\`\` block as your final output:\n{"summary": string, "commits": string[], "openItems": string[]}\n\nOptionally also call dashboard_reply with the summary text.`,
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
