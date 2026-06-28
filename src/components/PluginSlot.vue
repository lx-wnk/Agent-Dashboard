<!-- src/components/PluginSlot.vue -->
<script setup lang="ts" generic="S extends SlotName = SlotName">
import type { LoadedAddon, SlotContracts, SlotName, SlotParent, UnmountFn } from '../utils/pluginSlot'
import { onBeforeUnmount, onMounted, ref, toRaw } from 'vue'
import { loadSlotAddons } from '../composables/usePluginSlots'

const props = withDefaults(defineProps<{
  name: S
  ctx: SlotContracts[S]
  // Injectable for tests; defaults to the real discovery loader.
  loader?: (slot: SlotName) => Promise<LoadedAddon[]>
}>(), {
  loader: loadSlotAddons,
})

const containerEl = ref<HTMLElement | null>(null)
const unmounts: UnmountFn[] = []
let cancelled = false

onMounted(async () => {
  const addons = await props.loader(props.name)
  if (cancelled)
    return
  const container = containerEl.value
  if (!container)
    return

  const chain = addons.filter(a => a.mode === 'override' || a.mode === 'extend')
    .sort((a, b) => (b.priority ?? 0) - (a.priority ?? 0))
  let siblings = addons.filter(a => a.mode !== 'override' && a.mode !== 'extend')

  // An override at the top owns the slot exclusively: drop everything below it + all siblings.
  const overrideIdx = chain.findIndex(a => a.mode === 'override')
  let activeChain = chain
  if (overrideIdx !== -1) {
    activeChain = chain.slice(0, overrideIdx + 1) // keep down to (and incl.) the override
    siblings = []
  }

  // compose(i): build the parent handle for chain[i..]. override stops the chain (parent=null).
  const ctx = toRaw(props.ctx)
  const compose = (i: number): SlotParent | null => {
    if (i >= activeChain.length)
      return null
    const a = activeChain[i]
    if (a.mode === 'override')
      return { mount: (el: HTMLElement) => a.mount(el, ctx, null) }
    const parent = compose(i + 1)
    return { mount: (el: HTMLElement) => a.mount(el, ctx, parent) }
  }

  const root = compose(0)
  if (root) {
    const host = document.createElement('div')
    host.setAttribute('data-addon-host', '')
    container.appendChild(host)
    try {
      unmounts.push(root.mount(host))
    }
    catch {
      host.remove()
    }
  }

  for (const addon of siblings) {
    const host = document.createElement('div')
    host.setAttribute('data-addon-host', '')
    container.appendChild(host)
    try {
      unmounts.push(addon.mount(host, ctx))
    }
    catch {
      host.remove()
    }
  }
})

onBeforeUnmount(() => {
  cancelled = true
  for (const fn of unmounts) {
    try {
      fn()
    }
    catch {
      // Addon teardown failures must not break host unmount.
    }
  }
})
</script>

<template>
  <div ref="containerEl" class="contents" />
</template>
