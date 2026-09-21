<script setup lang="ts">
import type { WorkspacePage as Page } from '../layout'
import { computed, onMounted, ref, watch } from 'vue'
import { replacePage } from '../layout'
import { useWorkspace } from '../useWorkspace'
import WorkspaceEditBar from './WorkspaceEditBar.vue'
import WorkspaceGrid from './WorkspaceGrid.vue'

const props = defineProps<{ pageId: string }>()
const { layout, load, save, locked, saveError, editing, reset, page } = useWorkspace()
onMounted(load)

const current = computed(() => page(props.pageId))
const refusal = ref<string | null>(null)

// A locked layout is unreadable, so editing on top of it has nothing safe to
// save; force it off so the only way out is Reset to default.
watch(locked, (l) => {
  if (l)
    editing.value = false
})

function onChange(next: Page) {
  refusal.value = null
  save(replacePage(layout.value, next))
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-3" :data-testid="`workspace-page-${pageId}`">
    <div v-if="locked" role="alert" data-testid="workspace-locked" class="flex flex-wrap items-center gap-2 rounded-md bg-warning-soft px-3 py-2 text-[12.5px] text-warning-text">
      <p>{{ locked }}</p>
      <button type="button" data-testid="workspace-reset" class="rounded-md border border-line-strong px-2.5 py-1" @click="reset">
        Reset to default
      </button>
    </div>
    <p v-if="saveError" role="alert" data-testid="workspace-save-error" class="rounded-md bg-danger-soft px-3 py-2 text-[12.5px] text-danger-text">
      {{ saveError }}
    </p>
    <WorkspaceEditBar
      v-if="editing && current"
      :page="current"
      :refusal="refusal"
      @change="onChange"
      @refuse="r => (refusal = r)"
      @done="editing = false"
    />
    <WorkspaceGrid v-if="current" class="min-h-0 flex-1" :page="current" :editing="editing" @change="onChange" @refuse="r => (refusal = r)" />
  </div>
</template>
