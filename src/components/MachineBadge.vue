<script setup lang="ts">
defineProps<{ machine: string }>()
</script>

<template>
  <span class="machine-badge" :title="`Machine: ${machine}`">{{ machine }}</span>
</template>

<style scoped>
.machine-badge {
  display: inline-block;
  font-size: 9px;
  font-weight: 600;
  color: var(--accent-blue);
  border: 1px solid var(--accent-blue);
  border-radius: 3px;
  padding: 0 4px;
  max-width: 100px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: middle;
}
</style>
