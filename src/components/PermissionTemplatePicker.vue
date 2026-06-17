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
  <div class="space-y-1">
    <p class="text-xs text-fg-mute mb-2">
      Permission Template
    </p>
    <div class="flex flex-wrap gap-2">
      <button
        v-for="t in TEMPLATES"
        :key="t.id"
        type="button"
        :title="t.description"
        class="px-3 py-1.5 rounded-full text-xs font-medium border transition-colors"
        :class="modelValue === t.id
          ? 'bg-accent border-accent text-accent-contrast'
          : 'bg-raised border-line-strong text-fg-soft hover:border-accent'"
        @click="select(t.id)"
      >
        {{ t.label }}
      </button>
    </div>
    <p v-if="modelValue" class="text-[11px] text-fg-faint">
      {{ TEMPLATES.find(t => t.id === modelValue)?.description }}
    </p>
  </div>
</template>
