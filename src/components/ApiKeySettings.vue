<script setup lang="ts">
import type { ApiKey, McpScope } from '../types'
import { onMounted, onUnmounted, ref } from 'vue'

// --- State ---
const keys = ref<ApiKey[]>([])
const isLoading = ref(true)
const errorMsg = ref('')

// Create key dialog
const showCreateDialog = ref(false)
const newKeyName = ref('')
const newKeyGroup = ref<'viewer' | 'operator' | 'developer' | 'admin'>('viewer')
const isCreating = ref(false)
const createError = ref('')

// Token reveal modal
const revealedToken = ref<string | null>(null)
const copyHint = ref<string | null>(null)

// Revoke confirmation
const confirmRevokeId = ref<string | null>(null)

// --- Group → scopes mapping ---
const GROUP_SCOPES: Record<string, McpScope[]> = {
  viewer: ['tasks:read'],
  operator: ['tasks:read', 'pipeline:control'],
  developer: ['tasks:read', 'tasks:write', 'pipeline:control'],
  admin: ['tasks:read', 'tasks:write', 'pipeline:control', 'keys:manage'],
}

// --- Load keys ---
async function loadKeys() {
  isLoading.value = true
  errorMsg.value = ''
  try {
    const res = await fetch('/api/settings/api-keys')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    keys.value = await res.json()
  }
  catch (e) {
    errorMsg.value = (e as Error).message
  }
  finally {
    isLoading.value = false
  }
}

onMounted(loadKeys)

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    showCreateDialog.value = false
    confirmRevokeId.value = null
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

// --- Revoke key ---
async function revokeKey(key: ApiKey) {
  confirmRevokeId.value = null

  // Optimistic update
  key.active = false

  try {
    const res = await fetch(`/api/settings/api-keys/${key.id}`, { method: 'DELETE' })
    if (!res.ok) {
      // Rollback on failure
      key.active = true
      errorMsg.value = `Failed to revoke key: HTTP ${res.status}`
    }
  }
  catch (e) {
    key.active = true
    errorMsg.value = (e as Error).message
  }
}

// --- Create key ---
function openCreateDialog() {
  newKeyName.value = ''
  newKeyGroup.value = 'viewer'
  createError.value = ''
  showCreateDialog.value = true
}

function closeCreateDialog() {
  showCreateDialog.value = false
}

async function handleCreate() {
  if (isCreating.value)
    return
  createError.value = ''

  if (!newKeyName.value.trim()) {
    createError.value = 'Name is required'
    return
  }

  isCreating.value = true
  try {
    const res = await fetch('/api/settings/api-keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        name: newKeyName.value.trim(),
        scopes: GROUP_SCOPES[newKeyGroup.value],
      }),
    })
    if (!res.ok) {
      const body = await res.json().catch(() => ({}))
      throw new Error(body.error ?? `HTTP ${res.status}`)
    }
    const data = await res.json() as { key: ApiKey, token: string }
    keys.value.unshift(data.key)
    revealedToken.value = data.token
    closeCreateDialog()
  }
  catch (e) {
    createError.value = (e as Error).message
  }
  finally {
    isCreating.value = false
  }
}

// --- Copy token ---
async function copyToken() {
  if (!revealedToken.value)
    return
  try {
    await navigator.clipboard.writeText(revealedToken.value)
    copyHint.value = revealedToken.value
  }
  catch {
    copyHint.value = '__error__'
  }
  setTimeout(() => { copyHint.value = null }, 2000)
}

function dismissReveal() {
  revealedToken.value = null
  copyHint.value = null
}

// --- Helpers ---
function formatDate(iso: string | null) {
  if (!iso)
    return '—'
  return new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}
</script>

