<script setup lang="ts">
import type { Agent } from '@/types'
import { computed } from 'vue'
import { useAgents } from '@/features/agents'
import { friendlyProjectName } from '@/utils/friendlyProjectName'
import NeedsYouQueue from './NeedsYouQueue.vue'

const { agents } = useAgents({ autoStart: false })

function stateOf(a: Agent): 'waiting' | 'working' | 'idle' {
  if (a.status === 'waiting')
    return 'waiting'
  return a.working ? 'working' : 'idle'
}
const ORDER = { waiting: 0, working: 1, idle: 2 } as const
const rows = computed(() =>
  agents.value
    .filter(a => a.status !== 'finished')
    .map(a => ({ a, state: stateOf(a) }))
    .sort((x, y) => ORDER[x.state] - ORDER[y.state]),
)
</script>

<template>
  <section data-testid="hub" aria-label="Zentrale" class="flex h-full min-h-0 flex-col gap-4 overflow-hidden rounded-xl border border-line bg-card p-4">
    <NeedsYouQueue variant="docked" />
    <ul data-testid="hub-list" class="min-h-0 flex-1 overflow-y-auto">
      <li v-for="{ a, state } in rows" :key="a.pid" data-testid="hub-agent" class="flex items-center justify-between gap-3 border-b border-line py-1.5 text-[12.5px]">
        <span class="truncate text-fg">{{ friendlyProjectName(a.projectName) }}</span>
        <span :class="state === 'waiting' ? 'text-warning-text' : state === 'working' ? 'text-success-text' : 'text-fg-mute'">{{ state }}</span>
      </li>
    </ul>
  </section>
</template>
