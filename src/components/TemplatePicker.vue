<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { usePromptTemplates } from '../composables/usePromptTemplates'
import { fillPlaceholders, parsePlaceholders } from '../utils/promptTemplate'

defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const { templates } = usePromptTemplates()
const selectedId = ref('')
const fills = ref<Record<string, string>>({})

const selected = computed(() => templates.value.find(t => t.id === selectedId.value) ?? null)
const placeholders = computed(() => selected.value ? parsePlaceholders(selected.value.body) : [])

watch(selectedId, () => {
  fills.value = {}
})

function apply() {
  if (!selected.value)
    return
  const text = fillPlaceholders(selected.value.body, fills.value)
  emit('update:modelValue', text)
  selectedId.value = ''
}
</script>

<template>
  <div class="flex flex-wrap items-center gap-2 text-[12px]">
    <select
      v-model="selectedId"
      class="bg-raised border border-line rounded px-2 py-1 text-fg-soft text-[12px] focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-accent"
    >
      <option value="">
        Templates...
      </option>
      <option v-for="t in templates" :key="t.id" :value="t.id">
        {{ t.name }}
      </option>
    </select>
    <template v-if="placeholders.length">
      <input
        v-for="ph in placeholders"
        :key="ph"
        v-model="fills[ph]"
        :placeholder="ph"
        :data-placeholder="ph"
        class="bg-raised border border-line rounded px-2 py-1 text-fg text-[12px] font-mono w-28 focus-visible:outline-none focus-visible:ring-[2px] focus-visible:ring-accent"
      >
    </template>
    <button
      v-if="selectedId"
      type="button"
      data-apply
      class="px-2 py-1 bg-accent text-white rounded text-[12px] cursor-pointer hover:brightness-110 border-none"
      @click="apply"
    >
      Insert
    </button>
  </div>
</template>
