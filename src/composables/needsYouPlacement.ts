import type { ActiveView } from './useViewState'

export interface NeedsYouPlacement {
  strip: boolean
}

// error: the error line replaces the page, and with it the hub or the triage band.
// dashboard: its triage band reads the same agents and permission items.
// pageHasHub: the hub docks the queue itself.
export function needsYouPlacement({ view, pageHasHub, error }: { view: ActiveView, pageHasHub: boolean, error: boolean }): NeedsYouPlacement {
  if (error)
    return { strip: true }
  if (view === 'dashboard')
    return { strip: false }
  return { strip: !pageHasHub }
}
