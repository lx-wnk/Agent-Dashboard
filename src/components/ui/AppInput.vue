<script setup lang="ts">
import { computed, useAttrs, useId } from 'vue'

defineOptions({ inheritAttrs: false })

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

type ResizeProp = 'none' | 'y' | 'both' | 'x'

const inputId = useId()
const errorId = `${inputId}-error`

// The explicit binding below wins over v-bind="$attrs", so a caller's own
// description has to be folded in here or it never reaches the control.
const attrs = useAttrs()
const describedBy = computed(() => {
  const ids = [attrs['aria-describedby'], props.error ? errorId : null]
  return ids.filter(Boolean).join(' ') || undefined
})

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
      :aria-describedby="describedBy"
      class="w-full bg-app border border-line rounded-md px-3 py-1.5 text-sm text-fg placeholder:text-fg-faint focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent disabled:opacity-50 font-sans"
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
      :aria-describedby="describedBy"
      class="w-full bg-app border border-line rounded-md px-3 py-1.5 text-sm text-fg placeholder:text-fg-faint focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent disabled:opacity-50 font-sans"
      @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
    >
    <p
      v-if="error"
      :id="errorId"
      role="status"
      aria-live="polite"
      class="text-danger-text text-sm mt-1"
    >
      {{ error }}
    </p>
  </div>
</template>
