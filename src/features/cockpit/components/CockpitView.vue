<script setup lang="ts">
import AgentsPanel from './AgentsPanel.vue'
import MemoryPanel from './MemoryPanel.vue'
import PipelinePanel from './PipelinePanel.vue'
import RoutinesPanel from './RoutinesPanel.vue'

// A fifth panel (GitHub, Task 8) slots in here later — the grid below does not
// assume a fixed panel count, it just reflows whatever children it has.
</script>

<template>
  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3" data-testid="cockpit">
    <AgentsPanel />
    <PipelinePanel />
    <RoutinesPanel />
    <MemoryPanel />
  </div>
</template>
