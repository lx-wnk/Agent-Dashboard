<script setup lang="ts">
import type { Checkpoint } from '../../composables/useCheckpoints'
import { formatRelativeActivity, secondsSince } from '../../utils/format'

defineProps<{
  taskId: string
  checkpoints: Checkpoint[]
  loading: boolean
}>()
const emit = defineEmits<{ revert: [cpId: string] }>()

function confirmRevert(cpId: string) {
  // eslint-disable-next-line no-alert -- native confirm guard for a destructive revert
  if (globalThis.confirm('Revert the worktree to this checkpoint? The current state will be saved as a pre-revert checkpoint.'))
    emit('revert', cpId)
}

function relativeTime(iso: string) {
  return formatRelativeActivity(secondsSince(iso))
}
</script>

<template>
  <section class="p-5 space-y-4">
    <div v-if="loading" class="text-sm text-fg-mute">
      Loading…
    </div>
    <div v-else-if="checkpoints.length === 0" class="text-sm text-fg-mute">
      No checkpoints yet — checkpoints are captured automatically while the agent edits the worktree.
    </div>
    <ul v-else class="space-y-2">
      <li
        v-for="cp in checkpoints"
        :key="cp.id"
        data-testid="checkpoint-row"
        class="flex items-center justify-between rounded border border-line p-3 text-sm"
      >
        <div class="space-y-0.5">
          <span class="font-mono font-semibold text-fg">
            #{{ cp.seq }}
            <span v-if="cp.preRevert" class="ml-1 text-xs text-warn-text">(pre-revert)</span>
          </span>
          <div class="text-xs text-fg-mute">
            {{ cp.filesChanged }} file{{ cp.filesChanged !== 1 ? 's' : '' }} · {{ relativeTime(cp.createdAt) }}
          </div>
        </div>
        <button
          type="button"
          :data-testid="`revert-btn-${cp.id}`"
          class="rounded px-2 py-1 text-xs font-medium bg-danger-subtle text-danger hover:bg-danger-muted"
          @click="confirmRevert(cp.id)"
        >
          Revert
        </button>
      </li>
    </ul>
  </section>
</template>
