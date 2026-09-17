<script setup lang="ts">
import type { ApplicationEntry, ApplicationView } from '@/features/settings/composables/useApplications'
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import { useApplications } from '@/features/settings/composables/useApplications'
import { formatDateTime } from '@/utils/format'

const { applications, loading, error, drift, fetchApplications, setAttachAll, setRequiredEnv, setSecret, deleteSecret, refreshCatalogue, createApplication, deleteApplication, setEntry, setExport, fetchDrift, importApplication, subscribe, unsubscribe } = useApplications()
const actionError = ref<string | null>(null)
const drafts = reactive<Record<string, string>>({})
const requiredEnvDrafts = reactive<Record<string, string>>({})

onMounted(() => {
  void fetchApplications()
  void fetchDrift()
  subscribe()
})

onUnmounted(() => {
  unsubscribe()
})

type PanelState = 'loading' | 'error' | 'empty' | 'rows'
const panelState = computed<PanelState>(() => {
  if (loading.value)
    return 'loading'
  if (error.value)
    return 'error'
  return applications.value.length ? 'rows' : 'empty'
})

function isSet(app: ApplicationView, envName: string) {
  return app.secrets.some(s => s.envName === envName)
}

function draftKey(app: ApplicationView, envName: string) {
  return `${app.resourceId}-${envName}`
}

async function run(action: () => Promise<void>) {
  actionError.value = null
  try {
    await action()
  }
  catch (e) {
    actionError.value = (e as Error).message
  }
}

async function saveSecret(app: ApplicationView, envName: string) {
  const key = draftKey(app, envName)
  const value = drafts[key]
  if (!value)
    return
  await run(() => setSecret(app.resourceId, envName, value))
  drafts[key] = ''
}

const ENV_NAME_RE = /^[A-Z_][A-Z0-9_]*$/

async function addRequiredEnv(app: ApplicationView) {
  const name = (requiredEnvDrafts[app.resourceId] ?? '').trim()
  if (!name)
    return
  if (!ENV_NAME_RE.test(name)) {
    actionError.value = `${name} is not an environment variable name`
    return
  }
  if (app.requiredEnv.includes(name))
    return
  await run(() => setRequiredEnv(app.resourceId, [...app.requiredEnv, name]))
  requiredEnvDrafts[app.resourceId] = ''
}

async function removeRequiredEnv(app: ApplicationView, envName: string) {
  await run(() => setRequiredEnv(app.resourceId, app.requiredEnv.filter(n => n !== envName)))
}

const WHITESPACE_RE = /\s+/

function parseArgs(text: string): string[] {
  return text.trim().split(WHITESPACE_RE).filter(Boolean)
}

function parseEnv(text: string): Record<string, string> | null {
  const env: Record<string, string> = {}
  const lines = text.split('\n').map(l => l.trim()).filter(Boolean)
  for (const line of lines) {
    const eq = line.indexOf('=')
    if (eq <= 0)
      return null
    const key = line.slice(0, eq).trim()
    if (!ENV_NAME_RE.test(key))
      return null
    env[key] = line.slice(eq + 1).trim()
  }
  return env
}

const showAddForm = ref(false)
const newApp = reactive({ name: '', command: '', args: '', env: '' })

function resetNewApp() {
  newApp.name = ''
  newApp.command = ''
  newApp.args = ''
  newApp.env = ''
}

async function submitCreate() {
  const env = parseEnv(newApp.env)
  if (env === null) {
    actionError.value = 'Environment must be KEY=value, one per line'
    return
  }
  await run(async () => {
    await createApplication({ name: newApp.name.trim(), command: newApp.command.trim(), args: parseArgs(newApp.args), env })
    showAddForm.value = false
    resetNewApp()
  })
}

const editingId = ref<string | null>(null)
const editDrafts = reactive<Record<string, { command: string, args: string, env: string }>>({})

function entryToDraft(app: ApplicationView) {
  return {
    command: app.entry.command ?? '',
    args: (app.entry.args ?? []).join(' '),
    env: Object.entries(app.entry.env ?? {}).map(([k, v]) => `${k}=${v}`).join('\n'),
  }
}

