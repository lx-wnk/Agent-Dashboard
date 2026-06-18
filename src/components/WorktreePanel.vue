<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useWorktreeStatus } from '../composables/useWorktreeStatus'
import { EDITOR_SCHEMES, editorHref, loadEditorScheme, saveEditorScheme } from '../utils/worktree'

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

const { status, isLoading, error, refresh, create, remove } = useWorktreeStatus(taskIdRef, activeRef)

const editorScheme = ref(loadEditorScheme())
watch(editorScheme, v => saveEditorScheme(v))

const editorHrefComputed = computed(() => editorHref(props.worktreePath, editorScheme.value))

const copySuccess = ref(false)
const confirmingRemove = ref(false)

async function copyPath(): Promise<void> {
  if (!props.worktreePath)
    return
  await navigator.clipboard.writeText(props.worktreePath)
  copySuccess.value = true
  setTimeout(() => {
    copySuccess.value = false
  }, 2000)
}

async function handleRemove(): Promise<void> {
  if (status.value?.dirty) {
    confirmingRemove.value = true
    return
  }
  const code = await remove(false)
  if (code === 409) {
    confirmingRemove.value = true
  }
}

async function confirmRemove(): Promise<void> {
  confirmingRemove.value = false
  await remove(true)
}

async function handleCreate(): Promise<void> {
  await create()
}
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

    <p v-if="error" class="text-[11px] text-danger-text">
      {{ error }}
    </p>

    <!-- No worktree yet — offer to create one -->
    <template v-if="!worktreePath">
      <p class="text-[11px] text-fg-mute italic">
        No worktree for this task.
      </p>
      <button
        type="button"
        class="self-start text-[11px] px-2 py-1 rounded border bg-raised text-fg-soft border-line hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors disabled:opacity-50"
        :disabled="isLoading"
        data-testid="worktree-create-btn"
        @click="handleCreate"
      >
        {{ isLoading ? 'Creating…' : 'Create worktree' }}
      </button>
    </template>

    <template v-else>
      <p v-if="!status && !isLoading" class="text-[11px] text-fg-mute italic">
        No worktree status available.
      </p>

      <dl v-if="status" class="grid grid-cols-[auto_1fr] gap-y-1.5 gap-x-4 text-[13px]">
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
              <span class="text-success-text">↑ {{ status.ahead ?? 0 }}</span>
              <span class="mx-2 text-fg-mute">·</span>
              <span class="text-info-text">↓ {{ status.behind ?? 0 }}</span>
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
              class="ml-2 inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-px rounded border bg-warning-soft text-warning-text border-warning-line"
            >
              <span aria-hidden="true">●</span> dirty
            </span>
            <span
              v-else
              class="ml-2 inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-px rounded border bg-raised text-fg-mute border-line"
            >clean</span>
          </dd>
        </div>
        <div class="contents">
          <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
            Path
          </dt>
          <dd class="font-mono text-xs text-fg truncate">
            <a
              v-if="editorHrefComputed"
              :href="editorHrefComputed"
              class="hover:underline"
              target="_blank"
              rel="noopener noreferrer"
              :title="worktreePath ?? ''"
            >{{ worktreePath }}</a>
            <span v-else>{{ worktreePath }}</span>
          </dd>
        </div>
      </dl>

      <!-- Actions row -->
      <div class="flex flex-wrap items-center gap-1.5">
        <!-- Copy path -->
        <button
          type="button"
          class="text-[11px] px-2 py-1 rounded border bg-raised text-fg-soft border-line hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
          aria-label="Copy worktree path"
          data-testid="worktree-copy-btn"
          @click="copyPath"
        >
          {{ copySuccess ? 'Copied' : 'Copy path' }}
        </button>

        <!-- Open in editor -->
        <a
          v-if="editorHrefComputed"
          :href="editorHrefComputed"
          target="_blank"
          rel="noopener noreferrer"
          class="text-[11px] px-2 py-1 rounded border bg-raised text-fg-soft border-line hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
          data-testid="worktree-open-btn"
        >
          Open
        </a>
        <select
          v-model="editorScheme"
          class="text-[11px] bg-app border border-line rounded px-1 py-0.5 text-fg-soft focus:outline-none focus:border-blue-500"
          aria-label="Editor scheme"
          data-testid="worktree-editor-scheme"
        >
          <option v-for="s in EDITOR_SCHEMES" :key="s.id" :value="s.id">
            {{ s.label }}
          </option>
        </select>

        <!-- Remove worktree -->
        <template v-if="!confirmingRemove">
          <button
            type="button"
            class="text-[11px] px-2 py-1 rounded border bg-raised text-fg-soft border-line hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors disabled:opacity-50"
            :disabled="isLoading"
            data-testid="worktree-remove-btn"
            @click="handleRemove"
          >
            Remove
          </button>
        </template>

        <!-- Dirty confirm prompt -->
        <template v-else>
          <span class="text-[11px] text-amber-600 dark:text-amber-400">
            Discard {{ status?.fileCount ?? 0 }} uncommitted change{{ (status?.fileCount ?? 0) === 1 ? '' : 's' }}?
          </span>
          <button
            type="button"
            class="text-[11px] px-2 py-1 rounded border bg-raised text-red-600 dark:text-red-400 border-red-300/60 dark:border-red-700/60 hover:bg-red-50 dark:hover:bg-red-950/30 transition-colors"
            data-testid="worktree-confirm-remove-btn"
            @click="confirmRemove"
          >
            Remove
          </button>
          <button
            type="button"
            class="text-[11px] px-2 py-1 rounded border bg-raised text-fg-soft border-line hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors"
            data-testid="worktree-cancel-remove-btn"
            @click="confirmingRemove = false"
          >
            Cancel
          </button>
        </template>
      </div>

      <p
        v-if="status"
        class="text-[10px] text-fg-mute"
      >
        Refreshes every 30 s while this modal is open.
      </p>
    </template>
  </section>
</template>
