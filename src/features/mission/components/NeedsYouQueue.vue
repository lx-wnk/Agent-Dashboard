<script setup lang="ts">
import type { NextKind } from '../composables/useNextThing'
import { computed, inject, onMounted, ref, watch } from 'vue'
import { OPEN_TASK, PENDING_PERMISSIONS } from '@/composables/openTask'
import { useAgents } from '@/features/agents'
import { useTasks } from '@/features/pipeline'
import { rankNextThings } from '../composables/useNextThing'
import NextThing from './NextThing.vue'

const props = defineProps<{ variant: 'docked' | 'strip', kinds?: NextKind[] }>()

const { tasks, refetch } = useTasks()
// App.vue provides the one usePendingPermissions(tasks) instance — a local
// call here would open a second, out-of-sync cache.
const pendingPermissions = inject(PENDING_PERMISSIONS)
if (!pendingPermissions)
  throw new Error('NeedsYouQueue requires PENDING_PERMISSIONS to be provided by App.vue')
const { items: pending, refresh } = pendingPermissions
// autoStart: false — App.vue owns the stream.
const { agents } = useAgents({ autoStart: false })
const openTask = inject(OPEN_TASK, () => {})
onMounted(refetch)

const ranked = computed(() => {
  const all = rankNextThings(pending.value, tasks.value, agents.value)
  return props.kinds ? all.filter(t => props.kinds!.includes(t.kind)) : all
})
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
