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
import process from 'node:process'

export interface PromptBundle {
  systemPrompt: string
  userPrompt: string
}

const SHARED_CONTEXT = `You are an agent working inside a structured task pipeline. A human orchestrator will review your output at specific stages. Be concise, actionable, and honest about uncertainty. When you produce structured output, wrap it in a fenced \`\`\`json ... \`\`\` block for the orchestrator to parse.`

const UPFRONT_PERMISSIONS_DIRECTIVE = `## Permissions — declare upfront, in bulk (CRITICAL FIRST STEP)

Before any tool call, scan your task description and the work ahead. Build the FULL list of tools you anticipate needing — file ops (Read/Write/Edit/MultiEdit/Glob/Grep/LS), Bash patterns (e.g. \`pnpm test*\`, \`pnpm lint*\`, \`git commit*\`, \`git push*\` if applicable), WebFetch URLs, etc.

Then call the \`request_permission\` MCP tool ONCE with the full \`permissions: [...]\` array (each item: {tool, pattern?, reason?}). The dashboard auto-resolves any entries already pre-granted on the task — only truly new entries surface as ON HOLD. If everything is covered you keep running uninterrupted; if anything is missing the user grants the whole batch in one decision instead of N round-trips.

Spawning sub-tasks via \`create_task\`? Pass their permissions inline at creation time (\`permissions: [...]\` or \`template: 'feature_implementation'\` or \`inheritPermissions: true\` if a parent is set). Otherwise the child will need its own bulk request_permission step, slowing it down.

NEVER write prose like "please grant me X" — only \`request_permission\` is actionable.

After this upfront step, only request mid-task if you discover a tool you didn't anticipate.`

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

export function implementationPrompt(task: PipelineTask, prevOutput: unknown, feedback?: string): PromptBundle {
  const meta = (task.metadata ?? null) as Record<string, unknown> | null
  const allowGitPush = (meta && meta.allowGitPush === true) || process.env.DASHBOARD_ALLOW_GIT_PUSH === 'true'
  const pushPolicyLine = allowGitPush
    ? 'Commit AND push (`git push`) are permitted for this task — push your feature branch when work is complete.'
    : 'Commit your work via git when done — but NEVER `git push`; pushing is the user\'s responsibility.'

  const systemPrompt = `${SHARED_CONTEXT}

You are the Opus orchestrator for this task's implementation phase. Use the Task tool to dispatch subagents for parallel work when beneficial. ${pushPolicyLine} Call dashboard_reply when you need to communicate status.

${UPFRONT_PERMISSIONS_DIRECTIVE}`

  const feedbackBlock = feedback
    ? `\n\n## Review Feedback From Previous Iteration\n${feedback}\n\nAddress this feedback in your next attempt.`
    : ''

  return {
    systemPrompt,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Concept (spec, plan, toolRequests)\n\`\`\`json\n${JSON.stringify(prevOutput, null, 2)}\n\`\`\`${feedbackBlock}\n\n## Your Job: Implement\n\nWork step-by-step through the concept plan. Commit each logical change via git.\n\nWhen finished, produce a \`\`\`json\`\`\` block as your final output:\n{"summary": string, "commits": string[], "openItems": string[]}\n\nOptionally also call dashboard_reply with the summary text.`,
  }
}

export function selfReviewPrompt(task: PipelineTask, implementationOutput: unknown): PromptBundle {
  return {
    systemPrompt: `${SHARED_CONTEXT}\n\n${UPFRONT_PERMISSIONS_DIRECTIVE}`,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Implementation Output\n\`\`\`json\n${JSON.stringify(implementationOutput, null, 2)}\n\`\`\`\n\n## Your Job: Self-Review\n\nReview the implementation against:\n1. Original task requirements — are they all met?\n2. Security — any injection, XSS, SQL, auth bypass, secrets leaked?\n3. Code quality — DRY violations, dead code, missing error handling?\n4. Test coverage — are the changes tested?\n\nRespond with a \`\`\`json\`\`\` block: {"passed": bool, "findings": [{"severity": "high"|"medium"|"low", "description": string, "file": string|null}], "summary": string}.`,
  }
}

export function finalizationPrompt(task: PipelineTask, stageRuns: StageRun[]): PromptBundle {
  const history = stageRuns.map(r => `${r.stage} (iter ${r.iteration}): ${r.status}`).join('\n')
  return {
    systemPrompt: `${SHARED_CONTEXT}\n\n${UPFRONT_PERMISSIONS_DIRECTIVE}`,
    userPrompt: `## Task: ${task.title}\n\n${task.description || ''}\n\n## Stage History\n${history}\n\n## Your Job: Final Report\n\nProduce a user-facing summary of what was done. Include:\n- Short insights or lessons learned\n- Known open todos or caveats\n- Concrete test steps the user can run to verify the change\n\nRespond with a \`\`\`json\`\`\` block: {"summary": string, "insights": string[], "openTodos": string[], "testPlan": string[]}.`,
  }
}