<template>
  <div class="api-key-settings">
    <div class="settings-header">
      <div>
        <h2 class="settings-title">
          API Keys
        </h2>
        <p class="settings-subtitle">
          Manage MCP API keys for external access to this dashboard.
        </p>
      </div>
      <button class="btn btn-primary" @click="openCreateDialog">
        + Add Key
      </button>
    </div>

    <p v-if="errorMsg" class="error-msg">
      {{ errorMsg }}
    </p>

    <div v-if="isLoading" class="loading">
      Loading keys...
    </div>

    <div v-else-if="keys.length === 0 && !errorMsg" class="empty-state">
      No API keys yet. Create one to allow MCP clients to connect.
    </div>

    <table v-else class="keys-table">
      <thead>
        <tr>
          <th>Name</th>
          <th>Scopes</th>
          <th>Created</th>
          <th>Last Used</th>
          <th>Status</th>
          <th>Actions</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="key in keys"
          :key="key.id"
          :class="{ revoked: !key.active }"
        >
          <td class="cell-name">
            {{ key.name }}
          </td>
          <td class="cell-scopes">
            <span
              v-for="scope in key.scopes"
              :key="scope"
              class="scope-pill"
            >{{ scope }}</span>
          </td>
          <td class="cell-date">
            {{ formatDate(key.createdAt) }}
          </td>
          <td class="cell-date">
            {{ formatDate(key.lastUsedAt) }}
          </td>
          <td>
            <span v-if="key.active" class="badge badge-active">Active</span>
            <span v-else class="badge badge-revoked">Revoked</span>
          </td>
          <td>
            <template v-if="key.active">
              <template v-if="confirmRevokeId === key.id">
                <button class="btn btn-danger btn-sm" @click="revokeKey(key)">
                  Confirm
                </button>
                <button class="btn btn-secondary btn-sm" style="margin-left:4px" @click="confirmRevokeId = null">
                  Cancel
                </button>
              </template>
              <button v-else class="btn btn-danger btn-sm" @click="confirmRevokeId = key.id">
                Revoke
              </button>
            </template>
          </td>
        </tr>
      </tbody>
    </table>

    <!-- Create key dialog -->
    <Transition name="dialog">
      <div
        v-if="showCreateDialog"
        class="modal-backdrop"
        @click.self="closeCreateDialog"
      >
        <div class="modal">
          <header class="modal-header">
            <h2>Create API Key</h2>
            <button class="close-btn" @click="closeCreateDialog">
              &times;
            </button>
          </header>

          <form class="modal-body" @submit.prevent="handleCreate">
            <div class="field">
              <label for="key-name" class="field-label">Name</label>
              <input
                id="key-name"
                v-model="newKeyName"
                class="field-input"
                type="text"
                required
                placeholder="e.g. CI pipeline key"
                autofocus
              >
            </div>

            <div class="field">
              <label for="key-group" class="field-label">Role / Scope Group</label>
              <select
                id="key-group"
                v-model="newKeyGroup"
                class="field-input"
              >
                <option value="viewer">
                  Viewer — tasks:read
                </option>
                <option value="operator">
                  Operator — tasks:read, pipeline:control
                </option>
                <option value="developer">
                  Developer — tasks:read, tasks:write, pipeline:control
                </option>
                <option value="admin">
                  Admin — all scopes
                </option>
              </select>
            </div>

            <p v-if="createError" class="error-msg">
              {{ createError }}
            </p>
          </form>

          <footer class="modal-footer">
            <button type="button" class="btn btn-secondary" @click="closeCreateDialog">
              Cancel
            </button>
            <button
              type="button"
              class="btn btn-primary"
              :disabled="isCreating || !newKeyName.trim()"
              @click="handleCreate"
            >
              {{ isCreating ? 'Creating...' : 'Create Key' }}
            </button>
          </footer>
        </div>
      </div>
    </Transition>

    <!-- Token reveal modal -->
    <Transition name="dialog">
      <div
        v-if="revealedToken"
        class="modal-backdrop"
        @click.self="dismissReveal"
      >
        <div class="modal">
          <header class="modal-header">
            <h2>Your new API key</h2>
          </header>
          <div class="modal-body">
            <p class="reveal-warning">
              Save this token now — it will <strong>never be shown again</strong>.
            </p>
            <div class="token-box">
              <code class="token-text">{{ revealedToken }}</code>
            </div>
            <div class="reveal-actions">
              <button class="btn btn-primary" @click="copyToken">
                <span v-if="copyHint === '__error__'">Copy failed</span>
                <span v-else-if="copyHint">Copied!</span>
                <span v-else>Copy to clipboard</span>
              </button>
            </div>
          </div>
          <footer class="modal-footer">
            <button class="btn btn-secondary" @click="dismissReveal">
              Done — I have saved the token
            </button>
          </footer>
        </div>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.api-key-settings {
  padding: 24px;
  max-width: 900px;
}

