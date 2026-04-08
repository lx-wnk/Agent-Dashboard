<template>
  <div class="tool-timeline" v-if="tools.length > 0">
    <h4>Recent Tools</h4>
    <div class="tools">
      <span v-for="tool in tools" :key="tool" class="tool-tag">{{ tool }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  tools: string[]
}>()
</script>

<style scoped>
h4 {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 8px;
}

.tools {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.tool-tag {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  font-family: monospace;
}
</style>
