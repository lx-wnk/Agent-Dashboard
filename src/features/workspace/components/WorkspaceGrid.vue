<script setup lang="ts">
import type { PlacedTile, WorkspacePage } from '../layout'
import { computed } from 'vue'
import { readingOrder, rowsUsed } from '../layout'
import { WIDGETS } from '../widgetRegistry'

const props = defineProps<{ page: WorkspacePage, editing: boolean }>()
defineEmits<{ change: [page: WorkspacePage], refuse: [reason: string] }>()

// DOM order is reading order, which is what the single-column layout below md
// shows; from md up every tile is placed explicitly, so DOM order stops mattering.
const ordered = computed(() => readingOrder(props.page.tiles))
const rows = computed(() => rowsUsed(props.page.tiles))

function placement(t: PlacedTile): Record<string, number> {
  return { '--col': t.col, '--col-span': t.colSpan, '--row': t.row, '--row-span': t.rowSpan }
}
</script>

<template>
  <div data-testid="workspace-grid" class="workspace-grid" :style="{ '--rows': rows }">
    <div
      v-for="{ tile, index } in ordered"
      :key="`${tile.widget}-${index}`"
      :data-testid="`workspace-tile-${tile.widget}`"
      class="workspace-tile"
      :style="placement(tile)"
    >
      <component :is="WIDGETS[tile.widget].component" v-if="WIDGETS[tile.widget]" />
      <div v-else data-testid="workspace-unknown" class="h-full rounded-xl border border-dashed border-line p-4 text-[12px] text-fg-mute">
        {{ tile.widget }} is not available — the module that provides it may be inactive.
      </div>
    </div>
  </div>
</template>

<style scoped>
.workspace-grid { display: grid; gap: 12px; grid-template-columns: minmax(0, 1fr); }
.workspace-tile { min-width: 0; min-height: 0; }
.workspace-tile > :deep(*) { height: 100%; }
@media (min-width: 768px) {
  .workspace-grid {
    height: 100%;
    grid-template-columns: repeat(12, minmax(0, 1fr));
    grid-template-rows: repeat(var(--rows), minmax(56px, 1fr));
  }
  .workspace-tile {
    grid-column: var(--col) / span var(--col-span);
    grid-row: var(--row) / span var(--row-span);
  }
}
</style>
