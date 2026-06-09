<!-- src/components/PluginSlot.vue -->
<script setup lang="ts">
import type { SlotAddon, SlotContext, UnmountFn } from '../utils/pluginSlot'
import { onBeforeUnmount, onMounted, ref, toRaw } from 'vue'
import { loadSlotAddons } from '../composables/usePluginSlots'

const props = withDefaults(defineProps<{
  name: string
  ctx: SlotContext
  // Injectable for tests; defaults to the real discovery loader.
  loader?: (slot: string) => Promise<SlotAddon[]>
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
  for (const addon of addons) {
    const host = document.createElement('div')
    host.setAttribute('data-addon-host', '')
    container.appendChild(host)
    try {
      // toRaw: addons receive the plain ctx object, not Vue's reactive proxy.
      unmounts.push(addon.mount(host, toRaw(props.ctx)))
    }
    catch {
      // A broken addon's mount must not prevent other addons from mounting.
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
