<script setup lang="ts">
defineProps<{ tools: string[] }>()
</script>

<template>
  <div v-if="tools.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
      Recent Tools
    </h4>
    <div class="flex flex-wrap gap-1">
      <span
        v-for="(tool, i) in tools"
        :key="`${i}-${tool}`"
        class="text-[11px] px-2 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 font-mono"
      >
        {{ tool }}
      </span>
    </div>
  </div>
</template>
