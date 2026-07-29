<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, useId, watch } from 'vue'

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
const panelPosition = ref<{ top?: string, bottom?: string, left: string, minWidth: string, maxWidth: string }>({ left: '0px', minWidth: '0px', maxWidth: '0px' })

let typeaheadBuffer = ''
let typeaheadTimer: ReturnType<typeof setTimeout> | null = null

// One-shot suppression for the click that follows a dismissing outside
// mousedown, so closing the panel doesn't also activate whatever was under
// the pointer (a native popup consumed that click too). Cleared on the very
// next mousedown as well, in case the pointer is dragged away and no click
// ever follows.
let suppressNextClick: ((e: MouseEvent) => void) | null = null

function clearClickSuppression() {
  if (suppressNextClick) {
    document.removeEventListener('click', suppressNextClick, true)
    suppressNextClick = null
  }
}

const selectedIndex = computed(() => props.options.findIndex(o => o.value === props.modelValue))
const selectedLabel = computed(() => props.options[selectedIndex.value]?.label ?? '')
const activeOptionId = computed(() => (isOpen.value && activeIndex.value >= 0 ? optionId(activeIndex.value) : undefined))

function optionId(idx: number): string {
  return `${panelId}-option-${idx}`
}

// Active row uses the solid accent fill (same bg-accent/text-accent-contrast
// pairing as AppButton's primary variant) instead of bg-card, whose contrast
// against the bg-raised panel measured 1.10:1/1.13:1 (light/dark) — far below
// the 3:1 WCAG 2.2 non-text minimum and visually indistinguishable from the
// panel. Selected-only rows keep the lighter text-accent + checkmark cue so
// active and selected stay visually distinct even when combined.
function optionClass(opt: SelectOption, idx: number) {
  if (opt.disabled)
    return 'opacity-50 cursor-not-allowed text-fg-faint'
  if (idx === activeIndex.value) {
    return ['bg-accent text-accent-contrast', idx === selectedIndex.value ? 'font-medium' : '']
  }
  return idx === selectedIndex.value ? 'text-accent font-medium' : 'text-fg'
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

// Small gap kept between the panel and the viewport edge so it never
// touches the browser chrome.
const VIEWPORT_MARGIN = 8

function updatePosition() {
  const trigger = triggerRef.value
  if (!trigger)
    return
  const rect = trigger.getBoundingClientRect()
  const measuredHeight = panelRef.value?.getBoundingClientRect().height ?? PANEL_MAX_HEIGHT
  const spaceBelow = window.innerHeight - rect.bottom
  const spaceAbove = rect.top
  const flip = spaceBelow < measuredHeight && spaceAbove > spaceBelow
  // `left` is clamped to keep the trigger's own width on-screen; `maxWidth`
  // is then derived from whatever space remains to the right of that left
  // edge, so a long option label (or a long selected-value label) can never
  // push the fixed-position panel past the viewport's right edge.
  const left = Math.max(VIEWPORT_MARGIN, Math.min(rect.left, window.innerWidth - VIEWPORT_MARGIN - rect.width))
  const maxWidth = window.innerWidth - VIEWPORT_MARGIN - left
  panelPosition.value = flip
    ? { bottom: `${window.innerHeight - rect.top}px`, left: `${left}px`, minWidth: `${rect.width}px`, maxWidth: `${maxWidth}px` }
    : { top: `${rect.bottom}px`, left: `${left}px`, minWidth: `${rect.width}px`, maxWidth: `${maxWidth}px` }
}

async function openPanel() {
  if (props.disabled || isOpen.value)
    return
  isOpen.value = true
  activeIndex.value = selectedIndex.value >= 0 ? selectedIndex.value : firstEnabledIndex()
  // WKWebView (the desktop app's webview) does not focus a <button> on
  // click, so without this the trigger's @keydown handler never receives
  // events after a mouse-opened panel.
  triggerRef.value?.focus()
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
  const value = typeof props.modelValue === 'number' ? Number(opt.value) : opt.value
  // A native `change` never fired when the value was unchanged — re-picking
  // the current selection must not trigger downstream side effects (PATCH
  // requests, folder re-resolution, tab switches, ...).
  if (value !== props.modelValue)
    emit('update:modelValue', value)
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
        // Only swallow Escape while the panel is open — a native select
        // popup consumed that keypress too. When the panel is already
        // closed, Escape must keep bubbling so it can still close an
        // enclosing modal (SpawnDialog's window listener, AppModal's
        // @keydown.escape).
        e.preventDefault()
        e.stopPropagation()
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
  // The mousedown that dismisses the panel is immediately followed by a
  // click on the same element (the modal backdrop, an agent card, ...) — a
  // native popup swallowed that click too. Intercept it once, in the
  // capture phase, so it never reaches the target's own click handler.
  clearClickSuppression()
  suppressNextClick = (clickEvent: MouseEvent) => {
    clickEvent.preventDefault()
    clickEvent.stopPropagation()
    clearClickSuppression()
  }
  document.addEventListener('click', suppressNextClick, true)
}

// Runs on every mousedown, independent of isOpen, purely to drop a pending
// click-suppression left over from a previous outside-mousedown whose click
// never arrived (e.g. the pointer was pressed down and dragged elsewhere) —
// otherwise it would wrongly swallow an unrelated later click.
function onAnyMouseDown() {
  clearClickSuppression()
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

onMounted(() => {
  document.addEventListener('mousedown', onAnyMouseDown, true)
})

onUnmounted(() => {
  window.removeEventListener('scroll', onWindowScrollOrResize, true)
  window.removeEventListener('resize', onWindowScrollOrResize)
  document.removeEventListener('mousedown', onDocumentMouseDown, true)
  document.removeEventListener('mousedown', onAnyMouseDown, true)
  clearClickSuppression()
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
    <span class="truncate" :title="selectedLabel">{{ selectedLabel }}</span>
    <span aria-hidden="true" class="text-fg-mute text-xs leading-none flex-shrink-0">▾</span>
  </button>

  <Teleport to="body">
    <div
      v-if="isOpen"
      :id="panelId"
      ref="panelRef"
      role="listbox"
      :aria-label="ariaLabel"
      class="fixed z-[1100] bg-raised border border-line-strong rounded-md shadow-modal py-1 overflow-y-auto"
      :style="{ ...panelPosition, maxHeight: `${PANEL_MAX_HEIGHT}px` }"
    >
      <!--
        z-[1100] must stay above AppModal's z-index (TaskModal/AgentModal
        both pass :z-index="1000") — equal z-index left paint order to
        Teleport DOM insertion order, which only worked by accident.
      -->
      <div
        v-for="(opt, idx) in options"
        :id="optionId(idx)"
        :key="opt.value"
        role="option"
        :aria-selected="idx === selectedIndex"
        :aria-disabled="opt.disabled ? 'true' : undefined"
        :title="opt.label"
        class="px-3 py-1.5 text-sm flex items-center justify-between gap-2"
        :class="optionClass(opt, idx)"
        @click="onOptionClick(opt)"
        @mousemove="onOptionMouseMove(idx, opt)"
      >
        <span class="truncate">{{ opt.label }}</span>
        <span v-if="idx === selectedIndex" aria-hidden="true" class="flex-shrink-0" :class="idx === activeIndex ? 'text-accent-contrast' : 'text-accent'">✓</span>
      </div>
    </div>
  </Teleport>
</template>
