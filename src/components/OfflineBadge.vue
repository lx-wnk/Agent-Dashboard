<script setup lang="ts">
import { usePendingMessages } from '../composables/usePendingMessages'

const { pendingCount } = usePendingMessages()
</script>

<template>
  <span
    v-if="pendingCount > 0"
    class="inline-flex items-center gap-1 text-[11px] font-mono px-2 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400 border border-amber-300 dark:border-amber-700 whitespace-nowrap"
    title="Messages queued while offline — will be sent on reconnect"
    aria-label="`${pendingCount} message${pendingCount === 1 ? '' : 's'} queued offline`"
    role="status"
  >
    <span aria-hidden="true">⏳</span>
    {{ pendingCount }} queued
  </span>
</template>
