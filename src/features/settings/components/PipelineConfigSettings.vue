<script setup lang="ts">
import type { PipelineConfig } from '@/features/pipeline'
import { computed, onMounted, ref, watch } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { useSpawners } from '@/composables/useSpawners'
import { toast } from '@/composables/useToast'
import { usePipelineConfig } from '@/features/pipeline'
import { AVAILABLE_MODELS } from '@/utils/models'
import { STAGE_LABELS } from '@/utils/stageLabels'

const { config, loading, error, fetchConfig, saveConfig } = usePipelineConfig()
const { spawners } = useSpawners()

onMounted(() => {
  void fetchConfig()
})

// Local draft — only written back on save
const draft = ref<PipelineConfig | null>(null)
const saved = ref(false)

const effectiveConfig = computed(() => draft.value ?? config.value)

// Sync draft when config first arrives
watch(config, (val) => {
  if (val && !draft.value)
    draft.value = JSON.parse(JSON.stringify(val)) as PipelineConfig
}, { immediate: true })

// Surface load/save failures as toasts; the view shows empty/loading state instead.
watch(error, (msg) => {
  if (msg)
    toast.error(msg)
})

async function handleSave() {
  if (!draft.value)
    return
  saved.value = false
  await saveConfig({
    maxParallelOrchestrators: draft.value.maxParallelOrchestrators,
    stageTimeoutSeconds: draft.value.stageTimeoutSeconds,
    stageModels: { ...draft.value.stageModels },
    stageSpawners: { ...draft.value.stageSpawners },
  })
  if (!error.value) {
    saved.value = true
    setTimeout(() => {
      saved.value = false
    }, 2500)
  }
}

const STAGES = ['implementation', 'self_review', 'finalization'] as const

const stageSpawnerOptions = computed(() => [
  { value: '', label: 'Auto (Task → Projekt → Default)' },
  ...spawners.value.map(s => ({ value: s.id, label: `${s.name}${s.builtIn ? ' (built-in)' : ''}` })),
])

const stageModelOptions = AVAILABLE_MODELS.map(model => ({ value: model, label: model }))
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Pipeline Configuration
      </h3>
      <p class="text-xs text-fg-mute">
        Global pipeline settings. Changes take effect for newly started stage runs.
      </p>
    </div>

    <div v-if="loading && !effectiveConfig" class="text-sm text-fg-mute py-8 text-center">
      Loading…
    </div>

    <template v-else-if="effectiveConfig">
      <!-- Parallelism + timeout -->
      <div class="border border-line rounded-lg p-4 flex flex-col gap-4">
        <h4 class="text-sm font-semibold text-fg">
          Runner Settings
        </h4>

        <div class="grid grid-cols-2 gap-4">
          <div>
            <label
              class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1"
              for="pc-parallel"
            >
              Max Parallel Orchestrators
            </label>
            <input
              id="pc-parallel"
              v-model.number="effectiveConfig.maxParallelOrchestrators"
              type="number"
              min="1"
              max="20"
              class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus:outline-none focus:border-blue-500"
            >
            <p class="text-[11px] text-fg-mute mt-0.5">
              Concurrent agent-driven stage slots (1–20).
            </p>
          </div>

          <div>
            <label
              class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1"
              for="pc-timeout"
            >
              Stage Timeout (seconds)
            </label>
            <input
              id="pc-timeout"
              v-model.number="effectiveConfig.stageTimeoutSeconds"
              type="number"
              min="0"
              class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus:outline-none focus:border-blue-500"
            >
            <p class="text-[11px] text-fg-mute mt-0.5">
              SIGTERM is sent after this many seconds. Set to 0 to disable.
            </p>
          </div>
        </div>
      </div>

      <!-- Per-stage model + spawner pickers -->
      <div class="border border-line rounded-lg p-4 flex flex-col gap-4">
        <div>
          <h4 class="text-sm font-semibold text-fg mb-0.5">
            Per-Stage Settings
          </h4>
          <p class="text-[11px] text-fg-mute">
            Configure the spawner and model used for each pipeline stage. The model only applies to Claude-native spawners.
          </p>
        </div>

        <div class="grid grid-cols-1 gap-4">
          <div v-for="stage in STAGES" :key="stage" class="grid grid-cols-2 gap-3 items-end">
            <div>
              <label
                class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1"
                :for="`pc-spawner-${stage}`"
              >
                {{ STAGE_LABELS[stage] }} — Spawner
              </label>
              <AppSelect
                :id="`pc-spawner-${stage}`"
                :model-value="effectiveConfig.stageSpawners[stage]"
                :options="stageSpawnerOptions"
                class="w-full"
                @update:model-value="effectiveConfig.stageSpawners[stage] = $event as string"
              />
            </div>
            <div>
              <label
                class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1"
                :for="`pc-model-${stage}`"
              >
                {{ STAGE_LABELS[stage] }} — Model
              </label>
              <AppSelect
                :id="`pc-model-${stage}`"
                :model-value="effectiveConfig.stageModels[stage]"
                :options="stageModelOptions"
                class="w-full"
                @update:model-value="effectiveConfig.stageModels[stage] = $event as string"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Save bar -->
      <div class="flex items-center gap-3">
        <AppButton variant="info" :disabled="loading" @click="handleSave">
          {{ loading ? 'Saving…' : 'Save Changes' }}
        </AppButton>
        <span v-if="saved" class="text-xs text-emerald-600 dark:text-emerald-400">Saved.</span>
      </div>
    </template>
  </div>
</template>
