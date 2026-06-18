<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import AppInput from './ui/AppInput.vue'
import { useConfigExplorer } from '../composables/useConfigExplorer'

type Tab = 'skills' | 'commands' | 'memory'

// Pure presentation of the skills/commands/memory resolved for ONE spawner.
// The scope is driven entirely by the `spawnerId` prop — `undefined` resolves
// against the default (claude-default) CLAUDE_CONFIG_DIR. There is no in-view
// scope picker any more; the surrounding spawner detail view owns the scope.
const props = defineProps<{ spawnerId?: string }>()

const {
  skills,
  commands,
  memory,
  engineVersion,
  builtinsMayBeStale,
  scopeLabel,
  isLoading,
  error,
  refresh,
  setSpawner,
} = useConfigExplorer()

// Re-enumerate whenever the spawner under inspection changes. `immediate` so the
// first paint already reflects the requested scope rather than the singleton's
// last-loaded one.
watch(() => props.spawnerId, id => setSpawner(id), { immediate: true })

const activeTab = ref<Tab>('skills')
const searchQuery = ref('')
const expandedCommand = ref<string | null>(null)

const filteredSkills = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q)
    return skills.value
  return skills.value.filter(s =>
    s.name.toLowerCase().includes(q)
    || s.description.toLowerCase().includes(q)
    || s.source.toLowerCase().includes(q),
  )
})

const filteredCommands = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q)
    return commands.value
  return commands.value.filter(c =>
    c.name.toLowerCase().includes(q)
    || c.description.toLowerCase().includes(q)
    || c.source.toLowerCase().includes(q),
  )
})

const filteredMemory = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q)
    return memory.value
  return memory.value.filter(m =>
    m.path.toLowerCase().includes(q)
    || m.scope.toLowerCase().includes(q),
  )
})

function commandKey(c: { name: string, source: string }): string {
  return `${c.source}:${c.name}`
}

function toggleCommand(c: { name: string, source: string }) {
  const key = commandKey(c)
  expandedCommand.value = expandedCommand.value === key ? null : key
}

