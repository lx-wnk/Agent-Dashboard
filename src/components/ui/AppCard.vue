<script setup lang="ts">
withDefaults(defineProps<{
  padding?: string
  interactive?: boolean
}>(), {
  padding: 'p-4',
  interactive: false,
})

const emit = defineEmits<{ click: [event: MouseEvent | KeyboardEvent] }>()
</script>

<template>
  <div
    class="rounded-xl border border-slate-200 dark:border-slate-700/50 bg-white dark:bg-gradient-to-br dark:from-slate-800 dark:to-slate-900 shadow-sm dark:shadow-none"
    :class="[
      padding,
      interactive && 'cursor-pointer transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-lg active:scale-[0.99] focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]',
    ]"
    :role="interactive ? 'button' : undefined"
    :tabindex="interactive ? 0 : undefined"
    @click="interactive && emit('click', $event)"
    @keydown.enter.prevent="interactive && emit('click', $event)"
    @keydown.space.prevent="interactive && emit('click', $event)"
  >
    <slot />
  </div>
</template>