function startEdit(app: ApplicationView) {
  editDrafts[app.resourceId] = entryToDraft(app)
  editingId.value = app.resourceId
}

async function submitEdit(app: ApplicationView) {
  const draft = editDrafts[app.resourceId]
  const env = parseEnv(draft.env)
  if (env === null) {
    actionError.value = 'Environment must be KEY=value, one per line'
    return
  }
  const entry: ApplicationEntry = { command: draft.command.trim(), args: parseArgs(draft.args), env }
  await run(async () => {
    await setEntry(app.resourceId, entry)
    editingId.value = null
  })
}

const confirmRemoveId = ref<string | null>(null)

async function handleRemove(app: ApplicationView) {
  await run(() => deleteApplication(app.resourceId))
  confirmRemoveId.value = null
}

function appByName(name: string): ApplicationView | undefined {
  return applications.value.find(a => a.serverName === name)
}

async function takeFileVersion(name: string) {
  const app = appByName(name)
  if (!app)
    return
  await run(async () => {
    await setExport(app.resourceId, false)
    await fetchDrift()
  })
}

async function writeAppVersion(name: string) {
  const app = appByName(name)
  if (!app)
    return
  await run(async () => {
    await setExport(app.resourceId, true)
    await fetchDrift()
  })
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Applications
      </h3>
      <p class="text-sm text-fg-mute">
        MCP servers added here. A routine attaches the ones its tasks may use; tools run without asking only where a grant allows them.
      </p>
    </div>

    <p v-if="actionError" class="text-sm text-red-500" role="alert">
      {{ actionError }}
    </p>

    <div v-if="drift.found.length || drift.changed.length" class="flex flex-col gap-2">
      <div
        v-for="name in drift.found"
        :key="`found-${name}`"
        role="alert"
        class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-xs flex items-center justify-between gap-3"
        :data-testid="`application-drift-found-${name}`"
      >
        <span>Found {{ name }} — import?</span>
        <AppButton variant="info" size="sm" @click="run(() => importApplication(name))">
          Import
        </AppButton>
      </div>
      <div
        v-for="name in drift.changed"
        :key="`changed-${name}`"
        role="alert"
        class="rounded border border-warning-line bg-warning-soft text-warning-text px-3 py-2 text-xs flex items-center justify-between gap-3"
        :data-testid="`application-drift-changed-${name}`"
      >
        <span>Changed outside the app — {{ name }}</span>
        <div class="flex items-center gap-2">
          <AppButton variant="secondary" size="sm" title="Stop mirroring, so the edit in Claude's config stays" @click="run(() => takeFileVersion(name))">
            Take the change
          </AppButton>
          <AppButton variant="info" size="sm" title="Overwrite the edit with the definition stored here" @click="run(() => writeAppVersion(name))">
            Write the app's version back
          </AppButton>
        </div>
      </div>
    </div>

    <p v-if="panelState === 'loading'" class="text-sm text-fg-mute" aria-live="polite">
      Loading applications…
    </p>
    <p v-else-if="panelState === 'error'" class="text-sm text-red-500" role="alert">
      {{ error }}
    </p>
    <p v-else-if="panelState === 'empty'" data-testid="applications-empty" class="text-sm text-fg-mute">
      No MCP servers added yet. Add one below.
    </p>

    <section
      v-for="app in applications"
      v-else
      :key="app.resourceId"
      class="rounded-lg border border-line p-4 flex flex-col gap-3"
      :data-testid="`application-${app.resourceId}`"
    >
      <div class="flex items-center justify-between gap-3">
        <h4 class="font-semibold text-fg">
          {{ app.serverName }}
        </h4>
        <div class="flex items-center gap-3">
          <label class="flex items-center gap-2 text-sm text-fg-mute">
            <input
              type="checkbox"
              :checked="app.attachAll"
              @change="run(() => setAttachAll(app.resourceId, ($event.target as HTMLInputElement).checked))"
            >
            Attach to every run
          </label>
          <label class="flex items-center gap-2 text-sm text-fg-mute">
            Export to Claude Code
            <button
              type="button"
              role="switch"
              :aria-checked="app.exportToClaude"
              :aria-label="`Export ${app.serverName} to Claude Code`"
              :data-testid="`application-export-${app.resourceId}`"
              class="relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:ring-offset-2 dark:focus-visible:ring-offset-slate-900 disabled:cursor-not-allowed"
              :class="app.exportToClaude ? 'bg-accent' : 'bg-line-strong'"
              @click="run(() => setExport(app.resourceId, !app.exportToClaude))"
            >
              <span
                class="pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transform transition-transform"
                :class="app.exportToClaude ? 'translate-x-5' : 'translate-x-0'"
              />
            </button>
          </label>
        </div>
      </div>

      <div class="flex flex-col gap-2">
        <div class="flex items-center justify-between">
          <h5 class="text-sm font-medium text-fg">
            Server definition
          </h5>
          <button
            v-if="editingId !== app.resourceId"
            type="button"
            class="text-sm px-2 py-1 rounded border border-line"
            :data-testid="`application-edit-${app.resourceId}`"
            @click="startEdit(app)"
          >
            Edit
          </button>
        </div>
        <template v-if="editingId === app.resourceId">
          <div>
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Command</label>
            <input
              v-model="editDrafts[app.resourceId].command"
              type="text"
              class="border border-line rounded px-2 py-1 bg-raised w-full"
              :data-testid="`application-edit-command-${app.resourceId}`"
            >
          </div>
          <div>
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Args</label>
            <input
              v-model="editDrafts[app.resourceId].args"
              type="text"
              class="border border-line rounded px-2 py-1 bg-raised w-full"
              :data-testid="`application-edit-args-${app.resourceId}`"
            >
          </div>
          <div>
            <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Environment</label>
            <textarea
              v-model="editDrafts[app.resourceId].env"
              rows="3"
              class="border border-line rounded px-2 py-1 bg-raised w-full"
              :data-testid="`application-edit-env-${app.resourceId}`"
            />
          </div>
          <div class="flex items-center gap-2">
            <AppButton variant="info" size="sm" :data-testid="`application-edit-save-${app.resourceId}`" @click="submitEdit(app)">
              Save
            </AppButton>
            <AppButton variant="secondary" size="sm" @click="editingId = null">
              Cancel
            </AppButton>
          </div>
        </template>
        <p v-else class="text-sm text-fg-mute">
          <code>{{ app.entry.command || 'no command set' }} {{ (app.entry.args ?? []).join(' ') }}</code>
        </p>
      </div>

      <div class="flex flex-col gap-2">
        <h5 class="text-sm font-medium text-fg">
          Secrets
        </h5>
        <p v-if="!app.requiredEnv.length" class="text-sm text-fg-mute">
          No secrets declared. Add the environment variable names this server reads its credentials from.
        </p>
        <div v-for="envName in app.requiredEnv" :key="envName" class="flex items-center gap-2 text-sm">
          <code class="min-w-48">{{ envName }}</code>
          <span v-if="isSet(app, envName)" class="text-fg-mute">set</span>
          <span v-else :data-testid="`secret-missing-${envName}`" class="text-amber-600">missing — runs attaching this application will fail</span>
          <input
            v-model="drafts[draftKey(app, envName)]"
            type="password"
            autocomplete="off"
            class="border border-line rounded px-2 py-1 bg-raised"
            :aria-label="`New value for ${envName}`"
            :data-testid="`secret-input-${app.resourceId}-${envName}`"
          >
          <button
            type="button"
            class="px-2 py-1 rounded border border-line"
            :data-testid="`secret-save-${app.resourceId}-${envName}`"
            @click="saveSecret(app, envName)"
          >
            Save
          </button>
          <button
            v-if="isSet(app, envName)"
            type="button"
            class="px-2 py-1 rounded border border-line"
            @click="run(() => deleteSecret(app.resourceId, envName))"
          >
            Remove
          </button>
          <button
            type="button"
            class="px-2 py-1 rounded border border-line"
            :data-testid="`required-env-remove-${app.resourceId}-${envName}`"
            @click="removeRequiredEnv(app, envName)"
          >
            Stop requiring
          </button>
        </div>
        <div class="flex items-center gap-2 text-sm">
          <input
            v-model="requiredEnvDrafts[app.resourceId]"
            type="text"
            placeholder="VARIABLE_NAME"
            class="border border-line rounded px-2 py-1 bg-raised"
            :data-testid="`required-env-input-${app.resourceId}`"
          >
          <button
            type="button"
            class="px-2 py-1 rounded border border-line"
            :data-testid="`required-env-add-${app.resourceId}`"
            @click="addRequiredEnv(app)"
          >
            Add variable
          </button>
        </div>
      </div>

      <div class="flex flex-col gap-2">
        <div class="flex items-center justify-between">
          <h5 class="text-sm font-medium text-fg">
            Tools
          </h5>
          <button type="button" class="text-sm px-2 py-1 rounded border border-line" @click="run(() => refreshCatalogue(app.resourceId))">
            Refresh tool list
          </button>
        </div>
        <p v-if="app.catalogueError" class="text-sm text-red-500" role="alert">
          {{ app.catalogueError }}
        </p>
        <p v-if="app.catalogueRefreshedAt" class="text-xs text-fg-mute">
          Read {{ formatDateTime(app.catalogueRefreshedAt) }}. Hints come from the server and are not trusted; grants decide.
        </p>
        <p v-if="!app.tools.length" class="text-sm text-fg-mute">
          Tool list not read yet.
        </p>
        <ul v-else class="text-sm flex flex-col gap-1">
          <li v-for="tool in app.tools" :key="tool.capability" class="flex items-center gap-2">
            <code>{{ tool.capability }}</code>
            <span v-if="tool.readOnlyHint" class="text-xs text-fg-mute">read-only (per server)</span>
            <span v-if="tool.destructiveHint" class="text-xs text-amber-600">destructive (per server)</span>
          </li>
        </ul>
      </div>

      <div class="flex items-center gap-2">
        <template v-if="confirmRemoveId === app.resourceId">
          <AppButton variant="danger" size="sm" :data-testid="`application-remove-confirm-${app.resourceId}`" @click="handleRemove(app)">
            Confirm
          </AppButton>
          <AppButton variant="secondary" size="sm" @click="confirmRemoveId = null">
            Cancel
          </AppButton>
        </template>
        <button v-else type="button" class="text-sm px-2 py-1 rounded border border-line" :data-testid="`application-remove-${app.resourceId}`" @click="confirmRemoveId = app.resourceId">
          Remove
        </button>
      </div>
    </section>

    <div v-if="panelState === 'rows' || panelState === 'empty'">
      <AppButton v-if="!showAddForm" variant="info" size="sm" data-testid="application-add-toggle" @click="showAddForm = true">
        Add server
      </AppButton>
      <form
        v-else
        class="flex flex-col gap-2 rounded-lg border border-line p-4"
        data-testid="application-add-form"
        @submit.prevent="submitCreate"
      >
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Name</label>
          <input v-model="newApp.name" type="text" class="border border-line rounded px-2 py-1 bg-raised w-full" data-testid="application-add-name">
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Command</label>
          <input v-model="newApp.command" type="text" class="border border-line rounded px-2 py-1 bg-raised w-full" data-testid="application-add-command">
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Args</label>
          <input v-model="newApp.args" type="text" class="border border-line rounded px-2 py-1 bg-raised w-full" data-testid="application-add-args">
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1">Environment</label>
          <textarea v-model="newApp.env" rows="3" class="border border-line rounded px-2 py-1 bg-raised w-full" data-testid="application-add-env" />
        </div>
        <div class="flex items-center gap-2">
          <AppButton variant="info" size="sm" type="submit" data-testid="application-add-submit">
            Save
          </AppButton>
          <AppButton variant="secondary" size="sm" data-testid="application-add-cancel" @click="showAddForm = false; resetNewApp()">
            Cancel
          </AppButton>
        </div>
      </form>
    </div>
  </div>
</template>
