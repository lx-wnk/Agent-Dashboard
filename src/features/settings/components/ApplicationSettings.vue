<script setup lang="ts">
import type { ApplicationView } from '@/features/settings/composables/useApplications'
import { computed, onMounted, reactive, ref } from 'vue'
import { useApplications } from '@/features/settings/composables/useApplications'
import { formatDateTime } from '@/utils/format'

const { applications, loading, error, fetchApplications, setAttachAll, setSecret, deleteSecret, refreshCatalogue } = useApplications()
const actionError = ref<string | null>(null)
const drafts = reactive<Record<string, string>>({})

onMounted(() => {
  void fetchApplications()
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
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Applications
      </h3>
      <p class="text-sm text-fg-mute">
        MCP servers registered with <code>claude mcp add --scope user</code>. A routine attaches the ones its tasks may use; tools run without asking only where a grant allows them.
      </p>
    </div>

    <p v-if="actionError" class="text-sm text-red-500" role="alert">
      {{ actionError }}
    </p>

    <p v-if="panelState === 'loading'" class="text-sm text-fg-mute" aria-live="polite">
      Loading applications…
    </p>
    <p v-else-if="panelState === 'error'" class="text-sm text-red-500" role="alert">
      {{ error }}
    </p>
    <p v-else-if="panelState === 'empty'" data-testid="applications-empty" class="text-sm text-fg-mute">
      No MCP servers registered. Add one with <code>claude mcp add --scope user</code> and restart the dashboard.
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
        <label class="flex items-center gap-2 text-sm text-fg-mute">
          <input
            type="checkbox"
            :checked="app.attachAll"
            @change="run(() => setAttachAll(app.resourceId, ($event.target as HTMLInputElement).checked))"
          >
          Attach to every run
        </label>
      </div>

      <div v-if="app.requiredEnv.length" class="flex flex-col gap-2">
        <h5 class="text-sm font-medium text-fg">
          Secrets
        </h5>
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
    </section>
  </div>
</template>
