<script setup lang="ts">
import type { WorkflowsFilters, WorkflowTab } from '../composables/useWorkflows'
import { onMounted, ref, watch } from 'vue'
import { useSessions } from '../composables/useSessions'
import { defaultWorkflowsFilters, useWorkflows } from '../composables/useWorkflows'
import CoOccurrenceMatrix from './visualizations/CoOccurrenceMatrix.vue'
import SankeyChart from './visualizations/SankeyChart.vue'
import SessionDagChart from './visualizations/SessionDagChart.vue'
import SpawnTreeChart from './visualizations/SpawnTreeChart.vue'

defineEmits<{ navigate: [sessionId: string] }>()

const filters = ref<WorkflowsFilters>(defaultWorkflowsFilters())

const { sankey, dag, spawnTree, coOccurrence, activeTab, setActiveTab } = useWorkflows(filters)
const { sessions, refetch: refetchSessions } = useSessions()

onMounted(() => {
  void refetchSessions()
})

watch(() => filters.value.sessionId, (id) => {
  if (!id && activeTab.value === 'dag') {
    setActiveTab('sankey')
  }
})

function sessionLabel(s: { projectName: string, firstPrompt: string | null, sessionId: string, isRunning: boolean }): string {
  const shortId = s.sessionId ? s.sessionId.slice(0, 8) : '????????'
  const base = s.firstPrompt
    ? `${s.projectName} · ${s.firstPrompt.slice(0, 50)}${s.firstPrompt.length > 50 ? '…' : ''}`
    : `${s.projectName} · ${shortId}`
  return s.isRunning ? `${base} (running)` : base
}

function onSessionSelect(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  if (value) {
    setActiveTab('dag')
  }
}

function openSessionDag(id: string) {
  filters.value.sessionId = id
  setActiveTab('dag')
}

function onReset() {
  if (activeTab.value === 'dag') {
    setActiveTab('sankey')
  }
  filters.value = defaultWorkflowsFilters()
}

interface TabDescriptor {
  key: WorkflowTab
  label: string
}

const TABS: TabDescriptor[] = [
  { key: 'sankey', label: 'Sankey' },
  { key: 'spawnTree', label: 'Spawn Tree' },
  { key: 'coOccurrence', label: 'Co-occurrence' },
]
</script>

<template>
  <div class="workflows-view">
    <div class="flex flex-wrap items-center gap-3 px-4 py-3 border-b border-line bg-card">
      <label class="text-xs text-fg-mute flex items-center gap-1">
        Session
        <select
          v-model="filters.sessionId"
          class="bg-raised border border-line rounded-md px-2 py-1 text-[12px] text-fg w-[260px] max-w-[260px] truncate"
          @change="onSessionSelect"
        >
          <option value="">
            — Select session —
          </option>
          <option
            v-for="s in sessions"
            :key="s.sessionId"
            :value="s.sessionId"
          >
            {{ sessionLabel(s) }}
          </option>
        </select>
      </label>
      <label class="text-xs text-fg-mute flex items-center gap-1">
        or paste ID
        <input
          v-model="filters.sessionId"
          type="text"
          class="bg-raised border border-line rounded-md px-2 py-1 text-[12px] text-fg w-[150px]"
          placeholder="Session ID"
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
      <button
        type="button"
        class="ml-auto text-xs px-3 py-1 rounded-md bg-raised text-fg-mute hover:text-fg border border-line cursor-pointer"
        @click="onReset"
      >
        Reset
      </button>
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
        @click="setActiveTab(tab.key)"
      >
        {{ tab.label }}
      </button>
      <button
        v-if="filters.sessionId"
        type="button"
        role="tab"
        :aria-selected="activeTab === 'dag'"
        class="text-xs px-3 py-1 rounded-md border-none cursor-pointer"
        :class="activeTab === 'dag' ? 'bg-blue-600 text-white' : 'bg-raised text-fg-mute hover:text-fg-soft'"
        @click="setActiveTab('dag')"
      >
        Session DAG
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
        @navigate="openSessionDag"
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
