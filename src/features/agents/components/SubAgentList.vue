<script setup lang="ts">
import type { SubAgent } from '@/types'
import { computed, ref } from 'vue'
import SubAgentListItem from './SubAgentListItem.vue'

const props = defineProps<{ subagents: SubAgent[] }>()
defineEmits<{ open: [subagent: SubAgent] }>()

// Completed subagents collapse behind a disclosure once there are more than a
// handful — active ones are what a user checking in on a run needs to see
// without clicking, completed ones are reference material.
const COLLAPSE_THRESHOLD = 5

const activeSubagents = computed(() => props.subagents.filter(sa => sa.status === 'active'))
const completedSubagents = computed(() => props.subagents.filter(sa => sa.status !== 'active'))

// SSE keeps re-rendering this component every few seconds with a fresh
// subagents array. The native <details> `toggle` event fires as a delayed
// task rather than synchronously, so listening for it to sync an override ref
// leaves a window where a re-render reads the stale ref and stomps the user's
// choice. Driving `open` fully from Vue state, toggled synchronously on
// click, avoids that race entirely.
const userOpen = ref<boolean | null>(null)
const detailsOpen = computed(() => userOpen.value ?? completedSubagents.value.length <= COLLAPSE_THRESHOLD)
function toggleDisclosure() {
  userOpen.value = !detailsOpen.value
}
</script>

<template>
  <div v-if="subagents.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Subagents ({{ activeSubagents.length }} active of {{ subagents.length }})
    </h4>
    <div class="max-h-[120px] overflow-y-auto overflow-x-hidden pr-1" data-testid="subagent-scroll">
      <SubAgentListItem
        v-for="sa in activeSubagents"
        :key="sa.id"
        :subagent="sa"
        @open="$emit('open', sa)"
      />

      <details v-if="completedSubagents.length > 0" :open="detailsOpen">
        <summary
          class="cursor-pointer select-none text-[11px] text-fg-mute py-1 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent rounded"
          data-testid="subagent-completed-summary"
          @click.prevent="toggleDisclosure"
        >
          {{ completedSubagents.length }} completed
        </summary>
        <SubAgentListItem
          v-for="sa in completedSubagents"
          :key="sa.id"
          :subagent="sa"
          @open="$emit('open', sa)"
        />
      </details>
    </div>
  </div>
</template>