.settings-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
  gap: 12px;
}

.settings-title {
  font-size: 20px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 4px;
}

.settings-subtitle {
  font-size: 13px;
  color: var(--text-muted);
}

/* Table */
.keys-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.keys-table th {
  text-align: left;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
}

.keys-table td {
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--text-primary);
  vertical-align: middle;
}

.keys-table tr.revoked td {
  opacity: 0.45;
}

.cell-name {
  font-weight: 500;
}

.cell-scopes {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.cell-date {
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: 12px;
}

.scope-pill {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  border-radius: 4px;
  padding: 2px 6px;
  font-size: 11px;
  font-family: var(--font-mono);
  white-space: nowrap;
}

/* Badges */
.badge {
  display: inline-block;
  border-radius: 4px;
  padding: 2px 8px;
  font-size: 11px;
  font-weight: 600;
}

.badge-active {
  background: color-mix(in srgb, var(--accent-green) 15%, transparent);
  color: var(--accent-green);
}

.badge-revoked {
  background: var(--bg-tertiary);
  color: var(--accent-gray);
}

/* Buttons */
.btn {
  border: none;
  border-radius: 4px;
  padding: 8px 16px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
  white-space: nowrap;
}

.btn-sm {
  padding: 4px 10px;
  font-size: 12px;
}

.btn-primary {
  background: var(--accent-blue);
  color: white;
}

.btn-primary:hover:not(:disabled) {
  filter: brightness(1.1);
}

.btn-primary:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.btn-secondary {
  background: var(--bg-tertiary);
  color: var(--text-secondary);
}

.btn-secondary:hover {
  filter: brightness(1.15);
}

.btn-danger {
  background: color-mix(in srgb, var(--accent-red) 15%, transparent);
  color: var(--accent-red);
}

.btn-danger:hover {
  background: color-mix(in srgb, var(--accent-red) 25%, transparent);
}

/* Empty / loading */
.loading,
.empty-state {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
  font-size: 13px;
}

.error-msg {
  color: var(--accent-red);
  font-size: 12px;
  margin-bottom: 12px;
}

/* Modal shared */
.modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 200;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
}

.modal {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: 100%;
  max-width: 480px;
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
  line-height: 1;
}

.close-btn:hover {
  color: var(--text-primary);
}

.modal-body {
  padding: 20px;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 20px;
  border-top: 1px solid var(--border);
}

/* Form */
.field {
  margin-bottom: 14px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.field-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
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
}

.field-input:focus {
  outline: none;
  border-color: var(--accent-blue);
}

/* Token reveal */
.reveal-warning {
  font-size: 13px;
  color: var(--text-secondary);
  margin-bottom: 12px;
}

.reveal-warning strong {
  color: var(--accent-yellow);
}

.token-box {
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 4px;
  padding: 12px;
  overflow-x: auto;
  margin-bottom: 12px;
}

.token-text {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--accent-green);
  word-break: break-all;
  user-select: all;
}

.reveal-actions {
  display: flex;
  justify-content: flex-end;
}

/* Transitions */
.dialog-enter-active,
.dialog-leave-active {
  transition: opacity 0.2s ease;
}

.dialog-enter-from,
.dialog-leave-to {
  opacity: 0;
}
</style>
