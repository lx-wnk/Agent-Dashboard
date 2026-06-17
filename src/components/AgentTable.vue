<script setup lang="ts">
import type { AgentGrouping } from '../utils/agentGroup'
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
  groups?: AgentGrouping[]
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

// Compares two agents by the active column header sort. Applied in both the
// flat and grouped paths so the header controls stay live when grouping.
function compareAgents(a: Agent, b: Agent): number {
  const dir = sortDir.value === 'asc' ? 1 : -1
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
}

const sortedAgents = computed(() => [...props.agents].sort(compareAgents))

const tableGroups = computed<TableGroup[]>(() =>
  sortedAgents.value.map(agent => ({
    agent,
    showSubagents: expandedPids.value.has(agent.pid),
  })),
)

// Groups with non-null labels trigger the grouped rendering path.
const useGroups = computed(() =>
  !!props.groups && props.groups.some(g => g.label !== null),
)

// Column count = data columns (8) + expand-toggle column (1)
const COL_COUNT = 9

function groupTableItems(groupAgents: Agent[]): TableGroup[] {
  return [...groupAgents].sort(compareAgents).map(agent => ({
    agent,
    showSubagents: expandedPids.value.has(agent.pid),
  }))
}

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

      <!-- Grouped rendering: insert a header row before each group's agent rows -->
      <template v-if="useGroups && groups">
        <template v-for="group in groups" :key="group.key">
          <tbody>
            <tr>
              <td
                :colspan="COL_COUNT"
                class="px-3 py-1.5 bg-app border-b border-line"
              >
                <span class="font-mono text-[11px] font-semibold uppercase tracking-wider text-fg-mute">{{ group.label }}</span>
                <span class="ml-2 text-[11px] text-fg-faint">{{ group.agents.length }} {{ group.agents.length === 1 ? 'agent' : 'agents' }}</span>
              </td>
            </tr>
          </tbody>
          <tbody
            v-for="{ agent, showSubagents } in groupTableItems(group.agents)"
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
        </template>
      </template>

      <!-- Flat rendering: original behaviour -->
      <template v-else>
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
      </template>
    </table>
    <p v-if="agents.length === 0" class="text-center py-12 text-fg-mute text-sm">
      No running Claude agents found.
    </p>
  </div>
</template>
