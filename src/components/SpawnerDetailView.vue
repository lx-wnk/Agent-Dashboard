<script setup lang="ts">
import type { Spawner, SpawnerAdapterType } from '../types'
import { computed } from 'vue'
import ConfigExplorer from './ConfigExplorer.vue'
import AppButton from './ui/AppButton.vue'

// Read-only inspection of a single spawner: its settings shown non-editable,
// plus the skills/commands/memory that a stage agent spawned by it would have.
// Editing lives in SpawnerSettings' form; this view never mutates.
const props = defineProps<{ spawner: Spawner, settingDefault?: boolean }>()
const emit = defineEmits<{ back: [], edit: [spawner: Spawner], setDefault: [spawner: Spawner] }>()

const ADAPTER_TYPE_BADGE: Record<SpawnerAdapterType, string> = {
  claude: 'bg-raised text-fg-soft',
  ollama: 'bg-cyan-50 dark:bg-cyan-950/30 text-cyan-600 dark:text-cyan-400',
  openai: 'bg-emerald-50 dark:bg-emerald-950/30 text-emerald-600 dark:text-emerald-400',
  custom: 'bg-amber-50 dark:bg-amber-950/30 text-amber-600 dark:text-amber-400',
}

function adapterTypeOf(spawner: Spawner): SpawnerAdapterType {
  // Legacy rows pre-A1 may have empty adapterType; treat as claude.
  return (spawner.adapterType || 'claude') as SpawnerAdapterType
}

function usesCommandFields(type: SpawnerAdapterType): boolean {
  return type === 'claude' || type === 'custom'
}

const adapterType = computed(() => adapterTypeOf(props.spawner))
const showCommandFields = computed(() => usesCommandFields(adapterType.value))
const envEntries = computed(() => Object.entries(props.spawner.env ?? {}))
const adapterConfigEntries = computed(() => Object.entries(props.spawner.adapterConfig ?? {}))
</script>

<template>
  <div class="flex flex-col gap-4">
    <!-- Header: back + identity + (custom only) edit shortcut -->
    <div class="flex items-start gap-3">
      <button
        type="button"
        class="bg-transparent border-none text-fg-mute cursor-pointer text-sm px-2 py-1 -ml-2 rounded hover:bg-raised hover:text-fg"
        @click="emit('back')"
      >
        ← Back
      </button>
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2 flex-wrap">
          <h3 class="text-[17px] font-bold text-fg m-0 truncate">
            {{ spawner.name }}
          </h3>
          <span
            class="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-px rounded"
            :class="ADAPTER_TYPE_BADGE[adapterType]"
          >{{ adapterType }}</span>
          <span
            class="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-px rounded"
            :class="spawner.builtIn
              ? 'bg-raised text-fg-mute'
              : 'bg-blue-50 dark:bg-blue-950/30 text-blue-600 dark:text-blue-400'"
          >{{ spawner.builtIn ? 'Built-in' : 'Custom' }}</span>
          <span
            v-if="spawner.isDefault"
            class="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-px rounded bg-emerald-50 dark:bg-emerald-950/30 text-emerald-600 dark:text-emerald-400"
            title="Used when a task or its project names no spawner"
          >★ Default</span>
        </div>
        <p v-if="spawner.description" class="text-xs text-fg-mute mt-1">
          {{ spawner.description }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <AppButton
          v-if="!spawner.isDefault"
          variant="secondary"
          size="sm"
          :disabled="settingDefault"
          @click="emit('setDefault', spawner)"
        >
          {{ settingDefault ? 'Setting…' : 'Set as default' }}
        </AppButton>
        <AppButton variant="secondary" size="sm" @click="emit('edit', spawner)">
          Edit
        </AppButton>
      </div>
    </div>

    <!-- Settings (read-only) -->
    <section class="border border-line rounded-lg p-4 flex flex-col gap-3">
      <h4 class="text-sm font-semibold text-fg m-0">
        Settings
      </h4>
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-[13px] m-0">
        <dt class="text-fg-mute">
          Slug
        </dt>
        <dd class="font-mono text-fg m-0">
          {{ spawner.slug }}
        </dd>

        <template v-if="showCommandFields">
          <dt class="text-fg-mute">
            Command
          </dt>
          <dd class="font-mono text-fg m-0 break-all">
            {{ spawner.command || '—' }}
          </dd>

          <dt class="text-fg-mute">
            Args
          </dt>
          <dd class="font-mono text-fg m-0">
            <span v-if="!spawner.args.length" class="text-fg-faint">—</span>
            <span v-else class="whitespace-pre-wrap break-all">{{ spawner.args.join(' ') }}</span>
          </dd>

          <dt class="text-fg-mute">
            Model override
          </dt>
          <dd class="font-mono text-fg m-0">
            {{ spawner.modelOverride || '—' }}
          </dd>
        </template>
      </dl>

      <!-- Adapter config -->
      <div v-if="adapterConfigEntries.length" class="flex flex-col gap-1.5">
        <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Adapter config</span>
        <div
          v-for="[k, v] in adapterConfigEntries"
          :key="k"
          class="flex gap-2 items-baseline text-xs font-mono"
        >
          <span class="text-fg-mute">{{ k }}</span>
          <span class="text-fg-faint">=</span>
          <span class="text-fg break-all">{{ v }}</span>
        </div>
      </div>

      <!-- Env vars -->
      <div v-if="showCommandFields" class="flex flex-col gap-1.5">
        <span class="text-[10px] font-semibold uppercase tracking-wider text-fg-mute">Environment variables</span>
        <p v-if="!envEntries.length" class="text-xs text-fg-mute m-0">
          None set.
        </p>
        <div
          v-for="[k, v] in envEntries"
          v-else
          :key="k"
          class="flex gap-2 items-baseline text-xs font-mono"
        >
          <span class="text-fg-mute">{{ k }}</span>
          <span class="text-fg-faint">=</span>
          <span class="text-fg break-all">{{ v }}</span>
        </div>
      </div>
    </section>

    <!-- Agent capabilities resolved for this spawner -->
    <section class="flex flex-col gap-2">
      <div>
        <h4 class="text-sm font-semibold text-fg m-0">
          Agent capabilities
        </h4>
        <p class="text-[12px] text-fg-mute m-0 mt-0.5 leading-relaxed">
          The skills, slash commands &amp; memory a stage agent spawned by this row can use — resolved against its
          <code class="font-mono">CLAUDE_CONFIG_DIR</code>.
        </p>
      </div>
      <ConfigExplorer :spawner-id="spawner.id" />
    </section>
  </div>
</template>
