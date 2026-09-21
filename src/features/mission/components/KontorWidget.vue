<script setup lang="ts">
import { onKeyStroke } from '@vueuse/core'
import { computed, nextTick, ref } from 'vue'
import { useAgents } from '@/features/agents'
import { useKontorSession } from '../composables/useKontorSession'
import KontorTile from './KontorTile.vue'

const { pid, status } = useKontorSession()
const { agents } = useAgents({ autoStart: false })
const agent = computed(() => (pid.value === null ? null : agents.value.find(a => a.pid === pid.value) ?? null))

const state = computed(() => {
  const a = agent.value
  if (!a)
    return status.value === 'starting' ? 'starting' : 'no session'
  if (a.status === 'waiting')
    return 'waiting'
  return a.working ? 'working' : 'idle'
})
const last = computed(() => agent.value?.lastOutput ?? (agent.value ? '' : 'Start Kontor — ask for anything'))

const cell = ref<HTMLElement | null>(null)
const open = ref(false)
const box = ref<Record<string, string>>({})

// Grows out of its own cell towards the larger free side, up to 64% of the
// window, as an overlay: the grid underneath does not re-flow.
function place() {
  const r = cell.value!.getBoundingClientRect()
  const vh = window.innerHeight
  const up = r.top > vh - r.bottom
  const height = Math.min(vh * 0.64, (up ? r.bottom : vh - r.top) - 16)
  box.value = {
    left: `${r.left}px`,
    width: `${r.width}px`,
    height: `${height}px`,
    ...(up ? { bottom: `${vh - r.bottom}px` } : { top: `${r.top}px` }),
  }
}

function grow() {
  place()
  open.value = true
}

async function collapse() {
  open.value = false
  await nextTick()
  cell.value?.focus()
}

function typingIn(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  return !!el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.isContentEditable)
}

onKeyStroke('/', (e) => {
  if (open.value || typingIn(e.target))
    return
  e.preventDefault()
  grow()
})
onKeyStroke('Escape', () => {
  if (open.value)
    collapse()
})
</script>

<template>
  <div
    ref="cell"
    data-testid="kontor-collapsed"
    role="button"
    tabindex="0"
    aria-label="Kontor — open the session"
    class="flex h-full min-w-0 cursor-pointer items-center gap-3 rounded-xl border border-line bg-card px-4"
    @click="grow"
    @keydown.enter.prevent="grow"
  >
    <span data-testid="kontor-collapsed-state" class="shrink-0 font-mono text-[11px]" :class="state === 'waiting' ? 'text-warning-text' : state === 'working' ? 'text-success-text' : 'text-fg-mute'">
      ● {{ state }}
    </span>
    <b class="shrink-0 text-[13px] text-fg">Kontor</b>
    <span data-testid="kontor-collapsed-last" class="min-w-0 flex-1 truncate text-[12.5px] text-fg-soft">{{ last }}</span>
    <kbd class="shrink-0 rounded border border-line-strong px-1 font-mono text-[10px] text-fg-mute">/</kbd>
  </div>

  <Teleport to="body">
    <Transition enter-from-class="opacity-0 translate-y-2" leave-to-class="opacity-0 translate-y-2" enter-active-class="transition duration-200 motion-reduce:transition-none" leave-active-class="transition duration-150 motion-reduce:transition-none">
      <div v-if="open" data-testid="kontor-expanded" class="fixed z-40 flex flex-col overflow-hidden rounded-xl border border-accent bg-card shadow-2xl" :style="box">
        <button type="button" data-testid="kontor-collapse" aria-label="Collapse Kontor (Esc)" class="absolute right-2 top-2 z-10 rounded border border-line-strong px-1.5 text-[12px] text-fg-mute" @click="collapse">
          ▾
        </button>
        <KontorTile class="h-full min-h-0 flex-1" />
      </div>
    </Transition>
  </Teleport>
</template>
