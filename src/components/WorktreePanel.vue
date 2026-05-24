<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useWorktreeStatus } from '../composables/useWorktreeStatus'

const props = defineProps<{
  taskId: string
  worktreePath: string | null
  /** Tied to modal-open state by the caller so the 30 s poll only runs while open. */
  active: boolean
}>()

const taskIdRef = ref<string | null>(props.taskId)
watch(() => props.taskId, (v) => {
  taskIdRef.value = v
})
const activeRef = ref(props.active)
watch(() => props.active, (v) => {
  activeRef.value = v
})

const { status, isLoading, error, refresh } = useWorktreeStatus(taskIdRef, activeRef)

const editorHref = computed(() => {
  if (!props.worktreePath)
    return null
  // file:// URL — caller's OS resolves it. Works for VS Code (vscode://file/…)
  // would require a custom scheme; default to file:// so the browser at least
  // surfaces a download/reveal action. Optional link only.
  return `file://${props.worktreePath}`
})

const aheadBehindLabel = computed(() => {
  const s = status.value
  if (!s)
    return null
  if (s.ahead == null && s.behind == null)
    return 'no base'
  return `↑ ${s.ahead ?? 0}   ↓ ${s.behind ?? 0}`
})
</script>

<template>
  <section class="border-t border-line pt-3 flex flex-col gap-2" data-testid="worktree-panel">
    <div class="flex items-center justify-between">
      <h4 class="text-[11px] font-semibold uppercase tracking-[0.5px] text-fg-mute">
        Worktree
      </h4>
      <button
        type="button"
        class="text-[10px] uppercase tracking-[0.5px] text-fg-mute hover:text-fg disabled:opacity-50"
        :disabled="isLoading"
        title="Refresh worktree status"
        @click="refresh"
      >
        {{ isLoading ? 'Refreshing…' : 'Refresh' }}
      </button>
    </div>

    <p v-if="error" class="text-[11px] text-red-600 dark:text-red-400">
      {{ error }}
    </p>

    <p v-else-if="!status && !isLoading" class="text-[11px] text-fg-mute italic">
      No worktree status available.
    </p>

    <dl v-else-if="status" class="grid grid-cols-[auto_1fr] gap-y-1.5 gap-x-4 text-[13px]">
      <div class="contents">
        <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
          Branch
        </dt>
        <dd class="font-mono text-xs text-fg truncate" data-testid="worktree-panel-branch">
          {{ status.branch }}
        </dd>
      </div>
      <div class="contents">
        <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
          Ahead / Behind
        </dt>
        <dd class="font-mono text-xs text-fg" data-testid="worktree-panel-ahead-behind">
          <span v-if="status.ahead == null && status.behind == null" class="text-fg-mute italic">
            no base
          </span>
          <span v-else>
            <span class="text-green-600 dark:text-green-400">↑ {{ status.ahead ?? 0 }}</span>
            <span class="mx-2 text-fg-mute">·</span>
            <span class="text-blue-600 dark:text-blue-400">↓ {{ status.behind ?? 0 }}</span>
          </span>
        </dd>
      </div>
      <div class="contents">
        <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
          Files
        </dt>
        <dd class="text-fg" data-testid="worktree-panel-files">
          {{ status.fileCount }}
          <span
            v-if="status.dirty"
            class="ml-2 inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-px rounded border bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400 border-amber-300/60 dark:border-amber-700/60"
          >
            <span aria-hidden="true">●</span> dirty
          </span>
          <span
            v-else
            class="ml-2 inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-px rounded border bg-raised text-fg-mute border-line"
          >clean</span>
        </dd>
      </div>
      <div v-if="worktreePath" class="contents">
        <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
          Path
        </dt>
        <dd class="font-mono text-xs text-fg truncate">
          <a
            v-if="editorHref"
            :href="editorHref"
            class="hover:underline"
            target="_blank"
            rel="noopener noreferrer"
            :title="worktreePath ?? ''"
          >{{ worktreePath }}</a>
          <span v-else>{{ worktreePath }}</span>
        </dd>
      </div>
    </dl>

    <p
      v-if="status && (isLoading || aheadBehindLabel)"
      class="text-[10px] text-fg-mute"
    >
      Refreshes every 30 s while this modal is open.
    </p>
  </section>
</template>
