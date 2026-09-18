import type { ActiveView } from '@/composables/useViewState'
import { ACTIVE_VIEWS } from '@/composables/useViewState'

/**
 * What the input will do with what you typed, decided BEFORE you press Enter.
 *
 * Only readings this client can settle on its own are offered. Anything that
 * would need the server to interpret intent is deliberately absent: a reading
 * shown here is a promise, and a promise the client cannot keep is worse than
 * no reading at all.
 */
export type ReadingKind = 'navigate' | 'command' | 'capture' | 'empty'

export interface Reading {
  kind: ReadingKind
  /** The badge, in the user's words. */
  label: string
  /** What pressing Enter does, stated as a consequence. */
  will: string
  /** For navigate: the view to switch to. */
  view?: ActiveView
}

export function readInput(raw: string): Reading {
  const text = raw.trim()
  if (!text)
    return { kind: 'empty', label: '', will: '' }

  if (text.startsWith('/')) {
    return {
      kind: 'command',
      label: 'COMMAND',
      will: 'Runs the slash command against the agent this view is focused on.',
    }
  }

  const lower = text.toLowerCase()
  const view = ACTIVE_VIEWS.find(v => lower === v || lower === `go to ${v}` || lower === `open ${v}`)
  if (view) {
    return {
      kind: 'navigate',
      label: 'GO TO',
      will: `Switches to the ${view} view. Nothing is created.`,
      view,
    }
  }

  return {
    kind: 'capture',
    label: 'CAPTURE',
    will: 'Becomes a backlog item — the slug follows the title and the folder comes from your first project. Nothing runs yet.',
  }
}
