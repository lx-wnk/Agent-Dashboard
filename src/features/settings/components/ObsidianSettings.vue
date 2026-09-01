<script setup lang="ts">
import type { SettingView } from '@/features/settings/composables/useSettings'
import { computed, onMounted, ref, watch } from 'vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppSelect from '@/components/ui/AppSelect.vue'
import { toast } from '@/composables/useToast'
import { useSettings } from '@/features/settings/composables/useSettings'
import { errorMessage } from '@/utils/errorMessage'

const KEY_BASE_URL = 'obsidian.baseURL'
const KEY_VAULT_ROOT = 'obsidian.vaultRoot'
const KEY_API_KEY = 'obsidian.apiKey'
const KEY_TLS_MODE = 'obsidian.tlsMode'

// Fallback only for the brief window before the first GET /api/settings
// resolves; the registry's real enum (server/internal/settings/registry.go)
// is what actually renders once items load, so this list can't drift from it.
const FALLBACK_TLS_MODES = ['verify', 'pinned', 'insecure-loopback']

interface ObsidianFormState {
  baseURL: string
  vaultRoot: string
  apiKey: string
  tlsMode: string
}

const { items, loading, refetch, update } = useSettings()

const form = ref<ObsidianFormState>({ baseURL: '', vaultRoot: '', apiKey: '', tlsMode: 'verify' })
const saving = ref(false)

const tlsModeOptions = computed(() => {
  const enumValues = items.value.find(i => i.key === KEY_TLS_MODE)?.enum ?? FALLBACK_TLS_MODES
  return enumValues.map(value => ({ value, label: value }))
})

// Seeded exactly once from whatever the server first reports — items updates
// again after every save (useSettings.update patches the array in place),
// and re-seeding on that would clobber whatever the user is mid-typing.
const seeded = ref(false)
watch(items, (list: SettingView[]) => {
  if (seeded.value)
    return
  const byKey = new Map(list.map(i => [i.key, i]))
  const baseURL = byKey.get(KEY_BASE_URL)
  if (!baseURL)
    return
  form.value = {
    baseURL: baseURL.value,
    vaultRoot: byKey.get(KEY_VAULT_ROOT)?.value ?? '',
    apiKey: byKey.get(KEY_API_KEY)?.value ?? '',
    tlsMode: byKey.get(KEY_TLS_MODE)?.value ?? 'verify',
  }
  seeded.value = true
}, { immediate: true })

onMounted(refetch)

// baseURL, vaultRoot and apiKey are a required trio server-side
// (serverapp.buildObsidianClient) — some-but-not-all set is a state the
// server will refuse to boot with. Warn, but a partial fill is still a
// legitimate save: never block it.
const trioComplete = computed(() => {
  const setCount = [form.value.baseURL, form.value.vaultRoot, form.value.apiKey].filter(v => v !== '').length
  return setCount === 0 || setCount === 3
})

async function save() {
  saving.value = true
  try {
    const pairs: Array<[string, string]> = [
      [KEY_BASE_URL, form.value.baseURL],
      [KEY_VAULT_ROOT, form.value.vaultRoot],
      [KEY_TLS_MODE, form.value.tlsMode],
    ]
    // obsidian.apiKey always reads back as the mask sentinel once it is set;
    // sending it back untouched is how the server knows to leave it alone.
    // Skip it entirely when it was never configured and the user did not
    // type one — an empty string would encrypt and store an empty secret.
    if (form.value.apiKey !== '')
      pairs.push([KEY_API_KEY, form.value.apiKey])

    let applied: 'live' | 'restart' = 'live'
    for (const [key, value] of pairs) {
      const result = await update(key, value)
      if (result === 'restart')
        applied = 'restart'
    }
    toast.success(applied === 'restart' ? 'Saved — applies after a server restart.' : 'Saved.')
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to save Obsidian settings'))
  }
  finally {
    saving.value = false
  }
}

const indexing = ref(false)
const indexMessage = ref<string | null>(null)

async function runIndex() {
  indexing.value = true
  indexMessage.value = null
  try {
    const res = await fetch('/api/obsidian/index', { method: 'POST' })
    if (res.status === 403) {
      indexMessage.value = 'Indexing was denied — grant obsidian.search, obsidian.read, and memory.write to allow it.'
      return
    }
    if (res.status === 503) {
      indexMessage.value = 'The Obsidian vault is not configured yet — fill in the settings above and save.'
      return
    }
    if (!res.ok) {
      const body = await res.json().catch(() => ({})) as { error?: string }
      throw new Error(body.error ?? `HTTP ${res.status}`)
    }
    const data = await res.json() as { indexed: number }
    indexMessage.value = `Indexed ${data.indexed} note${data.indexed === 1 ? '' : 's'}.`
  }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to run indexing'))
  }
  finally {
    indexing.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <div>
      <h3 class="text-[17px] font-bold text-fg mb-1">
        Obsidian
      </h3>
      <p class="text-xs text-fg-mute">
        Connect a local Obsidian vault via its Local REST API plugin. All four settings apply after a server restart.
      </p>
    </div>

    <div v-if="loading" class="text-center py-12 text-fg-mute text-sm">
      Loading...
    </div>

    <template v-else>
      <div
        v-if="!trioComplete"
        data-testid="obsidian-trio-warning"
        class="text-xs rounded-md px-3 py-2 bg-warning-soft text-warning-text"
      >
        Base URL, vault root, and API key are a required trio — the server will not enable the vault at the next restart until all three are set.
      </div>

      <div class="grid grid-cols-1 gap-3 max-w-md">
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="obsidian-baseurl">Base URL</label>
          <input
            id="obsidian-baseurl"
            v-model="form.baseURL"
            data-testid="obsidian-baseurl"
            type="url"
            placeholder="https://127.0.0.1:27124"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="obsidian-vaultroot">Vault root</label>
          <input
            id="obsidian-vaultroot"
            v-model="form.vaultRoot"
            data-testid="obsidian-vaultroot"
            type="text"
            placeholder="claude-memory"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="obsidian-apikey">API key</label>
          <input
            id="obsidian-apikey"
            v-model="form.apiKey"
            data-testid="obsidian-apikey"
            type="password"
            autocomplete="off"
            placeholder="Local REST API key"
            class="w-full bg-card border border-line rounded px-2.5 py-1.5 text-sm text-fg focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent"
          >
        </div>
        <div>
          <label class="block text-[10px] font-semibold uppercase tracking-wider text-fg-mute mb-1" for="obsidian-tlsmode">TLS mode</label>
          <AppSelect
            id="obsidian-tlsmode"
            v-model="form.tlsMode"
            data-testid="obsidian-tlsmode"
            :options="tlsModeOptions"
            class="w-full"
          />
        </div>
      </div>

      <div>
        <AppButton variant="info" data-testid="obsidian-save" :disabled="saving" @click="save">
          {{ saving ? 'Saving…' : 'Save' }}
        </AppButton>
      </div>

      <div class="flex items-center gap-3 pt-3 border-t border-line">
        <AppButton variant="secondary" data-testid="obsidian-index" :disabled="indexing" @click="runIndex">
          {{ indexing ? 'Indexing…' : 'Index now' }}
        </AppButton>
        <span v-if="indexMessage" data-testid="obsidian-index-result" class="text-xs text-fg-mute">{{ indexMessage }}</span>
      </div>
    </template>
  </div>
</template>