function formatBytes(n: number): string {
  if (n < 1024)
    return `${n} B`
  if (n < 1024 * 1024)
    return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

function formatTimestamp(unixSeconds: number): string {
  const d = new Date(unixSeconds * 1000)
  return d.toLocaleString()
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <!-- Scope strip: which CLAUDE_CONFIG_DIR these capabilities resolved against. -->
    <div v-if="scopeLabel || engineVersion || builtinsMayBeStale" class="flex items-center gap-2 flex-wrap">
      <span v-if="scopeLabel" class="text-[11px] text-fg-mute px-1.5 py-0.5 rounded bg-raised">
        {{ scopeLabel }}
      </span>
      <span v-if="engineVersion" class="text-[11px] text-fg-faint">
        engine v{{ engineVersion }}
      </span>
      <span
        v-if="builtinsMayBeStale"
        class="text-[11px] text-amber-700 dark:text-amber-400 px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/30"
        title="The engine version differs from the version the built-in command list was curated against; built-ins shown may be out of date."
      >
        built-ins may be stale
      </span>
    </div>

    <div class="flex items-center gap-2 flex-wrap">
      <div class="flex bg-raised rounded-md overflow-hidden">
        <button
          type="button"
          class="px-3 py-1.5 text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="activeTab === 'skills' ? 'bg-blue-600 text-white' : 'bg-transparent text-fg-mute hover:text-fg-soft'"
          @click="activeTab = 'skills'"
        >
          Skills <span class="opacity-60">({{ skills.length }})</span>
        </button>
        <button
          type="button"
          class="px-3 py-1.5 text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="activeTab === 'commands' ? 'bg-blue-600 text-white' : 'bg-transparent text-fg-mute hover:text-fg-soft'"
          @click="activeTab = 'commands'"
        >
          Commands <span class="opacity-60">({{ commands.length }})</span>
        </button>
        <button
          type="button"
          class="px-3 py-1.5 text-[13px] font-sans border-none cursor-pointer transition-all"
          :class="activeTab === 'memory' ? 'bg-blue-600 text-white' : 'bg-transparent text-fg-mute hover:text-fg-soft'"
          @click="activeTab = 'memory'"
        >
          Memory <span class="opacity-60">({{ memory.length }})</span>
        </button>
      </div>
      <AppInput
        v-model="searchQuery"
        :placeholder="`Filter ${activeTab}...`"
        class="w-[260px]"
      />
      <button
        type="button"
        class="bg-raised text-fg-mute border-none rounded-md px-3 py-1.5 text-[13px] font-sans cursor-pointer hover:text-fg-soft"
        :disabled="isLoading"
        @click="refresh"
      >
        {{ isLoading ? 'Reloading...' : 'Reload' }}
      </button>
    </div>

    <p v-if="isLoading && skills.length === 0 && commands.length === 0 && memory.length === 0" class="text-fg-mute text-sm py-8 text-center">
      Loading configuration...
    </p>
    <p v-else-if="error" class="text-danger-text text-sm py-4 text-center">
      Error: {{ error }}
    </p>

    <!-- Skills tab -->
    <div v-if="activeTab === 'skills'" class="flex flex-col gap-1.5">
      <p v-if="filteredSkills.length === 0 && !isLoading" class="text-fg-mute text-sm py-8 text-center">
        No skills found.
      </p>
      <article
        v-for="skill in filteredSkills"
        :key="`${skill.source}:${skill.name}`"
        class="bg-card border border-line rounded-md p-3 flex flex-col gap-1"
      >
        <header class="flex items-baseline gap-2 flex-wrap">
          <h3 class="text-sm font-semibold text-fg m-0">
            {{ skill.name }}
          </h3>
          <span class="text-[11px] text-fg-mute px-1.5 py-0.5 rounded bg-raised">{{ skill.source }}</span>
        </header>
        <p v-if="skill.description" class="text-[12px] text-fg-mute m-0 leading-relaxed">
          {{ skill.description }}
        </p>
      </article>
    </div>

    <!-- Commands tab -->
    <div v-else-if="activeTab === 'commands'" class="flex flex-col gap-1.5">
      <p v-if="filteredCommands.length === 0 && !isLoading" class="text-fg-mute text-sm py-8 text-center">
        No commands found.
      </p>
      <article
        v-for="cmd in filteredCommands"
        :key="commandKey(cmd)"
        class="bg-card border border-line rounded-md p-3 flex flex-col gap-1"
      >
        <header
          class="flex items-baseline gap-2 flex-wrap cursor-pointer"
          @click="toggleCommand(cmd)"
        >
          <h3 class="text-sm font-semibold text-fg m-0">
            {{ cmd.name }}
          </h3>
          <span class="text-[11px] text-fg-mute px-1.5 py-0.5 rounded bg-raised">{{ cmd.source }}</span>
          <span class="ml-auto text-[11px] text-fg-faint">
            {{ expandedCommand === commandKey(cmd) ? '▲ hide' : '▼ show body' }}
          </span>
        </header>
        <p v-if="cmd.description" class="text-[12px] text-fg-mute m-0 leading-relaxed">
          {{ cmd.description }}
        </p>
        <pre
          v-if="expandedCommand === commandKey(cmd)"
          class="bg-app border border-line rounded p-2 text-[11px] text-fg-soft m-0 mt-1 overflow-x-auto max-h-[400px] overflow-y-auto whitespace-pre-wrap break-words font-mono"
        >{{ cmd.body }}</pre>
      </article>
    </div>

    <!-- Memory tab -->
    <div v-else-if="activeTab === 'memory'" class="flex flex-col gap-1.5">
      <p v-if="filteredMemory.length === 0 && !isLoading" class="text-fg-mute text-sm py-8 text-center">
        No memory files found.
      </p>
      <article
        v-for="mem in filteredMemory"
        :key="mem.path"
        class="bg-card border border-line rounded-md p-3 flex flex-col gap-1"
      >
        <header class="flex items-baseline gap-2 flex-wrap">
          <span class="text-[11px] text-fg-mute px-1.5 py-0.5 rounded bg-raised">{{ mem.scope }}</span>
          <code class="text-[12px] text-fg font-mono break-all">{{ mem.path }}</code>
        </header>
        <p class="text-[11px] text-fg-mute m-0">
          {{ formatBytes(mem.size) }} · modified {{ formatTimestamp(mem.mtime) }}
        </p>
      </article>
    </div>
  </div>
</template>
