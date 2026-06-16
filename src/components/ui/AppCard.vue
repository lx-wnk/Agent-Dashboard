<script setup lang="ts">
withDefaults(defineProps<{
  surface?: 'card' | 'app'
  radius?: 'md' | 'lg'
  interactive?: boolean
  lift?: boolean
}>(), {
  surface: 'card',
  radius: 'lg',
  interactive: false,
  lift: false,
})

// Literal class strings so Tailwind v4 can statically extract all utilities.
const surfaceClasses = {
  card: 'bg-card',
  app: 'bg-app',
} as const

const radiusClasses = {
  md: 'rounded-md',
  lg: 'rounded-lg',
} as const
</script>

<template>
  <div
    class="border border-line"
    :class="[
      surfaceClasses[surface],
      radiusClasses[radius],
      interactive && 'transition-all hover:border-line-strong',
      interactive && lift && 'hover:-translate-y-px',
      interactive && !lift && 'hover:shadow-card-hover',
    ]"
  >
    <slot />
  </div>
</template>
