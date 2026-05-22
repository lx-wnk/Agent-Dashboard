<script setup lang="ts">
defineProps<{ tools: string[] }>()
</script>

<template>
  <div v-if="tools.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Recent Tools
    </h4>
    <div class="flex flex-wrap gap-1">
      <span
        v-for="(tool, i) in tools"
        :key="`${i}-${tool}`"
        class="text-[11px] px-2 py-0.5 rounded bg-raised text-fg-mute font-mono"
      >
        {{ tool }}
      </span>
    </div>
  </div>
</template>
