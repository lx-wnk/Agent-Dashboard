<script setup lang="ts">
import type { SubAgent } from '@/types'
import AppBadge from '@/components/ui/AppBadge.vue'

defineProps<{ subagents: SubAgent[] }>()
defineEmits<{ open: [subagent: SubAgent] }>()
</script>

<template>
  <div v-if="subagents.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Subagents ({{ subagents.length }})
    </h4>
    <button
      v-for="sa in subagents"
      :key="sa.id"
      type="button"
      class="w-full text-left p-2 rounded-md bg-app mb-1.5 hover:bg-raised transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
      :aria-label="`Open subagent ${sa.id} transcript`"
      data-testid="subagent-open"
      @click="$emit('open', sa)"
    >
      <div class="flex items-center gap-2">
        <AppBadge :variant="sa.status" />
        <span class="font-mono text-[11px] text-fg-mute">{{ sa.id.substring(0, 16) }}</span>
        <span aria-hidden="true" class="ml-auto text-[11px] text-fg-faint">›</span>
      </div>
      <div v-if="sa.type !== 'unknown'" class="text-[11px] text-fg-mute mt-1 truncate">
        {{ sa.type }}
      </div>
      <div v-if="sa.currentAction" class="text-[11px] text-fg-mute mt-0.5">
        Last tool: {{ sa.currentAction }}
      </div>
    </button>
  </div>
</template>
