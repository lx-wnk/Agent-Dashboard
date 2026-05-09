<script setup lang="ts">
import { ref, computed, watch } from 'vue'

export interface SlashCommand {
  name: string
  description: string
}

const props = defineProps<{
  modelValue: string
  commands: SlashCommand[]
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'select': [command: SlashCommand]
}>()

const selectedIndex = ref(0)

const suggestions = computed(() => {
  const val = props.modelValue.trim()
  if (!val.startsWith('/') || val.includes(' '))
    return []
  const query = val.toLowerCase()
  return props.commands.filter(c => c.name.startsWith(query))
})

const visible = computed(() => suggestions.value.length > 0)

watch(suggestions, () => { selectedIndex.value = 0 })

function confirm(cmd: SlashCommand) {
  emit('update:modelValue', `${cmd.name} `)
  emit('select', cmd)
}

function onKeydown(e: KeyboardEvent) {
  if (!visible.value)
    return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIndex.value = Math.min(selectedIndex.value + 1, suggestions.value.length - 1)
  }
  else if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIndex.value = Math.max(selectedIndex.value - 1, 0)
  }
  else if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
    if (visible.value) {
      e.preventDefault()
      const cmd = suggestions.value[selectedIndex.value]
      if (cmd)
        confirm(cmd)
    }
  }
  else if (e.key === 'Escape') {
    e.preventDefault()
    emit('update:modelValue', '')
  }
}

defineExpose({ onKeydown, visible, confirm, suggestions, selectedIndex })
</script>

<template>
  <div
    v-if="visible"
    class="absolute bottom-full left-0 right-0 bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 border-b-0 rounded-t-md max-h-52 overflow-y-auto z-20"
  >
    <button
      v-for="(cmd, i) in suggestions"
      :key="cmd.name"
      type="button"
      class="flex items-center gap-2.5 w-full px-4 py-2 bg-transparent border-none text-slate-500 dark:text-slate-400 text-[13px] font-mono cursor-pointer text-left hover:bg-slate-100 dark:hover:bg-slate-800"
      :class="{ 'bg-slate-100 dark:bg-slate-800': i === selectedIndex }"
      @mousedown.prevent="confirm(cmd)"
    >
      <span class="text-blue-600 dark:text-blue-400 font-semibold flex-shrink-0">{{ cmd.name }}</span>
      <span class="text-slate-400 dark:text-slate-600 text-xs">{{ cmd.description }}</span>
    </button>
  </div>
</template>
