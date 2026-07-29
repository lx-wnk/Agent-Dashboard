<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, useId, watch } from 'vue'

interface SelectOption { value: string | number, label: string, disabled?: boolean }

defineOptions({ inheritAttrs: false })

const props = defineProps<{
  modelValue: string | number
  options: SelectOption[]
  id?: string
  ariaLabel?: string
  disabled?: boolean
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string | number] }>()

// Panel is measured by actual rendered height once mounted; this is only the
// pre-render estimate used to decide flip direction before that measurement
// exists, and the CSS max-height applied to the panel itself.
const PANEL_MAX_HEIGHT = 320

const panelId = useId()
const triggerRef = ref<HTMLButtonElement | null>(null)
const panelRef = ref<HTMLDivElement | null>(null)

const isOpen = ref(false)
const activeIndex = ref(-1)
const panelPosition = ref<{ top?: string, bottom?: string, left: string, minWidth: string }>({ left: '0px', minWidth: '0px' })

let typeaheadBuffer = ''
let typeaheadTimer: ReturnType<typeof setTimeout> | null = null

const selectedIndex = computed(() => props.options.findIndex(o => o.value === props.modelValue))
const selectedLabel = computed(() => props.options[selectedIndex.value]?.label ?? '')
const activeOptionId = computed(() => (isOpen.value && activeIndex.value >= 0 ? optionId(activeIndex.value) : undefined))

function optionId(idx: number): string {
  return `${panelId}-option-${idx}`
}

function optionClass(opt: SelectOption, idx: number) {
  if (opt.disabled)
    return 'opacity-50 cursor-not-allowed text-fg-faint'
  return [
    idx === activeIndex.value ? 'bg-card' : '',
    idx === selectedIndex.value ? 'text-accent font-medium' : 'text-fg',
  ]
}

function firstEnabledIndex(): number {
  return props.options.findIndex(o => !o.disabled)
}

function lastEnabledIndex(): number {
  for (let i = props.options.length - 1; i >= 0; i--) {
    if (!props.options[i].disabled)
      return i
  }
  return -1
}

function moveActive(delta: number) {
  const len = props.options.length
  if (len === 0)
    return
  let idx = activeIndex.value
  for (let step = 0; step < len; step++) {
    idx = (idx + delta + len) % len
    if (!props.options[idx]?.disabled) {
      activeIndex.value = idx
      scrollActiveIntoView()
      return
    }
  }
}

function scrollActiveIntoView() {
  nextTick(() => {
    const panel = panelRef.value
    const idx = activeIndex.value
    if (!panel || idx < 0)
      return
    const el = panel.children[idx] as HTMLElement | undefined
    el?.scrollIntoView?.({ block: 'nearest' })
  })
}

function updatePosition() {
  const trigger = triggerRef.value
  if (!trigger)
    return
  const rect = trigger.getBoundingClientRect()
  const measuredHeight = panelRef.value?.getBoundingClientRect().height ?? PANEL_MAX_HEIGHT
  const spaceBelow = window.innerHeight - rect.bottom
  const spaceAbove = rect.top
  const flip = spaceBelow < measuredHeight && spaceAbove > spaceBelow
  panelPosition.value = flip
    ? { bottom: `${window.innerHeight - rect.top}px`, left: `${rect.left}px`, minWidth: `${rect.width}px` }
    : { top: `${rect.bottom}px`, left: `${rect.left}px`, minWidth: `${rect.width}px` }
}

async function openPanel() {
  if (props.disabled || isOpen.value)
    return
  isOpen.value = true
  activeIndex.value = selectedIndex.value >= 0 ? selectedIndex.value : firstEnabledIndex()
  updatePosition()
  await nextTick()
  updatePosition()
  scrollActiveIntoView()
}

function closePanel(opts: { refocus?: boolean } = {}) {
  isOpen.value = false
  resetTypeahead()
  if (opts.refocus)
    nextTick(() => triggerRef.value?.focus())
}

function toggle() {
  if (props.disabled)
    return
  if (isOpen.value)
    closePanel()
  else
    openPanel()
}

function selectOption(opt: SelectOption) {
  if (opt.disabled)
    return
  emit('update:modelValue', typeof props.modelValue === 'number' ? Number(opt.value) : opt.value)
  closePanel()
}

function commitActive() {
  const opt = props.options[activeIndex.value]
  if (!opt || opt.disabled)
    return
  selectOption(opt)
}

function onOptionClick(opt: SelectOption) {
  selectOption(opt)
}

function onOptionMouseMove(idx: number, opt: SelectOption) {
  if (opt.disabled || activeIndex.value === idx)
    return
  activeIndex.value = idx
}

function resetTypeahead() {
  typeaheadBuffer = ''
  if (typeaheadTimer) {
    clearTimeout(typeaheadTimer)
    typeaheadTimer = null
  }
}

