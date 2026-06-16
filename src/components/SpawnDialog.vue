<script setup lang="ts">
import type { Project } from '../types'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { fetchProjectFolders } from '../composables/useProjectFolders'
import { useProjects } from '../composables/useProjects'
import { useSpawnDialog } from '../composables/useSpawnDialog'
import { useSpawners } from '../composables/useSpawners'
import { errorMessage } from '../utils/errorMessage'
import { SPAWN_AUTOCLOSE_MS } from '../utils/timing'
import QuickCreateProjectPanel from './QuickCreateProjectPanel.vue'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'
import AppModal from './ui/AppModal.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const { projects } = useProjects()
const { spawners } = useSpawners()

const sortedProjects = computed(() =>
  projects.value.slice().sort((a, b) => a.name.localeCompare(b.name)),
)

const dlg = useSpawnDialog({
  fetchFolders: fetchProjectFolders,
  lookupSpawner: id => spawners.value.find(s => s.id === id),
})

const projectChoice = ref<string>('')
const showQuickCreate = ref(false)
const prompt = ref('')
const systemPrompt = ref('')
type PermissionMode = 'default' | 'plan' | 'acceptEdits' | 'auto' | 'bypassPermissions' | 'dontAsk'
const permissionMode = ref<PermissionMode>('default')
const bypassConfirmed = ref(false)
const isSpawning = ref(false)
const errorMsg = ref('')
const spawnStatusMsg = ref('')

let errorTimer: ReturnType<typeof setTimeout> | null = null
let statusPollTimer: ReturnType<typeof setTimeout> | null = null
let autoCloseTimer: ReturnType<typeof setTimeout> | null = null

const folderPickerVisible = computed(() => dlg.folders.value.length > 1)

// Modes that skip every confirmation prompt are gated behind a click-again
// confirmation. 'auto' and 'plan' are not dangerous and need no gate.
const dangerousMode = computed(() =>
  permissionMode.value === 'bypassPermissions' || permissionMode.value === 'dontAsk')

function stopStatusPoll() {
  if (statusPollTimer) {
    clearTimeout(statusPollTimer)
    statusPollTimer = null
  }
}

function resetForm() {
  prompt.value = ''
  systemPrompt.value = ''
  permissionMode.value = 'default'
  bypassConfirmed.value = false
  isSpawning.value = false
  errorMsg.value = ''
  spawnStatusMsg.value = ''
  projectChoice.value = ''
  showQuickCreate.value = false
  dlg.clearProject()
  stopStatusPoll()
  if (errorTimer) {
    clearTimeout(errorTimer)
    errorTimer = null
  }
  if (autoCloseTimer) {
    clearTimeout(autoCloseTimer)
    autoCloseTimer = null
  }
}

watch(projectChoice, async (v) => {
  if (v === '__create__') {
    showQuickCreate.value = true
    return
  }
  showQuickCreate.value = false
  if (!v) {
    dlg.clearProject()
    return
  }
  const proj = projects.value.find(p => p.id === v)
  if (proj)
    await dlg.selectProject(proj)
})

watch(permissionMode, () => {
  bypassConfirmed.value = false
})

function onProjectCreated(p: Project) {
  showQuickCreate.value = false
  projectChoice.value = p.id
}

function onQuickCreateCancel() {
  showQuickCreate.value = false
  projectChoice.value = ''
}

async function pollSpawnStatus(pid: number, attempts = 0) {
  if (attempts > 15) {
    stopStatusPoll()
    return
  }
  try {
    const res = await fetch(`/api/agents/spawn/${pid}/status`)
    if (!res.ok)
      return
    const data = await res.json()
    if (data.status === 'running') {
      spawnStatusMsg.value = `Agent PID ${pid} running...`
      statusPollTimer = setTimeout(pollSpawnStatus, 2000, pid, attempts + 1)
    }
    else if (data.status === 'exited' && data.exitCode !== 0) {
      const stderr = data.stderr?.trim()
      errorMsg.value = `Agent exited with code ${data.exitCode}${stderr ? `: ${stderr.slice(-300)}` : ''}`
      spawnStatusMsg.value = ''
      isSpawning.value = false
    }
    else if (data.status === 'error') {
      errorMsg.value = data.stderr?.trim() || 'Spawn error'
      spawnStatusMsg.value = ''
      isSpawning.value = false
    }
    else {
      spawnStatusMsg.value = ''
      stopStatusPoll()
    }
  }
  catch {
    statusPollTimer = setTimeout(pollSpawnStatus, 2000, pid, attempts + 1)
  }
}

async function handleSpawn() {
  if (isSpawning.value || !prompt.value.trim() || !dlg.cwd.value.trim())
    return

  if (dangerousMode.value && !bypassConfirmed.value) {
    bypassConfirmed.value = true
    return
  }

  isSpawning.value = true
  errorMsg.value = ''
  spawnStatusMsg.value = ''

  const body: Record<string, unknown> = {
    prompt: prompt.value.trim(),
    cwd: dlg.cwd.value.trim(),
    enableChannel: true,
    permissionMode: permissionMode.value,
  }
  if (systemPrompt.value.trim())
    body.systemPrompt = systemPrompt.value.trim()
  if (dlg.spawnerId.value)
    body.spawnerId = dlg.spawnerId.value
  if (dlg.project.value?.id)
    body.projectId = dlg.project.value.id

  try {
    const res = await fetch('/api/agents/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const data = await res.json().catch(() => null)
      throw new Error(data?.error || `Server responded with ${res.status}`)
    }
    const data = await res.json()
    const pid = data.pid as number
    spawnStatusMsg.value = `Agent PID ${pid} spawned, verifying...`
    pollSpawnStatus(pid)
    autoCloseTimer = setTimeout(() => {
      if (isSpawning.value && !errorMsg.value) {
        resetForm()
        emit('close')
      }
    }, SPAWN_AUTOCLOSE_MS)
  }
  catch (err: unknown) {
    errorMsg.value = errorMessage(err, 'Failed to spawn agent')
    isSpawning.value = false
  }
}

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    errorMsg.value = ''
    spawnStatusMsg.value = ''
    if (errorTimer) {
      clearTimeout(errorTimer)
      errorTimer = null
    }
  }
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open)
    emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  stopStatusPoll()
  if (autoCloseTimer) {
    clearTimeout(autoCloseTimer)
    autoCloseTimer = null
  }
})
</script>

