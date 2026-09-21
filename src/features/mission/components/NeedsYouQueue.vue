<script setup lang="ts">
import { computed, inject, onMounted, ref, watch } from 'vue'
import { OPEN_TASK } from '@/composables/openTask'
import { usePendingPermissions } from '@/composables/usePendingPermissions'
import { useAgents } from '@/features/agents'
import { useTasks } from '@/features/pipeline'
import { rankNextThings } from '../composables/useNextThing'
import NextThing from './NextThing.vue'

defineProps<{ variant: 'docked' | 'strip' }>()

const { tasks, refetch } = useTasks()
const { items: pending, refresh } = usePendingPermissions(tasks)
// autoStart: false — App.vue owns the stream.
const { agents } = useAgents({ autoStart: false })
const openTask = inject(OPEN_TASK, () => {})
onMounted(refetch)

const ranked = computed(() => rankNextThings(pending.value, tasks.value, agents.value))
const index = ref(0)
watch(() => ranked.value.length, (n) => {
  if (index.value >= n)
    index.value = 0
})
const current = computed(() => ranked.value[index.value] ?? null)

function step(by: number) {
  const n = ranked.value.length
  index.value = (index.value + by + n) % n
}
</script>

<template>
  <section
    v-if="variant === 'docked' || ranked.length > 0"
    data-testid="needs-you"
    :data-variant="variant"
    aria-label="Needs you"
    :class="variant === 'strip' ? 'rounded-xl border border-warning-line bg-warning-soft px-4 py-3' : ''"
  >
    <div v-if="ranked.length > 1" class="mb-2 flex items-center gap-2 text-[11px] uppercase tracking-widest text-warning-text">
      <span>Needs you</span>
      <span data-testid="needs-you-position">{{ index + 1 }} of {{ ranked.length }}</span>
      <span class="flex-grow" />
      <button type="button" data-testid="needs-you-prev" aria-label="Previous" class="rounded border border-line-strong px-1.5" @click="step(-1)">
        ‹
      </button>
      <button type="button" data-testid="needs-you-next" aria-label="Next" class="rounded border border-line-strong px-1.5" @click="step(1)">
        ›
      </button>
    </div>
    <NextThing :next="current" @resolved="refresh" @open="openTask" />
  </section>
</template>
