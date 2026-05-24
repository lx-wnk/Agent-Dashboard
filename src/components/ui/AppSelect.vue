<script setup lang="ts">
defineProps<{
  modelValue: string | number
  options: Array<{ value: string | number; label: string }>
  id?: string
  ariaLabel?: string
  disabled?: boolean
}>()

defineEmits<{ 'update:modelValue': [value: string | number] }>()
</script>

<template>
  <select
    :id="id"
    :value="modelValue"
    :aria-label="ariaLabel"
    :disabled="disabled"
    class="bg-card border border-line rounded-md px-3 py-2 text-sm text-fg focus:outline-none focus:ring-2 focus:ring-accent disabled:opacity-50 cursor-pointer"
    @change="$emit('update:modelValue', ($event.target as HTMLSelectElement).value)"
  >
    <option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option>
  </select>
</template>
