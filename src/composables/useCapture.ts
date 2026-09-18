import type { Project } from '@/types'
import { suggestFolders } from '@/composables/useProjectFolders'
import { createTask } from '@/features/pipeline'
import { slugFollowingName } from '@/utils/validation'

/**
 * Turning one line of text into a backlog item, for every field that offers to
 * do it — the spotlight and mission control both call this rather than keeping
 * a copy each. The derivation IS the feature: the slug follows the title and
 * the working directory comes from the first project's default folder, so
 * nothing has to be chosen before a thought can be written down.
 */
export class CaptureUnavailable extends Error {}

async function deriveCwd(project: Project): Promise<string | null> {
  const folders = await suggestFolders(project.id)
  return (folders.find(f => f.isDefault) ?? folders[0])?.path ?? null
}

/**
 * Throws CaptureUnavailable when the system cannot capture at all — no
 * project, or a project with no folder. Those are the operator's to fix and
 * read differently from a failed request, so they carry their own type.
 */
export async function captureTask(title: string, projects: Project[]): Promise<string> {
  const trimmed = title.trim()
  if (!trimmed)
    throw new CaptureUnavailable('Nothing to capture.')

  const project = projects[0]
  if (!project)
    throw new CaptureUnavailable('No project exists yet — create one in Settings before capturing.')

  const cwd = await deriveCwd(project)
  if (!cwd)
    throw new CaptureUnavailable(`Project "${project.name}" has no folder — add one in Settings.`)

  const task = await createTask({
    title: trimmed,
    slug: slugFollowingName(trimmed, '', false),
    cwd,
    projectId: project.id,
  })
  return task.id
}
