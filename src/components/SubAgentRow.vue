<script setup lang="ts">
import type { SubAgent } from '../types'
import AppBadge from './ui/AppBadge.vue'

defineProps<{ subagent: SubAgent }>()
</script>

<template>
  <tr>
    <td class="px-3 py-1.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-900">
      <AppBadge :variant="subagent.status" />
    </td>
    <td colspan="2" class="pl-9 pr-3 py-1.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-900 max-w-[350px] overflow-hidden text-ellipsis whitespace-nowrap">
      <span class="text-slate-400 dark:text-slate-600 mr-1.5">↳</span>
      {{ subagent.type === 'unknown' ? subagent.id.substring(0, 16) : subagent.type }}
    </td>
    <td class="px-3 py-1.5 border-b border-slate-200 dark:border-slate-800 text-xs text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-900">
      {{ subagent.currentAction || '—' }}
    </td>
    <td colspan="5" class="bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-800" />
  </tr>
</template>
