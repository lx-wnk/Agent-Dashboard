<script setup lang="ts">
import AgentsPanel from './AgentsPanel.vue'
import GitHubPanel from './GitHubPanel.vue'
import MemoryPanel from './MemoryPanel.vue'
import PipelinePanel from './PipelinePanel.vue'
import RoutinesPanel from './RoutinesPanel.vue'
</script>

<template>
  <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3" data-testid="cockpit">
    <AgentsPanel />
    <PipelinePanel />
    <RoutinesPanel />
    <MemoryPanel />
    <GitHubPanel />
  </div>
</template>
