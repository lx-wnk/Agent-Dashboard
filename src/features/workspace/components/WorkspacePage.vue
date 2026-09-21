<script setup lang="ts">
import type { WorkspacePage as Page } from '../layout'
import { computed, onMounted } from 'vue'
import { replacePage } from '../layout'
import { useWorkspace } from '../useWorkspace'
import WorkspaceGrid from './WorkspaceGrid.vue'

const props = defineProps<{ pageId: string }>()
const { layout, load, save, locked, saveError, editing, page } = useWorkspace()
onMounted(load)

const current = computed(() => page(props.pageId))

function onChange(next: Page) {
  save(replacePage(layout.value, next))
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-3" :data-testid="`workspace-page-${pageId}`">
    <p v-if="locked" role="alert" data-testid="workspace-locked" class="rounded-md bg-warning-soft px-3 py-2 text-[12.5px] text-warning-text">
      {{ locked }}
    </p>
    <p v-if="saveError" role="alert" data-testid="workspace-save-error" class="rounded-md bg-danger-soft px-3 py-2 text-[12.5px] text-danger-text">
      {{ saveError }}
    </p>
    <WorkspaceGrid v-if="current" class="min-h-0 flex-1" :page="current" :editing="editing" @change="onChange" />
  </div>
</template>
