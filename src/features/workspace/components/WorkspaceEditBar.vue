<script setup lang="ts">
import type { WorkspacePage } from '../layout'
import { computed, ref } from 'vue'
import { addTile } from '../layout'
import { widgetIds, WIDGETS } from '../widgetRegistry'

const props = defineProps<{ page: WorkspacePage, refusal: string | null, locked: boolean }>()
const emit = defineEmits<{ change: [page: WorkspacePage], refuse: [reason: string], reset: [], done: [] }>()

const choice = ref('')
const addable = computed(() => widgetIds().filter(id => !props.page.tiles.some(t => t.widget === id)))

function add() {
  if (!choice.value)
    return
  const r = addTile(props.page, choice.value)
  if (r.ok)
    emit('change', r.value)
  else
    emit('refuse', r.reason)
  choice.value = ''
}
</script>

<template>
  <div data-testid="workspace-edit-bar" class="flex flex-wrap items-center gap-2 rounded-lg border border-line bg-card px-3 py-2 text-[12.5px]">
    <span class="font-medium text-fg">Editing {{ page.title }}</span>
    <span class="text-fg-mute">Arrows move · Shift+arrows resize · Delete removes</span>
    <span class="flex-grow" />
    <select v-model="choice" data-testid="workspace-add" aria-label="Tile to add" class="rounded-md border border-line-strong bg-app px-2 py-1">
      <option value="" disabled>
        Add a tile…
      </option>
      <option v-for="id in addable" :key="id" :value="id">
        {{ WIDGETS[id].title }}
      </option>
    </select>
    <button type="button" data-testid="workspace-add-submit" class="rounded-md border border-line-strong px-2.5 py-1" :disabled="!choice" @click="add">
      Add
    </button>
    <button v-if="locked" type="button" data-testid="workspace-reset" class="rounded-md border border-line-strong px-2.5 py-1" @click="emit('reset')">
      Reset to default
    </button>
    <button type="button" data-testid="workspace-done" class="rounded-md bg-accent px-2.5 py-1 text-accent-contrast" @click="emit('done')">
      Done
    </button>
    <p data-testid="workspace-refusal" aria-live="polite" class="basis-full text-warning-text empty:hidden">
      {{ refusal ?? '' }}
    </p>
  </div>
</template>
