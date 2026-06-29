<script setup lang="ts">
import { watch } from 'vue'
import { useInjectedTask } from '../../composables/taskModalContext'
import { useTaskCoordination } from '../../composables/useTaskCoordination'
import { toast } from '../../composables/useToast'

const task = useInjectedTask()
const { scratchpads, locks, loading, error } = useTaskCoordination(task)

// Surface async load failures as toasts; the panel keeps its empty/loading state.
watch(error, (msg) => {
  if (msg)
    toast.error(msg)
})
</script>

<template>
  <section class="p-5 space-y-6">
    <div v-if="loading" class="text-sm text-fg-mute">
      Loading...
    </div>
    <template v-else>
      <div>
        <h3 class="text-[12px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
          Scratchpads
        </h3>
        <div v-if="scratchpads.length === 0" class="text-sm text-fg-mute">
          No scratchpad entries
        </div>
        <dl v-else class="space-y-2">
          <div v-for="entry in scratchpads" :key="entry.key" class="text-sm">
            <dt class="font-mono font-semibold text-fg">
              {{ entry.key }}
            </dt>
            <dd class="font-mono text-fg-soft ml-3">
              {{ entry.value }}
            </dd>
            <dd class="text-xs text-fg-mute ml-3">
              by {{ entry.updated_by_task_id }}
            </dd>
          </div>
        </dl>
      </div>

      <div>
        <h3 class="text-[12px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
          Active locks
        </h3>
        <div v-if="locks.length === 0" class="text-sm text-fg-mute">
          No active locks
        </div>
        <dl v-else class="space-y-2">
          <div v-for="lock in locks" :key="lock.key" class="text-sm">
            <dt class="font-mono font-semibold text-fg">
              {{ lock.key }}
            </dt>
            <dd class="font-mono text-fg-soft ml-3">
              owner: {{ lock.owner_task_id }}
            </dd>
            <dd class="text-xs text-fg-mute ml-3">
              expires: {{ lock.expires_at }}
            </dd>
          </div>
        </dl>
      </div>
    </template>
  </section>
</template>
