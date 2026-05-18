<script setup lang="ts">
import { computed } from 'vue'
import { statusLabel } from '../../utils/statusColors'

type Variant = 'active' | 'waiting' | 'idle' | 'completed' | 'error' | 'info'

const props = defineProps<{ variant: Variant; label?: string }>()

const dotClass: Record<Variant, string> = {
  active: 'bg-green-400 dark:bg-green-400',
  waiting: 'bg-yellow-600 dark:bg-yellow-400',
  idle: 'bg-slate-400 dark:bg-slate-500',
  completed: 'bg-slate-400 dark:bg-slate-500',
  error: 'bg-red-400 dark:bg-red-400',
  info: 'bg-blue-400 dark:bg-blue-400',
}

const labelClass: Record<Variant, string> = {
  active: 'text-green-600 dark:text-green-400',
  waiting: 'text-yellow-700 dark:text-yellow-400',
  idle: 'text-slate-600 dark:text-slate-500',
  completed: 'text-slate-600 dark:text-slate-500',
  error: 'text-red-600 dark:text-red-400',
  info: 'text-blue-600 dark:text-blue-400',
}

const displayLabel = computed(() => props.label ?? statusLabel(props.variant))
</script>

<template>
  <span class="inline-flex items-center gap-1.5 text-xs" :aria-label="`Status: ${displayLabel}`">
    <span class="size-2 rounded-full flex-shrink-0" :class="dotClass[variant]" aria-hidden="true" />
    <span :class="labelClass[variant]">{{ displayLabel }}</span>
  </span>
</template>