<template>
  <AppModal :open="open" @close="emit('close')">
    <header class="shrink-0 flex justify-between items-center px-5 py-4 border-b border-line">
      <h2 class="text-lg font-semibold text-fg">
        New Agent
      </h2>
      <button type="button" class="bg-transparent border-none text-fg-mute text-2xl cursor-pointer px-1 leading-none hover:text-fg" @click="emit('close')">
        &times;
      </button>
    </header>

    <form class="flex-1 min-h-0 overflow-y-auto p-5" @submit.prevent>
      <div class="mb-4">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-prompt">Prompt</label>
        <AppInput
          id="spawn-prompt"
          v-model="prompt"
          type="textarea"
          :rows="4"
          required
          placeholder="What should the agent do?"
          data-testid="spawn-prompt-wrap"
        />
      </div>

      <div class="mb-4">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-project">Project</label>
        <select id="spawn-project" v-model="projectChoice" class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent">
          <option
            v-for="p in sortedProjects"
            :key="p.id"
            :value="p.id"
            :disabled="p.folderCount === 0"
          >
            {{ p.name }}{{ p.folderCount === 0 ? ' — no folder, add one in /settings/projects' : '' }}
          </option>
          <option value="__create__">
            + Create new project…
          </option>
        </select>
      </div>

      <QuickCreateProjectPanel
        v-if="showQuickCreate"
        :spawners="spawners"
        @created="onProjectCreated"
        @cancel="onQuickCreateCancel"
      />

      <div v-if="folderPickerVisible" class="mb-4">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-folder">Folder</label>
        <select
          id="spawn-folder"
          :value="dlg.selectedFolderId.value ?? ''"
          class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2"
          @change="dlg.selectFolder(($event.target as HTMLSelectElement).value)"
        >
          <option v-for="f in dlg.folders.value" :key="f.id" :value="f.id">
            {{ f.label || f.path }}{{ f.isDefault ? ' (default)' : '' }}
          </option>
        </select>
      </div>

      <div class="mb-4">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-spawner">Spawner</label>
        <select
          id="spawn-spawner"
          v-model="dlg.spawnerId.value"
          data-testid="spawn-spawner"
          class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
          <option value="">
            {{ projectChoice && projectChoice !== '__create__' ? 'Project default' : 'Claude default' }}
          </option>
          <option v-for="s in spawners" :key="s.id" :value="s.id">
            {{ s.name }}{{ s.builtIn ? ' (built-in)' : '' }}
          </option>
        </select>
      </div>

      <div class="mb-4">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-system">System Prompt</label>
        <AppInput
          id="spawn-system"
          v-model="systemPrompt"
          type="textarea"
          :rows="2"
          placeholder="Custom system instructions (optional)"
        />
      </div>

      <div class="mb-4">
        <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1.5" for="spawn-permission-mode">Permissions</label>
        <select
          id="spawn-permission-mode"
          v-model="permissionMode"
          data-testid="spawn-permission-mode"
          class="w-full bg-app border border-line rounded text-fg text-[13px] px-2.5 py-2 leading-snug focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
        >
          <option value="default">
            Ask for permission (default)
          </option>
          <option value="plan">
            Plan mode (read-only)
          </option>
          <option value="acceptEdits">
            Auto-accept edits
          </option>
          <option value="auto">
            Auto (smart approvals)
          </option>
          <option value="bypassPermissions">
            Bypass all permissions (dangerous)
          </option>
          <option value="dontAsk">
            Never ask (dangerous)
          </option>
        </select>
      </div>

      <div
        v-if="dangerousMode"
        data-testid="bypass-warning"
        class="bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded p-2 px-3 text-xs leading-relaxed text-yellow-600 dark:text-yellow-400 mb-3"
      >
        The agent will execute all tool calls without asking for confirmation. This includes file writes, deletions, git operations, and shell commands. Only use this in isolated environments or with trusted prompts.
      </div>

      <div v-if="bypassConfirmed" role="alert" data-testid="bypass-confirm-msg" class="text-xs text-red-600 dark:text-red-400 font-semibold mb-2">
        Click "Spawn Agent" again to confirm.
      </div>

      <p v-if="spawnStatusMsg" class="text-xs text-green-600 dark:text-green-400 mt-1 leading-snug">
        {{ spawnStatusMsg }}
      </p>
      <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400 mt-1 leading-snug whitespace-pre-wrap break-words max-h-[120px] overflow-y-auto">
        {{ errorMsg }}
      </p>
    </form>

    <footer class="shrink-0 flex justify-end gap-2 px-5 py-3 border-t border-line">
      <AppButton variant="secondary" @click="emit('close')">
        Cancel
      </AppButton>
      <AppButton
        data-testid="spawn-btn"
        variant="primary"
        :disabled="isSpawning || !prompt.trim() || !dlg.cwd.value.trim()"
        @click="handleSpawn"
      >
        {{ isSpawning ? 'Spawning...' : 'Spawn Agent' }}
      </AppButton>
    </footer>
  </AppModal>
</template>
