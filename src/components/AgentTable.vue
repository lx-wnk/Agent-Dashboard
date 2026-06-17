<script setup lang="ts">
import type { Agent } from '../types'
import { computed, ref } from 'vue'
import { STATUS_ORDER } from '@/utils/agentSort'
import { formatUptime, shortModel, totalTokenCount } from '../utils/format'
import AgentRow from './AgentRow.vue'
import SubAgentRow from './SubAgentRow.vue'

type SortField = 'status' | 'projectName' | 'currentAction' | 'model' | 'tokens' | 'costEstimate' | 'uptime' | 'pid'
type SortDir = 'asc' | 'desc'

interface TableGroup {
  agent: Agent
  showSubagents: boolean
}

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

const tableGroups = computed<TableGroup[]>(() =>
  sortedAgents.value.map(agent => ({
    agent,
    showSubagents: expandedPids.value.has(agent.pid),
  })),
)

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
            class="px-3 py-2 text-left text-[11px] font-semibold uppercase tracking-wider text-fg-mute border-b border-line bg-app sticky top-0 z-[1] cursor-pointer select-none hover:text-fg transition-colors focus-visible:outline-2 focus-visible:outline-ring focus-visible:outline-offset-[-2px]"
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
      <tbody
        v-for="{ agent, showSubagents } in tableGroups"
        :key="agent.pid"
        :id="`subagents-${agent.sessionId}`"
      >
        <AgentRow
          v-memo="[agent.status, agent.projectName, agent.currentAction, agent.model, totalTokenCount(agent.tokenUsage), agent.costEstimate, agent.costUnknown, formatUptime(agent.uptime), shortModel(agent.model ?? null), agent.channelAvailable, agent.provider, agent.machine, agent.projectPath, agent.subagents.length, showSubagents]"
          :agent="agent"
          :expanded="showSubagents"
          @select="$emit('select', agent)"
          @toggle-subagents="toggleSubagents(agent.pid)"
        />
        <SubAgentRow
          v-for="sub in agent.subagents"
          v-show="showSubagents"
          :key="sub.id"
          :subagent="sub"
        />
      </tbody>
    </table>
    <p v-if="agents.length === 0" class="text-center py-12 text-fg-mute text-sm">
      No running Claude agents found.
    </p>
  </div>
</template>
