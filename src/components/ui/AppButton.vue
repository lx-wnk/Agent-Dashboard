<script setup lang="ts">
type Variant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'info' | 'success' | 'outline'
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
    class="inline-flex items-center justify-center font-semibold rounded-md cursor-pointer transition-all font-sans disabled:opacity-40 disabled:cursor-not-allowed focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-accent"
    :class="[
      size === 'sm' ? 'px-2.5 py-1 text-xs' : 'px-3.5 py-1.5 text-sm',
      variant !== 'outline' && 'border-0',
      variant === 'primary' && 'bg-accent text-white hover:brightness-110',
      variant === 'secondary' && 'bg-raised text-fg-mute hover:brightness-110',
      variant === 'ghost' && 'bg-transparent text-fg-mute hover:text-fg-soft',
      variant === 'danger' && 'bg-danger text-white hover:brightness-110',
      variant === 'info' && 'bg-info text-white hover:brightness-110',
      variant === 'success' && 'bg-success text-white hover:brightness-110',
      variant === 'outline' && 'border border-line-strong text-fg-soft hover:bg-raised',
    ]"
  >
    <slot />
  </button>
</template>