function handleTypeahead(char: string) {
  typeaheadBuffer += char.toLowerCase()
  if (typeaheadTimer)
    clearTimeout(typeaheadTimer)
  typeaheadTimer = setTimeout(resetTypeahead, 500)
  const match = props.options.findIndex(o => !o.disabled && o.label.toLowerCase().startsWith(typeaheadBuffer))
  if (match !== -1) {
    activeIndex.value = match
    scrollActiveIntoView()
  }
}

function onTriggerKeydown(e: KeyboardEvent) {
  if (props.disabled)
    return
  switch (e.key) {
    case 'Enter':
    case ' ':
      e.preventDefault()
      if (isOpen.value)
        commitActive()
      else
        openPanel()
      break
    case 'ArrowDown':
      e.preventDefault()
      if (isOpen.value)
        moveActive(1)
      else
        openPanel()
      break
    case 'ArrowUp':
      e.preventDefault()
      if (isOpen.value)
        moveActive(-1)
      else
        openPanel()
      break
    case 'Home':
      e.preventDefault()
      activeIndex.value = firstEnabledIndex()
      if (isOpen.value)
        scrollActiveIntoView()
      break
    case 'End':
      e.preventDefault()
      activeIndex.value = lastEnabledIndex()
      if (isOpen.value)
        scrollActiveIntoView()
      break
    case 'Escape':
      if (isOpen.value) {
        e.preventDefault()
        closePanel({ refocus: true })
      }
      break
    case 'Tab':
      if (isOpen.value)
        closePanel()
      break
    default:
      if (e.key.length === 1 && !e.altKey && !e.ctrlKey && !e.metaKey)
        handleTypeahead(e.key)
  }
}

function onWindowScrollOrResize() {
  if (isOpen.value)
    updatePosition()
}

function onDocumentMouseDown(e: MouseEvent) {
  const target = e.target as Node
  if (triggerRef.value?.contains(target) || panelRef.value?.contains(target))
    return
  closePanel()
}

// Scroll uses the capture phase so a scroll on any ancestor (not just window)
// still repositions the teleported panel — chosen over closing on scroll
// because the panel would otherwise vanish for the common case of scrolling
// a modal or a card list that merely shifts the trigger, not the whole page.
watch(isOpen, (open) => {
  if (open) {
    window.addEventListener('scroll', onWindowScrollOrResize, true)
    window.addEventListener('resize', onWindowScrollOrResize)
    document.addEventListener('mousedown', onDocumentMouseDown, true)
  }
  else {
    window.removeEventListener('scroll', onWindowScrollOrResize, true)
    window.removeEventListener('resize', onWindowScrollOrResize)
    document.removeEventListener('mousedown', onDocumentMouseDown, true)
  }
})

onUnmounted(() => {
  window.removeEventListener('scroll', onWindowScrollOrResize, true)
  window.removeEventListener('resize', onWindowScrollOrResize)
  document.removeEventListener('mousedown', onDocumentMouseDown, true)
  resetTypeahead()
})
</script>

<template>
  <button
    :id="id"
    ref="triggerRef"
    v-bind="$attrs"
    type="button"
    role="combobox"
    :aria-expanded="isOpen"
    aria-haspopup="listbox"
    :aria-controls="panelId"
    :aria-activedescendant="activeOptionId"
    :aria-label="ariaLabel"
    :disabled="disabled"
    class="bg-card border border-line rounded-md px-3 py-2 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent disabled:opacity-50 cursor-pointer flex items-center justify-between gap-2 text-left"
    @click="toggle"
    @keydown="onTriggerKeydown"
  >
    <span class="truncate">{{ selectedLabel }}</span>
    <span aria-hidden="true" class="text-fg-mute text-xs leading-none flex-shrink-0">▾</span>
  </button>

  <Teleport to="body">
    <div
      v-if="isOpen"
      :id="panelId"
      ref="panelRef"
      role="listbox"
      :aria-label="ariaLabel"
      class="fixed z-[1000] bg-raised border border-line-strong rounded-md shadow-modal py-1 overflow-y-auto"
      :style="{ ...panelPosition, maxHeight: `${PANEL_MAX_HEIGHT}px` }"
    >
      <div
        v-for="(opt, idx) in options"
        :id="optionId(idx)"
        :key="opt.value"
        role="option"
        :aria-selected="idx === selectedIndex"
        :aria-disabled="opt.disabled ? 'true' : undefined"
        class="px-3 py-1.5 text-sm flex items-center justify-between gap-2"
        :class="optionClass(opt, idx)"
        @click="onOptionClick(opt)"
        @mousemove="onOptionMouseMove(idx, opt)"
      >
        <span class="truncate">{{ opt.label }}</span>
        <span v-if="idx === selectedIndex" aria-hidden="true" class="text-accent flex-shrink-0">✓</span>
      </div>
    </div>
  </Teleport>
</template>
