<script setup lang="ts">
import { computed, ref } from 'vue'
import { PHASE_LABELS, PHASE_ORDER } from '../composables/useRefinementChat'

const props = defineProps<{
  status: 'none' | 'refining' | 'draft_ready' | 'failed' | null
  error: string | null
  lastOutput: string
  completedPhases?: string[]
}>()

const expanded = ref(false)
const show = computed(() => props.status === 'refining' || props.status === 'draft_ready' || props.status === 'failed')

const done = computed(() => new Set(props.completedPhases ?? []))
const currentPhase = computed(() => {
  if (props.status !== 'refining')
    return null
  return PHASE_ORDER.find(p => !done.value.has(p)) ?? null
})
function phaseState(p: string): 'done' | 'current' | 'pending' {
  if (done.value.has(p))
    return 'done'
  if (p === currentPhase.value)
    return 'current'
  return 'pending'
}
const showStepper = computed(() => props.status === 'refining' || done.value.size > 0)

const badge = computed(() => {
  switch (props.status) {
    case 'refining': return { text: 'Refining…', cls: 'text-blue-600 dark:text-blue-300' }
    case 'draft_ready': return { text: 'Ready', cls: 'text-green-600 dark:text-green-400' }
    case 'failed': return { text: 'Failed', cls: 'text-danger-text' }
    default: return { text: '', cls: '' }
  }
})
</script>

<template>
  <div v-if="show" class="rounded-md border border-line bg-surface px-3.5 py-2.5 text-sm">
    <button type="button" class="flex items-center gap-2 w-full text-left" @click="expanded = !expanded">
      <span v-if="status === 'refining'" class="inline-block h-2 w-2 rounded-full bg-blue-500 animate-pulse" />
      <span class="font-semibold" :class="badge.cls">Refinement: {{ badge.text }}</span>
    </button>
    <ol v-if="showStepper" class="flex flex-wrap gap-x-3 gap-y-1 mt-2 text-[11px]">
      <li
        v-for="p in PHASE_ORDER"
        :key="p"
        :data-phase-state="phaseState(p)"
        class="flex items-center gap-1"
        :class="{
          'text-green-600 dark:text-green-400': phaseState(p) === 'done',
          'text-blue-600 dark:text-blue-300 font-semibold': phaseState(p) === 'current',
          'text-muted': phaseState(p) === 'pending',
        }"
      >
        <span>{{ phaseState(p) === 'done' ? '✓' : phaseState(p) === 'current' ? '◷' : '○' }}</span>
        <span>{{ PHASE_LABELS[p] }}</span>
      </li>
    </ol>
    <p v-if="status === 'failed' && error" class="mt-1.5 text-[0.8rem] text-danger-text whitespace-pre-wrap">
      {{ error }}
    </p>
    <pre
      v-else-if="lastOutput"
      class="mt-1.5 text-[0.8rem] text-muted whitespace-pre-wrap overflow-hidden"
      :class="expanded ? '' : 'max-h-16'"
    >{{ lastOutput }}</pre>
  </div>
</template>
