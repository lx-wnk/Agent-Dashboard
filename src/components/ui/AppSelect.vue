<script setup lang="ts">
const props = defineProps<{
  modelValue: string | number
  options: Array<{ value: string | number, label: string }>
  id?: string
  ariaLabel?: string
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string | number] }>()

function onChange(e: Event) {
  const raw = (e.target as HTMLSelectElement).value
  emit('update:modelValue', typeof props.modelValue === 'number' ? Number(raw) : raw)
}
</script>

<template>
  <select
    :id="id"
    :value="modelValue"
    :aria-label="ariaLabel"
    :disabled="disabled"
    class="bg-card border border-line rounded-md px-3 py-2 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent disabled:opacity-50 cursor-pointer"
    @change="onChange"
  >
    <option v-for="o in options" :key="o.value" :value="o.value">
      {{ o.label }}
    </option>
  </select>
</template>
