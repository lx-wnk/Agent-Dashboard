<script setup lang="ts">
withDefaults(defineProps<{
  type?: 'input' | 'textarea'
  modelValue?: string
  placeholder?: string
  disabled?: boolean
  rows?: number
}>(), {
  type: 'input',
  modelValue: '',
  disabled: false,
  rows: 1,
})

defineEmits<{
  'update:modelValue': [value: string]
}>()
</script>

<template>
  <textarea
    v-if="type === 'textarea'"
    :value="modelValue"
    :placeholder="placeholder"
    :disabled="disabled"
    :rows="rows"
    class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 disabled:opacity-50 font-sans resize-none"
    @input="$emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
  />
  <input
    v-else
    :value="modelValue"
    :placeholder="placeholder"
    :disabled="disabled"
    class="w-full bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-1.5 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 disabled:opacity-50 font-sans"
    @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
  >
</template>
