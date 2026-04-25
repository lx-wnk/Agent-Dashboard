<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import AppModal from './ui/AppModal.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const prompt = ref('')
const cwd = ref('')
const model = ref('')
const systemPrompt = ref('')
const enableChannel = ref(true)
const skipPermissions = ref(false)
const skipPermissionsConfirmed = ref(false)
const isSpawning = ref(false)
const errorMsg = ref('')
const spawnStatusMsg = ref('')

let errorTimer: ReturnType<typeof setTimeout> | null = null
let statusPollTimer: ReturnType<typeof setTimeout> | null = null
let autoCloseTimer: ReturnType<typeof setTimeout> | null = null

function stopStatusPoll() {
  if (statusPollTimer) {
    clearTimeout(statusPollTimer)
    statusPollTimer = null
  }
}

function resetForm() {
  prompt.value = ''
  cwd.value = ''
  model.value = ''
  systemPrompt.value = ''
  enableChannel.value = true
  skipPermissions.value = false
  skipPermissionsConfirmed.value = false
  isSpawning.value = false
  errorMsg.value = ''
  spawnStatusMsg.value = ''
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

async function pollSpawnStatus(pid: number, attempts = 0) {
  if (attempts > 15) { // ~30s max polling
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
      // exited with code 0 = success
      spawnStatusMsg.value = ''
      stopStatusPoll()
    }
  }
  catch {
    // Network error, keep polling
    statusPollTimer = setTimeout(pollSpawnStatus, 2000, pid, attempts + 1)
  }
}

async function handleSpawn() {
  if (isSpawning.value || !prompt.value.trim() || !cwd.value.trim())
    return

  // Require explicit confirmation for skip-permissions
  if (skipPermissions.value && !skipPermissionsConfirmed.value) {
    skipPermissionsConfirmed.value = true
    return
  }

  isSpawning.value = true
  errorMsg.value = ''
  spawnStatusMsg.value = ''

  const body: Record<string, unknown> = {
    prompt: prompt.value.trim(),
    cwd: cwd.value.trim(),
    enableChannel: enableChannel.value,
    skipPermissions: skipPermissions.value,
  }
  if (model.value)
    body.model = model.value
  if (systemPrompt.value.trim())
    body.systemPrompt = systemPrompt.value.trim()

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

    // Keep dialog open and poll for early exit errors
    spawnStatusMsg.value = `Agent PID ${pid} spawned, verifying...`
    pollSpawnStatus(pid)

    // Auto-close after 3s if still running (no early error)
    autoCloseTimer = setTimeout(() => {
      if (isSpawning.value && !errorMsg.value) {
        resetForm()
        emit('close')
      }
    }, 3000)
  }
  catch (err: unknown) {
    errorMsg.value = err instanceof Error ? err.message : 'Failed to spawn agent'
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
    <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-xl">
      <header class="flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700">
        <h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
          New Agent
        </h2>
        <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" @click="emit('close')">
          &times;
        </button>
      </header>

      <form class="p-5" @submit.prevent>
        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="spawn-prompt">Prompt</label>
          <textarea
            id="spawn-prompt"
            v-model="prompt"
            class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 leading-snug resize-y focus:outline-none focus:border-green-500"
            rows="4"
            required
            placeholder="What should the agent do?"
          />
        </div>

        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="spawn-cwd">Working Directory</label>
          <input
            id="spawn-cwd"
            v-model="cwd"
            class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-green-500"
            type="text"
            required
            placeholder="/path/to/project"
          >
        </div>

        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="spawn-model">Model</label>
          <select id="spawn-model" v-model="model" class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 leading-snug focus:outline-none focus:border-green-500">
            <option value="">
              Auto
            </option>
            <option value="claude-opus-4-6">
              claude-opus-4-6
            </option>
            <option value="claude-sonnet-4-6">
              claude-sonnet-4-6
            </option>
            <option value="claude-haiku-4-5">
              claude-haiku-4-5
            </option>
          </select>
        </div>

        <div class="mb-4">
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-1.5" for="spawn-system">System Prompt</label>
          <textarea
            id="spawn-system"
            v-model="systemPrompt"
            class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 leading-snug resize-y focus:outline-none focus:border-green-500"
            rows="2"
            placeholder="Custom system instructions (optional)"
          />
        </div>

        <div class="flex items-center gap-2 mb-4">
          <input
            id="spawn-channel"
            v-model="enableChannel"
            type="checkbox"
          >
          <label for="spawn-channel">Enable dashboard control channel</label>
        </div>

        <div class="flex items-center gap-2 mb-4">
          <input
            id="spawn-yolo"
            v-model="skipPermissions"
            type="checkbox"
            @change="skipPermissionsConfirmed = false"
          >
          <label for="spawn-yolo">Skip permission prompts <span class="text-[10px] text-slate-400 dark:text-slate-600 font-mono">(--dangerously-skip-permissions)</span></label>
        </div>

        <div v-if="skipPermissions" class="bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded p-2 px-3 text-xs leading-relaxed text-yellow-600 dark:text-yellow-400 mb-3">
          The agent will execute all tool calls without asking for confirmation. This includes file writes, deletions, git operations, and shell commands. Only use this in isolated environments or with trusted prompts.
        </div>

        <div v-if="skipPermissionsConfirmed" class="text-xs text-red-600 dark:text-red-400 font-semibold mb-2">
          Click "Spawn Agent" again to confirm.
        </div>

        <p v-if="spawnStatusMsg" class="text-xs text-green-600 dark:text-green-400 mt-1 leading-snug">
          {{ spawnStatusMsg }}
        </p>
        <p v-if="errorMsg" class="text-xs text-red-600 dark:text-red-400 mt-1 leading-snug whitespace-pre-wrap break-words max-h-[120px] overflow-y-auto">
          {{ errorMsg }}
        </p>
      </form>

      <footer class="flex justify-end gap-2 px-5 py-3 border-t border-slate-200 dark:border-slate-700">
        <button
          type="button"
          class="border-none rounded px-4 py-2 text-[13px] font-semibold cursor-pointer whitespace-nowrap font-sans bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:brightness-110"
          @click="emit('close')"
        >
          Cancel
        </button>
        <button
          type="button"
          class="border-none rounded px-4 py-2 text-[13px] font-semibold cursor-pointer whitespace-nowrap font-sans bg-green-600 text-white hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed"
          :disabled="isSpawning || !prompt.trim() || !cwd.trim()"
          @click="handleSpawn"
        >
          {{ isSpawning ? 'Spawning...' : 'Spawn Agent' }}
        </button>
      </footer>
    </div>
  </AppModal>
</template>
