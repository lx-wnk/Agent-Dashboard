<script setup lang="ts">
withDefaults(defineProps<{
  padding?: string
  interactive?: boolean
}>(), {
  padding: 'p-4',
  interactive: false,
})
</script>

<template>
  <div
    :class="[
      'rounded-xl border border-slate-200 dark:border-slate-700/50',
      'bg-white dark:bg-gradient-to-br dark:from-slate-800 dark:to-slate-900',
      'shadow-sm dark:shadow-none',
      padding,
      interactive && 'cursor-pointer transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-lg focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]',
    ]"
  >
    <slot />
  </div>
</template>
