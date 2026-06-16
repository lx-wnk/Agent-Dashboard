<script setup lang="ts">
import type { ChipTone } from '../../utils/statusColors'

const props = withDefaults(defineProps<{
  tone?: ChipTone
  mono?: boolean
  uppercase?: boolean
  bordered?: boolean
}>(), {
  tone: 'neutral',
  mono: false,
  uppercase: false,
  bordered: true,
})

// Full literal class strings per tone so Tailwind v4 can statically extract them.
const toneClasses: Record<ChipTone, string> = {
  success: 'bg-success-soft text-success-text border-success-line',
  warning: 'bg-warning-soft text-warning-text border-warning-line',
  danger: 'bg-danger-soft text-danger-text border-danger-line',
  info: 'bg-info-soft text-info-text border-info-line',
  neutral: 'bg-neutral-soft text-neutral-text border-neutral-line',
}
</script>

<template>
  <span
    class="inline-flex items-center rounded px-1.5 py-px text-[10px] leading-tight"
    :class="[
      toneClasses[props.tone],
      props.bordered ? 'border' : '',
      props.mono ? 'font-mono' : '',
      props.uppercase ? 'uppercase' : '',
    ]"
  ><slot /></span>
</template>
