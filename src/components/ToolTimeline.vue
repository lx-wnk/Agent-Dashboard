<script setup lang="ts">
defineProps<{
  tools: string[]
}>()
</script>

<template>
  <div v-if="tools.length > 0" class="tool-timeline">
    <h4>Recent Tools</h4>
    <div class="tools">
      <span v-for="(tool, i) in tools" :key="`${i}-${tool}`" class="tool-tag">{{ tool }}</span>
    </div>
  </div>
</template>

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
  font-family: var(--font-mono);
}
</style>
