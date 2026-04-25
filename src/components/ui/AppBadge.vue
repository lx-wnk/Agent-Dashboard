<script setup lang="ts">
type Variant = 'active' | 'waiting' | 'idle' | 'completed' | 'error' | 'info'

defineProps<{ variant: Variant }>()

const dotClass: Record<Variant, string> = {
  active: 'bg-green-400 dark:bg-green-400',
  waiting: 'bg-yellow-400 dark:bg-yellow-400',
  idle: 'bg-slate-400 dark:bg-slate-500',
  completed: 'bg-slate-400 dark:bg-slate-500',
  error: 'bg-red-400 dark:bg-red-400',
  info: 'bg-blue-400 dark:bg-blue-400',
}

const labelClass: Record<Variant, string> = {
  active: 'text-green-600 dark:text-green-400',
  waiting: 'text-yellow-600 dark:text-yellow-400',
  idle: 'text-slate-400 dark:text-slate-500',
  completed: 'text-slate-400 dark:text-slate-500',
  error: 'text-red-600 dark:text-red-400',
  info: 'text-blue-600 dark:text-blue-400',
}
</script>

<template>
  <span class="inline-flex items-center gap-1.5 text-xs capitalize">
    <span class="size-2 rounded-full flex-shrink-0" :class="dotClass[variant]" />
    <span :class="labelClass[variant]">{{ variant }}</span>
  </span>
</template>
