<script setup lang="ts">
import type { GitStatus } from '../types'
import { onMounted, ref, watch } from 'vue'

const props = defineProps<{ taskId: string }>()

const status = ref<GitStatus | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const actionInFlight = ref(false)
const actionSuccess = ref<string | null>(null)
const actionError = ref<string | null>(null)

function formatRelativeDate(dateStr: string): string {
  const now = Date.now()
  const then = new Date(dateStr).getTime()
  if (Number.isNaN(then))
    return dateStr
  const diffMs = now - then
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60)
    return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60)
    return `${diffMin} min ago`
  const diffHours = Math.floor(diffMin / 60)
  if (diffHours < 24)
    return `${diffHours} hour${diffHours === 1 ? '' : 's'} ago`
  const diffDays = Math.floor(diffHours / 24)
  return `${diffDays} day${diffDays === 1 ? '' : 's'} ago`
}

async function fetchStatus(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const res = await fetch(`/api/tasks/${props.taskId}/git-status`)
    if (!res.ok) {
      error.value = `Failed to load git status (${res.status})`
      return
    }
    status.value = await res.json()
  }
  catch {
    error.value = 'Failed to load git status'
  }
  finally {
    loading.value = false
  }
}

async function runAction(action: 'fetch' | 'pull'): Promise<void> {
  if (actionInFlight.value)
    return
  actionInFlight.value = true
  actionSuccess.value = null
  actionError.value = null
  try {
    const res = await fetch(`/api/tasks/${props.taskId}/git-action`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action }),
    })
    if (!res.ok) {
      const body = await res.json().catch(() => ({}))
      actionError.value = (body as { error?: string }).error ?? `Action failed (${res.status})`
      return
    }
    actionSuccess.value = action === 'fetch' ? 'Fetched' : 'Pulled'
    await fetchStatus()
    setTimeout(() => {
      actionSuccess.value = null
    }, 2000)
  }
  catch {
    actionError.value = `${action} failed`
  }
  finally {
    actionInFlight.value = false
  }
}

onMounted(fetchStatus)
watch(() => props.taskId, fetchStatus)
</script>

<template>
  <div class="text-sm rounded-md border border-line bg-app px-3.5 py-3">
    <!-- Loading state -->
    <div v-if="loading && !status" class="text-fg-mute text-xs animate-pulse">
      Loading git status…
    </div>

    <!-- Error state -->
    <div v-else-if="error && !status" class="text-red-500 dark:text-red-400 text-xs">
      {{ error }}
    </div>

    <!-- Content -->
    <template v-else-if="status">
      <!-- Branch + ahead/behind -->
      <div class="flex items-center justify-between gap-3 mb-2.5">
        <div class="flex items-center gap-1.5 flex-wrap">
          <span class="font-mono text-xs bg-raised text-fg-soft px-2 py-0.5 rounded font-semibold">
            {{ status.branch }}
          </span>
          <span v-if="status.aheadCount > 0" class="text-[11px] text-green-600 dark:text-green-400 font-mono">
            ↑{{ status.aheadCount }}
          </span>
          <span v-if="status.behindCount > 0" class="text-[11px] text-yellow-600 dark:text-yellow-400 font-mono">
            ↓{{ status.behindCount }}
          </span>
        </div>

        <!-- Action buttons -->
        <div class="flex items-center gap-1.5 shrink-0">
          <button
            type="button"
            class="px-2 py-1 rounded text-[11px] font-medium bg-raised text-fg-soft border border-line hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="actionInFlight"
            @click="runAction('fetch')"
          >
            Fetch
          </button>
          <button
            type="button"
            class="px-2 py-1 rounded text-[11px] font-medium bg-raised text-fg-soft border border-line hover:bg-slate-200 dark:hover:bg-slate-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="actionInFlight"
            @click="runAction('pull')"
          >
            Pull (ff-only)
          </button>
        </div>
      </div>

      <!-- Last commit -->
      <div v-if="status.lastCommit" class="mb-2.5 flex items-baseline gap-2 flex-wrap">
        <code class="font-mono text-[11px] text-fg-mute">{{ status.lastCommit.shortHash }}</code>
        <span class="text-xs text-fg-soft flex-1 min-w-0 truncate">{{ status.lastCommit.message }}</span>
        <span class="text-[11px] text-fg-mute shrink-0">{{ status.lastCommit.author }}</span>
        <span class="text-[11px] text-fg-mute shrink-0">{{ formatRelativeDate(status.lastCommit.date) }}</span>
      </div>

      <!-- File counts -->
      <div
        v-if="status.staged.length > 0 || status.unstaged.length > 0 || status.untracked.length > 0"
        class="flex items-center gap-3 mb-2.5 flex-wrap"
      >
        <span v-if="status.staged.length > 0" class="text-[11px] text-green-600 dark:text-green-400 font-medium">
          {{ status.staged.length }} staged
        </span>
        <span v-if="status.unstaged.length > 0" class="text-[11px] text-yellow-600 dark:text-yellow-400 font-medium">
          {{ status.unstaged.length }} unstaged
        </span>
        <span v-if="status.untracked.length > 0" class="text-[11px] text-fg-mute">
          {{ status.untracked.length }} untracked
        </span>
      </div>

      <!-- Clean working tree -->
      <div
        v-else-if="status.aheadCount === 0 && status.behindCount === 0"
        class="text-[11px] text-fg-mute mb-2.5"
      >
        Clean working tree
      </div>

      <!-- Remote URL -->
      <div v-if="status.remoteUrl" class="text-[10px] text-fg-faint font-mono truncate">
        {{ status.remoteUrl }}
      </div>

      <!-- Action feedback -->
      <div v-if="actionSuccess" class="mt-2 text-[11px] text-green-600 dark:text-green-400">
        {{ actionSuccess }}
      </div>
      <div v-if="actionError" class="mt-2 text-[11px] text-red-500 dark:text-red-400">
        {{ actionError }}
      </div>
    </template>
  </div>
</template>
