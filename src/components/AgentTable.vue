<script setup lang="ts">
import type { Agent } from '../types'
import { computed, ref } from 'vue'
import { totalTokenCount } from '../utils/format'
import AgentRow from './AgentRow.vue'
import SubAgentRow from './SubAgentRow.vue'

type SortField = 'status' | 'projectName' | 'currentAction' | 'model' | 'tokens' | 'costEstimate' | 'uptime' | 'pid'
type SortDir = 'asc' | 'desc'

const props = defineProps<{
  agents: Agent[]
}>()

defineEmits<{
  select: [agent: Agent]
}>()

const STATUS_ORDER: Record<string, number> = { active: 0, waiting: 1, idle: 2 }

const expandedPids = ref(new Set<number>())
const sortField = ref<SortField>('status')
const sortDir = ref<SortDir>('asc')

function toggleSort(field: SortField) {
  if (sortField.value === field) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc'
  }
  else {
    sortField.value = field
    sortDir.value = field === 'costEstimate' || field === 'tokens' || field === 'uptime' ? 'desc' : 'asc'
  }
}

function sortIndicator(field: SortField): string {
  if (sortField.value !== field)
    return ''
  return sortDir.value === 'asc' ? ' ▲' : ' ▼'
}

const sortedAgents = computed(() => {
  const list = [...props.agents]
  const dir = sortDir.value === 'asc' ? 1 : -1
  list.sort((a, b) => {
    let cmp = 0
    switch (sortField.value) {
      case 'status':
        cmp = (STATUS_ORDER[a.status] ?? 9) - (STATUS_ORDER[b.status] ?? 9)
        break
      case 'projectName':
        cmp = a.projectName.localeCompare(b.projectName)
        break
      case 'currentAction':
        cmp = (a.currentAction ?? '').localeCompare(b.currentAction ?? '')
        break
      case 'model':
        cmp = (a.model ?? '').localeCompare(b.model ?? '')
        break
      case 'tokens':
        cmp = totalTokenCount(a.tokenUsage) - totalTokenCount(b.tokenUsage)
        break
      case 'costEstimate':
        cmp = a.costEstimate - b.costEstimate
        break
      case 'uptime':
        cmp = a.uptime - b.uptime
        break
      case 'pid':
        cmp = a.pid - b.pid
        break
    }
    return cmp * dir
  })
  return list
})

function toggleSubagents(pid: number) {
  if (expandedPids.value.has(pid)) {
    expandedPids.value.delete(pid)
  }
  else {
    expandedPids.value.add(pid)
  }
}
</script>

<template>
  <div class="overflow-x-auto">
    <table class="w-full border-collapse">
      <thead>
        <tr>
          <th
            v-for="[field, label] in ([['status', 'Status'], ['projectName', 'Project'], ['currentAction', 'Current Action'], ['model', 'Model'], ['tokens', 'Tokens'], ['costEstimate', 'Cost'], ['uptime', 'Uptime'], ['pid', 'PID']] as const)"
            :key="field"
            class="px-3 py-2 text-left text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 border-b border-slate-200 dark:border-slate-800 bg-slate-50 dark:bg-slate-950 sticky top-0 z-[1] cursor-pointer select-none hover:text-slate-600 dark:hover:text-slate-400 transition-colors focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
            tabindex="0"
            @click="toggleSort(field as SortField)"
            @keydown.enter="toggleSort(field as SortField)"
            @keydown.space.prevent="toggleSort(field as SortField)"
          >
            {{ label }}{{ sortIndicator(field as SortField) }}
          </th>
          <th class="px-3 py-2 bg-slate-50 dark:bg-slate-950 sticky top-0 z-[1] border-b border-slate-200 dark:border-slate-800" />
        </tr>
      </thead>
      <tbody>
        <template v-for="agent in sortedAgents" :key="agent.pid">
          <AgentRow
            :agent="agent"
            :expanded="expandedPids.has(agent.pid)"
            @select="$emit('select', agent)"
            @toggle-subagents="toggleSubagents(agent.pid)"
          />
          <template v-if="expandedPids.has(agent.pid)">
            <SubAgentRow
              v-for="sub in agent.subagents"
              :key="sub.id"
              :subagent="sub"
            />
          </template>
        </template>
      </tbody>
    </table>
    <p v-if="agents.length === 0" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
      No running Claude agents found.
    </p>
  </div>
</template>
