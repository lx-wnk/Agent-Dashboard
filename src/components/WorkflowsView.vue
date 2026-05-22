<script setup lang="ts">
import { ref } from 'vue'
import { useWorkflows, type WorkflowsFilters, type WorkflowTab } from '../composables/useWorkflows'
import SankeyChart from './visualizations/SankeyChart.vue'
import SessionDagChart from './visualizations/SessionDagChart.vue'
import SpawnTreeChart from './visualizations/SpawnTreeChart.vue'
import CoOccurrenceMatrix from './visualizations/CoOccurrenceMatrix.vue'

const emit = defineEmits<{ navigate: [sessionId: string] }>()

const filters = ref<WorkflowsFilters>({})

const { sankey, dag, spawnTree, coOccurrence, activeTab, setActiveTab } = useWorkflows(filters)

interface TabDescriptor {
  key: WorkflowTab
  label: string
  requiresSession?: boolean
}

const TABS: TabDescriptor[] = [
  { key: 'sankey', label: 'Sankey' },
  { key: 'dag', label: 'Session DAG', requiresSession: true },
  { key: 'spawnTree', label: 'Spawn Tree' },
  { key: 'coOccurrence', label: 'Co-occurrence' },
]
</script>

<template>
  <div class="workflows-view">
    <div class="flex flex-wrap items-center gap-3 px-4 py-3 border-b border-line bg-card">
      <label class="text-xs text-fg-mute flex items-center gap-1">
        Session
        <input
          v-model="filters.sessionId"
          type="text"
          class="bg-raised border border-line rounded-md px-2 py-1 text-[12px] text-fg w-[240px]"
          placeholder="Session ID (required for DAG)"
        >
      </label>
      <label class="text-xs text-fg-mute flex items-center gap-1">
        From
        <input
          v-model="filters.from"
          type="datetime-local"
          class="bg-raised border border-line rounded-md px-2 py-1 text-[12px] text-fg"
        >
      </label>
      <label class="text-xs text-fg-mute flex items-center gap-1">
        To
        <input
          v-model="filters.to"
          type="datetime-local"
          class="bg-raised border border-line rounded-md px-2 py-1 text-[12px] text-fg"
        >
      </label>
    </div>

    <div role="tablist" class="flex gap-1 px-4 py-2 border-b border-line bg-card">
      <button
        v-for="tab in TABS"
        :key="tab.key"
        type="button"
        role="tab"
        :aria-selected="activeTab === tab.key"
        class="text-xs px-3 py-1 rounded-md border-none cursor-pointer"
        :class="activeTab === tab.key ? 'bg-blue-600 text-white' : 'bg-raised text-fg-mute hover:text-fg-soft'"
        :disabled="tab.requiresSession && !filters.sessionId"
        :title="tab.requiresSession && !filters.sessionId ? 'Enter a session ID to enable this tab' : ''"
        @click="setActiveTab(tab.key)"
      >
        {{ tab.label }}
      </button>
    </div>

    <div class="p-4">
      <SankeyChart
        v-show="activeTab === 'sankey'"
        :data="sankey.data"
        :loading="sankey.loading"
        :error="sankey.error"
      />
      <SessionDagChart
        v-show="activeTab === 'dag'"
        :data="dag.data"
        :loading="dag.loading"
        :error="dag.error"
      />
      <SpawnTreeChart
        v-show="activeTab === 'spawnTree'"
        :data="spawnTree.data"
        :loading="spawnTree.loading"
        :error="spawnTree.error"
        @navigate="(id) => emit('navigate', id)"
      />
      <CoOccurrenceMatrix
        v-show="activeTab === 'coOccurrence'"
        :data="coOccurrence.data"
        :loading="coOccurrence.loading"
        :error="coOccurrence.error"
      />
    </div>
  </div>
</template>
