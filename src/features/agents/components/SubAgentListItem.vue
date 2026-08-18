<script setup lang="ts">
import type { SubAgent } from '@/types'
import AppBadge from '@/components/ui/AppBadge.vue'

defineProps<{ subagent: SubAgent }>()
defineEmits<{ open: [subagent: SubAgent] }>()
</script>

<template>
  <button
    type="button"
    class="w-full text-left p-2 rounded-md bg-app mb-1.5 hover:bg-raised transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent"
    :aria-label="`Open subagent ${subagent.id} transcript`"
    data-testid="subagent-open"
    @click="$emit('open', subagent)"
  >
    <div class="flex items-center gap-2">
      <AppBadge :variant="subagent.status" />
      <span class="font-mono text-[11px] text-fg-mute">{{ subagent.id.substring(0, 16) }}</span>
      <span aria-hidden="true" class="ml-auto text-[11px] text-fg-faint">›</span>
    </div>
    <div v-if="subagent.type !== 'unknown'" class="text-[11px] text-fg-mute mt-1 truncate">
      {{ subagent.type }}
    </div>
    <div v-if="subagent.currentAction" class="text-[11px] text-fg-mute mt-0.5">
      Last tool: {{ subagent.currentAction }}
    </div>
  </button>
</template>
