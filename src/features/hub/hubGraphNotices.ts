import type { GraphStatus } from './composables/useObsidianGraph'

// Shared with HubList's "Recently touched" empty state — one wording per status kept in one place.
export const GRAPH_NOTICES: Partial<Record<GraphStatus, string>> = {
  unconfigured: 'Connect Obsidian to see your notes here.',
  denied: 'Memory reads are not granted, so your notes stay hidden.',
  failed: 'Your notes could not be loaded; retrying when you come back to this window.',
}
