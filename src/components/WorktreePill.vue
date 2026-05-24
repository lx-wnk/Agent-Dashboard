<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useWorktreeStatus } from '../composables/useWorktreeStatus'

const props = defineProps<{
  taskId: string
  /**
   * Optional. When false, the pill stays mounted but suspends polling.
   * Defaults to true — the parent (card/list) is rendered, so we
   * fetch once and keep a 30 s refresh going. Card consumers MAY
   * later flip this on intersection-observer; v1 simply polls.
   */
  active?: boolean
}>()

const BRANCH_MAX = 20

const taskIdRef = ref<string | null>(props.taskId)
watch(() => props.taskId, (v) => {
  taskIdRef.value = v
})
const activeRef = computed(() => props.active !== false)

const { status } = useWorktreeStatus(taskIdRef, activeRef)

const truncatedBranch = computed(() => {
  const b = status.value?.branch
  if (!b)
    return ''
  return b.length > BRANCH_MAX ? `${b.slice(0, BRANCH_MAX - 1)}…` : b
})

const showAheadBehind = computed(() => {
  const s = status.value
  if (!s)
    return false
  return s.ahead != null || s.behind != null
})
</script>

<template>
  <span
    v-if="status"
    class="inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-px rounded border bg-raised text-fg-mute border-line"
    :title="`Worktree on branch ${status.branch}${
      status.ahead != null || status.behind != null
        ? ` — ahead ${status.ahead ?? 0}, behind ${status.behind ?? 0}`
        : ''
    }${status.dirty ? ` — ${status.fileCount} dirty file${status.fileCount === 1 ? '' : 's'}` : ''}`"
    data-testid="worktree-pill"
  >
    <span aria-hidden="true">⎇</span>
    <span data-testid="worktree-pill-branch">{{ truncatedBranch }}</span>
    <span
      v-if="showAheadBehind"
      class="flex items-center gap-0.5"
      data-testid="worktree-pill-counts"
    >
      <template v-if="status.ahead != null">
        <span aria-label="ahead" class="text-fg">↑{{ status.ahead }}</span>
      </template>
      <template v-if="status.behind != null">
        <span aria-label="behind" class="text-fg">↓{{ status.behind }}</span>
      </template>
    </span>
    <span
      v-if="status.dirty"
      aria-label="dirty"
      class="inline-block w-1.5 h-1.5 rounded-full bg-amber-500"
      data-testid="worktree-pill-dirty"
    />
  </span>
</template>
