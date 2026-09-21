<script setup lang="ts">
import type { OpResult, WorkspacePage as Page, WorkspaceLayout } from '../layout'
import { computed, onMounted, ref, watch } from 'vue'
import { useViewState } from '@/composables/useViewState'
import { removePage, renamePage, replacePage } from '../layout'
import { useWorkspace } from '../useWorkspace'
import WorkspaceEditBar from './WorkspaceEditBar.vue'
import WorkspaceGrid from './WorkspaceGrid.vue'

const props = defineProps<{ pageId: string }>()
const { layout, loaded, load, save, locked, saveError, editing, reset, retry, page } = useWorkspace()
const { activeView } = useViewState()
onMounted(load)

const current = computed(() => page(props.pageId))
const refusal = ref<string | null>(null)

// Nothing is saved while the layout is locked, so edit mode ends with the lock.
watch(locked, (l) => {
  if (l)
    editing.value = false
})

async function commit(r: OpResult<WorkspaceLayout>): Promise<boolean> {
  if (!r.ok) {
    refusal.value = r.reason
    return false
  }
  refusal.value = null
  return save(r.value)
}

function onChange(next: Page) {
  refusal.value = null
  save(replacePage(layout.value, next))
}

async function onRemove() {
  if (!await commit(removePage(layout.value, props.pageId)))
    return
  editing.value = false
  activeView.value = 'zentrale'
}
</script>

<template>
  <div class="flex h-full min-h-0 flex-col gap-3" :data-testid="`workspace-page-${pageId}`">
    <div v-if="locked" role="alert" data-testid="workspace-locked" class="flex flex-wrap items-center gap-2 rounded-md bg-warning-soft px-3 py-2 text-[12.5px] text-warning-text">
      <p>{{ locked.message }}</p>
      <button v-if="locked.kind === 'unreadable'" type="button" data-testid="workspace-reset" class="rounded-md border border-line-strong px-2.5 py-1" @click="reset">
        Reset to default
      </button>
      <button v-else type="button" data-testid="workspace-retry" class="rounded-md border border-line-strong px-2.5 py-1" @click="retry">
        Retry
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
      @rename="title => commit(renamePage(layout, pageId, title))"
      @remove="onRemove"
    />
    <WorkspaceGrid v-if="loaded && current" class="min-h-0 flex-1" :page="current" :editing="editing" @change="onChange" @refuse="r => (refusal = r)" />
  </div>
</template>
