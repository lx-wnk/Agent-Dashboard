<script setup lang="ts">
import type { SubAgent } from '../types'
import AppBadge from './ui/AppBadge.vue'

defineProps<{ subagents: SubAgent[] }>()
</script>

<template>
  <div v-if="subagents.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Subagents ({{ subagents.length }})
    </h4>
    <div v-for="sa in subagents" :key="sa.id" class="p-2 rounded-md bg-app mb-1.5">
      <div class="flex items-center gap-2">
        <AppBadge :variant="sa.status" />
        <span class="font-mono text-[11px] text-fg-mute">{{ sa.id.substring(0, 16) }}</span>
      </div>
      <div v-if="sa.type !== 'unknown'" class="text-[11px] text-fg-mute mt-1 truncate">
        {{ sa.type }}
      </div>
      <div v-if="sa.currentAction" class="text-[11px] text-fg-mute mt-0.5">
        Last tool: {{ sa.currentAction }}
      </div>
    </div>
  </div>
</template>
