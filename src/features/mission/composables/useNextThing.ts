import type { PermissionItem } from '@/composables/usePendingPermissions'
import type { PermissionRequest, PipelineTask } from '@/types'

/**
 * The one item mission control puts in the centre, and the reason it is first.
 *
 * The rule is about who is WAITING, not about how important the work is: a
 * pipeline stopped on a permission has an agent sitting idle, while a plan
 * waiting for approval costs nothing until a human looks. Ordering by
 * blocked-ness is what makes the answer explainable in one sentence, and an
 * order nobody can explain is one people stop trusting.
 */
export type NextKind = 'permission' | 'plan'

export interface NextThing {
  kind: NextKind
  taskId: string
  /** The task's own words, for the plan kind and for context on both. */
  taskTitle: string
  projectName: string
  stage: string
  /** The request to resolve, for the permission kind. */
  request?: PermissionRequest
  /** What the centre puts in its headline. */
  title: string
  /** Why this one is first, shown verbatim — never a rank number. */
  why: string
}

/** Lower sorts first. Exported so a new kind has to state its place. */
export const KIND_RANK: Record<NextKind, number> = { permission: 0, plan: 1 }

export const WHY: Record<NextKind, string> = {
  permission: 'First because an agent is stopped until you answer.',
  plan: 'Waiting on your approval — nothing is running while it waits.',
}

/**
 * Every candidate, in the order the centre hands them over. The caller takes
 * [0] for the centre and counts the rest, so the centre can never disagree
 * with what "2 more after this" says.
 */
export function rankNextThings(items: PermissionItem[], tasks: PipelineTask[]): NextThing[] {
  const out: NextThing[] = []

  for (const item of items) {
    const task = tasks.find(t => t.id === item.taskId)
    for (const request of item.requests) {
      out.push({
        kind: 'permission',
        taskId: item.taskId,
        taskTitle: item.title,
        projectName: item.projectName,
        stage: task?.currentStage ?? '',
        request,
        title: request.pattern ? `${request.tool}(${request.pattern})` : request.tool,
        why: WHY.permission,
      })
    }
  }

  for (const task of tasks) {
    if (task.currentStage !== 'plan_review')
      continue
    out.push({
      kind: 'plan',
      taskId: task.id,
      taskTitle: task.title,
      projectName: '',
      stage: task.currentStage,
      title: task.title,
      why: WHY.plan,
    })
  }

  return out.sort((a, b) => {
    const byKind = KIND_RANK[a.kind] - KIND_RANK[b.kind]
    // Same kind: the longest wait first, so nothing starves behind a steady
    // trickle of newer requests.
    return byKind !== 0 ? byKind : waitedSince(a) - waitedSince(b)
  })
}

function waitedSince(n: NextThing): number {
  const at = n.request?.requestedAt
  if (!at)
    return Number.MAX_SAFE_INTEGER
  const t = Date.parse(at)
  return Number.isNaN(t) ? Number.MAX_SAFE_INTEGER : t
}
