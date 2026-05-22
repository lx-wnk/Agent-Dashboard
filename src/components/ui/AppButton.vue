<script setup lang="ts">
type Variant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'info'
type Size = 'sm' | 'md'
type ButtonType = 'button' | 'submit' | 'reset'

withDefaults(defineProps<{
  variant?: Variant
  size?: Size
  disabled?: boolean
  type?: ButtonType
}>(), {
  variant: 'secondary',
  size: 'md',
  disabled: false,
  type: 'button',
})
</script>

<template>
  <button
    :type="type"
    :disabled="disabled"
    class="inline-flex items-center justify-center font-semibold rounded-md cursor-pointer transition-all font-sans border-0 disabled:opacity-40 disabled:cursor-not-allowed focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-blue-500"
    :class="[
      size === 'sm' ? 'px-2.5 py-1 text-xs' : 'px-3.5 py-1.5 text-sm',
      variant === 'primary' && 'bg-green-600 text-white hover:brightness-110',
      variant === 'secondary' && 'bg-raised text-fg-mute hover:brightness-110',
      variant === 'ghost' && 'bg-transparent text-fg-mute hover:text-fg-soft',
      variant === 'danger' && 'bg-red-600 text-white hover:brightness-110',
      variant === 'info' && 'bg-blue-600 text-white hover:brightness-110',
    ]"
  >
    <slot />
  </button>
</template>
