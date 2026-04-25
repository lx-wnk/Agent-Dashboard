<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import BaseModal from './BaseModal.vue'
import { createTask } from '../composables/useTasks'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const slug = ref('')
const title = ref('')
const description = ref('')
const cwd = ref('')
const sourceBranch = ref('')
const targetBranch = ref('')
const useWorktree = ref(true)
const maxIterations = ref(20)
const tokenBudget = ref<number | null>(null)
const silverBullet = ref(false)
const priority = ref<'high' | 'medium' | 'low'>('medium')
const errorMsg = ref('')
const isCreating = ref(false)

watch(() => props.open, (val) => {
  if (val)
    errorMsg.value = ''
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open)
    emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

function resetForm() {
  slug.value = ''
  title.value = ''
  description.value = ''
  cwd.value = ''
  sourceBranch.value = ''
  targetBranch.value = ''
  useWorktree.value = true
  maxIterations.value = 20
  tokenBudget.value = null
  silverBullet.value = false
  priority.value = 'medium'
  errorMsg.value = ''
}

function slugifyTitle() {
  if (slug.value)
    return
  slug.value = title.value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
}

async function handleCreate() {
  if (isCreating.value)
    return
  errorMsg.value = ''

  if (!title.value.trim() || !cwd.value.trim()) {
    errorMsg.value = 'Title and working directory are required'
    return
  }
  if (!slug.value.trim())
    slugifyTitle()

  isCreating.value = true
  try {
    await createTask({
      slug: slug.value.trim(),
      title: title.value.trim(),
      description: description.value.trim() || undefined,
      cwd: cwd.value.trim(),
      sourceBranch: sourceBranch.value.trim() || undefined,
      targetBranch: targetBranch.value.trim() || undefined,
      useWorktree: useWorktree.value,
      maxIterations: maxIterations.value,
      tokenBudget: tokenBudget.value ?? undefined,
      silverBullet: silverBullet.value,
      priority: priority.value,
    })
    resetForm()
    emit('close')
  }
  catch (err) {
    errorMsg.value = (err as Error).message
  }
  finally {
    isCreating.value = false
  }
}
</script>

<template>
  <BaseModal :open="open" @close="emit('close')">
    <div class="backlog-modal">
        <header class="modal-header">
          <h2>New Task</h2>
          <button class="close-btn" @click="emit('close')">
            &times;
          </button>
        </header>

        <form class="modal-body" @submit.prevent="handleCreate">
          <div class="field">
            <label for="task-title" class="field-label">Title</label>
            <input
              id="task-title"
              v-model="title"
              class="field-input"
              type="text"
              required
              placeholder="Fix login bug in auth middleware"
              @blur="slugifyTitle"
            >
          </div>

          <div class="field">
            <label for="task-slug" class="field-label">Slug (url-safe id)</label>
            <input
              id="task-slug"
              v-model="slug"
              class="field-input"
              type="text"
              pattern="[a-z0-9][a-z0-9-]{0,63}"
              placeholder="auto-generated from title"
            >
          </div>

          <div class="field">
            <label for="task-desc" class="field-label">Description</label>
            <textarea
              id="task-desc"
              v-model="description"
              class="field-input"
              rows="3"
              placeholder="What should the agent do? Include screenshots or references as needed."
            />
          </div>

          <div class="field">
            <label for="task-cwd" class="field-label">Working Directory</label>
            <input
              id="task-cwd"
              v-model="cwd"
              class="field-input"
              type="text"
              required
              placeholder="/path/to/project"
            >
          </div>

          <div class="field-row">
            <div class="field">
              <label for="task-src" class="field-label">Source Branch</label>
              <input
                id="task-src"
                v-model="sourceBranch"
                class="field-input"
                type="text"
                placeholder="main"
              >
            </div>
            <div class="field">
              <label for="task-tgt" class="field-label">Target Branch</label>
              <input
                id="task-tgt"
                v-model="targetBranch"
                class="field-input"
                type="text"
                placeholder="feat/my-branch"
              >
            </div>
          </div>

          <div class="field-checkbox">
            <input
              id="task-worktree"
              v-model="useWorktree"
              type="checkbox"
            >
            <label for="task-worktree">Use isolated git worktree (recommended)</label>
          </div>

          <div class="field-row">
            <div class="field">
              <label for="task-priority" class="field-label">Priority</label>
              <select
                id="task-priority"
                v-model="priority"
                class="field-input"
              >
                <option value="high">
                  High
                </option>
                <option value="medium">
                  Medium
                </option>
                <option value="low">
                  Low
                </option>
              </select>
            </div>
            <div class="field-checkbox silver-bullet">
              <input
                id="task-silver"
                v-model="silverBullet"
                type="checkbox"
              >
              <label for="task-silver">Silver bullet (jump the queue)</label>
            </div>
          </div>

          <div class="field-row">
            <div class="field">
              <label for="task-iter" class="field-label">Max Iterations</label>
              <input
                id="task-iter"
                v-model.number="maxIterations"
                class="field-input"
                type="number"
                min="1"
                max="50"
              >
            </div>
            <div class="field">
              <label for="task-tokens" class="field-label">Token Budget (optional)</label>
              <input
                id="task-tokens"
                v-model.number="tokenBudget"
                class="field-input"
                type="number"
                min="0"
                placeholder="∞"
              >
            </div>
          </div>

          <p v-if="errorMsg" class="error-msg">
            {{ errorMsg }}
          </p>
        </form>

        <footer class="modal-footer">
          <button type="button" class="btn btn-secondary" @click="emit('close')">
            Cancel
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="isCreating || !title.trim() || !cwd.trim()"
            @click="handleCreate"
          >
            {{ isCreating ? 'Creating...' : 'Create Task' }}
          </button>
        </footer>
      </div>
    </div>
  </BaseModal>
</template>

<style scoped>
.backlog-modal {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  width: 100%;
  max-width: 560px;
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
.modal-header h2 { font-size: 18px; font-weight: 600; color: var(--text-primary); }
.close-btn {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 24px;
  cursor: pointer;
  line-height: 1;
}
.close-btn:hover { color: var(--text-primary); }
.modal-body { padding: 20px; }
.field { margin-bottom: 14px; display: flex; flex-direction: column; gap: 4px; flex: 1; }
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
  resize: vertical;
}
.field-input:focus { outline: none; border-color: var(--accent-blue); }
.field-row { display: flex; gap: 12px; }
.field-checkbox { display: flex; align-items: center; gap: 8px; margin-bottom: 14px; }
.field-checkbox label { font-size: 13px; color: var(--text-secondary); cursor: pointer; }
.field-checkbox input[type="checkbox"] { accent-color: var(--accent-blue); cursor: pointer; }
.silver-bullet { flex: 1; align-self: center; margin-bottom: 0; }
.error-msg { color: var(--accent-red); font-size: 12px; line-height: 1.4; }
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
  font-family: inherit;
}
.btn-secondary { background: var(--bg-tertiary); color: var(--text-secondary); }
.btn-secondary:hover { filter: brightness(1.15); }
.btn-primary { background: var(--accent-blue); color: white; }
.btn-primary:hover:not(:disabled) { filter: brightness(1.1); }
.btn-primary:disabled { opacity: 0.4; cursor: not-allowed; }

</style>
