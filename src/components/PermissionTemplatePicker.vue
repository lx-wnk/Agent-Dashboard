<script setup lang="ts">
type TemplateId = 'research_only' | 'test_only' | 'review_only' | 'feature_implementation'

const props = defineProps<{ modelValue: TemplateId | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: TemplateId | null] }>()

const TEMPLATES: ReadonlyArray<{ id: TemplateId, label: string, description: string }> = [
  { id: 'research_only', label: 'Research Only', description: 'Read + WebFetch' },
  { id: 'test_only', label: 'Tests Only', description: 'Read, Write, Bash (test cmds)' },
  { id: 'review_only', label: 'Review Only', description: 'Read, Grep, Glob' },
  { id: 'feature_implementation', label: 'Full Access', description: 'All standard tools' },
]

function select(id: TemplateId) {
  emit('update:modelValue', props.modelValue === id ? null : id)
}
</script>

<template>
  <div>
    <p class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
      Permission Template
    </p>
    <div class="grid grid-cols-2 gap-2">
      <button
        v-for="t in TEMPLATES"
        :key="t.id"
        type="button"
        class="text-left px-3 py-2.5 rounded-md border transition-colors flex flex-col gap-0.5"
        :class="modelValue === t.id
          ? 'border-accent bg-accent-soft'
          : 'border-line bg-app hover:border-accent'"
        @click="select(t.id)"
      >
        <span class="flex items-center gap-1.5 text-[13px] font-semibold" :class="modelValue === t.id ? 'text-accent' : 'text-fg'">
          <span aria-hidden="true">{{ modelValue === t.id ? '◉' : '○' }}</span>
          {{ t.label }}
        </span>
        <span class="text-[11px] text-fg-mute leading-snug pl-[18px]">{{ t.description }}</span>
      </button>
    </div>
  </div>
</template>
