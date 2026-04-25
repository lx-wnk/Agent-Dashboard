<script setup lang="ts">
type Variant = 'primary' | 'secondary' | 'ghost' | 'danger'
type Size = 'sm' | 'md'

withDefaults(defineProps<{
  variant?: Variant
  size?: Size
  disabled?: boolean
}>(), {
  variant: 'secondary',
  size: 'md',
  disabled: false,
})
</script>

<template>
  <button
    type="button"
    :disabled="disabled"
    class="inline-flex items-center justify-center font-semibold rounded-md cursor-pointer transition-all font-sans border-0 disabled:opacity-40 disabled:cursor-not-allowed"
    :class="[
      size === 'sm' ? 'px-2.5 py-1 text-xs' : 'px-3.5 py-1.5 text-sm',
      variant === 'primary' && 'bg-green-600 text-white hover:brightness-110',
      variant === 'secondary' && 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 hover:brightness-110',
      variant === 'ghost' && 'bg-transparent text-slate-400 dark:text-slate-600 hover:text-slate-700 dark:hover:text-slate-300',
      variant === 'danger' && 'bg-red-600 text-white hover:brightness-110',
    ]"
  >
    <slot />
  </button>
</template>
