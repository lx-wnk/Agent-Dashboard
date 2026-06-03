<script setup lang="ts">
defineProps<{ live: boolean }>()
</script>

<template>
  <div role="status" class="flex items-center gap-1.5 text-[11px]" :aria-label="live ? 'Live updates connected' : 'Reconnecting to live updates'">
    <span
      data-dot
      class="w-2 h-2 rounded-full"
      :class="live
        ? 'bg-green-500 animate-pulse motion-reduce:animate-none'
        : 'bg-yellow-500'"
      aria-hidden="true"
    />
    <span :class="live ? 'text-fg-mute' : 'text-yellow-600 dark:text-yellow-400'">
      {{ live ? 'Live' : 'Reconnecting…' }}
    </span>
  </div>
</template>
