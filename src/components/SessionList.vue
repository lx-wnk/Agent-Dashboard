<template>
  <Transition name="dialog">
    <div
      v-if="open"
      class="sessions-backdrop"
      @click.self="emit('close')"
      @keydown.escape="emit('close')"
    >
      <div class="sessions-modal">
        <header class="modal-header">
          <h2>Past Sessions</h2>
          <button class="close-btn" @click="emit('close')">&times;</button>
        </header>

        <div class="modal-body">
          <div class="search-row">
            <input
              v-model="search"
              class="search-input"
              type="text"
              placeholder="Filter by project or prompt..."
            />
          </div>

          <p v-if="isLoading" class="status-msg">Loading sessions...</p>
          <p v-else-if="filtered.length === 0" class="status-msg">No sessions found.</p>

          <div v-else class="session-list">
            <div
              v-for="s in filtered"
              :key="s.sessionId"
              class="session-card"
              :class="{ running: s.isRunning }"
            >
              <div class="session-top">
                <span class="session-project">{{ s.projectName }}</span>
                <span class="session-date">{{ formatDate(s.lastModified) }}</span>
              </div>
              <code class="session-path">{{ shortenPath(s.projectPath) }}</code>
              <p v-if="s.firstPrompt" class="session-prompt">{{ s.firstPrompt }}</p>
              <div class="session-meta">
                <span v-if="s.model" class="meta-tag model">{{ shortModel(s.model) }}</span>
                <span v-if="s.costEstimate > 0" class="meta-tag cost">${{ s.costEstimate.toFixed(2) }}</span>
                <span class="meta-tag session-id-tag" :title="s.sessionId">{{ s.sessionId.slice(0, 8) }}</span>
              </div>
              <div class="session-actions">
                <input
                  v-model="resumePrompts[s.sessionId]"
                  class="resume-input"
                  type="text"
                  placeholder="Follow-up prompt..."
                  @keydown.enter="resumeSession(s)"
                />
                <button
                  class="resume-btn"
                  :disabled="!resumePrompts[s.sessionId]?.trim() || spawning === s.sessionId"
                  @click="resumeSession(s)"
                >
                  {{ spawning === s.sessionId ? '...' : 'Resume' }}
                </button>
              </div>
              <p v-if="resumeMsg[s.sessionId]" class="resume-status" :class="{ error: resumeError[s.sessionId] }">
                {{ resumeMsg[s.sessionId] }}
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'

interface SessionInfo {
  sessionId: string
  projectPath: string
  projectName: string
  lastModified: string
  model: string | null
  firstPrompt: string | null
  totalInputTokens: number
  totalOutputTokens: number
  costEstimate: number
  isRunning: boolean
}

const props = defineProps<{ open: boolean; homeDir: string }>()
const emit = defineEmits<{ close: [], spawned: [pid: number] }>()

const sessions = ref<SessionInfo[]>([])
const isLoading = ref(false)
const search = ref('')
const resumePrompts = ref<Record<string, string>>({})
const spawning = ref<string | null>(null)
const resumeMsg = ref<Record<string, string>>({})
const resumeError = ref<Record<string, boolean>>({})

const filtered = computed(() => {
  const q = search.value.toLowerCase()
  // Exclude currently running sessions — they don't need resuming
  const inactive = sessions.value.filter(s => !s.isRunning)
  if (!q) return inactive
  return inactive.filter(s =>
    s.projectName.toLowerCase().includes(q) ||
    s.projectPath.toLowerCase().includes(q) ||
    (s.firstPrompt && s.firstPrompt.toLowerCase().includes(q))
  )
})

function formatDate(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffH = diffMs / 3600000

  if (diffH < 1) return `${Math.round(diffMs / 60000)}m ago`
  if (diffH < 24) return `${Math.round(diffH)}h ago`
  if (diffH < 168) return `${Math.round(diffH / 24)}d ago`
  return d.toLocaleDateString()
}

function shortModel(model: string): string {
  return model.replace('claude-', '').replace(/-\d+$/, '')
}

function shortenPath(path: string): string {
  if (props.homeDir && path.startsWith(props.homeDir)) {
    return '~' + path.slice(props.homeDir.length)
  }
  return path
}

