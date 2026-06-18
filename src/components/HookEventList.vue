<script setup lang="ts">
import type { HookEvent } from '../types'

defineProps<{ events: HookEvent[] }>()

function formatTime(at: string): string {
  if (!at)
    return ''
  const d = new Date(at)
  return Number.isNaN(d.getTime()) ? at : d.toLocaleTimeString()
}
</script>

<template>
  <div v-if="events.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Hook events
    </h4>
    <ul class="flex flex-col gap-1">
      <li
        v-for="(event, i) in events"
        :key="`${i}-${event.at}`"
        class="flex items-baseline gap-2 text-[11px]"
      >
        <span class="px-1.5 py-0.5 rounded bg-raised text-fg-mute font-mono shrink-0">
          {{ event.type || 'event' }}
        </span>
        <span v-if="event.tool" class="font-mono text-fg-soft shrink-0">{{ event.tool }}</span>
        <span v-if="event.summary" class="text-fg-mute truncate" :title="event.summary">{{ event.summary }}</span>
        <span class="ml-auto text-fg-mute tabular-nums shrink-0">{{ formatTime(event.at) }}</span>
      </li>
    </ul>
  </div>
</template>
