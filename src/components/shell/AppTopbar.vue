<script setup lang="ts">
import type { ActiveView } from '../../composables/useViewState'
import { computed } from 'vue'
import { viewTitle } from '../../utils/navConfig'
import LivePulse from './LivePulse.vue'
import OfflineBadge from '../OfflineBadge.vue'

const props = defineProps<{
  activeView: ActiveView
  searchQuery: string
  live: boolean
}>()
defineEmits<{ 'update:searchQuery': [value: string] }>()

const title = computed(() => viewTitle(props.activeView))
const searchPlaceholder = computed(() =>
  props.activeView === 'pipeline' ? 'Search tasks…' : 'Search…')
</script>

<template>
  <header class="h-12 shrink-0 flex items-center gap-3 px-4 border-b border-line bg-card">
    <h1 class="text-[15px] font-semibold text-fg">{{ title }}</h1>
    <input
      :value="searchQuery"
      type="text"
      :aria-label="searchPlaceholder"
      :placeholder="searchPlaceholder"
      class="ml-auto bg-raised border border-line rounded-lg px-3 py-1.5 text-[13px] text-fg placeholder:text-fg-faint w-[200px] focus:outline-none focus:border-accent focus:w-[260px] transition-[width,border-color] duration-200"
      @input="$emit('update:searchQuery', ($event.target as HTMLInputElement).value)"
    >
    <slot name="cta" />
    <LivePulse :live="live" />
    <OfflineBadge />
  </header>
</template>
