<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import AgentsPanel from './AgentsPanel.vue'
import MemoryPanel from './MemoryPanel.vue'
import PipelinePanel from './PipelinePanel.vue'
import RoutinesPanel from './RoutinesPanel.vue'

// GitHub is the only panel that reaches a third party; loading it on demand
// keeps its composable out of the first-load chunk, matching how App.vue
// splits the heavy views.
const GitHubPanel = defineAsyncComponent(() => import('./GitHubPanel.vue'))
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
