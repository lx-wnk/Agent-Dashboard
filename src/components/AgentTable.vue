<script setup lang="ts">
import type { Agent } from '../types'
import { computed, ref } from 'vue'
import { STATUS_ORDER } from '@/utils/agentSort'
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
            class="px-3 py-2 text-left text-[11px] font-semibold uppercase tracking-wider text-fg-mute border-b border-line bg-app sticky top-0 z-[1] cursor-pointer select-none hover:text-slate-600 dark:hover:text-slate-400 transition-colors focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
            tabindex="0"
            @click="toggleSort(field as SortField)"
            @keydown.enter="toggleSort(field as SortField)"
            @keydown.space.prevent="toggleSort(field as SortField)"
          >
            {{ label }}{{ sortIndicator(field as SortField) }}
          </th>
          <th class="px-3 py-2 bg-app sticky top-0 z-[1] border-b border-line" />
        </tr>
      </thead>
      <template v-for="agent in sortedAgents" :key="agent.pid">
        <tbody>
          <AgentRow
            :agent="agent"
            :expanded="expandedPids.has(agent.pid)"
            @select="$emit('select', agent)"
            @toggle-subagents="toggleSubagents(agent.pid)"
          />
        </tbody>
        <tbody
          v-if="expandedPids.has(agent.pid)"
          :id="`subagents-${agent.sessionId}`"
        >
          <SubAgentRow
            v-for="sub in agent.subagents"
            :key="sub.id"
            :subagent="sub"
          />
        </tbody>
      </template>
    </table>
    <p v-if="agents.length === 0" class="text-center py-12 text-fg-mute text-sm">
      No running Claude agents found.
    </p>
  </div>
</template>
