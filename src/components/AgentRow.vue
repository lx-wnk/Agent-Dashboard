<script setup lang="ts">
import type { Agent } from '../types'
import { useAgentIdentity } from '../composables/useAgentIdentity'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import MachineBadge from './MachineBadge.vue'
import ProviderBadge from './ProviderBadge.vue'
import AppBadge from './ui/AppBadge.vue'

defineProps<{ agent: Agent, expanded: boolean }>()
defineEmits<{ select: [agent: Agent], toggleSubagents: [] }>()

const { getIdentity } = useAgentIdentity()
</script>

<template>
  <tr
    class="cursor-pointer transition-colors hover:bg-app"
    @click="$emit('select', agent)"
  >
    <td class="w-24 px-3 py-2.5 border-b border-line text-sm">
      <AppBadge :variant="agent.status" />
    </td>
    <td class="px-3 py-2.5 border-b border-line text-sm text-fg font-medium">
      <button
        type="button"
        class="inline-flex items-center gap-1 bg-transparent border-none p-0 text-sm font-medium text-fg cursor-pointer rounded focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-2"
        :aria-label="`Open details for ${agent.projectName}`"
        @click.stop="$emit('select', agent)"
      >
        <span class="mr-1 text-sm" aria-hidden="true">{{ getIdentity(agent.projectPath).emoji }}</span>
        <span
          :style="{ backgroundColor: getIdentity(agent.projectPath).color }"
          class="inline-block w-2 h-2 rounded-full mr-1 flex-shrink-0"
          aria-hidden="true"
        />
        {{ agent.projectName }}
      </button>
      <ProviderBadge :provider="agent.provider" />
      <span
        v-if="agent.channelAvailable"
        title="Channel active"
        class="inline-block ml-1.5 px-1 text-[9px] font-semibold text-green-600 dark:text-green-400 border border-green-600 dark:border-green-400 rounded align-middle tracking-wider"
      >CH</span>
      <MachineBadge v-if="agent.machine" :machine="agent.machine" />
    </td>
    <td class="max-w-[250px] overflow-hidden text-ellipsis whitespace-nowrap px-3 py-2.5 border-b border-line text-sm text-fg-mute">
      {{ agent.currentAction || '—' }}
    </td>
    <td class="px-3 py-2.5 border-b border-line text-xs text-fg-mute whitespace-nowrap">
      {{ shortModel(agent.model ?? null) }}
    </td>
    <td class="px-3 py-2.5 border-b border-line text-xs font-mono text-fg-mute whitespace-nowrap">
      {{ formatTokens(totalTokenCount(agent.tokenUsage)) }}
    </td>
    <td class="px-3 py-2.5 border-b border-line text-xs font-mono text-green-600 dark:text-green-400 whitespace-nowrap">
      <span v-if="agent.costUnknown" class="text-fg-mute" title="Cost unknown — no pricing data for this provider/model">?</span>
      <template v-else>{{ formatCost(agent.costEstimate) }}</template>
    </td>
    <td class="w-20 px-3 py-2.5 border-b border-line text-xs text-fg-mute">
      {{ formatUptime(agent.uptime) }}
    </td>
    <td class="w-[70px] px-3 py-2.5 border-b border-line text-xs font-mono text-fg-mute">
      {{ agent.pid }}
    </td>
    <td class="w-[50px] text-center px-3 py-2.5 border-b border-line">
      <button
        v-if="agent.subagents.length > 0"
        type="button"
        :aria-expanded="expanded ? 'true' : 'false'"
        :aria-controls="`subagents-${agent.sessionId}`"
        :aria-label="`${expanded ? 'Collapse' : 'Expand'} ${agent.subagents.length} sub-agent${agent.subagents.length === 1 ? '' : 's'}`"
        class="bg-transparent border-none text-fg-mute cursor-pointer text-[11px] px-1.5 py-0.5 rounded hover:bg-raised hover:text-fg-soft"
        @click.stop="$emit('toggleSubagents')"
      >
        <span aria-hidden="true">{{ expanded ? '▼' : '▶' }}</span>
        {{ agent.subagents.length }}
      </button>
    </td>
  </tr>
</template>
