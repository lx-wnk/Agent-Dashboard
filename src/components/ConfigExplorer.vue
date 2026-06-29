<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ConflictError, useConfigExplorer } from '../composables/useConfigExplorer'
import { toast } from '../composables/useToast'
import AppInput from './ui/AppInput.vue'

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
  loadFile,
  saveFile,
} = useConfigExplorer()

// Re-enumerate whenever the spawner under inspection changes. `immediate` so the
// first paint already reflects the requested scope rather than the singleton's
// last-loaded one.
watch(() => props.spawnerId, id => setSpawner(id), { immediate: true })

// Surface config-enumeration load failures as toasts; the view keeps its empty state.
watch(error, (msg) => {
  if (msg)
    toast.error(msg)
})

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

// ---- inline editor for user/project files ----
const editorPath = ref<string | null>(null)
const editorSource = ref('')
const editorContent = ref('')
const editorOriginal = ref('')
const editorBaseMtime = ref(0)
const editorLoading = ref(false)
const editorSaving = ref(false)
const editorError = ref<string | null>(null)
const editorConflict = ref(false)

const isDirty = computed(() => editorContent.value !== editorOriginal.value)

async function doLoad(path: string): Promise<void> {
  editorError.value = null
  editorConflict.value = false
  editorLoading.value = true
  editorPath.value = path
  try {
    const f = await loadFile(path, props.spawnerId)
    editorContent.value = f.content
    editorOriginal.value = f.content
    editorBaseMtime.value = f.mtime
    editorSource.value = f.source
  }
  catch (e) {
    editorError.value = (e as Error).message
    editorPath.value = null
  }
  finally {
    editorLoading.value = false
  }
}

function confirmDiscard(): boolean {
  // eslint-disable-next-line no-alert
  return !isDirty.value || window.confirm('Discard unsaved changes?')
}

function openEditor(path?: string): void {
  if (!path || editorSaving.value)
    return
  if (!confirmDiscard())
    return
  void doLoad(path)
}

function reloadEditor(): void {
  if (editorPath.value)
    void doLoad(editorPath.value)
}

async function saveEditor(): Promise<void> {
  if (!editorPath.value || editorSaving.value)
    return
  editorSaving.value = true
  editorError.value = null
  editorConflict.value = false
  try {
    const res = await saveFile(editorPath.value, editorContent.value, editorBaseMtime.value, props.spawnerId)
    editorBaseMtime.value = res.mtime
    editorOriginal.value = editorContent.value
    await refresh()
  }
  catch (e) {
    if (e instanceof ConflictError)
      editorConflict.value = true
    else
      editorError.value = (e as Error).message
  }
  finally {
    editorSaving.value = false
  }
}

function closeEditor(): void {
  if (!confirmDiscard())
    return
  editorPath.value = null
  editorContent.value = ''
  editorOriginal.value = ''
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
          <button
            v-if="skill.editable && skill.path"
            type="button"
            class="ml-auto text-[11px] text-blue-600 dark:text-blue-400 border-none bg-transparent cursor-pointer hover:underline"
            @click="openEditor(skill.path)"
          >
            Edit
          </button>
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
          <button
            v-if="cmd.editable && cmd.path"
            type="button"
            class="ml-auto text-[11px] text-blue-600 dark:text-blue-400 border-none bg-transparent cursor-pointer hover:underline"
            @click.stop="openEditor(cmd.path)"
          >
            Edit
          </button>
          <span class="text-[11px] text-fg-faint" :class="cmd.editable && cmd.path ? '' : 'ml-auto'">
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
          <button
            v-if="mem.editable"
            type="button"
            class="ml-auto text-[11px] text-blue-600 dark:text-blue-400 border-none bg-transparent cursor-pointer hover:underline"
            @click="openEditor(mem.path)"
          >
            Edit
          </button>
        </header>
        <p class="text-[11px] text-fg-mute m-0">
          {{ formatBytes(mem.size) }} · modified {{ formatTimestamp(mem.mtime) }}
        </p>
      </article>
    </div>

    <!-- Inline editor for an editable (user/project) file -->
    <div
      v-if="editorPath"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      @click.self="closeEditor"
    >
      <div class="bg-card border border-line rounded-lg shadow-xl w-full max-w-3xl max-h-[85vh] flex flex-col">
        <header class="flex items-center gap-2 p-3 border-b border-line">
          <span class="text-[11px] text-fg-mute px-1.5 py-0.5 rounded bg-raised">{{ editorSource }}</span>
          <code class="text-[12px] text-fg font-mono break-all flex-1">{{ editorPath }}</code>
          <span v-if="isDirty" class="text-[11px] text-amber-600 dark:text-amber-400">● unsaved</span>
          <button
            type="button"
            class="text-fg-mute border-none bg-transparent cursor-pointer hover:text-fg-soft text-lg leading-none px-1"
            title="Close"
            @click="closeEditor"
          >
            ×
          </button>
        </header>

        <div
          v-if="editorConflict"
          class="m-3 mb-0 px-3 py-2 rounded bg-amber-100 dark:bg-amber-900/30 text-[12px] text-amber-800 dark:text-amber-300 flex items-center gap-2"
        >
          <span class="flex-1">File changed on disk since you loaded it. Reload to get the latest (your edits will be lost).</span>
          <button
            type="button"
            class="text-[11px] font-semibold text-amber-900 dark:text-amber-200 border border-amber-400 rounded px-2 py-0.5 cursor-pointer hover:bg-amber-200/50"
            @click="reloadEditor"
          >
            Reload
          </button>
        </div>

        <p v-if="editorError" class="m-3 mb-0 text-red-600 dark:text-red-400 text-[12px]">
          {{ editorError }}
        </p>

        <div class="p-3 flex-1 overflow-hidden flex">
          <textarea
            v-if="!editorLoading"
            v-model="editorContent"
            spellcheck="false"
            class="w-full h-full min-h-[320px] resize-none bg-app border border-line rounded p-2 text-[12px] text-fg-soft font-mono focus:outline-none focus:border-blue-500"
          />
          <p v-else class="text-fg-mute text-sm m-auto">
            Loading file...
          </p>
        </div>

        <footer class="flex items-center gap-2 p-3 border-t border-line">
          <span class="text-[11px] text-fg-faint mr-auto">{{ editorContent.length }} chars</span>
          <button
            type="button"
            class="bg-raised text-fg-mute border-none rounded-md px-3 py-1.5 text-[13px] cursor-pointer hover:text-fg-soft"
            @click="closeEditor"
          >
            Cancel
          </button>
          <button
            type="button"
            class="bg-blue-600 text-white border-none rounded-md px-3 py-1.5 text-[13px] cursor-pointer hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="!isDirty || editorSaving || editorLoading"
            @click="saveEditor"
          >
            {{ editorSaving ? 'Saving...' : 'Save' }}
          </button>
        </footer>
      </div>
    </div>
  </div>
</template>
