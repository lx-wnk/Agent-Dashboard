<script setup lang="ts">
import type { OpResult, PlacedTile, WorkspacePage } from '../layout'
import { computed } from 'vue'
import { fitsMinimum, moveTile, readingOrder, removeTile, resizeTile, rowsUsed, swapTile } from '../layout'
import { widgetIds, WIDGETS } from '../widgetRegistry'

const props = defineProps<{ page: WorkspacePage, editing: boolean }>()
const emit = defineEmits<{ change: [page: WorkspacePage], refuse: [reason: string] }>()

// DOM order is reading order, which is what the single-column layout below md
// shows; from md up every tile is placed explicitly, so DOM order stops mattering.
const ordered = computed(() => readingOrder(props.page.tiles))
const rows = computed(() => rowsUsed(props.page.tiles))

function placement(t: PlacedTile): Record<string, number> {
  return { '--col': t.col, '--col-span': t.colSpan, '--row': t.row, '--row-span': t.rowSpan }
}

function widgetTitle(tile: PlacedTile): string {
  return WIDGETS[tile.widget]?.title ?? tile.widget
}

function tileAriaLabel(tile: PlacedTile): string {
  return `${widgetTitle(tile)}, column ${tile.col}, row ${tile.row}, ${tile.colSpan} by ${tile.rowSpan}`
}

function apply(r: OpResult<WorkspacePage>) {
  if (r.ok)
    emit('change', r.value)
  else
    emit('refuse', r.reason)
}

const MOVES: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }

function onKey(e: KeyboardEvent, index: number) {
  if (!props.editing)
    return
  const t = props.page.tiles[index]
  if (e.key === 'Delete' || e.key === 'Backspace') {
    e.preventDefault()
    emit('change', removeTile(props.page, index))
    return
  }
  const d = MOVES[e.key]
  if (!d)
    return
  e.preventDefault()
  apply(e.shiftKey
    ? resizeTile(props.page, index, t.colSpan + d[0], t.rowSpan + d[1])
    : moveTile(props.page, index, t.col + d[0], t.row + d[1]))
}

// Candidates for "swap": everything not already on the page; the ones whose
// minimum does not fit this tile are listed disabled with the reason.
function swapOptions(index: number) {
  const t = props.page.tiles[index]
  const placed = new Set(props.page.tiles.map(p => p.widget))
  return widgetIds().filter(id => !placed.has(id)).map(id => ({ id, title: WIDGETS[id].title, reason: fitsMinimum(id, t.colSpan, t.rowSpan) }))
}
</script>

<template>
  <div data-testid="workspace-grid" class="workspace-grid" :style="{ '--rows': rows }">
    <div
      v-for="{ tile, index } in ordered"
      :key="`${tile.widget}-${index}`"
      :data-testid="`workspace-tile-${tile.widget}`"
      class="workspace-tile"
      :class="{ 'workspace-tile--editing': editing }"
      :style="placement(tile)"
      :tabindex="editing ? 0 : undefined"
      :aria-label="editing ? tileAriaLabel(tile) : undefined"
      @keydown="onKey($event, index)"
    >
      <div v-if="editing" class="workspace-chrome">
        <select
          :data-testid="`workspace-swap-${tile.widget}`"
          :aria-label="`Swap ${widgetTitle(tile)} for`"
          class="rounded-md border border-line-strong bg-card px-1.5 text-[12px]"
          @change="apply(swapTile(page, index, ($event.target as HTMLSelectElement).value))"
        >
          <option value="" selected disabled>
            ⇄ Swap…
          </option>
          <option v-for="o in swapOptions(index)" :key="o.id" :value="o.id" :disabled="!!o.reason">
            {{ o.title }}{{ o.reason ? ` — ${o.reason}` : '' }}
          </option>
        </select>
        <button
          type="button"
          :data-testid="`workspace-remove-${tile.widget}`"
          :aria-label="`Remove ${widgetTitle(tile)}`"
          class="rounded-md border border-line-strong bg-card px-2 text-[12px]"
          @click="emit('change', removeTile(page, index))"
        >
          ✕
        </button>
      </div>
      <component :is="WIDGETS[tile.widget].component" v-if="WIDGETS[tile.widget]" />
      <div v-else data-testid="workspace-unknown" class="h-full rounded-xl border border-dashed border-line p-4 text-[12px] text-fg-mute">
        {{ tile.widget }} is not available — the module that provides it may be inactive.
      </div>
    </div>
  </div>
</template>

<style scoped>
.workspace-grid { display: grid; gap: 12px; grid-template-columns: minmax(0, 1fr); }
.workspace-tile { position: relative; min-width: 0; min-height: 0; }
.workspace-tile > :deep(*) { height: 100%; }
.workspace-tile--editing { outline: 1px dashed var(--color-line-strong); outline-offset: 2px; border-radius: 12px; cursor: grab; }
.workspace-tile--editing:focus-visible { outline: 2px solid var(--color-accent); }
.workspace-tile--editing > :deep(:not(.workspace-chrome)) { pointer-events: none; }
.workspace-chrome { position: absolute; top: 6px; right: 6px; z-index: 2; display: flex; gap: 4px; }
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
