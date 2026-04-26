<script setup lang="ts">
defineProps<{
  label: string
  targetName: string
  buttonText?: string
}>()

const emit = defineEmits<{ click: [] }>()
</script>

<template>
  <div class="flex items-center justify-between flex-shrink-0 px-4 py-1.5 bg-blue-50/50 dark:bg-blue-950/20 border-b border-blue-200/50 dark:border-blue-800/40">
    <span class="text-[11px] text-slate-500 dark:text-slate-400">
      ⬡ {{ label }} <strong class="text-blue-600 dark:text-blue-400">{{ targetName }}</strong>
    </span>
    <button
      class="bg-transparent border-none text-[11px] text-blue-600 dark:text-blue-400 cursor-pointer underline p-0 whitespace-nowrap hover:text-slate-700 dark:hover:text-slate-300"
      @click="emit('click')"
    >
      {{ buttonText ?? 'Open →' }}
    </button>
  </div>
</template>
