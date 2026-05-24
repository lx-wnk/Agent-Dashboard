<script lang="ts">
export default {
  inheritAttrs: false,
}
</script>

<script setup lang="ts">
import { useId } from 'vue'

type ResizeProp = 'none' | 'y' | 'both' | 'x'

const props = withDefaults(defineProps<{
  type?: 'input' | 'textarea'
  modelValue?: string
  placeholder?: string
  disabled?: boolean
  rows?: number
  label?: string
  error?: string
  resize?: ResizeProp
}>(), {
  type: 'input',
  modelValue: '',
  disabled: false,
  rows: 1,
  resize: 'none',
})

defineEmits<{
  'update:modelValue': [value: string]
}>()

const inputId = useId()
const errorId = `${inputId}-error`

const resizeClass: Record<ResizeProp, string> = {
  none: 'resize-none',
  y: 'resize-y',
  both: 'resize',
  x: 'resize-x',
}
</script>

<template>
  <div class="w-full">
    <label
      v-if="label"
      :for="inputId"
      class="block text-sm font-medium text-fg-soft mb-1"
    >{{ label }}</label>
    <textarea
      v-if="type === 'textarea'"
      :id="inputId"
      v-bind="$attrs"
      :value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      :rows="rows"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="error ? errorId : undefined"
      class="w-full bg-app border border-line rounded-md px-3 py-1.5 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 disabled:opacity-50 font-sans"
      :class="resizeClass[resize]"
      @input="$emit('update:modelValue', ($event.target as HTMLTextAreaElement).value)"
    />
    <input
      v-else
      :id="inputId"
      v-bind="$attrs"
      :value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      :aria-invalid="error ? 'true' : undefined"
      :aria-describedby="error ? errorId : undefined"
      class="w-full bg-app border border-line rounded-md px-3 py-1.5 text-sm text-fg placeholder:text-fg-faint focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 disabled:opacity-50 font-sans"
      @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
    >
    <p
      v-if="error"
      :id="errorId"
      role="status"
      aria-live="polite"
      class="text-red-500 text-sm mt-1"
    >
      {{ error }}
    </p>
  </div>
</template>
