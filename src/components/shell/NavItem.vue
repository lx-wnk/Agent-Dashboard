<script setup lang="ts">
defineProps<{
  icon: string
  label: string
  active: boolean
  expanded: boolean
}>()
defineEmits<{ select: [] }>()
</script>

<template>
  <button
    type="button"
    class="nav-item relative flex items-center gap-3 w-full rounded-lg px-2.5 min-h-[40px] text-[13px] transition-colors focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-card"
    :class="active
      ? 'bg-accent-soft text-accent font-semibold'
      : 'text-fg-mute hover:text-fg hover:bg-raised'"
    :aria-current="active ? 'page' : undefined"
    :title="!expanded ? label : undefined"
    @click="$emit('select')"
  >
    <span class="text-[16px] w-5 shrink-0 text-center" aria-hidden="true">{{ icon }}</span>
    <span v-if="expanded" class="truncate">{{ label }}</span>
    <span v-else class="sr-only">{{ label }}</span>
    <span v-if="expanded" class="ml-auto"><slot name="badge" /></span>
    <span
      v-if="!expanded"
      aria-hidden="true"
      class="nav-tooltip pointer-events-none absolute left-full top-1/2 -translate-y-1/2 ml-2 whitespace-nowrap rounded-md border border-line bg-card shadow-card-hover px-2 py-1 text-xs font-medium text-fg opacity-0 invisible transition-opacity z-10"
    >{{ label }}</span>
  </button>
</template>

<style scoped>
.nav-item:hover .nav-tooltip,
.nav-item:focus-visible .nav-tooltip {
  opacity: 1;
  visibility: visible;
}
</style>
