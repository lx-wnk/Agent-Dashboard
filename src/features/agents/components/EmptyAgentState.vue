<script setup lang="ts">
defineProps<{
  searchQuery: string
}>()
</script>

<template>
  <div
    class="flex flex-col items-center justify-center py-24 text-center gap-3"
    role="status"
    aria-label="No agents found"
  >
    <span class="text-4xl" aria-hidden="true">🤖</span>
    <p class="text-fg-mute text-sm font-medium">
      {{ searchQuery ? 'No agents match your search.' : 'No agents are currently running.' }}
    </p>
    <p v-if="!searchQuery" class="text-fg-mute text-xs">
      Start a Claude Code session or click <strong>+ New Agent</strong> to spawn one.
    </p>
  </div>
</template>
