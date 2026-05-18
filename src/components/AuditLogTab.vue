<script setup lang="ts">
import type { AuditEntry } from '../types'
import { onMounted, ref, watch } from 'vue'

const props = defineProps<{
  taskId?: string
  limit?: number
}>()

const entries = ref<AuditEntry[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const expandedId = ref<string | null>(null)

const ACTOR_COLORS: Record<AuditEntry['actor'], string> = {
  user: 'bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-400',
  agent: 'bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-400',
  orchestrator: 'bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-400',
  system: 'bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300',
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
    error.value = e instanceof Error ? e.message : 'Network error'
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
      <span class="font-semibold text-slate-700 dark:text-slate-300 text-[13px]">Audit Log</span>
      <button
        type="button"
        class="px-2 py-0.5 rounded bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 hover:brightness-110 disabled:opacity-40"
        :disabled="loading"
        @click="load"
      >
        Refresh
      </button>
    </div>

    <div v-if="loading" class="text-slate-400 dark:text-slate-600 text-center py-6">
      Loading...
    </div>
    <div v-else-if="error" class="text-red-600 dark:text-red-400">
      {{ error }}
    </div>
    <div v-else-if="entries.length === 0" class="text-slate-400 dark:text-slate-600 text-center py-6">
      No audit entries.
    </div>

    <table v-else class="w-full border-collapse">
      <thead>
        <tr class="text-left text-slate-400 dark:text-slate-600 border-b border-slate-200 dark:border-slate-700">
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
          <tr class="border-b border-slate-100 dark:border-slate-800 hover:bg-slate-50 dark:hover:bg-slate-900/50">
            <td class="py-1.5 pr-3 font-mono text-slate-500 dark:text-slate-400 whitespace-nowrap">
              <time :datetime="entry.timestamp">{{ new Date(entry.timestamp).toLocaleTimeString() }}</time>
            </td>
            <td class="py-1.5 pr-3">
              <span class="px-1.5 py-0.5 rounded-full text-[10px] font-medium" :class="ACTOR_COLORS[entry.actor] ?? ACTOR_COLORS.system">
                {{ entry.actor }}
              </span>
            </td>
            <td class="py-1.5 pr-3 font-mono text-slate-700 dark:text-slate-300">
              {{ entry.action }}
            </td>
            <td class="py-1.5">
              <button
                v-if="entry.details"
                type="button"
                class="text-blue-500 hover:underline"
                @click="toggleDetails(entry.id)"
              >
                {{ expandedId === entry.id ? 'hide' : 'show' }}
              </button>
              <span v-else class="text-slate-300 dark:text-slate-700">—</span>
            </td>
          </tr>
          <tr v-if="expandedId === entry.id && entry.details" :key="`${entry.id}-detail`">
            <td colspan="4" class="pb-2 pl-2">
              <pre class="bg-slate-100 dark:bg-slate-800 rounded p-2 text-[11px] font-mono overflow-x-auto whitespace-pre-wrap">{{ JSON.stringify(entry.details, null, 2) }}</pre>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>
