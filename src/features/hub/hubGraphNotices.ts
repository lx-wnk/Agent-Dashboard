import type { GraphStatus } from './composables/useObsidianGraph'

// Shown at the top of the hub while its own notes are hidden.
export const GRAPH_NOTICES: Partial<Record<GraphStatus, string>> = {
  unconfigured: 'Connect Obsidian to see your notes here.',
  denied: 'Memory reads are not granted, so your notes stay hidden.',
  failed: 'Your notes could not be loaded; retrying when you come back to this window.',
}

// Shown by HubList's "Recently touched" list: its own wording for unconfigured,
// denied/failed reused from GRAPH_NOTICES so the two can't drift apart, and its
// own ready-but-empty case the hub notice has no equivalent for.
export const LIST_GRAPH_NOTICES: Partial<Record<GraphStatus, string>> = {
  unconfigured: 'Connect Obsidian to see recently touched notes.',
  denied: GRAPH_NOTICES.denied,
  failed: GRAPH_NOTICES.failed,
  ready: 'No notes yet.',
}
