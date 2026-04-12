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
  <div class="table-container">
    <table class="agent-table">
      <thead>
        <tr>
          <th class="sortable" tabindex="0" @click="toggleSort('status')" @keydown.enter="toggleSort('status')" @keydown.space.prevent="toggleSort('status')">
            Status{{ sortIndicator('status') }}
          </th>
          <th class="sortable" tabindex="0" @click="toggleSort('projectName')" @keydown.enter="toggleSort('projectName')" @keydown.space.prevent="toggleSort('projectName')">
            Project{{ sortIndicator('projectName') }}
          </th>
          <th class="sortable" tabindex="0" @click="toggleSort('currentAction')" @keydown.enter="toggleSort('currentAction')" @keydown.space.prevent="toggleSort('currentAction')">
            Current Action{{ sortIndicator('currentAction') }}
          </th>
          <th class="sortable" tabindex="0" @click="toggleSort('model')" @keydown.enter="toggleSort('model')" @keydown.space.prevent="toggleSort('model')">
            Model{{ sortIndicator('model') }}
          </th>
          <th class="sortable" tabindex="0" @click="toggleSort('tokens')" @keydown.enter="toggleSort('tokens')" @keydown.space.prevent="toggleSort('tokens')">
            Tokens{{ sortIndicator('tokens') }}
          </th>
          <th class="sortable" tabindex="0" @click="toggleSort('costEstimate')" @keydown.enter="toggleSort('costEstimate')" @keydown.space.prevent="toggleSort('costEstimate')">
            Cost{{ sortIndicator('costEstimate') }}
          </th>
          <th class="sortable" tabindex="0" @click="toggleSort('uptime')" @keydown.enter="toggleSort('uptime')" @keydown.space.prevent="toggleSort('uptime')">
            Uptime{{ sortIndicator('uptime') }}
          </th>
          <th class="sortable" tabindex="0" @click="toggleSort('pid')" @keydown.enter="toggleSort('pid')" @keydown.space.prevent="toggleSort('pid')">
            PID{{ sortIndicator('pid') }}
          </th>
          <th />
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
    <p v-if="agents.length === 0" class="empty">
      No running Claude agents found.
    </p>
  </div>
</template>

<style scoped>
.table-container {
  overflow-x: auto;
}

.agent-table {
  width: 100%;
  border-collapse: collapse;
}

.agent-table th {
  padding: 8px 12px;
  text-align: left;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  border-bottom: 1px solid var(--border);
  background: var(--bg-primary);
  position: sticky;
  top: 0;
  z-index: 1;
}

.agent-table th.sortable {
  cursor: pointer;
  user-select: none;
  transition: color 0.15s;
}

.agent-table th.sortable:hover {
  color: var(--text-secondary);
}

.agent-table th.sortable:focus-visible {
  outline: 2px solid var(--accent-blue);
  outline-offset: -2px;
}

.empty {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
