<script setup lang="ts">
defineProps<{ machine: string }>()
</script>

<template>
  <span
    :title="`Machine: ${machine}`"
    class="inline-block text-[9px] font-semibold text-blue-600 dark:text-blue-400 border border-blue-600 dark:border-blue-400 rounded px-1 max-w-[100px] overflow-hidden text-ellipsis whitespace-nowrap align-middle"
  >
    {{ machine }}
  </span>
</template>
