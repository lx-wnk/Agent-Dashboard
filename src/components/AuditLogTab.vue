<script setup lang="ts">
import type { AuditEntry } from '../types'
import { onMounted, ref, watch } from 'vue'
import { errorMessage } from '../utils/errorMessage'

const props = withDefaults(defineProps<{
  taskId?: string
  limit?: number
  hideTitle?: boolean
}>(), { hideTitle: false })

const entries = ref<AuditEntry[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const expandedId = ref<string | null>(null)

const ACTOR_COLORS: Record<AuditEntry['actor'], string> = {
  user: 'bg-info-soft text-info-text',
  agent: 'bg-success-soft text-success-text',
  orchestrator: 'bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-400',
  system: 'bg-raised text-fg-soft',
}

async function load() {
  loading.value = true
  error.value = null
  try {
    const url = props.taskId
      ? `/api/tasks/${props.taskId}/audit`
      : `/api/audit?limit=${props.limit ?? 100}`
    const res = await fetch(url)
    if (!res.ok) {
      error.value = `HTTP ${res.status}`
      return
    }
    entries.value = await res.json()
  }
  catch (e) {
    error.value = errorMessage(e, 'Network error')
  }
  finally {
    loading.value = false
  }
}

function toggleDetails(id: string) {
  expandedId.value = expandedId.value === id ? null : id
}

onMounted(load)
watch(() => props.taskId, load)
</script>

<template>
  <div class="text-xs">
    <div class="flex items-center justify-between mb-3">
      <span v-if="!hideTitle" class="font-semibold text-fg-soft text-[13px]">Audit Log</span>
      <button
        type="button"
        class="ml-auto px-2 py-0.5 rounded bg-raised text-fg-soft hover:brightness-110 disabled:opacity-40"
        :disabled="loading"
        @click="load"
      >
        Refresh
      </button>
    </div>

    <div v-if="loading" class="text-fg-mute text-center py-6">
      Loading...
    </div>
    <div v-else-if="error" class="text-danger-text">
      {{ error }}
    </div>
    <div v-else-if="entries.length === 0" class="text-fg-mute text-center py-6">
      No audit entries.
    </div>

    <table v-else class="w-full border-collapse">
      <thead>
        <tr class="text-left text-fg-mute border-b border-line">
          <th class="pb-1.5 pr-3 font-medium">
            Time
          </th>
          <th class="pb-1.5 pr-3 font-medium">
            Actor
          </th>
          <th class="pb-1.5 pr-3 font-medium">
            Action
          </th>
          <th class="pb-1.5 font-medium">
            Details
          </th>
        </tr>
      </thead>
      <tbody>
        <template v-for="entry in entries" :key="entry.id">
          <tr class="border-b border-line last:border-b-0 hover:bg-app/50">
            <td class="py-1.5 pr-3 font-mono text-fg-mute whitespace-nowrap">
              <time :datetime="entry.timestamp">{{ new Date(entry.timestamp).toLocaleTimeString() }}</time>
            </td>
            <td class="py-1.5 pr-3">
              <span class="px-1.5 py-0.5 rounded-full text-[10px] font-medium" :class="ACTOR_COLORS[entry.actor] ?? ACTOR_COLORS.system">
                {{ entry.actor }}
              </span>
            </td>
            <td class="py-1.5 pr-3 font-mono text-fg-soft">
              {{ entry.action }}
            </td>
            <td class="py-1.5">
              <button
                v-if="entry.details"
                type="button"
                class="text-accent hover:underline"
                @click="toggleDetails(entry.id)"
              >
                {{ expandedId === entry.id ? 'hide' : 'show' }}
              </button>
              <span v-else class="text-fg-faint">—</span>
            </td>
          </tr>
          <tr v-if="expandedId === entry.id && entry.details" :key="`${entry.id}-detail`">
            <td colspan="4" class="pb-2 pl-2">
              <pre class="bg-raised text-fg-soft rounded p-2 text-[11px] font-mono overflow-x-auto whitespace-pre-wrap">{{ JSON.stringify(entry.details, null, 2) }}</pre>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>