async function resumeSession(s: SessionInfo) {
  const prompt = resumePrompts.value[s.sessionId]?.trim()
  if (!prompt || spawning.value) return

  spawning.value = s.sessionId
  resumeMsg.value[s.sessionId] = ''
  resumeError.value[s.sessionId] = false

  try {
    const res = await fetch('/api/agents/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt,
        cwd: s.projectPath,
        resumeSessionId: s.sessionId,
        enableChannel: true,
      }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error || 'Spawn failed')

    resumeMsg.value[s.sessionId] = `PID ${data.pid} spawned`
    resumePrompts.value[s.sessionId] = ''
    emit('spawned', data.pid)
    setTimeout(() => { resumeMsg.value[s.sessionId] = '' }, 4000)
  } catch (err: unknown) {
    resumeError.value[s.sessionId] = true
    resumeMsg.value[s.sessionId] = err instanceof Error ? err.message : 'Failed'
    setTimeout(() => { resumeMsg.value[s.sessionId] = '' }, 4000)
  } finally {
    spawning.value = null
  }
}

async function loadSessions() {
  isLoading.value = true
  try {
    const res = await fetch('/api/sessions')
    if (res.ok) sessions.value = await res.json()
  } catch { /* ignore */ }
  isLoading.value = false
}

watch(() => props.open, (isOpen) => {
  if (isOpen) loadSessions()
})
</script>

<style scoped>
.sessions-backdrop {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}

.sessions-modal {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: 100%;
  max-width: 640px;
  max-height: 85vh;
  display: flex;
  flex-direction: column;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}

.modal-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.modal-header h2 {
  font-size: 18px;
  font-weight: 600;
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

.close-btn:hover { color: var(--text-primary); }

.modal-body {
  padding: 16px 20px;
  overflow-y: auto;
  flex: 1;
}

.search-row {
  margin-bottom: 12px;
}

.search-input {
  width: 100%;
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  padding: 8px 10px;
}

.search-input:focus {
  outline: none;
  border-color: var(--accent-green);
}

.search-input::placeholder { color: var(--text-muted); }

.status-msg {
  text-align: center;
  color: var(--text-muted);
  padding: 24px;
  font-size: 13px;
}

.session-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.session-card {
  background: var(--bg-primary);
  border-radius: 6px;
  padding: 10px 12px;
  border: 1px solid transparent;
}

.session-card.running {
  border-color: rgba(74, 222, 128, 0.2);
}

.session-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 4px;
}

.session-project {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}

.session-date {
  font-size: 11px;
  color: var(--text-muted);
}

.session-path {
  display: block;
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--font-mono);
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-id-tag {
  font-family: var(--font-mono);
  letter-spacing: 0;
}

.session-prompt {
  font-size: 12px;
  color: var(--text-secondary);
  line-height: 1.3;
  margin-bottom: 6px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.session-meta {
  display: flex;
  gap: 6px;
  margin-bottom: 8px;
}

.meta-tag {
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--bg-tertiary);
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.meta-tag.cost { color: var(--accent-green); }
.meta-tag.running-badge { color: var(--accent-green); background: rgba(74, 222, 128, 0.1); }

.session-actions {
  display: flex;
  gap: 6px;
}

.resume-input {
  flex: 1;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 12px;
  font-family: inherit;
  padding: 4px 8px;
}

.resume-input:focus {
  outline: none;
  border-color: var(--accent-green);
}

.resume-input::placeholder { color: var(--text-muted); }

.resume-btn {
  flex-shrink: 0;
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 4px 12px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
}

.resume-btn:hover:not(:disabled) {
  color: var(--accent-green);
  border-color: var(--accent-green);
}

.resume-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.resume-status {
  font-size: 11px;
  color: var(--accent-green);
  margin-top: 4px;
}

.resume-status.error { color: var(--accent-red); }

/* Dialog transition */
.dialog-enter-active,
.dialog-leave-active {
  transition: opacity 0.2s ease;
}

.dialog-enter-active .sessions-modal,
.dialog-leave-active .sessions-modal {
  transition: transform 0.2s ease, opacity 0.2s ease;
}

.dialog-enter-from,
.dialog-leave-to {
  opacity: 0;
}

.dialog-enter-from .sessions-modal,
.dialog-leave-to .sessions-modal {
  transform: scale(0.95);
  opacity: 0;
}
</style>
