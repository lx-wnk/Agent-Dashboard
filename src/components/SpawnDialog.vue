<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const prompt = ref('')
const cwd = ref('')
const model = ref('')
const systemPrompt = ref('')
const enableChannel = ref(true)
const skipPermissions = ref(false)
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

onUnmounted(() => {
  stopStatusPoll()
  if (autoCloseTimer) {
    clearTimeout(autoCloseTimer)
    autoCloseTimer = null
  }
})
</script>

<template>
  <Transition name="dialog">
    <div
      v-if="open"
      class="spawn-backdrop"
      @click.self="emit('close')"
      @keydown.escape="emit('close')"
    >
      <div class="spawn-modal">
        <header class="modal-header">
          <h2>New Agent</h2>
          <button class="close-btn" @click="emit('close')">
            &times;
          </button>
        </header>

        <form class="modal-body" @submit.prevent>
          <div class="field">
            <label class="field-label" for="spawn-prompt">Prompt</label>
            <textarea
              id="spawn-prompt"
              v-model="prompt"
              class="field-input"
              rows="4"
              required
              placeholder="What should the agent do?"
            />
          </div>

          <div class="field">
            <label class="field-label" for="spawn-cwd">Working Directory</label>
            <input
              id="spawn-cwd"
              v-model="cwd"
              class="field-input"
              type="text"
              required
              placeholder="/path/to/project"
            >
          </div>

          <div class="field">
            <label class="field-label" for="spawn-model">Model</label>
            <select id="spawn-model" v-model="model" class="field-input">
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

          <div class="field">
            <label class="field-label" for="spawn-system">System Prompt</label>
            <textarea
              id="spawn-system"
              v-model="systemPrompt"
              class="field-input"
              rows="2"
              placeholder="Custom system instructions (optional)"
            />
          </div>

          <div class="field-checkbox">
            <input
              id="spawn-channel"
              v-model="enableChannel"
              type="checkbox"
            >
            <label for="spawn-channel">Enable dashboard control channel</label>
          </div>

          <div class="field-checkbox">
            <input
              id="spawn-yolo"
              v-model="skipPermissions"
              type="checkbox"
            >
            <label for="spawn-yolo">Skip permission prompts <span class="yolo-hint">(--dangerously-skip-permissions)</span></label>
          </div>

          <p v-if="spawnStatusMsg" class="status-msg">
            {{ spawnStatusMsg }}
          </p>
          <p v-if="errorMsg" class="error-msg">
            {{ errorMsg }}
          </p>
        </form>

        <footer class="modal-footer">
          <button
            type="button"
            class="btn btn-secondary"
            @click="emit('close')"
          >
            Cancel
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="isSpawning || !prompt.trim() || !cwd.trim()"
            @click="handleSpawn"
          >
            {{ isSpawning ? 'Spawning...' : 'Spawn Agent' }}
          </button>
        </footer>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.spawn-backdrop {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}

.spawn-modal {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: 100%;
  max-width: 520px;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
}

.modal-header h2 {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
}

.close-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 24px;
  cursor: pointer;
  padding: 0 4px;
  line-height: 1;
}

.close-btn:hover {
  color: var(--text-primary);
}

.modal-body {
  padding: 20px;
}

.field {
  margin-bottom: 16px;
}

.field-label {
  display: block;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 6px;
}

.field-input {
  width: 100%;
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  padding: 8px 10px;
  line-height: 1.4;
  resize: vertical;
}

.field-input::placeholder {
  color: var(--text-muted);
}

.field-input:focus {
  outline: none;
  border-color: var(--accent-green);
}

select.field-input {
  appearance: none;
  background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 12 12'%3E%3Cpath fill='%2364748b' d='M3 5l3 3 3-3'/%3E%3C/svg%3E");
  background-repeat: no-repeat;
  background-position: right 10px center;
  padding-right: 28px;
  cursor: pointer;
}

select.field-input option {
  background: var(--bg-primary);
  color: var(--text-primary);
}

.field-checkbox {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
}

.field-checkbox input[type="checkbox"] {
  width: 16px;
  height: 16px;
  accent-color: var(--accent-green);
  cursor: pointer;
  flex-shrink: 0;
}

.field-checkbox label {
  font-size: 13px;
  color: var(--text-secondary);
  cursor: pointer;
}

.yolo-hint {
  font-size: 10px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}

.status-msg {
  font-size: 12px;
  color: var(--accent-green);
  margin-top: 4px;
  line-height: 1.4;
}

.error-msg {
  font-size: 12px;
  color: var(--accent-red);
  margin-top: 4px;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 120px;
  overflow-y: auto;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 20px;
  border-top: 1px solid var(--border);
}

.btn {
  border: none;
  border-radius: 4px;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  font-family: inherit;
}

.btn-secondary {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.btn-secondary:hover {
  filter: brightness(1.15);
}

.btn-primary {
  background: var(--accent-green);
  color: var(--bg-primary);
}

.btn-primary:hover:not(:disabled) {
  filter: brightness(1.1);
}

.btn-primary:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

/* Dialog transition */
.dialog-enter-active,
.dialog-leave-active {
  transition: opacity 0.2s ease;
}

.dialog-enter-active .spawn-modal,
.dialog-leave-active .spawn-modal {
  transition: transform 0.2s ease, opacity 0.2s ease;
}

.dialog-enter-from,
.dialog-leave-to {
  opacity: 0;
}

.dialog-enter-from .spawn-modal,
.dialog-leave-to .spawn-modal {
  transform: scale(0.95);
  opacity: 0;
}
</style>
