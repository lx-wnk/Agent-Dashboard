<script setup lang="ts">
import type { SubAgent } from '@/types'
import AppBadge from '@/components/ui/AppBadge.vue'

defineProps<{ subagent: SubAgent }>()
</script>

<template>
  <tr>
    <td class="px-3 py-1.5 border-b border-line text-xs text-fg-mute bg-card">
      <AppBadge :variant="subagent.status" />
    </td>
    <td colspan="2" class="pl-9 pr-3 py-1.5 border-b border-line text-xs text-fg-mute bg-card max-w-[350px] overflow-hidden text-ellipsis whitespace-nowrap">
      <span class="text-fg-mute mr-1.5">↳</span>
      {{ subagent.type === 'unknown' ? subagent.id.substring(0, 16) : subagent.type }}
    </td>
    <td class="px-3 py-1.5 border-b border-line text-xs text-fg-mute bg-card">
      {{ subagent.currentAction || '—' }}
    </td>
    <td colspan="5" class="bg-card border-b border-line" />
  </tr>
</template>
